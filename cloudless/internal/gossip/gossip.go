package gossip

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"

	"cloudless/internal/config"
	"cloudless/internal/registry"
)

// Meta is the per-node metadata gossiped to peers. It must stay small:
// memberlist caps NodeMeta at 512 bytes.
type Meta struct {
	BackendURL string `json:"backend_url"`
	Location   string `json:"location,omitempty"`
}

type Options struct {
	NodeName   string
	BindAddr   string   // host:port for gossip traffic
	Join       []string // seed peers, host:port
	BackendURL string   // this node's inference endpoint advertised to peers
	Location   string   // hierarchical locality label
	Secret     []byte   // shared cluster key; encrypts and authenticates gossip
	// OnRevoke applies a revocation received from a peer (persist + drop).
	OnRevoke func(name string)
}

type Mesh struct {
	list  *memberlist.Memberlist
	bcast *memberlist.TransmitLimitedQueue
}

// revokeMsg is gossiped when a node is evicted so every peer refuses it.
type revokeMsg struct {
	Type string `json:"type"` // "revoke"
	Name string `json:"name"`
}

type broadcast struct{ msg []byte }

func (b *broadcast) Invalidates(memberlist.Broadcast) bool { return false }
func (b *broadcast) Message() []byte                       { return b.msg }
func (b *broadcast) Finished()                             {}

type delegate struct {
	meta    []byte
	bcast   *memberlist.TransmitLimitedQueue
	onApply func(name string) // applies a received revocation locally
	limiter *rateLimiter      // S6: bounds gossip-message processing rate
}

// rateLimiter is a small token bucket: a compromised-but-authenticated peer
// can still gossip, but can't force this node to do unbounded work per
// message it receives (S6) — e.g. flooding fabricated revoke messages,
// each of which would otherwise trigger a disk write and grow the revoked
// set forever (internal/revoke.Set.Add persists on every new name).
type rateLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens refilled per second
	last   time.Time
}

func newRateLimiter(burst, perSecond float64) *rateLimiter {
	return &rateLimiter{tokens: burst, max: burst, rate: perSecond, last: time.Now()}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Seconds() * r.rate
	if r.tokens > r.max {
		r.tokens = r.max
	}
	r.last = now
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}

func (d *delegate) NodeMeta(limit int) []byte {
	if len(d.meta) > limit {
		return nil
	}
	return d.meta
}
func (d *delegate) NotifyMsg(b []byte) {
	if len(b) == 0 {
		return
	}
	if d.limiter != nil && !d.limiter.Allow() {
		return // dropped: over budget (S6) — a legitimate revoke is rare and will retransmit
	}
	var m revokeMsg
	if json.Unmarshal(b, &m) == nil && m.Type == "revoke" && m.Name != "" && d.onApply != nil {
		d.onApply(m.Name)
	}
}
func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	if d.bcast == nil {
		return nil
	}
	return d.bcast.GetBroadcasts(overhead, limit)
}
func (d *delegate) LocalState(bool) []byte        { return nil }
func (d *delegate) MergeRemoteState([]byte, bool) {}

// events feeds membership changes into the registry so the gateway's
// routing table tracks the live mesh.
type events struct {
	reg  *registry.Registry
	self string
}

func (e *events) NotifyJoin(n *memberlist.Node) {
	if n.Name == e.self {
		return
	}
	var m Meta
	if err := json.Unmarshal(n.Meta, &m); err != nil || m.BackendURL == "" {
		log.Printf("gossip: peer %s joined without usable metadata", n.Name)
		return
	}
	log.Printf("gossip: peer %s joined, backend %s", n.Name, m.BackendURL)
	e.reg.Upsert(config.Backend{Name: n.Name, BaseURL: m.BackendURL, Location: m.Location})
}

func (e *events) NotifyLeave(n *memberlist.Node) {
	if n.Name == e.self {
		return
	}
	log.Printf("gossip: peer %s left", n.Name)
	e.reg.Remove(n.Name)
}

func (e *events) NotifyUpdate(n *memberlist.Node) { e.NotifyJoin(n) }

func Start(opts Options, reg *registry.Registry) (*Mesh, error) {
	host, portStr, err := net.SplitHostPort(opts.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("gossip bind %q: %w", opts.BindAddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("gossip bind port %q: %w", portStr, err)
	}

	meta, err := json.Marshal(Meta{BackendURL: opts.BackendURL, Location: opts.Location})
	if err != nil {
		return nil, err
	}

	bcast := &memberlist.TransmitLimitedQueue{NumNodes: func() int { return 1 }, RetransmitMult: 3}
	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeName
	if host != "" {
		cfg.BindAddr = host
	}
	cfg.BindPort = port
	cfg.AdvertisePort = port
	cfg.Delegate = &delegate{meta: meta, bcast: bcast, onApply: opts.OnRevoke, limiter: newRateLimiter(50, 10)}
	cfg.Events = &events{reg: reg, self: opts.NodeName}
	if len(opts.Secret) > 0 {
		cfg.SecretKey = opts.Secret // AES-GCM; peers without the key cannot join
	}
	cfg.LogOutput = logWriter{}

	list, err := memberlist.Create(cfg)
	if err != nil {
		return nil, err
	}
	bcast.NumNodes = list.NumMembers
	if len(opts.Join) > 0 {
		if _, err := list.Join(opts.Join); err != nil {
			log.Printf("gossip: initial join failed (%v); will serve standalone until peers arrive", err)
		}
	}
	return &Mesh{list: list, bcast: bcast}, nil
}

// BroadcastRevoke gossips a node revocation to the whole mesh.
func (m *Mesh) BroadcastRevoke(name string) {
	msg, err := json.Marshal(revokeMsg{Type: "revoke", Name: name})
	if err != nil {
		return
	}
	m.bcast.QueueBroadcast(&broadcast{msg: msg})
}

func (m *Mesh) Leave() {
	m.list.Leave(time.Second)
	m.list.Shutdown()
}

// Members lists currently known peer names, including self (R3) — used to
// give a joining node an explicit, unmissable confirmation that it's
// actually connected to the mesh rather than silently running standalone.
func (m *Mesh) Members() []string {
	nodes := m.list.Members()
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}

// logWriter routes memberlist's internal logs through the standard logger
// at reduced noise.
type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) { return len(p), nil }

package relay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"cloudless/internal/pki"
)

// BlobStore is the subset of the model store the relay serves to peers.
type BlobStore interface {
	List() []storeEntry
	Path(name string) (string, bool)
}

// storeEntry mirrors store.Entry for JSON without importing the store here.
type storeEntry struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Format string `json:"format"`
}

// Server is the node's mutual-TLS front door for peer traffic: peers holding
// a CA-signed cert may proxy inference requests to this node's local runtime
// and pull model blobs from its store.
type Server struct {
	backendURL string // local runtime base, e.g. http://127.0.0.1:11434/v1
	list       func() []storeEntry
	path       func(string) (string, bool)
	slots      func() int // shared-work concurrency budget (0 = not sharing)
	inflight   atomic.Int64
	client     *http.Client
}

func NewServer(backendURL string, list func() []storeEntry, path func(string) (string, bool), slots func() int) *Server {
	if slots == nil {
		slots = func() int { return 1 }
	}
	return &Server{backendURL: backendURL, list: list, path: path, slots: slots, client: &http.Client{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", s.proxy)
	mux.HandleFunc("GET /store", s.storeList)
	mux.HandleFunc("GET /blob", s.blob)
	return mux
}

func (s *Server) storeList(w http.ResponseWriter, _ *http.Request) {
	var list []storeEntry
	if s.list != nil {
		list = s.list()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"artifacts": list})
}

func (s *Server) blob(w http.ResponseWriter, r *http.Request) {
	if s.path == nil {
		http.Error(w, "no store", http.StatusServiceUnavailable)
		return
	}
	p, ok := s.path(r.URL.Query().Get("name"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, p)
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	if s.backendURL == "" {
		http.Error(w, `{"error":"node has no local runtime"}`, http.StatusServiceUnavailable)
		return
	}
	// Enforce the owner's share limit: shared (peer-served) work may occupy
	// only the declared CPU budget. 0 slots = not sharing right now.
	budget := s.slots()
	if budget <= 0 {
		http.Error(w, `{"error":"node is not sharing capacity right now"}`, http.StatusServiceUnavailable)
		return
	}
	if s.inflight.Add(1) > int64(budget) {
		s.inflight.Add(-1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"node at its shared-capacity limit"}`, http.StatusServiceUnavailable)
		return
	}
	defer s.inflight.Add(-1)
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	target := s.backendURL + strings.TrimPrefix(r.URL.Path, "/v1")
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, `{"error":"local runtime unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// ListenAndServe runs the relay with mutual TLS from the cluster PKI.
// list/path (may be nil) expose the local model store to peers for pulls.
func ListenAndServe(addr, pkiDir, backendURL string, list func() []storeEntry, path func(string) (string, bool), slots func() int) error {
	tlsCfg, err := pki.ServerTLS(pkiDir)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: NewServer(backendURL, list, path, slots).Handler(), TLSConfig: tlsCfg}
	log.Printf("relay: mutual-TLS peer endpoint on %s", addr)
	return srv.ListenAndServeTLS("", "")
}

// Entry is the exported artifact shape for callers assembling store adapters.
type Entry = storeEntry

// ---- enrollment (A2-lite) ---------------------------------------------------
// The CA-holding node signs a joiner's public key. Authentication binds the
// request to the cluster secret via HMAC over name|pubkey, so the secret
// itself never travels and a tampered pubkey fails verification.

type EnrollRequest struct {
	Name string `json:"name"`
	Pub  string `json:"pub"` // base64 PKIX DER
	MAC  string `json:"mac"` // base64 HMAC-SHA256(secret, name|pub)
}

type EnrollResponse struct {
	CA   string `json:"ca"`   // PEM
	Cert string `json:"cert"` // base64 DER
}

func Sign(secret []byte, name string, pub []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(name))
	m.Write([]byte("|"))
	m.Write(pub)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// EnrollHandler serves POST /enroll on the CA-holding node.
func EnrollHandler(pkiDir string, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EnrollRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		pub, err := base64.StdEncoding.DecodeString(req.Pub)
		if err != nil || req.Name == "" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if !hmac.Equal([]byte(req.MAC), []byte(Sign(secret, req.Name, pub))) {
			http.Error(w, `{"error":"enrollment rejected"}`, http.StatusUnauthorized)
			return
		}
		certDER, err := pki.SignPubKey(pkiDir, req.Name, pub)
		if err != nil {
			log.Printf("enroll: signing failed: %v", err)
			http.Error(w, `{"error":"signing failed"}`, http.StatusInternalServerError)
			return
		}
		caPEM, err := pki.CACertPEM(pkiDir)
		if err != nil {
			http.Error(w, `{"error":"ca unavailable"}`, http.StatusInternalServerError)
			return
		}
		log.Printf("enroll: issued certificate to %s", req.Name)
		json.NewEncoder(w).Encode(EnrollResponse{CA: string(caPEM), Cert: base64.StdEncoding.EncodeToString(certDER)})
	}
}

// Enroll runs the joiner side against the seed's gateway.
func Enroll(seedAPI, pkiDir, name string, secret []byte) error {
	pub, err := pki.NewNodeKey(pkiDir)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(EnrollRequest{
		Name: name,
		Pub:  base64.StdEncoding.EncodeToString(pub),
		MAC:  Sign(secret, name, pub),
	})
	resp, err := http.Post(seedAPI+"/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return &enrollError{status: resp.StatusCode, body: string(b)}
	}
	var er EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return err
	}
	certDER, err := base64.StdEncoding.DecodeString(er.Cert)
	if err != nil {
		return err
	}
	return pki.SaveNodeCredsWithCA(pkiDir, certDER, []byte(er.CA))
}

type enrollError struct {
	status int
	body   string
}

func (e *enrollError) Error() string { return "enroll failed: " + e.body }

// PeerTLS exposes the client TLS config for dialing peer relays.
func PeerTLS(pkiDir string) (*tls.Config, error) { return pki.ClientTLS(pkiDir) }

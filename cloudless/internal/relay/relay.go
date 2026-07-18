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

	"cloudless/internal/pki"
)

// Server is the node's mutual-TLS front door for peer traffic: peers holding
// a CA-signed cert may proxy inference requests to this node's local runtime.
type Server struct {
	backendURL string // local runtime base, e.g. http://127.0.0.1:11434/v1
	client     *http.Client
}

func NewServer(backendURL string) *Server {
	return &Server{backendURL: backendURL, client: &http.Client{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", s.proxy)
	return mux
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	if s.backendURL == "" {
		http.Error(w, `{"error":"node has no local runtime"}`, http.StatusServiceUnavailable)
		return
	}
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
func ListenAndServe(addr, pkiDir, backendURL string) error {
	tlsCfg, err := pki.ServerTLS(pkiDir)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: NewServer(backendURL).Handler(), TLSConfig: tlsCfg}
	log.Printf("relay: mutual-TLS peer endpoint on %s", addr)
	return srv.ListenAndServeTLS("", "")
}

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

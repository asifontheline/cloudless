package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudless/internal/audit"
)

// A5: GET /audit reports whether entries are cryptographically signed, not
// just hash-chain intact.

type stubSigner struct{ ok bool }

func (s stubSigner) Sign(data []byte) ([]byte, error) { return []byte("sig"), nil }
func (s stubSigner) Verify(data, sig []byte) bool     { return s.ok }

func TestAuditEndpointReportsUnsigned(t *testing.T) {
	g := newTestGateway(t)
	g.Audit = newTestAudit(t)
	g.Audit.Append("cluster", "keys.create", "alice", "")

	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	var resp struct {
		Intact bool `json:"intact"`
		Signed bool `json:"signed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Intact || resp.Signed {
		t.Fatalf("unsigned log: intact=%v signed=%v, want true/false", resp.Intact, resp.Signed)
	}
}

func TestAuditEndpointReportsSigned(t *testing.T) {
	g := newTestGateway(t)
	g.Audit = newTestAudit(t)
	g.Audit.SetSigner(stubSigner{ok: true})
	g.Audit.Append("cluster", "keys.create", "alice", "")

	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	var resp struct {
		Intact  bool          `json:"intact"`
		Signed  bool          `json:"signed"`
		Entries []audit.Entry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Intact || !resp.Signed {
		t.Fatalf("signed log: intact=%v signed=%v, want true/true", resp.Intact, resp.Signed)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Sig == "" {
		t.Fatalf("returned entry must carry its signature: %+v", resp.Entries)
	}
}

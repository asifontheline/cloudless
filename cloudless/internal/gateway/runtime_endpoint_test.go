package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// B5: GET /runtime reports the supervised local backend's process status.

func TestRuntimeEndpointUnsupervised(t *testing.T) {
	g := newTestGateway(t)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["supervised"] != false {
		t.Fatalf("expected supervised:false with no RuntimeStatus set, got %+v", resp)
	}
}

func TestRuntimeEndpointReportsStatus(t *testing.T) {
	g := newTestGateway(t)
	g.RuntimeStatus = func() any {
		return map[string]any{"supervised": true, "running": true, "pid": 1234, "restarts": 2}
	}
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["supervised"] != true || resp["running"] != true || resp["restarts"] != float64(2) {
		t.Fatalf("unexpected runtime status response: %+v", resp)
	}
}

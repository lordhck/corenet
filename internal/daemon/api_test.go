package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"corenet/internal/registry"
	"corenet/pkg/protocol"
)

func testAPI(t *testing.T, services ...protocol.Service) (*API, *registry.Registry) {
	t.Helper()
	reg := registry.New()
	reg.SetConfigServices(services)
	api := &API{
		Dir:    reg,
		Status: func() protocol.Status { return protocol.Status{Version: protocol.Version, NodeID: "node-a"} },
		Info:   func() protocol.Info { return protocol.Info{Version: protocol.Version, NodeID: "node-a"} },
		Nodes: func() []protocol.Node {
			return []protocol.Node{{ID: "node-a", Local: true, Online: true}}
		},
		Logger: log.New(io.Discard, "", 0),
	}
	return api, reg
}

func call(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestControlAPIRegistersAndRemovesServices(t *testing.T) {
	api, _ := testAPI(t)
	mux := api.ControlMux()

	rec := call(t, mux, http.MethodPost, "/v1/services", `{"name":"wiki.core","address":"127.0.0.1","port":8080}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if svc := decode[protocol.Service](t, rec); svc.Source != protocol.SourceRuntime {
		t.Errorf("source = %q, want runtime", svc.Source)
	}

	if rec := call(t, mux, http.MethodPost, "/v1/services", `{"name":"wiki.core","address":"127.0.0.1","port":8081}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate registration: status = %d, want 409", rec.Code)
	}
	if rec := call(t, mux, http.MethodPost, "/v1/services", `{"name":"wiki.test","address":"127.0.0.1","port":80}`); rec.Code != http.StatusBadRequest {
		t.Errorf("name outside .core: status = %d, want 400", rec.Code)
	}
	if rec := call(t, mux, http.MethodPost, "/v1/services", `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status = %d, want 400", rec.Code)
	}
	if rec := call(t, mux, http.MethodDelete, "/v1/services/wiki.core", ""); rec.Code != http.StatusNoContent {
		t.Errorf("removal: status = %d, want 204", rec.Code)
	}
	if rec := call(t, mux, http.MethodDelete, "/v1/services/wiki.core", ""); rec.Code != http.StatusNotFound {
		t.Errorf("removing it twice: status = %d, want 404", rec.Code)
	}
}

func TestControlAPIRefusesToRemoveAConfiguredService(t *testing.T) {
	api, _ := testAPI(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 8081})
	rec := call(t, api.ControlMux(), http.MethodDelete, "/v1/services/hello.core", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestResolve(t *testing.T) {
	api, _ := testAPI(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 8081})
	mux := api.ControlMux()

	rec := call(t, mux, http.MethodGet, "/v1/resolve?name=Hello.Core", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc := decode[protocol.Service](t, rec); svc.Addr() != "127.0.0.1:8081" {
		t.Errorf("resolved to %s", svc.Addr())
	}

	for path, want := range map[string]int{
		"/v1/resolve?name=nope.core": http.StatusNotFound,
		"/v1/resolve?name=nope.test": http.StatusBadRequest,
		"/v1/resolve":                http.StatusBadRequest,
	} {
		if rec := call(t, mux, http.MethodGet, path, ""); rec.Code != want {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestErrorsCarryACodeAndMessage(t *testing.T) {
	api, _ := testAPI(t)
	rec := call(t, api.ControlMux(), http.MethodGet, "/v1/resolve?name=nope.core", "")
	body := decode[protocol.ErrorResponse](t, rec)
	if body.Error.Code != protocol.CodeNotFound || body.Error.Message == "" {
		t.Errorf("error body = %+v", body.Error)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
}

func TestPeerAPIIsReadOnlyAndLocalOnly(t *testing.T) {
	api, reg := testAPI(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 8081})
	reg.SetRemote("node-b", []protocol.Service{{Name: "wiki.core", Address: "10.0.0.2", Port: 80}})
	mux := api.PeerMux()

	list := decode[protocol.ServiceList](t, call(t, mux, http.MethodGet, "/v1/services", ""))
	if len(list.Services) != 1 || list.Services[0].Name != "hello.core" {
		t.Errorf("a node must advertise only its own services, got %+v", list.Services)
	}

	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/v1/services"},
		{http.MethodDelete, "/v1/services/hello.core"},
		{http.MethodGet, "/v1/status"},
		{http.MethodGet, "/v1/nodes"},
	} {
		if rec := call(t, mux, req.method, req.path, "{}"); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 on the peer listener", req.method, req.path, rec.Code)
		}
	}

	if info := decode[protocol.Info](t, call(t, mux, http.MethodGet, "/v1/info", "")); info.NodeID != "node-a" {
		t.Errorf("info = %+v", info)
	}
}

func TestControlAPIListsEverythingAndNodes(t *testing.T) {
	api, reg := testAPI(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 8081})
	reg.SetRemote("node-b", []protocol.Service{{Name: "wiki.core", Address: "10.0.0.2", Port: 80}})
	mux := api.ControlMux()

	list := decode[protocol.ServiceList](t, call(t, mux, http.MethodGet, "/v1/services", ""))
	if len(list.Services) != 2 {
		t.Errorf("services = %+v, want the local and the remote one", list.Services)
	}
	if nodes := decode[protocol.NodeList](t, call(t, mux, http.MethodGet, "/v1/nodes", "")); len(nodes.Nodes) != 1 {
		t.Errorf("nodes = %+v", nodes.Nodes)
	}
	if rec := call(t, mux, http.MethodGet, "/v1/nodes/node-a", ""); rec.Code != http.StatusOK {
		t.Errorf("node lookup: status = %d", rec.Code)
	}
	if rec := call(t, mux, http.MethodGet, "/v1/nodes/absent", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown node: status = %d, want 404", rec.Code)
	}
	if rec := call(t, mux, http.MethodGet, "/v1/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown endpoint: status = %d, want 404", rec.Code)
	}
}

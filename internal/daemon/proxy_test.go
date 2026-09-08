package daemon

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"corenet/internal/registry"
	"corenet/pkg/protocol"
)

func testProxy(t *testing.T, services ...protocol.Service) (http.Handler, *registry.Registry) {
	t.Helper()
	reg := registry.New()
	reg.SetConfigServices(services)
	return NewProxy(reg, log.New(io.Discard, "", 0)), reg
}

func request(t *testing.T, handler http.Handler, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestProxyRoutesOnHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The backend must still see the CoreNet name, so it can serve
		// several .core names from one server.
		_, _ = io.WriteString(w, "served "+r.Host)
	}))
	defer backend.Close()

	addr := strings.TrimPrefix(backend.URL, "http://")
	host, port, _ := strings.Cut(addr, ":")
	proxy, reg := testProxy(t)
	if _, err := reg.Add(protocol.Service{Name: "hello.core", Address: host, Port: atoi(t, port)}); err != nil {
		t.Fatal(err)
	}

	rec := request(t, proxy, "hello.core")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "served hello.core" {
		t.Errorf("body = %q; the Host header was not preserved", body)
	}
}

func TestProxyIgnoresThePortInTheHostHeader(t *testing.T) {
	proxy, _ := testProxy(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 1})
	// Port 1 is closed, so a 502 proves the name was matched.
	if rec := request(t, proxy, "hello.core:8080"); rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestProxyUnknownNameIsHTML404(t *testing.T) {
	proxy, _ := testProxy(t)
	rec := request(t, proxy, "nope.core")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want HTML for a browser", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "nope.core") || strings.Contains(body, "<script") {
		t.Errorf("unexpected error page: %s", body)
	}
}

func TestProxyUnreachableBackendIs502(t *testing.T) {
	proxy, _ := testProxy(t, protocol.Service{Name: "hello.core", Address: "127.0.0.1", Port: 1})
	rec := request(t, proxy, "hello.core")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "did not answer") {
		t.Errorf("unexpected error page: %s", rec.Body.String())
	}
}

func TestErrorPageEscapesTheName(t *testing.T) {
	proxy, _ := testProxy(t)
	rec := request(t, proxy, "<script>x</script>.core")
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Error("the error page must escape the requested name")
	}
}

func atoi(t *testing.T, value string) int {
	t.Helper()
	port := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			t.Fatalf("not a port: %q", value)
		}
		port = port*10 + int(r-'0')
	}
	return port
}

func TestProxyConflictedNameIs409(t *testing.T) {
	reg := registry.New()
	reg.SetDocker(nil, []protocol.Conflict{{Name: "blog.core", Reason: "claimed by 2 Docker containers"}})
	proxy := NewProxy(reg, log.New(io.Discard, "", 0))

	rec := request(t, proxy, "blog.core")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "2 Docker containers") {
		t.Errorf("the page should explain the conflict: %s", body)
	}
}

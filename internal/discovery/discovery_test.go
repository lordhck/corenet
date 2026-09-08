package discovery

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"corenet/pkg/protocol"
)

// fakeDocker serves the container list over a Unix socket, the way the real
// Docker daemon does, so the client's transport is exercised for real.
type fakeDocker struct {
	mu         sync.Mutex
	containers []map[string]any
	status     int
	socket     string
	server     *httptest.Server
}

func newFakeDocker(t *testing.T) *fakeDocker {
	t.Helper()
	f := &fakeDocker{socket: filepath.Join(t.TempDir(), "docker.sock")}

	listener, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	f.server = &httptest.Server{
		Listener: listener,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			if !strings.HasSuffix(r.URL.Path, "/containers/json") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(f.containers)
		})},
	}
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDocker) set(containers ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.containers, f.status = containers, 0
}

func (f *fakeDocker) fail(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// labelled builds a container list entry with CoreNet labels.
func labelled(name, coreName, service, port, ip string) map[string]any {
	return map[string]any{
		"Id":    name + "-id",
		"Names": []string{"/" + name},
		"State": "running",
		"Labels": map[string]string{
			LabelName:    coreName,
			LabelService: service,
			LabelPort:    port,
		},
		"NetworkSettings": map[string]any{
			"Networks": map[string]any{
				"bridge": map[string]any{"IPAddress": ip},
			},
		},
	}
}

// captureStore records what discovery writes to the registry.
type captureStore struct {
	mu        sync.Mutex
	services  []protocol.Service
	conflicts []protocol.Conflict
}

func (c *captureStore) SetDocker(services []protocol.Service, conflicts []protocol.Conflict) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services, c.conflicts = services, conflicts
}

func testDocker(t *testing.T, f *fakeDocker, network string) (*Docker, *captureStore) {
	t.Helper()
	store := &captureStore{}
	return NewDocker(store, f.socket, network, time.Second, log.New(io.Discard, "", 0)), store
}

func TestPollDiscoversLabelledContainers(t *testing.T) {
	f := newFakeDocker(t)
	f.set(labelled("wiki", "Wiki.Core", "http", "80", "172.17.0.2"))

	d, store := testDocker(t, f, "")
	d.Poll(t.Context())

	if len(store.services) != 1 {
		t.Fatalf("services = %+v", store.services)
	}
	svc := store.services[0]
	if svc.Name != "wiki.core" || svc.Addr() != "172.17.0.2:80" || svc.Source != protocol.SourceDocker {
		t.Errorf("service = %+v", svc)
	}
}

func TestPollIgnoresUnusableContainers(t *testing.T) {
	f := newFakeDocker(t)
	f.set(
		map[string]any{"Id": "plain", "Names": []string{"/plain"}, "Labels": map[string]string{}},
		labelled("bad-name", "wiki.test", "http", "80", "172.17.0.2"),
		labelled("bad-type", "a.core", "tcp", "80", "172.17.0.2"),
		labelled("no-type", "b.core", "", "80", "172.17.0.2"),
		labelled("bad-port", "c.core", "http", "0", "172.17.0.2"),
		labelled("no-address", "d.core", "http", "80", ""),
		labelled("good", "good.core", "http", "80", "172.17.0.3"),
	)

	d, store := testDocker(t, f, "")
	d.Poll(t.Context())

	if len(store.services) != 1 || store.services[0].Name != "good.core" {
		t.Errorf("services = %+v, want only good.core", store.services)
	}
}

func TestPollReportsDuplicateNamesAsConflicts(t *testing.T) {
	f := newFakeDocker(t)
	f.set(
		labelled("a", "blog.core", "http", "80", "172.17.0.2"),
		labelled("b", "blog.core", "http", "80", "172.17.0.3"),
		labelled("c", "wiki.core", "http", "80", "172.17.0.4"),
	)

	d, store := testDocker(t, f, "")
	d.Poll(t.Context())

	if len(store.services) != 1 || store.services[0].Name != "wiki.core" {
		t.Errorf("services = %+v, want only wiki.core", store.services)
	}
	if len(store.conflicts) != 1 || store.conflicts[0].Name != "blog.core" {
		t.Fatalf("conflicts = %+v", store.conflicts)
	}
	if reason := store.conflicts[0].Reason; !strings.Contains(reason, "a") || !strings.Contains(reason, "b") {
		t.Errorf("the reason should name the containers: %q", reason)
	}
}

func TestPollFollowsTheContainerLifecycle(t *testing.T) {
	f := newFakeDocker(t)
	f.set(labelled("wiki", "wiki.core", "http", "80", "172.17.0.2"))

	d, store := testDocker(t, f, "")
	d.Poll(t.Context())
	if len(store.services) != 1 {
		t.Fatalf("services = %+v", store.services)
	}

	// The container restarts on a new address.
	f.set(labelled("wiki", "wiki.core", "http", "80", "172.17.0.9"))
	d.Poll(t.Context())
	if store.services[0].Address != "172.17.0.9" {
		t.Errorf("address = %s, want the new one", store.services[0].Address)
	}

	// The container goes away.
	f.set()
	d.Poll(t.Context())
	if len(store.services) != 0 {
		t.Errorf("services = %+v, want none", store.services)
	}
}

func TestPollPrefersTheConfiguredNetwork(t *testing.T) {
	container := map[string]any{
		"Id":    "multi-id",
		"Names": []string{"/multi"},
		"State": "running",
		"Labels": map[string]string{
			LabelName: "wiki.core", LabelService: "http", LabelPort: "80",
		},
		"NetworkSettings": map[string]any{"Networks": map[string]any{
			"bridge":  map[string]any{"IPAddress": "172.17.0.2"},
			"corenet": map[string]any{"IPAddress": "10.42.0.10"},
		}},
	}

	f := newFakeDocker(t)
	f.set(container)

	d, store := testDocker(t, f, "corenet")
	d.Poll(t.Context())
	if len(store.services) != 1 || store.services[0].Address != "10.42.0.10" {
		t.Fatalf("services = %+v, want the corenet network address", store.services)
	}

	// Without a preference, the first network by name wins.
	d, store = testDocker(t, f, "")
	d.Poll(t.Context())
	if store.services[0].Address != "172.17.0.2" {
		t.Errorf("address = %s, want the bridge address", store.services[0].Address)
	}

	// A container that is not on the configured network is skipped.
	d, store = testDocker(t, f, "absent")
	d.Poll(t.Context())
	if len(store.services) != 0 {
		t.Errorf("services = %+v, want none", store.services)
	}
}

func TestPollSurvivesDockerFailing(t *testing.T) {
	f := newFakeDocker(t)
	f.set(labelled("wiki", "wiki.core", "http", "80", "172.17.0.2"))

	d, store := testDocker(t, f, "")
	d.Poll(t.Context())
	if len(store.services) != 1 {
		t.Fatalf("services = %+v", store.services)
	}

	f.fail(http.StatusInternalServerError)
	d.Poll(t.Context())
	if len(store.services) != 0 {
		t.Error("a broken Docker must withdraw its services, not keep stale ones")
	}

	f.set(labelled("wiki", "wiki.core", "http", "80", "172.17.0.2"))
	d.Poll(t.Context())
	if len(store.services) != 1 {
		t.Error("discovery should resume when Docker comes back")
	}
}

func TestPollWithoutADockerSocket(t *testing.T) {
	store := &captureStore{}
	d := NewDocker(store, filepath.Join(t.TempDir(), "absent.sock"), "", time.Second, log.New(io.Discard, "", 0))
	d.Poll(t.Context()) // must not panic and must leave the registry empty
	if len(store.services) != 0 {
		t.Errorf("services = %+v", store.services)
	}
}

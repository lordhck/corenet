package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"corenet/internal/registry"
	"corenet/pkg/protocol"
)

// peerServer is a stand-in for another node's read-only listener.
type peerServer struct {
	nodeID   string
	services []protocol.Service
}

func (p *peerServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/info":
			_ = json.NewEncoder(w).Encode(protocol.Info{Version: protocol.Version, NodeID: p.nodeID})
		case "/v1/services":
			_ = json.NewEncoder(w).Encode(protocol.ServiceList{Services: p.services})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newPeers(t *testing.T, reg *registry.Registry, localID string, endpoints ...string) *Peers {
	t.Helper()
	return NewPeers(reg, localID, endpoints, time.Second, log.New(io.Discard, "", 0))
}

func endpointOf(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func TestPullLearnsAPeerDirectory(t *testing.T) {
	peer := &peerServer{nodeID: "node-b", services: []protocol.Service{
		{Name: "wiki.core", Address: "10.0.0.2", Port: 80},
	}}
	server := httptest.NewServer(peer.handler())
	defer server.Close()

	reg := registry.New()
	peers := newPeers(t, reg, "node-a", endpointOf(server))
	peers.PullAll(t.Context())

	svc, ok := reg.Lookup("wiki.core")
	if !ok || svc.Source != "node-b" {
		t.Fatalf("service = %+v, ok = %v", svc, ok)
	}
	nodes := peers.Nodes()
	if len(nodes) != 1 || !nodes[0].Online || nodes[0].ID != "node-b" || nodes[0].Services != 1 {
		t.Errorf("nodes = %+v", nodes)
	}
}

func TestPullWithdrawsWhenAPeerGoesAway(t *testing.T) {
	peer := &peerServer{nodeID: "node-b", services: []protocol.Service{
		{Name: "wiki.core", Address: "10.0.0.2", Port: 80},
	}}
	server := httptest.NewServer(peer.handler())

	reg := registry.New()
	peers := newPeers(t, reg, "node-a", endpointOf(server))
	peers.PullAll(t.Context())
	if _, ok := reg.Lookup("wiki.core"); !ok {
		t.Fatal("the service was never learned")
	}

	server.Close()
	peers.PullAll(t.Context())
	if _, ok := reg.Lookup("wiki.core"); ok {
		t.Error("an unreachable node must leave no services behind")
	}
	if node := peers.Nodes()[0]; node.Online || node.Error == "" {
		t.Errorf("node = %+v, want offline with a reason", node)
	}
}

func TestPullReplacesTheWholeDirectory(t *testing.T) {
	peer := &peerServer{nodeID: "node-b", services: []protocol.Service{
		{Name: "wiki.core", Address: "10.0.0.2", Port: 80},
		{Name: "docs.core", Address: "10.0.0.2", Port: 81},
	}}
	server := httptest.NewServer(peer.handler())
	defer server.Close()

	reg := registry.New()
	peers := newPeers(t, reg, "node-a", endpointOf(server))
	peers.PullAll(t.Context())

	peer.services = peer.services[:1]
	peers.PullAll(t.Context())
	if _, ok := reg.Lookup("docs.core"); ok {
		t.Error("a service the node stopped advertising must disappear")
	}
	if _, ok := reg.Lookup("wiki.core"); !ok {
		t.Error("the remaining service should still resolve")
	}
}

func TestPullIgnoresANodeClaimingOurOwnID(t *testing.T) {
	peer := &peerServer{nodeID: "node-a", services: []protocol.Service{
		{Name: "wiki.core", Address: "10.0.0.2", Port: 80},
	}}
	server := httptest.NewServer(peer.handler())
	defer server.Close()

	reg := registry.New()
	peers := newPeers(t, reg, "node-a", endpointOf(server))
	peers.PullAll(t.Context())

	if _, ok := reg.Lookup("wiki.core"); ok {
		t.Error("a node claiming this node's own id must be ignored")
	}
}

func TestSetEndpointsForgetsRemovedPeers(t *testing.T) {
	peer := &peerServer{nodeID: "node-b", services: []protocol.Service{
		{Name: "wiki.core", Address: "10.0.0.2", Port: 80},
	}}
	server := httptest.NewServer(peer.handler())
	defer server.Close()

	reg := registry.New()
	peers := newPeers(t, reg, "node-a", endpointOf(server))
	peers.PullAll(t.Context())

	peers.SetEndpoints(nil)
	if _, ok := reg.Lookup("wiki.core"); ok {
		t.Error("removing a node from the configuration must drop its services")
	}
	if len(peers.Nodes()) != 0 {
		t.Errorf("nodes = %+v, want none", peers.Nodes())
	}
}

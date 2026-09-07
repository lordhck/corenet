package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"corenet/internal/config"
	"corenet/pkg/protocol"
)

const (
	peerRequestTimeout = 5 * time.Second
	maxPeerResponse    = 4 << 20 // a directory is small; refuse to read more
)

// remoteStore is the part of the registry the peer puller writes to.
type remoteStore interface {
	SetRemote(nodeID string, services []protocol.Service)
	CountRemote(nodeID string) int
}

// Peers keeps the directories of the statically configured nodes up to date.
//
// CoreNet 0.1 pulls: there is no gossip, no signing and no authentication.
// A node simply asks the nodes it was told about what they serve, and drops
// everything it learned from a node it can no longer reach.
type Peers struct {
	store       remoteStore
	localNodeID string
	interval    time.Duration
	client      *http.Client
	logger      *log.Logger

	mu     sync.RWMutex
	states map[string]*peerState
	order  []string
}

type peerState struct {
	endpoint string
	nodeID   string
	nodeName string
	online   bool
	lastSeen time.Time
	lastErr  string
}

// NewPeers builds a puller for the given peer endpoints (host:port).
func NewPeers(store remoteStore, localNodeID string, endpoints []string, interval time.Duration, logger *log.Logger) *Peers {
	if interval <= 0 {
		interval = config.DefaultPullSeconds * time.Second
	}
	p := &Peers{
		store:       store,
		localNodeID: localNodeID,
		interval:    interval,
		client:      &http.Client{Timeout: peerRequestTimeout},
		logger:      logger,
		states:      make(map[string]*peerState),
	}
	p.SetEndpoints(endpoints)
	return p
}

// SetEndpoints replaces the peer list, forgetting the services of any node
// that is no longer configured. Used at startup and on reload.
func (p *Peers) SetEndpoints(endpoints []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	next := make(map[string]*peerState, len(endpoints))
	order := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if _, seen := next[endpoint]; seen {
			continue
		}
		if existing, ok := p.states[endpoint]; ok {
			next[endpoint] = existing
		} else {
			next[endpoint] = &peerState{endpoint: endpoint}
		}
		order = append(order, endpoint)
	}
	for endpoint, state := range p.states {
		if _, kept := next[endpoint]; !kept && state.nodeID != "" {
			p.store.SetRemote(state.nodeID, nil)
		}
	}
	p.states, p.order = next, order
}

// Run pulls every peer immediately and then once per interval, until ctx is
// cancelled.
func (p *Peers) Run(ctx context.Context) {
	p.PullAll(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.PullAll(ctx)
		}
	}
}

// PullAll refreshes every configured peer once.
func (p *Peers) PullAll(ctx context.Context) {
	p.mu.RLock()
	endpoints := append([]string(nil), p.order...)
	p.mu.RUnlock()

	for _, endpoint := range endpoints {
		p.pull(ctx, endpoint)
	}
}

func (p *Peers) pull(ctx context.Context, endpoint string) {
	var info protocol.Info
	err := p.get(ctx, endpoint, "/v1/info", &info)
	if err == nil && info.NodeID == "" {
		err = fmt.Errorf("node reported no id")
	}
	if err == nil && info.NodeID == p.localNodeID {
		err = fmt.Errorf("node id %q is this node's own id", info.NodeID)
	}
	var list protocol.ServiceList
	if err == nil {
		err = p.get(ctx, endpoint, "/v1/services", &list)
	}
	if err != nil {
		p.markOffline(endpoint, err)
		return
	}

	p.mu.Lock()
	state := p.states[endpoint]
	if state == nil { // removed by a reload while we were fetching
		p.mu.Unlock()
		return
	}
	previousID, wasOnline := state.nodeID, state.online
	state.nodeID, state.nodeName = info.NodeID, info.NodeName
	state.online, state.lastSeen, state.lastErr = true, time.Now(), ""
	p.mu.Unlock()

	if previousID != "" && previousID != info.NodeID {
		p.store.SetRemote(previousID, nil)
	}
	p.store.SetRemote(info.NodeID, list.Services)
	if !wasOnline {
		p.logger.Printf("node %s (%s) is reachable: %d services", info.NodeID, endpoint, len(list.Services))
	}
}

func (p *Peers) markOffline(endpoint string, cause error) {
	p.mu.Lock()
	state := p.states[endpoint]
	if state == nil {
		p.mu.Unlock()
		return
	}
	nodeID := state.nodeID
	// Report the first failure as well as every online -> offline change,
	// so a peer that was never reachable does not fail silently.
	report := state.online || state.lastErr == ""
	state.online, state.lastErr = false, cause.Error()
	p.mu.Unlock()

	if nodeID != "" {
		p.store.SetRemote(nodeID, nil)
	}
	if report {
		p.logger.Printf("node %s is unreachable: %v", endpoint, cause)
	}
}

func (p *Peers) get(ctx context.Context, endpoint, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, peerRequestTimeout)
	defer cancel()

	url := "http://" + endpoint + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", path, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxPeerResponse)).Decode(out)
}

// Nodes reports the configured peers, in configuration order.
func (p *Peers) Nodes() []protocol.Node {
	p.mu.RLock()
	defer p.mu.RUnlock()

	nodes := make([]protocol.Node, 0, len(p.order))
	for _, endpoint := range p.order {
		state := p.states[endpoint]
		node := protocol.Node{
			ID:       state.nodeID,
			Name:     state.nodeName,
			Endpoint: state.endpoint,
			Online:   state.online,
			Error:    state.lastErr,
		}
		if state.nodeID != "" {
			node.Services = p.store.CountRemote(state.nodeID)
		}
		if !state.lastSeen.IsZero() {
			node.LastSeen = state.lastSeen.Unix()
		}
		nodes = append(nodes, node)
	}
	return nodes
}

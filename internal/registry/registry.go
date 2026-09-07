// Package registry holds the local CoreNet directory: the services this node
// knows about, whether they were configured here, registered at runtime, or
// learned from another node.
package registry

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"corenet/pkg/protocol"
)

// Errors returned by the registry. The API layer maps them to status codes.
var (
	ErrNotFound = errors.New("service not found")
	ErrConflict = errors.New("service already registered")
)

// Registry is a safe-for-concurrent-use service directory.
//
// A name may be known from several sources at once. Local services always win
// over services learned from another node, so a node can override anything a
// peer advertises. Between two nodes advertising the same name, the lowest
// node ID wins, which keeps resolution deterministic.
type Registry struct {
	mu      sync.RWMutex
	config  map[string]protocol.Service
	runtime map[string]protocol.Service
	remote  map[string]map[string]protocol.Service // node ID -> name -> service
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		config:  make(map[string]protocol.Service),
		runtime: make(map[string]protocol.Service),
		remote:  make(map[string]map[string]protocol.Service),
	}
}

// SetConfigServices replaces every configured service. Used at startup and on
// configuration reload.
func (r *Registry) SetConfigServices(services []protocol.Service) {
	next := make(map[string]protocol.Service, len(services))
	for _, svc := range services {
		svc.Name = protocol.NormalizeName(svc.Name)
		svc.Source = protocol.SourceConfig
		next[svc.Name] = svc
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = next
}

// Add registers a service at runtime. It fails if the name is already served
// locally, so a registration never silently replaces a configured service.
func (r *Registry) Add(svc protocol.Service) (protocol.Service, error) {
	svc.Name = protocol.NormalizeName(svc.Name)
	if err := protocol.ValidateName(svc.Name); err != nil {
		return protocol.Service{}, err
	}
	if svc.Address == "" {
		return protocol.Service{}, fmt.Errorf("service %s has no address", svc.Name)
	}
	if svc.Port < 1 || svc.Port > 65535 {
		return protocol.Service{}, fmt.Errorf("service %s has an invalid port %d", svc.Name, svc.Port)
	}
	svc.Source = protocol.SourceRuntime

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.config[svc.Name]; ok {
		return protocol.Service{}, fmt.Errorf("%w: %s is configured on this node", ErrConflict, svc.Name)
	}
	if _, ok := r.runtime[svc.Name]; ok {
		return protocol.Service{}, fmt.Errorf("%w: %s", ErrConflict, svc.Name)
	}
	r.runtime[svc.Name] = svc
	return svc, nil
}

// Remove deletes a runtime registration. Configured services are owned by the
// configuration file and cannot be removed through the API.
func (r *Registry) Remove(name string) error {
	name = protocol.NormalizeName(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runtime[name]; ok {
		delete(r.runtime, name)
		return nil
	}
	if _, ok := r.config[name]; ok {
		return fmt.Errorf("%s is configured on this node; edit the configuration file instead", name)
	}
	return fmt.Errorf("%w: %s", ErrNotFound, name)
}

// SetRemote replaces everything learned from one node. Passing no services
// forgets that node's directory, which is what happens when a node goes away.
func (r *Registry) SetRemote(nodeID string, services []protocol.Service) {
	next := make(map[string]protocol.Service, len(services))
	for _, svc := range services {
		svc.Name = protocol.NormalizeName(svc.Name)
		if protocol.ValidateName(svc.Name) != nil || svc.Address == "" || svc.Port < 1 || svc.Port > 65535 {
			continue // never trust a peer's directory blindly
		}
		svc.Source = nodeID
		next[svc.Name] = svc
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(next) == 0 {
		delete(r.remote, nodeID)
		return
	}
	r.remote[nodeID] = next
}

// Lookup returns the service currently serving name.
func (r *Registry) Lookup(name string) (protocol.Service, bool) {
	name = protocol.NormalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if svc, ok := r.runtime[name]; ok {
		return svc, true
	}
	if svc, ok := r.config[name]; ok {
		return svc, true
	}
	for _, nodeID := range r.sortedNodeIDs() {
		if svc, ok := r.remote[nodeID][name]; ok {
			return svc, true
		}
	}
	return protocol.Service{}, false
}

// List returns every service this node can resolve, one entry per name,
// sorted by name.
func (r *Registry) List() []protocol.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	merged := make(map[string]protocol.Service, len(r.config)+len(r.runtime))
	// Lowest precedence first: later writes win.
	for _, nodeID := range reverse(r.sortedNodeIDs()) {
		for name, svc := range r.remote[nodeID] {
			merged[name] = svc
		}
	}
	for name, svc := range r.config {
		merged[name] = svc
	}
	for name, svc := range r.runtime {
		merged[name] = svc
	}

	services := make([]protocol.Service, 0, len(merged))
	for _, svc := range merged {
		services = append(services, svc)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services
}

// ListLocal returns only the services this node serves itself. It is what a
// node advertises to other nodes: a directory is never relayed onward.
func (r *Registry) ListLocal() []protocol.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	merged := make(map[string]protocol.Service, len(r.config)+len(r.runtime))
	for name, svc := range r.config {
		merged[name] = svc
	}
	for name, svc := range r.runtime {
		merged[name] = svc
	}

	services := make([]protocol.Service, 0, len(merged))
	for _, svc := range merged {
		services = append(services, svc)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services
}

// CountRemote returns how many services were learned from one node.
func (r *Registry) CountRemote(nodeID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.remote[nodeID])
}

func (r *Registry) sortedNodeIDs() []string {
	ids := make([]string, 0, len(r.remote))
	for id := range r.remote {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func reverse(values []string) []string {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
	return values
}

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
// A name may be known from several sources at once. The precedence is
// runtime, then configuration, then Docker, then another node: an operator can
// always override what was discovered, and local services always win over
// services learned from a peer. Between two nodes advertising the same name,
// the lowest node ID wins, which keeps resolution deterministic.
type Registry struct {
	mu        sync.RWMutex
	config    map[string]protocol.Service
	runtime   map[string]protocol.Service
	docker    map[string]protocol.Service
	remote    map[string]map[string]protocol.Service // node ID -> name -> service
	conflicts map[string]protocol.Conflict
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		config:    make(map[string]protocol.Service),
		runtime:   make(map[string]protocol.Service),
		docker:    make(map[string]protocol.Service),
		remote:    make(map[string]map[string]protocol.Service),
		conflicts: make(map[string]protocol.Conflict),
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
	// A name provided only by Docker is overridden, not refused: the
	// container's service stays in the directory and is served again when
	// this registration is removed.
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
	if _, ok := r.docker[name]; ok {
		return fmt.Errorf("%s is provided by a Docker container; stop the container instead", name)
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

// SetDocker replaces everything discovered from Docker, together with the
// names Docker cannot provide unambiguously. Like SetRemote it replaces the
// whole tier, so a container that disappeared needs no special handling.
func (r *Registry) SetDocker(services []protocol.Service, conflicts []protocol.Conflict) {
	next := make(map[string]protocol.Service, len(services))
	for _, svc := range services {
		svc.Name = protocol.NormalizeName(svc.Name)
		if protocol.ValidateName(svc.Name) != nil || svc.Address == "" || svc.Port < 1 || svc.Port > 65535 {
			continue // container metadata is untrusted input
		}
		svc.Source = protocol.SourceDocker
		next[svc.Name] = svc
	}

	conflicted := make(map[string]protocol.Conflict, len(conflicts))
	for _, conflict := range conflicts {
		conflict.Name = protocol.NormalizeName(conflict.Name)
		if protocol.ValidateName(conflict.Name) != nil {
			continue
		}
		delete(next, conflict.Name) // a conflicted name is never routed
		conflicted[conflict.Name] = conflict
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.docker, r.conflicts = next, conflicted
}

// Conflict reports whether a name is known but unroutable, and why.
func (r *Registry) Conflict(name string) (protocol.Conflict, bool) {
	name = protocol.NormalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	conflict, ok := r.conflicts[name]
	return conflict, ok
}

// Conflicts lists every unroutable name, sorted by name.
func (r *Registry) Conflicts() []protocol.Conflict {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conflicts := make([]protocol.Conflict, 0, len(r.conflicts))
	for _, conflict := range r.conflicts {
		conflicts = append(conflicts, conflict)
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Name < conflicts[j].Name })
	return conflicts
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
	if svc, ok := r.docker[name]; ok {
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

	merged := make(map[string]protocol.Service, len(r.config)+len(r.runtime)+len(r.docker))
	// Lowest precedence first: later writes win.
	for _, nodeID := range reverse(r.sortedNodeIDs()) {
		for name, svc := range r.remote[nodeID] {
			merged[name] = svc
		}
	}
	for name, svc := range r.docker {
		merged[name] = svc
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

// ListLocal returns only the services this node serves itself, which is what
// it advertises to other nodes: a directory is never relayed onward, and a
// conflicted name is never offered.
func (r *Registry) ListLocal() []protocol.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	merged := make(map[string]protocol.Service, len(r.config)+len(r.runtime)+len(r.docker))
	for name, svc := range r.docker {
		merged[name] = svc
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

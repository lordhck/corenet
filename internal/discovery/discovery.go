package discovery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"corenet/pkg/protocol"
)

// Store is the part of the registry Docker discovery writes to.
type Store interface {
	SetDocker(services []protocol.Service, conflicts []protocol.Conflict)
}

// Docker keeps the registry's Docker tier in step with the running
// containers.
//
// It polls rather than subscribing to the event stream: one code path, no
// long-lived connection to nurse, and a resync would be needed anyway.
type Docker struct {
	client   *Client
	store    Store
	network  string
	interval time.Duration
	logger   *log.Logger

	// available and skipped keep the log quiet: a repeated failure or a
	// repeatedly ignored container is reported once, not every poll.
	available  bool
	skipped    map[string]string
	conflicted map[string]bool
}

// NewDocker builds a discovery loop for the Docker socket at socket.
func NewDocker(store Store, socket, network string, interval time.Duration, logger *log.Logger) *Docker {
	return &Docker{
		client:     NewClient(socket),
		store:      store,
		network:    network,
		interval:   interval,
		logger:     logger,
		available:  true,
		skipped:    make(map[string]string),
		conflicted: make(map[string]bool),
	}
}

// Run polls immediately and then once per interval, until ctx is cancelled.
func (d *Docker) Run(ctx context.Context) {
	d.Poll(ctx)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Poll(ctx)
		}
	}
}

// Poll reads the running containers once and updates the registry.
func (d *Docker) Poll(ctx context.Context) {
	containers, err := d.client.listContainers(ctx)
	if err != nil {
		// Docker being absent or broken is not fatal: the node keeps
		// serving its configured, runtime and remote services.
		if d.available {
			d.logger.Printf("docker: unavailable, discovery paused: %v", err)
			d.available = false
			d.store.SetDocker(nil, nil)
		}
		return
	}
	if !d.available {
		d.logger.Print("docker: available again")
		d.available = true
	}

	services, conflicts := d.convert(containers)
	d.store.SetDocker(services, conflicts)
}

// convert turns containers into services, reporting the names that more than
// one container claims.
func (d *Docker) convert(containers []container) ([]protocol.Service, []protocol.Conflict) {
	claimed := make(map[string][]string) // name -> container names
	services := make(map[string]protocol.Service)
	seen := make(map[string]string)

	for _, c := range containers {
		svc, err := service(c, d.network)
		if errors.Is(err, errSkip) {
			continue
		}
		if err != nil {
			d.reportSkip(c.name(), err)
			continue
		}
		seen[c.name()] = svc.Name
		claimed[svc.Name] = append(claimed[svc.Name], c.name())
		services[svc.Name] = svc
	}
	d.forgetSkips(seen)

	var conflicts []protocol.Conflict
	for name, owners := range claimed {
		if len(owners) < 2 {
			continue
		}
		sort.Strings(owners)
		delete(services, name)
		conflicts = append(conflicts, protocol.Conflict{
			Name:   name,
			Reason: fmt.Sprintf("claimed by %d Docker containers: %v", len(owners), owners),
		})
	}

	list := make([]protocol.Service, 0, len(services))
	for _, svc := range services {
		list = append(list, svc)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Name < conflicts[j].Name })
	d.reportConflicts(conflicts)
	return list, conflicts
}

// reportSkip logs a container CoreNet cannot use, once per reason.
func (d *Docker) reportSkip(container string, cause error) {
	if d.skipped[container] == cause.Error() {
		return
	}
	d.skipped[container] = cause.Error()
	d.logger.Printf("docker: ignoring container %s: %v", container, cause)
}

func (d *Docker) forgetSkips(seen map[string]string) {
	for container := range d.skipped {
		if _, ok := seen[container]; ok {
			delete(d.skipped, container)
		}
	}
}

// reportConflicts logs conflicts as they appear and as they clear.
func (d *Docker) reportConflicts(conflicts []protocol.Conflict) {
	current := make(map[string]bool, len(conflicts))
	for _, conflict := range conflicts {
		current[conflict.Name] = true
		if !d.conflicted[conflict.Name] {
			d.logger.Printf("docker: %s is not routed: %s", conflict.Name, conflict.Reason)
		}
	}
	for name := range d.conflicted {
		if !current[name] {
			d.logger.Printf("docker: %s is no longer in conflict", name)
		}
	}
	d.conflicted = current
}

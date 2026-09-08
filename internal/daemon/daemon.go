// Package daemon wires the CoreNet node together: DNS for the .core
// namespace, an HTTP proxy for the services, a local control API for the CLI,
// and a read-only API for other nodes.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"corenet/internal/config"
	"corenet/internal/discovery"
	coredns "corenet/internal/dns"
	"corenet/internal/registry"
	"corenet/pkg/protocol"
)

const shutdownTimeout = 5 * time.Second

// Daemon is a running CoreNet node.
type Daemon struct {
	logger  *log.Logger
	started time.Time

	mu  sync.RWMutex
	cfg config.Config

	registry *registry.Registry
	peers    *Peers
	docker   *discovery.Docker

	dns        *coredns.Server
	httpSrv    *http.Server
	peerSrv    *http.Server
	controlSrv *http.Server
}

// New prepares a daemon from a configuration. Nothing is bound until Run.
func New(cfg config.Config, logger *log.Logger) *Daemon {
	reg := registry.New()
	reg.SetConfigServices(cfg.Services)
	d := &Daemon{
		logger:   logger,
		started:  time.Now(),
		cfg:      cfg,
		registry: reg,
	}
	d.peers = NewPeers(reg, cfg.Node.ID, cfg.Nodes, cfg.PullInterval(), logger)
	if cfg.Docker.IsEnabled() {
		d.docker = discovery.NewDocker(reg, cfg.Docker.SocketPath(), cfg.Docker.Network,
			cfg.Docker.PollInterval(), logger)
	}
	return d
}

// Run binds every listener and serves until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	cfg := d.config()
	api := &API{
		Dir:    d.registry,
		Status: d.Status,
		Info:   d.Info,
		Nodes:  d.Nodes,
		Logger: d.logger,
	}

	if err := d.startDNS(cfg); err != nil {
		return err
	}
	if err := d.startHTTP(cfg); err != nil {
		return err
	}
	if err := d.startPeerAPI(cfg, api); err != nil {
		return err
	}
	if err := d.startControlAPI(cfg, api); err != nil {
		return err
	}

	d.logger.Printf("corenetd %s: node %s", protocol.Version, cfg.Node.ID)
	if len(cfg.Nodes) > 0 {
		go d.peers.Run(ctx)
	}
	if d.docker != nil {
		d.logger.Printf("docker: discovering services through %s", cfg.Docker.SocketPath())
		go d.docker.Run(ctx)
	} else {
		d.logger.Print("docker: discovery disabled")
	}

	<-ctx.Done()
	return d.shutdown()
}

func (d *Daemon) startDNS(cfg config.Config) error {
	if cfg.Listen.DNS == "" {
		d.logger.Print("dns: disabled")
		return nil
	}
	answer := proxyAddress(cfg.Listen.HTTP)
	server, err := coredns.New(cfg.Listen.DNS, answer, d.registry)
	if err != nil {
		return err
	}
	if err := server.Start(); err != nil {
		return bindError("dns", cfg.Listen.DNS, err)
	}
	d.dns = server
	d.logger.Printf("dns: listening on %s, .core resolves to %s", cfg.Listen.DNS, answer)
	return nil
}

func (d *Daemon) startHTTP(cfg config.Config) error {
	if cfg.Listen.HTTP == "" {
		d.logger.Print("http: disabled")
		return nil
	}
	listener, err := net.Listen("tcp", cfg.Listen.HTTP)
	if err != nil {
		return bindError("http", cfg.Listen.HTTP, err)
	}
	d.httpSrv = &http.Server{
		Handler:           NewProxy(d.registry, d.logger),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          d.logger,
	}
	d.serve("http", d.httpSrv, listener)
	d.logger.Printf("http: serving .core services on %s", cfg.Listen.HTTP)
	return nil
}

func (d *Daemon) startPeerAPI(cfg config.Config, api *API) error {
	if cfg.Listen.Peer == "" {
		return nil
	}
	listener, err := net.Listen("tcp", cfg.Listen.Peer)
	if err != nil {
		return bindError("peer", cfg.Listen.Peer, err)
	}
	d.peerSrv = &http.Server{
		Handler:           api.PeerMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          d.logger,
	}
	d.serve("peer", d.peerSrv, listener)
	d.logger.Printf("peer: read-only directory on %s", cfg.Listen.Peer)
	return nil
}

func (d *Daemon) startControlAPI(cfg config.Config, api *API) error {
	path := cfg.Listen.Control
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("control socket directory: %w", err)
	}
	// A socket left behind by a crashed daemon would block the bind.
	if err := removeStaleSocket(path); err != nil {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("control socket %s: %w", path, err)
	}
	// The control API may change what this node serves: keep it off-limits
	// to other local users.
	if err := os.Chmod(path, 0o660); err != nil {
		return fmt.Errorf("control socket %s: %w", path, err)
	}
	d.controlSrv = &http.Server{
		Handler:           api.ControlMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          d.logger,
	}
	d.serve("control", d.controlSrv, listener)
	d.logger.Printf("control: listening on %s", path)
	return nil
}

func (d *Daemon) serve(name string, srv *http.Server, listener net.Listener) {
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.logger.Printf("%s: %v", name, err)
		}
	}()
}

func (d *Daemon) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var firstErr error
	for _, srv := range []*http.Server{d.httpSrv, d.peerSrv, d.controlSrv} {
		if srv == nil {
			continue
		}
		if err := srv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if d.dns != nil {
		if err := d.dns.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	_ = os.Remove(d.config().Listen.Control)
	d.logger.Print("corenetd: stopped")
	return firstErr
}

// Reload re-reads the configuration file and applies the parts that can
// change while running: services and peers. Listen addresses are fixed for
// the lifetime of the process.
func (d *Daemon) Reload(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	current := d.config()
	if cfg.Listen != current.Listen || cfg.Node.ID != current.Node.ID ||
		cfg.PullIntervalSeconds != current.PullIntervalSeconds || !cfg.Docker.Equal(current.Docker) {
		d.logger.Print("reload: listen addresses, node id, pull interval and docker settings " +
			"are only applied on restart")
		cfg.Listen, cfg.Node = current.Listen, current.Node
		cfg.PullIntervalSeconds = current.PullIntervalSeconds
		cfg.Docker = current.Docker
	}

	d.mu.Lock()
	d.cfg = cfg
	d.mu.Unlock()

	d.registry.SetConfigServices(cfg.Services)
	d.peers.SetEndpoints(cfg.Nodes)
	d.logger.Printf("reload: %d configured services, %d nodes", len(cfg.Services), len(cfg.Nodes))
	return nil
}

// Status answers GET /v1/status.
func (d *Daemon) Status() protocol.Status {
	cfg := d.config()
	return protocol.Status{
		Version:  protocol.Version,
		NodeID:   cfg.Node.ID,
		NodeName: cfg.Node.Name,
		Uptime:   int64(time.Since(d.started).Seconds()),
		Listeners: protocol.Listeners{
			DNS:     cfg.Listen.DNS,
			HTTP:    cfg.Listen.HTTP,
			Peer:    cfg.Listen.Peer,
			Control: cfg.Listen.Control,
		},
		Services: len(d.registry.List()),
		Nodes:    len(d.Nodes()),
	}
}

// Info answers GET /v1/info on the peer listener.
func (d *Daemon) Info() protocol.Info {
	cfg := d.config()
	return protocol.Info{
		Version:  protocol.Version,
		NodeID:   cfg.Node.ID,
		NodeName: cfg.Node.Name,
		Services: len(d.registry.ListLocal()),
	}
}

// Nodes lists this node followed by the configured peers.
func (d *Daemon) Nodes() []protocol.Node {
	cfg := d.config()
	local := protocol.Node{
		ID:       cfg.Node.ID,
		Name:     cfg.Node.Name,
		Endpoint: cfg.Listen.Peer,
		Local:    true,
		Online:   true,
		Services: len(d.registry.ListLocal()),
	}
	return append([]protocol.Node{local}, d.peers.Nodes()...)
}

func (d *Daemon) config() config.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

// proxyAddress is the address .core names should resolve to: the host part of
// the HTTP listener, or loopback when it listens on every interface.
func proxyAddress(httpAddr string) string {
	host, _, err := net.SplitHostPort(httpAddr)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

// removeStaleSocket deletes a socket file that no daemon is listening on.
func removeStaleSocket(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("control socket %s: not a socket", path)
	}
	if conn, err := net.Dial("unix", path); err == nil {
		_ = conn.Close()
		return fmt.Errorf("control socket %s: another corenetd is already running", path)
	}
	return os.Remove(path)
}

// bindError explains the most common startup failure: privileged ports.
func bindError(name, addr string, err error) error {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("%s: cannot bind %s: %w (ports below 1024 need root or "+
			"'setcap cap_net_bind_service=+ep corenetd')", name, addr, err)
	}
	return fmt.Errorf("%s: cannot bind %s: %w", name, addr, err)
}

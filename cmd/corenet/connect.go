package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"corenet/pkg/protocol"
)

// CoreNet configures name resolution through systemd-resolved: one drop-in
// file that routes the .core domain to the local corenetd, and nothing else.
const (
	dropInDir  = "/etc/systemd/resolved.conf.d"
	dropInPath = dropInDir + "/corenet.conf"
	resolvedID = "systemd-resolved"
)

func connect(opts *options) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("connect must be run as root (try: sudo corenet connect)")
	}
	c, err := newClient(opts)
	if err != nil {
		return err
	}
	var st protocol.Status
	if err := c.get("/v1/status", &st, false); err != nil {
		return err
	}
	if st.Listeners.DNS == "" {
		return fmt.Errorf("corenetd is running without a DNS listener; set listen.dns in the configuration")
	}

	dropIn := dropInContent(st.Listeners.DNS)
	if !resolvedIsActive() {
		fmt.Print(manualInstructions(dropIn))
		return fmt.Errorf("%s is not active; configure your resolver manually", resolvedID)
	}
	if err := writeFileAtomic(dropInPath, dropIn, 0o644); err != nil {
		return err
	}
	if err := restartResolved(); err != nil {
		return err
	}

	fmt.Printf("connected: .core resolves through %s\n", st.Listeners.DNS)
	fmt.Printf("wrote %s\n", dropInPath)
	if st.Services == 0 {
		fmt.Println("no services are registered yet (try: corenet service add ...)")
	}
	return nil
}

func disconnect() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("disconnect must be run as root (try: sudo corenet disconnect)")
	}
	if _, err := os.Stat(dropInPath); os.IsNotExist(err) {
		fmt.Println("not connected")
		return nil
	}
	if err := os.Remove(dropInPath); err != nil {
		return err
	}
	if resolvedIsActive() {
		if err := restartResolved(); err != nil {
			return err
		}
	}
	fmt.Printf("disconnected: removed %s\n", dropInPath)
	return nil
}

// resolutionState reports whether this machine is currently configured to
// resolve .core names.
func resolutionState() string {
	if _, err := os.Stat(dropInPath); err == nil {
		return "connected (" + resolvedID + ")"
	}
	return "not connected"
}

// dropInContent renders the systemd-resolved configuration for a corenetd
// DNS listener. Only the .core domain is routed to it, so ordinary Internet
// resolution is untouched.
func dropInContent(dnsAddr string) string {
	host, port, err := net.SplitHostPort(dnsAddr)
	if err != nil {
		host, port = dnsAddr, "53"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	server := host
	if port != "53" {
		server = net.JoinHostPort(host, port)
	}
	return fmt.Sprintf("# Managed by corenet. Remove with: corenet disconnect\n"+
		"[Resolve]\nDNS=%s\nDomains=~%s\n", server, protocol.TLD)
}

func manualInstructions(dropIn string) string {
	return fmt.Sprintf("%s is not running, so CoreNet did not change anything.\n\n"+
		"Point your resolver at corenetd for the .%s domain. With systemd-resolved that is:\n\n"+
		"  %s\n\n%s\n", resolvedID, protocol.TLD, dropInPath, indent(dropIn, "  "))
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func resolvedIsActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", resolvedID).Run() == nil
}

func restartResolved() error {
	if out, err := exec.Command("systemctl", "restart", resolvedID).CombinedOutput(); err != nil {
		return fmt.Errorf("restarting %s: %v: %s", resolvedID, err, strings.TrimSpace(string(out)))
	}
	// Best effort: a stale cache would hide a name that now resolves.
	_ = exec.Command("resolvectl", "flush-caches").Run()
	return nil
}

// writeFileAtomic writes through a temporary file in the same directory, so a
// half-written resolver configuration is never visible.
func writeFileAtomic(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".corenet-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Package dns serves the .core namespace.
//
// The server is deliberately not a resolver: it answers for names this node
// knows, refuses everything outside .core, and never forwards a query.
package dns

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"

	"corenet/pkg/protocol"
)

// Directory is the part of the registry the DNS server needs.
type Directory interface {
	Lookup(name string) (protocol.Service, bool)
}

// Handler answers .core queries. Every known name resolves to Answer, the
// address of this node's CoreNet HTTP listener, which then proxies the
// request to the real backend.
type Handler struct {
	Answer net.IP
	Dir    Directory
}

// ServeDNS implements dns.Handler.
func (h *Handler) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	reply := new(dns.Msg)
	reply.SetReply(req)
	reply.Authoritative = true
	reply.RecursionAvailable = false

	if req.Opcode != dns.OpcodeQuery || len(req.Question) != 1 {
		reply.Rcode = dns.RcodeFormatError
		_ = w.WriteMsg(reply)
		return
	}

	question := req.Question[0]
	name := protocol.NormalizeName(question.Name)

	if question.Qclass != dns.ClassINET || !isCoreName(name) {
		reply.Rcode = dns.RcodeRefused
		_ = w.WriteMsg(reply)
		return
	}

	if _, ok := h.Dir.Lookup(name); !ok {
		reply.Rcode = dns.RcodeNameError // NXDOMAIN
		_ = w.WriteMsg(reply)
		return
	}

	// The name exists. Only A carries data; anything else is an empty
	// NOERROR, which keeps browsers from stalling on AAAA lookups.
	if question.Qtype == dns.TypeA {
		reply.Answer = append(reply.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   question.Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    0,
			},
			A: h.Answer,
		})
	}
	_ = w.WriteMsg(reply)
}

func isCoreName(name string) bool {
	return strings.HasSuffix(name, "."+protocol.TLD) && len(name) > len(protocol.TLD)+1
}

// Server runs the DNS handler on UDP and TCP.
type Server struct {
	udp *dns.Server
	tcp *dns.Server
}

// New builds a server listening on addr, answering known names with answerIP.
func New(addr, answerIP string, dir Directory) (*Server, error) {
	ip := net.ParseIP(answerIP)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("dns: %q is not an IPv4 address", answerIP)
	}
	handler := &Handler{Answer: ip.To4(), Dir: dir}
	return &Server{
		udp: &dns.Server{Addr: addr, Net: "udp", Handler: handler},
		tcp: &dns.Server{Addr: addr, Net: "tcp", Handler: handler},
	}, nil
}

// Start binds both listeners and serves them in the background. It returns
// once the sockets are bound, so a bind failure is reported at startup.
func (s *Server) Start() error {
	udpConn, err := net.ListenPacket("udp", s.udp.Addr)
	if err != nil {
		return fmt.Errorf("dns udp: %w", err)
	}
	tcpListener, err := net.Listen("tcp", s.tcp.Addr)
	if err != nil {
		_ = udpConn.Close()
		return fmt.Errorf("dns tcp: %w", err)
	}
	s.udp.PacketConn = udpConn
	s.tcp.Listener = tcpListener
	go func() { _ = s.udp.ActivateAndServe() }()
	go func() { _ = s.tcp.ActivateAndServe() }()
	return nil
}

// Shutdown stops both listeners.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error
	for _, srv := range []*dns.Server{s.udp, s.tcp} {
		if err := srv.ShutdownContext(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

package dns

import (
	"net"
	"testing"

	"github.com/miekg/dns"

	"corenet/pkg/protocol"
)

type fakeDirectory map[string]protocol.Service

func (f fakeDirectory) Lookup(name string) (protocol.Service, bool) {
	svc, ok := f[protocol.NormalizeName(name)]
	return svc, ok
}

// capturingWriter records the reply instead of writing it to a socket.
type capturingWriter struct {
	dns.ResponseWriter
	msg *dns.Msg
}

func (c *capturingWriter) WriteMsg(m *dns.Msg) error {
	c.msg = m
	return nil
}

func query(t *testing.T, h *Handler, name string, qtype uint16) *dns.Msg {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(name), qtype)
	w := &capturingWriter{}
	h.ServeDNS(w, req)
	if w.msg == nil {
		t.Fatal("handler wrote no reply")
	}
	return w.msg
}

func testHandler() *Handler {
	return &Handler{
		Answer: net.IPv4(127, 0, 0, 1).To4(),
		Dir: fakeDirectory{
			"hello.core": {Name: "hello.core", Address: "127.0.0.1", Port: 8081},
		},
	}
}

func TestKnownNameResolvesToTheLocalProxy(t *testing.T) {
	reply := query(t, testHandler(), "Hello.Core", dns.TypeA)
	if reply.Rcode != dns.RcodeSuccess {
		t.Fatalf("rcode = %s, want NOERROR", dns.RcodeToString[reply.Rcode])
	}
	if !reply.Authoritative {
		t.Error("reply should be authoritative")
	}
	if len(reply.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(reply.Answer))
	}
	a, ok := reply.Answer[0].(*dns.A)
	if !ok || !a.A.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("answer = %v, want 127.0.0.1", reply.Answer[0])
	}
}

func TestUnknownCoreNameIsNXDOMAIN(t *testing.T) {
	reply := query(t, testHandler(), "nope.core", dns.TypeA)
	if reply.Rcode != dns.RcodeNameError {
		t.Fatalf("rcode = %s, want NXDOMAIN", dns.RcodeToString[reply.Rcode])
	}
}

func TestNamesOutsideCoreAreRefused(t *testing.T) {
	for _, name := range []string{"example.com", "core", "hello.test"} {
		reply := query(t, testHandler(), name, dns.TypeA)
		if reply.Rcode != dns.RcodeRefused {
			t.Errorf("%s: rcode = %s, want REFUSED", name, dns.RcodeToString[reply.Rcode])
		}
	}
}

func TestAAAAForKnownNameIsEmptyNoError(t *testing.T) {
	reply := query(t, testHandler(), "hello.core", dns.TypeAAAA)
	if reply.Rcode != dns.RcodeSuccess {
		t.Fatalf("rcode = %s, want NOERROR", dns.RcodeToString[reply.Rcode])
	}
	if len(reply.Answer) != 0 {
		t.Errorf("answers = %d, want 0", len(reply.Answer))
	}
}

func TestServerAnswersOverTheWire(t *testing.T) {
	srv, err := New("127.0.0.1:0", "127.0.0.1", testHandler().Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()

	addr := srv.udp.PacketConn.LocalAddr().String()
	req := new(dns.Msg)
	req.SetQuestion("hello.core.", dns.TypeA)
	reply, err := dns.Exchange(req, addr)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(reply.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(reply.Answer))
	}
}

func TestNewRejectsNonIPv4Answer(t *testing.T) {
	if _, err := New("127.0.0.1:0", "::1", fakeDirectory{}); err == nil {
		t.Fatal("expected an error for a non-IPv4 answer address")
	}
}

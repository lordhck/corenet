package registry

import (
	"errors"
	"testing"

	"corenet/pkg/protocol"
)

func svc(name, address string, port int) protocol.Service {
	return protocol.Service{Name: name, Address: address, Port: port}
}

func TestAddAndLookup(t *testing.T) {
	r := New()
	if _, err := r.Add(svc("Wiki.Core", "127.0.0.1", 8081)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Lookup("wiki.core.")
	if !ok {
		t.Fatal("Lookup failed for a registered service")
	}
	if got.Addr() != "127.0.0.1:8081" || got.Source != protocol.SourceRuntime {
		t.Errorf("got %+v", got)
	}
}

func TestAddRejectsInvalidServices(t *testing.T) {
	r := New()
	cases := map[string]protocol.Service{
		"non-core name": svc("wiki.test", "127.0.0.1", 80),
		"no address":    svc("wiki.core", "", 80),
		"bad port":      svc("wiki.core", "127.0.0.1", 70000),
	}
	for name, s := range cases {
		if _, err := r.Add(s); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAddConflictsWithLocalServices(t *testing.T) {
	r := New()
	r.SetConfigServices([]protocol.Service{svc("wiki.core", "127.0.0.1", 80)})
	if _, err := r.Add(svc("wiki.core", "127.0.0.1", 81)); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if _, err := r.Add(svc("docs.core", "127.0.0.1", 82)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(svc("docs.core", "127.0.0.1", 83)); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestRemove(t *testing.T) {
	r := New()
	r.SetConfigServices([]protocol.Service{svc("wiki.core", "127.0.0.1", 80)})
	if _, err := r.Add(svc("docs.core", "127.0.0.1", 82)); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove("docs.core"); err != nil {
		t.Fatalf("Remove runtime service: %v", err)
	}
	if _, ok := r.Lookup("docs.core"); ok {
		t.Error("service still resolves after removal")
	}
	if err := r.Remove("wiki.core"); err == nil {
		t.Error("configured services must not be removable through the API")
	}
	if err := r.Remove("absent.core"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLocalWinsOverRemote(t *testing.T) {
	r := New()
	r.SetRemote("node-b", []protocol.Service{svc("wiki.core", "10.0.0.2", 80)})
	if got, _ := r.Lookup("wiki.core"); got.Address != "10.0.0.2" {
		t.Fatalf("remote service not resolvable: %+v", got)
	}
	r.SetConfigServices([]protocol.Service{svc("wiki.core", "127.0.0.1", 8080)})
	got, _ := r.Lookup("wiki.core")
	if got.Address != "127.0.0.1" || got.Source != protocol.SourceConfig {
		t.Errorf("local service should win: %+v", got)
	}
}

func TestLowestNodeIDWinsBetweenRemotes(t *testing.T) {
	r := New()
	r.SetRemote("node-c", []protocol.Service{svc("wiki.core", "10.0.0.3", 80)})
	r.SetRemote("node-b", []protocol.Service{svc("wiki.core", "10.0.0.2", 80)})
	for i := 0; i < 5; i++ {
		if got, _ := r.Lookup("wiki.core"); got.Source != "node-b" {
			t.Fatalf("resolution is not deterministic: %+v", got)
		}
	}
	list := r.List()
	if len(list) != 1 || list[0].Source != "node-b" {
		t.Errorf("List = %+v, want one entry from node-b", list)
	}
}

func TestSetRemoteReplacesAndForgets(t *testing.T) {
	r := New()
	r.SetRemote("node-b", []protocol.Service{
		svc("wiki.core", "10.0.0.2", 80),
		svc("docs.core", "10.0.0.2", 81),
	})
	if n := r.CountRemote("node-b"); n != 2 {
		t.Fatalf("CountRemote = %d, want 2", n)
	}
	r.SetRemote("node-b", []protocol.Service{svc("wiki.core", "10.0.0.2", 80)})
	if _, ok := r.Lookup("docs.core"); ok {
		t.Error("a service dropped by a node must disappear")
	}
	r.SetRemote("node-b", nil)
	if _, ok := r.Lookup("wiki.core"); ok {
		t.Error("an offline node must leave no services behind")
	}
}

func TestSetRemoteIgnoresInvalidEntries(t *testing.T) {
	r := New()
	r.SetRemote("node-b", []protocol.Service{
		svc("wiki.test", "10.0.0.2", 80),
		svc("bad.core", "", 80),
		svc("good.core", "10.0.0.2", 80),
	})
	if n := r.CountRemote("node-b"); n != 1 {
		t.Fatalf("CountRemote = %d, want 1", n)
	}
	if _, ok := r.Lookup("good.core"); !ok {
		t.Error("valid remote service was dropped")
	}
}

func TestListAndListLocal(t *testing.T) {
	r := New()
	r.SetConfigServices([]protocol.Service{svc("b.core", "127.0.0.1", 80)})
	if _, err := r.Add(svc("a.core", "127.0.0.1", 81)); err != nil {
		t.Fatal(err)
	}
	r.SetRemote("node-b", []protocol.Service{svc("c.core", "10.0.0.2", 80)})

	all := r.List()
	if len(all) != 3 || all[0].Name != "a.core" || all[2].Name != "c.core" {
		t.Errorf("List = %+v", all)
	}
	local := r.ListLocal()
	if len(local) != 2 {
		t.Errorf("ListLocal = %+v, want only local services", local)
	}
	for _, s := range local {
		if s.Source != protocol.SourceConfig && s.Source != protocol.SourceRuntime {
			t.Errorf("ListLocal returned a remote service: %+v", s)
		}
	}
}

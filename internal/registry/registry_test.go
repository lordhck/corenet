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

func dockerSvc(name, address string, port int) protocol.Service {
	return protocol.Service{Name: name, Address: address, Port: port}
}

func TestDockerPrecedence(t *testing.T) {
	r := New()
	r.SetRemote("node-b", []protocol.Service{dockerSvc("wiki.core", "10.0.0.2", 80)})
	r.SetDocker([]protocol.Service{dockerSvc("wiki.core", "172.17.0.2", 80)}, nil)

	got, _ := r.Lookup("wiki.core")
	if got.Address != "172.17.0.2" || got.Source != protocol.SourceDocker {
		t.Fatalf("docker should beat a remote node: %+v", got)
	}

	r.SetConfigServices([]protocol.Service{dockerSvc("wiki.core", "127.0.0.1", 8080)})
	if got, _ := r.Lookup("wiki.core"); got.Source != protocol.SourceConfig {
		t.Errorf("configuration should beat docker: %+v", got)
	}
}

func TestRuntimeRegistrationOverridesDocker(t *testing.T) {
	r := New()
	r.SetDocker([]protocol.Service{dockerSvc("wiki.core", "172.17.0.2", 80)}, nil)

	// Overriding a Docker name is allowed; only config and runtime conflict.
	if _, err := r.Add(dockerSvc("wiki.core", "127.0.0.1", 9000)); err != nil {
		t.Fatalf("a Docker name must be overridable: %v", err)
	}
	if got, _ := r.Lookup("wiki.core"); got.Address != "127.0.0.1" {
		t.Errorf("runtime should win: %+v", got)
	}

	// Removing the override hands the name back to the container.
	if err := r.Remove("wiki.core"); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Lookup("wiki.core")
	if !ok || got.Address != "172.17.0.2" || got.Source != protocol.SourceDocker {
		t.Errorf("the docker service should serve again: %+v", got)
	}
}

func TestDockerServicesCannotBeRemoved(t *testing.T) {
	r := New()
	r.SetDocker([]protocol.Service{dockerSvc("wiki.core", "172.17.0.2", 80)}, nil)
	err := r.Remove("wiki.core")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a refusal naming the container", err)
	}
	if _, ok := r.Lookup("wiki.core"); !ok {
		t.Error("the service must still be there")
	}
}

func TestConflictedNamesAreNotRouted(t *testing.T) {
	r := New()
	r.SetDocker(
		[]protocol.Service{
			dockerSvc("blog.core", "172.17.0.2", 80),
			dockerSvc("wiki.core", "172.17.0.3", 80),
		},
		[]protocol.Conflict{{Name: "blog.core", Reason: "two containers"}},
	)

	if _, ok := r.Lookup("blog.core"); ok {
		t.Error("a conflicted name must not resolve")
	}
	for _, svc := range append(r.List(), r.ListLocal()...) {
		if svc.Name == "blog.core" {
			t.Error("a conflicted name must not be listed or advertised")
		}
	}
	conflict, ok := r.Conflict("blog.core")
	if !ok || conflict.Reason == "" {
		t.Errorf("conflict = %+v, ok = %v", conflict, ok)
	}
	if conflicts := r.Conflicts(); len(conflicts) != 1 || conflicts[0].Name != "blog.core" {
		t.Errorf("Conflicts = %+v", conflicts)
	}
	if _, ok := r.Lookup("wiki.core"); !ok {
		t.Error("the other container should be unaffected")
	}
}

func TestConflictClearsWhenDockerReports(t *testing.T) {
	r := New()
	r.SetDocker(nil, []protocol.Conflict{{Name: "blog.core", Reason: "two containers"}})
	if _, ok := r.Conflict("blog.core"); !ok {
		t.Fatal("the conflict was not recorded")
	}
	r.SetDocker([]protocol.Service{dockerSvc("blog.core", "172.17.0.2", 80)}, nil)
	if _, ok := r.Conflict("blog.core"); ok {
		t.Error("the conflict should have cleared")
	}
	if _, ok := r.Lookup("blog.core"); !ok {
		t.Error("the remaining container should route again")
	}
}

func TestConflictDoesNotBlockALocalService(t *testing.T) {
	r := New()
	r.SetConfigServices([]protocol.Service{dockerSvc("blog.core", "127.0.0.1", 8080)})
	r.SetDocker(nil, []protocol.Conflict{{Name: "blog.core", Reason: "two containers"}})

	got, ok := r.Lookup("blog.core")
	if !ok || got.Source != protocol.SourceConfig {
		t.Errorf("a configured service must still be routed: %+v", got)
	}
	if _, ok := r.Conflict("blog.core"); !ok {
		t.Error("the docker conflict should still be reported")
	}
}

func TestSetDockerIgnoresInvalidContainers(t *testing.T) {
	r := New()
	r.SetDocker([]protocol.Service{
		dockerSvc("wiki.test", "172.17.0.2", 80),
		dockerSvc("bad.core", "", 80),
		dockerSvc("worse.core", "172.17.0.2", 0),
		dockerSvc("good.core", "172.17.0.2", 80),
	}, nil)
	if list := r.ListLocal(); len(list) != 1 || list[0].Name != "good.core" {
		t.Errorf("ListLocal = %+v, want only good.core", list)
	}
}

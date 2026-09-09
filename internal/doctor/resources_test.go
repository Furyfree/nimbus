package doctor

import (
	"strings"
	"testing"

	defs "github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestResourceDoctorReportsGreeterReadinessAndUnknownState(t *testing.T) {
	enabled := true
	r := &defs.Resolved{Machine: "vm", Services: []defs.ResolvedService{{ServiceDecl: defs.ServiceDecl{Unit: "greetd.service", Enabled: &enabled}}}}
	src := &facts.FakeSource{Commands: map[string][]byte{
		facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", "greetd.service"): []byte("LoadState=loaded\nUnitFileState=enabled\nActiveState=inactive\n"),
	}}
	applied := &state.Applied{Receipts: map[string]state.Receipt{"service:greetd.service": {Verified: true, Resource: "service:greetd.service", Machine: "vm", Provider: "service"}}}
	checks := SystemResources(src, r, applied, "test")
	if len(checks) != 2 || checks[0].Status != Pass || checks[1].ID != "greeter-login" || checks[1].Status != Fail {
		t.Fatalf("%+v", checks)
	}
	src.Commands = map[string][]byte{}
	checks = SystemResources(src, r, applied, "test")
	if len(checks) != 1 || checks[0].Status != Unknown {
		t.Fatalf("%+v", checks)
	}
}

func TestResourceDoctorDistinguishesFileDriftAndOwnership(t *testing.T) {
	r := &defs.Resolved{Machine: "vm", Files: []defs.ResolvedFile{{Target: "/etc/nimbus.conf", Owner: "root", Group: "root", Mode: "0644", Content: []byte("desired\n")}}}
	src := &facts.FakeSource{Commands: map[string][]byte{
		facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc"):             []byte("directory|root|root|755|1\n"),
		facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/nimbus.conf"): []byte("regular file|root|root|644|1\n"),
	}, Dirs: map[string][]string{"/": {"etc"}, "/etc": {"nimbus.conf"}}, Files: map[string][]byte{"/etc/nimbus.conf": []byte("desired\n")}}
	applied := &state.Applied{Receipts: map[string]state.Receipt{}}
	checks := SystemResources(src, r, applied, "test")
	if checks[0].Status != Fail || !strings.Contains(checks[0].Observation, "ownership") {
		t.Fatalf("%+v", checks)
	}
	applied.Receipts["file:/etc/nimbus.conf"] = state.Receipt{Resource: "file:/etc/nimbus.conf", Provider: "system-file", Machine: "vm", Verified: true}
	checks = SystemResources(src, r, applied, "test")
	if checks[0].Status != Pass {
		t.Fatalf("%+v", checks)
	}
	src.Files["/etc/nimbus.conf"] = []byte("drift\n")
	checks = SystemResources(src, r, applied, "test")
	if checks[0].Status != Fail || !strings.Contains(checks[0].Observation, "content matches false") {
		t.Fatalf("%+v", checks)
	}
}

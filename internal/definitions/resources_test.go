package definitions

import "testing"

func TestResourcesRejectAmbiguousAndExecutableInput(t *testing.T) {
	for _, comp := range []*Component{
		{Services: []ServiceDecl{{Unit: "--help", Enabled: new(true)}}},
		{Services: []ServiceDecl{{Unit: "demo.service"}}},
		{Groups: []GroupDecl{{Name: "docker", User: "--root"}}},
		{DefaultTarget: "rescue.target"},
		{Files: []FileDecl{{Triggers: []string{"sh -c evil"}}}},
	} {
		var errs ErrorList
		validateResources(comp, "component", &errs)
		if len(errs) == 0 {
			t.Fatalf("accepted %+v", comp)
		}
	}
}
func TestRecoveryBoundaryIsExact(t *testing.T) {
	if !RecoveryTarget("/usr/local/lib/nimbus/recovery/hyprland.lua") {
		t.Fatal("missing recovery target")
	}
	for _, target := range []string{"/usr/bin/sh", "/usr/share/wayland-sessions/foreign.desktop", "/etc/recovery"} {
		if RecoveryTarget(target) {
			t.Fatalf("accepted %s", target)
		}
	}
}

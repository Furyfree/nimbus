package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOwnershipViews(t *testing.T) {
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	base := []string{"--checkout", root, "--machine", "desktop"}

	code, out, _ := run(t, append([]string{"managed"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "managed    dnf:dnf5-plugins 5.") || strings.Contains(out, "unmanaged") {
		t.Fatalf("managed: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"unmanaged"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "unmanaged  dnf:gzip 1.14-2.fc44") || strings.Contains(out, "dnf5-plugins") || strings.Contains(out, "dnf:bash") {
		t.Fatalf("unmanaged: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"packages", "installed", "z"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "managed    dnf:bzip2") || !strings.Contains(out, "unmanaged  dnf:gzip ") || strings.Contains(out, "bash") {
		t.Fatalf("packages installed z: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"packages", "installed", "--json"}, base...)...)
	var views []struct {
		State string `json:"state"`
	}
	env := struct {
		Data *[]struct {
			State string `json:"state"`
		} `json:"data"`
	}{Data: &views}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != ExitOK || len(views) == 0 {
		t.Fatalf("json installed: %d %v\n%s", code, err, out)
	}
	for _, v := range views {
		if v.State == "dependency" {
			t.Fatal("installed view must not list dependencies")
		}
	}
}

func TestWhyAndSelectionLists(t *testing.T) {
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	base := []string{"--checkout", root, "--machine", "desktop"}

	code, out, _ := run(t, append([]string{"why", "docker:docker-ce"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "package docker:docker-ce") || !strings.Contains(out, "selected by component:docker") {
		t.Fatalf("why package: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"why", "docker"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "component docker") || !strings.Contains(out, "profile:development") || !strings.Contains(out, "component:windows-vm") {
		t.Fatalf("why component: %d\n%s", code, out)
	}
	if code, _, errOut := run(t, append([]string{"why", "nothing-selects-this"}, base...)...); code != ExitFailure || !strings.Contains(errOut, "not selected") {
		t.Fatalf("why unknown: %d %q", code, errOut)
	}
	if code, _, _ := run(t, append([]string{"why"}, base...)...); code != ExitUsage {
		t.Fatalf("why without argument exited %d", code)
	}
	code, out, _ = run(t, append([]string{"profiles", "list"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "* gaming") || !strings.Contains(out, "  laptop-gaming") {
		t.Fatalf("profiles list: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"components", "list"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "* docker  <- component:windows-vm, profile:development") || !strings.Contains(out, "  laptop-power") {
		t.Fatalf("components list: %d\n%s", code, out)
	}
}

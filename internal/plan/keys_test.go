package plan

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

func TestChangedDeclaredPinRequiresKeyReconciliation(t *testing.T) {
	c, resolved := repository(t)
	src, f := readyHost(t, c)
	root := c.Definitions()
	r := root.Repositories["docker"]
	r.Key = "1111111111111111111111111111111111111111"
	root.Repositories["docker"] = r
	p, _ := Build(Inputs{Resolved: resolved, Root: root, Definitions: c.Digest(), Facts: f, Source: src})
	op := find(p, "repository:docker")
	if op == nil || op.Action != ActionRepair || !strings.Contains(op.Summary, "reconcile signing key") {
		t.Fatalf("changed pin did not schedule verification: %+v", op)
	}
	var verify, install, override bool
	for _, step := range op.Steps {
		verify = verify || strings.Contains(step.Description, "fingerprint is "+r.Key)
		install = install || len(step.Argv) > 0 && step.Argv[0] == "install"
		override = override || strings.Contains(strings.Join(step.Argv, " "), ".gpgkey=file://"+KeyPath("docker"))
	}
	if !verify || !install || !override {
		t.Fatalf("incomplete key reconciliation: %+v", op.Steps)
	}
}

func TestExistingFlatpakRemoteRequiresExactVerifiedTrust(t *testing.T) {
	c, resolved := repository(t)
	want := definitions.NormalizeFingerprint(c.Definitions().Repositories["flathub"].Key)
	for _, tc := range []struct {
		name   string
		remote facts.FlatpakRemote
	}{
		{"unknown", facts.FlatpakRemote{KeyError: "key unreadable", GPGVerify: true}},
		{"disabled", facts.FlatpakRemote{KeyFingerprints: []string{want}}},
		{"additional key", facts.FlatpakRemote{GPGVerify: true, KeyFingerprints: []string{want, strings.Repeat("1", 40)}}},
		{"different key", facts.FlatpakRemote{GPGVerify: true, KeyFingerprints: []string{strings.Repeat("1", 40)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, f := readyHost(t, c)
			remote := tc.remote
			remote.Name, remote.URL = "flathub", "https://dl.flathub.org/repo/"
			f.Flatpak.Value.Remotes = []facts.FlatpakRemote{remote}
			p, _ := Build(Inputs{Resolved: resolved, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
			if op := find(p, "flatpak-remote:flathub"); op == nil || op.Blocked == "" {
				t.Fatalf("unverified trust accepted: %+v", op)
			}
		})
	}
}

package plan

import (
	"slices"
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
	verify := slices.ContainsFunc(op.Steps, func(step Step) bool { return strings.Contains(step.Description, "fingerprint is "+r.Key) })
	install := slices.ContainsFunc(op.Steps, func(step Step) bool { return len(step.Argv) > 0 && step.Argv[0] == "install" })
	override := slices.ContainsFunc(op.Steps, func(step Step) bool {
		return strings.Contains(strings.Join(step.Argv, " "), ".gpgkey=file://"+KeyPath("docker"))
	})
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

func TestConflictingRepositoryOverridesBlockBeforeMutation(t *testing.T) {
	c, resolved := repository(t)
	for _, key := range []string{"baseurl", "metalink", "mirrorlist", "sslverify"} {
		t.Run(key, func(t *testing.T) {
			src, f := readyHost(t, c)
			for i := range f.Repositories.Value {
				r := &f.Repositories.Value[i]
				if r.ID == "nimbus-docker" {
					value := "https://other.invalid/repo"
					if key == "sslverify" {
						value = "0"
					}
					r.OverrideOptions = map[string]string{key: value}
					r.Overrides = []string{"other.repo"}
				}
			}
			p, err := Build(Inputs{Resolved: resolved, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
			if err != nil {
				t.Fatal(err)
			}
			op := find(p, "repository:docker")
			if p.Complete || op == nil || !strings.Contains(op.Blocked, key+" override") {
				t.Fatalf("conflicting override accepted: %+v", op)
			}
		})
	}
}

func TestEnabledTLSVerificationOverrideIsCompatible(t *testing.T) {
	c, _ := repository(t)
	for _, value := range []string{"1", "true", "yes"} {
		_, f := readyHost(t, c)
		for i := range f.Repositories.Value {
			if f.Repositories.Value[i].ID == "nimbus-docker" {
				f.Repositories.Value[i].OverrideOptions = map[string]string{"sslverify": value}
			}
		}
		ready, repair, blocked := CheckRepository(c.Definitions(), "docker", f.Repositories.Value)
		if !ready || repair != "" || blocked != "" {
			t.Fatalf("safe TLS override %q rejected: ready=%t repair=%s blocked=%s", value, ready, repair, blocked)
		}
	}
}

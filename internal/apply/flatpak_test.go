package apply

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestFlatpakAdoptionVerifiesAndRecordsObservedState(t *testing.T) {
	for _, tc := range []struct {
		name, version, origin, failure string
		missing                        bool
	}{
		{name: "other remote", version: "2.0", origin: "fedora"},
		{name: "no version", origin: "flathub"},
		{name: "disappeared after plan", version: "2.0", origin: "fedora", missing: true},
		{name: "unknown after plan", version: "2.0", origin: "fedora", failure: "inventory unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "org.example.App"
			const key = "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F"
			root := definitions.Root{Repositories: map[string]definitions.Repository{"flathub": {Kind: "flatpak", URL: "https://dl.flathub.org/repo/flathub.flatpakrepo", Key: key}}}
			list := nativetest.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin")
			src := &nativetest.FakeSource{
				Dirs:  map[string][]string{inspect.RepoDir: {}},
				Files: map[string][]byte{filepath.Join(inspect.FlatpakRepoPath, "config"): []byte("[remote \"flathub\"]\ngpg-verify=true\n")},
				Commands: map[string][]byte{
					nativetest.Key("dnf5", inspect.PackageQueryArgs...):                                                                 nil,
					nativetest.Key("dnf5", "--cacheonly", "check-upgrade"):                                                              nil,
					nativetest.Key("flatpak", "remotes", "--system", "--columns=name,url"):                                              []byte("flathub\thttps://dl.flathub.org/repo/\n"),
					nativetest.Key("gpg", inspect.KeyInspectArgs(filepath.Join(inspect.FlatpakRepoPath, "flathub.trustedkeys.gpg"))...): []byte("pub:::::::::\nfpr:::::::::" + key + ":\n"),
					list: []byte(id + "\t1.0\tflathub\n"),
				},
			}
			desired := &definitions.Resolved{Machine: "vm", Repositories: []string{"flathub"}, Packages: []definitions.ResolvedPackage{{Canonical: "flatpak:" + id, Prefix: definitions.PrefixFlatpak, Name: id}}}
			p, err := plan.Build(plan.Inputs{Resolved: desired, Root: root, Facts: inspect.Inspect(src, ""), Source: src})
			if err != nil || !p.Complete || len(p.Operations) != 1 || p.Operations[0].Action != plan.ActionAdopt {
				t.Fatalf("adoption plan: %+v %v", p, err)
			}
			src.Commands[list] = fmt.Appendf(nil, "%s\t%s\t%s\n", id, tc.version, tc.origin)
			if tc.missing {
				src.Commands[list] = nil
			}
			if tc.failure != "" {
				src.Failures = map[string]string{list: tc.failure}
			}
			var recorded []state.Receipt
			result := Run(p, Options{Source: src, Record: func(_ string, stage *state.Stage) error {
				recorded = append(recorded, stage.Receipts...)
				return nil
			}})
			if tc.missing || tc.failure != "" {
				want := tc.failure
				if tc.missing {
					want = "not installed"
				}
				if result.Error == "" || !strings.Contains(result.Error, want) || len(recorded) != 0 || len(result.Executed) != 0 {
					t.Fatalf("unverified adoption: %+v receipts=%+v", result, recorded)
				}
				return
			}
			if result.Error != "" || len(recorded) != 1 || len(result.Executed) != 1 {
				t.Fatalf("adoption failed: %+v receipts=%+v", result, recorded)
			}
			want := "installed " + tc.version + " from " + tc.origin
			if r := recorded[0]; r.Previous != want || r.Intended != want || !r.Verified || r.Operation != plan.ActionAdopt {
				t.Fatalf("adopted observation lost: %+v", r)
			}
		})
	}
}

func TestVerificationFailureGetsNoReceipt(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	// Install another application so verification catches the missing request.
	p := samplePlan(t)
	i := slices.IndexFunc(p.Operations, func(op plan.Operation) bool { return op.ID == "flatpak:com.spotify.Client" })
	if i < 0 {
		t.Fatal("fixture has no Flatpak installation")
	}
	p.Operations[i].Steps[0].Argv = []string{"flatpak", "install", "--system", "--noninteractive", "flathub", "org.example.Other"}
	r := Run(p, opts)
	if r.Failed != "flatpak:com.spotify.Client" || !strings.Contains(r.Error, "not installed after") {
		t.Fatalf("result = %+v", r)
	}
	a, _ := state.Read(root)
	if _, ok := a.Receipts["flatpak:com.spotify.Client"]; ok {
		t.Fatal("unverified operation got a receipt")
	}
}

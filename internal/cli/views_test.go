package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestOwnershipViews(t *testing.T) {
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	base := []string{"--checkout", root, "--machine", "desktop"}

	code, out, _ := run(t, append([]string{"managed"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "adopt      dnf:dnf5-plugins 5.") || strings.Contains(out, "unmanaged") {
		t.Fatalf("managed before any apply lists adoptable packages: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"unmanaged"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "unmanaged  dnf:gzip.x86_64 1.14-2.fc44") || strings.Contains(out, "dnf5-plugins") || !strings.Contains(out, "unmanaged  dnf:bash.x86_64") {
		t.Fatalf("unmanaged: %d\n%s", code, out)
	}
	code, out, _ = run(t, append([]string{"packages", "installed", "z"}, base...)...)
	if code != ExitOK || !strings.Contains(out, "unmanaged  dnf:bzip2.x86_64") || !strings.Contains(out, "unmanaged  dnf:gzip.x86_64 ") || strings.Contains(out, "bash") {
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

func TestDesiredFlatpakIsAdoptableFromAnyRemote(t *testing.T) {
	for _, remote := range []string{"flathub", "other-remote"} {
		t.Run(remote, func(t *testing.T) {
			root := repoRoot(t)
			src := fixtureSource(t, root)
			src.Commands[nativetest.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin")] = []byte("com.spotify.Client\t1.0\t" + remote + "\n")
			withSource(t, src)
			for _, command := range [][]string{{"managed"}, {"packages", "installed", "spotify"}} {
				args := append(command, "--checkout", root, "--machine", "desktop")
				code, out, errOut := run(t, args...)
				if code != ExitOK || !strings.Contains(out, "adopt      flatpak:com.spotify.Client 1.0 ("+remote+")") {
					t.Fatalf("%v: %d\n%s%s", command, code, out, errOut)
				}
			}
		})
	}
}

func TestInstalledFlatpaksDoNotRequireVersionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, app, want string
		present, owned  bool
	}{
		{"desired adoption", "com.spotify.Client", "adopt", true, false},
		{"desired managed", "com.spotify.Client", "managed", true, true},
		{"unselected unmanaged", "org.example.App", "unmanaged", true, false},
		{"unselected managed", "org.example.App", "managed", true, true},
		{"desired absent", "com.spotify.Client", "", false, false},
		{"owned absent", "com.spotify.Client", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repoRoot(t)
			src := fixtureSource(t, root)
			if tc.present {
				src.Commands[nativetest.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin")] = []byte(tc.app + "\t\tflathub\n")
			}
			withSource(t, src)
			if tc.owned {
				stage := &state.Stage{Schema: state.Schema, PlanDigest: "sha256:p", Receipts: []state.Receipt{{
					Schema: state.ReceiptSchema, Resource: "flatpak:" + tc.app, Provider: "flatpak",
					Operation: "adopt", PlanDigest: "sha256:p", Verified: true,
				}}}
				if err := state.Record(stateRoot, stage.PlanDigest, stage); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"packages", "installed", tc.app, "--checkout", root, "--machine", "desktop"}
			code, out, errOut := run(t, append(args, "--json")...)
			var env struct{ Data []packageView }
			if err := json.Unmarshal([]byte(out), &env); err != nil || code != ExitOK {
				t.Fatalf("installed JSON: code=%d, error=%v\n%s%s", code, err, out, errOut)
			}
			if !tc.present {
				if len(env.Data) != 0 {
					t.Fatalf("absent Flatpak listed as installed: %+v", env.Data)
				}
				return
			}
			if len(env.Data) != 1 || env.Data[0].Canonical != "flatpak:"+tc.app || env.Data[0].State != tc.want || env.Data[0].Installed != "" || env.Data[0].Repository != "flathub" {
				t.Fatalf("installed Flatpak lost presence or ownership: %+v", env.Data)
			}
			code, out, errOut = run(t, args...)
			if code != ExitOK || !strings.Contains(out, tc.want) || !strings.Contains(out, "flatpak:"+tc.app+" (flathub)") {
				t.Fatalf("installed human output: code=%d\n%s%s", code, out, errOut)
			}
		})
	}
}

func TestRPMViewsResolveReceiptOwnershipByNativeIdentity(t *testing.T) {
	multilib := []inspect.Package{
		{Name: "demo", Arch: "i686", Version: "1", Reason: "user"},
		{Name: "demo", Arch: "x86_64", Version: "1", Reason: "user"},
	}
	legacy := state.Receipt{Schema: 1, Resource: "package:dnf:demo", Provider: "dnf", Verified: true}
	native := state.Receipt{Schema: 2, Resource: "package:dnf:demo", Provider: "dnf", Verified: true, Package: "demo.i686"}
	for _, tc := range []struct {
		name     string
		packages []inspect.Package
		receipts []state.Receipt
		baseline []string
		desired  []definitions.ResolvedPackage
		want     map[string]string
	}{
		{
			name: "unselected native receipt owns only i686", packages: multilib, receipts: []state.Receipt{native},
			want: map[string]string{"dnf:demo.i686": "managed", "dnf:demo.x86_64": "unmanaged"},
		},
		{
			name: "native receipt takes precedence over baseline", packages: multilib, receipts: []state.Receipt{native}, baseline: []string{"demo.i686", "demo.x86_64"},
			want: map[string]string{"dnf:demo.i686": "managed", "dnf:demo.x86_64": "pre-existing"},
		},
		{
			name: "provide receipt owns its recorded native package", packages: multilib,
			receipts: []state.Receipt{{Schema: 2, Resource: "package:terra:virtual-tool", Provider: "dnf", Verified: true, Package: "demo.x86_64"}},
			want:     map[string]string{"dnf:demo.i686": "unmanaged", "dnf:demo.x86_64": "managed"},
		},
		{
			name: "legacy receipt with one architecture", packages: multilib[:1], receipts: []state.Receipt{legacy},
			want: map[string]string{"dnf:demo.i686": "managed"},
		},
		{
			name: "legacy receipt with explicit architecture", packages: multilib,
			receipts: []state.Receipt{{Schema: 1, Resource: "package:dnf:demo.i686", Provider: "dnf", Verified: true}},
			want:     map[string]string{"dnf:demo.i686": "managed", "dnf:demo.x86_64": "unmanaged"},
		},
		{
			name: "legacy multilib ambiguity blocks both architectures", packages: multilib, receipts: []state.Receipt{legacy},
			want: map[string]string{"dnf:demo.i686": "blocked", "dnf:demo.x86_64": "blocked"},
		},
		{
			name: "desired RPM cannot hide legacy ambiguity", packages: multilib, receipts: []state.Receipt{legacy},
			desired: []definitions.ResolvedPackage{{Name: "demo", Prefix: "dnf", Canonical: "dnf:demo"}},
			want:    map[string]string{"dnf:demo.i686": "blocked", "dnf:demo": "blocked"},
		},
		{
			name: "direct selected ambiguity survives another native receipt", packages: multilib,
			receipts: []state.Receipt{legacy, {Schema: 2, Resource: "package:terra:demo", Provider: "dnf", Verified: true, Package: "demo.x86_64"}},
			desired:  []definitions.ResolvedPackage{{Name: "demo", Prefix: "dnf", Canonical: "dnf:demo"}},
			want:     map[string]string{"dnf:demo.i686": "blocked", "dnf:demo": "blocked"},
		},
		{
			name: "native ownership survives an earlier ambiguous receipt", packages: multilib,
			receipts: []state.Receipt{legacy, {Schema: 2, Resource: "package:terra:demo", Provider: "dnf", Verified: true, Package: "demo.x86_64"}},
			want:     map[string]string{"dnf:demo.i686": "blocked", "dnf:demo.x86_64": "managed"},
		},
		{
			name: "native ownership survives a later ambiguous receipt", packages: multilib,
			receipts: []state.Receipt{native, {Schema: 1, Resource: "package:terra:demo", Provider: "dnf", Verified: true}},
			want:     map[string]string{"dnf:demo.i686": "managed", "dnf:demo.x86_64": "blocked"},
		},
		{
			name: "legacy provide prose does not prove native ownership", packages: multilib,
			receipts: []state.Receipt{{Schema: 1, Resource: "package:dnf:virtual-tool", Provider: "dnf", Verified: true, Intended: "installed demo 1"}},
			want:     map[string]string{"dnf:demo.i686": "unmanaged", "dnf:demo.x86_64": "unmanaged"},
		},
		{
			name: "unverified receipt does not prove ownership", packages: multilib,
			receipts: []state.Receipt{{Schema: 2, Resource: "package:dnf:demo", Provider: "dnf", Package: "demo.i686"}},
			want:     map[string]string{"dnf:demo.i686": "unmanaged", "dnf:demo.x86_64": "unmanaged"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &selected{Checkout: &definitions.Checkout{}, Resolved: &definitions.Resolved{Packages: tc.desired}}
			f := &inspect.Facts{Packages: inspect.Section[[]inspect.Package]{Value: tc.packages}}
			applied := &state.Applied{Receipts: map[string]state.Receipt{}, Baseline: &state.Baseline{Packages: tc.baseline}}
			for _, receipt := range tc.receipts {
				applied.Receipts[receipt.Resource] = receipt
			}
			views := packageViews(s, f, applied)
			if len(views) != len(tc.want) {
				t.Fatalf("views = %+v, want states %v", views, tc.want)
			}
			for _, view := range views {
				if view.State != tc.want[view.Canonical] || view.Reason != "user" {
					t.Fatalf("view = %+v, want state %q and preserved native reason", view, tc.want[view.Canonical])
				}
				if view.State == "blocked" {
					if !strings.Contains(view.Blocked, "does not identify which installed architecture Nimbus owns") {
						t.Fatalf("ambiguous ownership has no explanation: %+v", view)
					}
					if out := string(renderPackageViews([]packageView{view})); !strings.Contains(out, view.Blocked) {
						t.Fatalf("human view omitted ownership explanation: %s", out)
					}
					data, err := json.Marshal(view)
					if err != nil || !strings.Contains(string(data), `"blocked":"`+view.Blocked+`"`) {
						t.Fatalf("JSON view omitted ownership explanation: %s, %v", data, err)
					}
				} else if view.Blocked != "" {
					t.Fatalf("resolved ownership retains a blocked reason: %+v", view)
				}
			}
		})
	}
}

func TestRPMViewsAgreeWithPlanForExplicitArchitectureMigration(t *testing.T) {
	for _, names := range [][]string{{"demo.i686", "demo.x86_64"}, {"demo.i686"}} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			s := &selected{Checkout: &definitions.Checkout{}, Resolved: &definitions.Resolved{Machine: "vm"}}
			for _, name := range names {
				s.Resolved.Packages = append(s.Resolved.Packages, definitions.ResolvedPackage{Name: name, Prefix: "dnf", Canonical: "dnf:" + name})
			}
			f := &inspect.Facts{Packages: inspect.Section[[]inspect.Package]{Value: []inspect.Package{
				{Name: "demo", Arch: "i686", Version: "1", Release: "1", FromRepo: "fedora", Reason: "user"},
				{Name: "demo", Arch: "x86_64", Version: "1", Release: "1", FromRepo: "fedora", Reason: "user"},
			}}}
			const legacyID = "package:dnf:demo"
			applied := &state.Applied{Receipts: map[string]state.Receipt{
				legacyID: {Schema: 1, Resource: legacyID, Provider: "dnf", Verified: true},
			}}
			src := &nativetest.FakeSource{Commands: map[string][]byte{"dnf5 --cacheonly check-upgrade": []byte("Repositories loaded.\n")}}
			p, err := plan.Build(plan.Inputs{Resolved: s.Resolved, Root: s.Checkout.Definitions(), Facts: f, Applied: applied, Source: src})
			if err != nil {
				t.Fatal(err)
			}
			views := packageViews(s, f, applied)
			for _, name := range names {
				i := slices.IndexFunc(p.Operations, func(op plan.Operation) bool { return op.ID == "package:dnf:"+name })
				if i < 0 || p.Operations[i].Action != plan.ActionAdopt || p.Operations[i].Blocked != "" {
					t.Fatalf("explicit architecture is not adoptable in plan: %+v", p.Operations)
				}
				j := slices.IndexFunc(views, func(view packageView) bool { return view.Canonical == "dnf:"+name })
				if j < 0 || views[j].State != "adopt" || views[j].Blocked != "" {
					t.Fatalf("ownership view disagrees with adoption plan: %+v", views)
				}
			}
			i := slices.IndexFunc(p.Operations, func(op plan.Operation) bool { return op.ID == legacyID })
			if i < 0 {
				t.Fatalf("plan omitted legacy receipt resolution: %+v", p.Operations)
			}
			if len(names) == 2 {
				if !p.Complete || p.Operations[i].Action != plan.ActionRetire || p.Operations[i].Blocked != "" {
					t.Fatalf("complete architecture selection did not retire the old receipt: %+v", p)
				}
			} else {
				j := slices.IndexFunc(views, func(view packageView) bool { return view.Canonical == "dnf:demo.x86_64" })
				if p.Complete || p.Operations[i].Blocked == "" || j < 0 || views[j].State != "blocked" || views[j].Blocked == "" {
					t.Fatalf("partial selection lost unresolved ownership: plan=%+v views=%+v", p, views)
				}
			}
		})
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

func TestViewsReadReceiptsAndBaseline(t *testing.T) {
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	saved := stateRoot
	stateRoot = t.TempDir()
	t.Cleanup(func() { stateRoot = saved })
	st := &state.Stage{Schema: state.Schema, PlanDigest: "sha256:p",
		Baseline: &state.Baseline{Packages: []string{"gzip"}},
		Receipts: []state.Receipt{{Schema: state.ReceiptSchema, Resource: "package:dnf:dnf5-plugins", Provider: "dnf", Operation: "adopt", PlanDigest: "sha256:p", Verified: true}}}
	if err := state.Record(stateRoot, "sha256:p", st); err != nil {
		t.Fatal(err)
	}
	base := []string{"--checkout", root, "--machine", "desktop"}
	if _, out, _ := run(t, append([]string{"managed"}, base...)...); !strings.Contains(out, "managed    dnf:dnf5-plugins") {
		t.Fatalf("receipt not reflected:\n%s", out)
	}
	if _, out, _ := run(t, append([]string{"unmanaged"}, base...)...); strings.Contains(out, "gzip") {
		t.Fatalf("baseline package listed without --all:\n%s", out)
	}
	if _, out, _ := run(t, append([]string{"unmanaged", "--all"}, base...)...); !strings.Contains(out, "pre-existing dnf:gzip.x86_64") {
		t.Fatalf("--all lacks the pre-existing marker:\n%s", out)
	}
}

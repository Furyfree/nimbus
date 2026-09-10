package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestRepositoryStateIsVerifiedNotAssumed(t *testing.T) {
	c, r := repository(t)
	cases := []struct {
		name string
		repo inspect.Repository
		want func(*Operation) bool
	}{
		{"correct nimbus file is ready", ownedRepoFile(c, "docker", nil),
			func(op *Operation) bool { return op == nil }},
		{"gpgcheck off is repaired", ownedRepoFile(c, "docker", func(h *inspect.Repository) { h.GPGCheck, h.Options["gpgcheck"] = "0", "0" }),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "gpgcheck=0") && op.Blocked == ""
			}},
		{"wrong baseurl is repaired", ownedRepoFile(c, "docker", func(h *inspect.Repository) {
			h.BaseURL, h.Options["baseurl"] = "https://example.invalid/docker", "https://example.invalid/docker"
		}),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "baseurl")
			}},
		// The first VM apply wrote repo_gpgcheck=1, which DNF could not
		// satisfy; a key Nimbus no longer writes must be repaired away.
		{"a stray key is repaired", ownedRepoFile(c, "docker", func(h *inspect.Repository) { h.Options["repo_gpgcheck"] = "1" }),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "repo_gpgcheck=1, which Nimbus does not write") && op.Steps[0].Argv[2] == "addrepo" && slices.Contains(op.Steps[0].Argv, "--overwrite")
			}},
		{"foreign file with our id blocks", ownedRepoFile(c, "docker", func(h *inspect.Repository) { h.File = "docker-ce.repo" }),
			func(op *Operation) bool { return op != nil && strings.Contains(op.Blocked, "docker-ce.repo") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, f := host(t)
			withoutTerraFile(f)
			f.Repositories.Value = append(f.Repositories.Value, tc.repo)
			p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
			if err != nil {
				t.Fatal(err)
			}
			if op := find(p, "repository:docker"); !tc.want(op) {
				t.Fatalf("repository:docker = %+v", op)
			}
		})
	}
}

func TestRepairRestoresSignatureChecking(t *testing.T) {
	c := repositoryOnly(t)
	steps := PrioritySteps("rpmfusion-free", c.Definitions().Repositories["rpmfusion-free"])
	argv := strings.Join(steps[0].Argv, " ")
	if !strings.Contains(argv, "rpmfusion-free.gpgcheck=1") || !strings.Contains(argv, "rpmfusion-free.priority=120") || !strings.Contains(argv, "rpmfusion-free-updates.gpgcheck=1") {
		t.Fatalf("repair argv = %s", argv)
	}
}

func TestADuplicateMakerRepositoryIsDisabledByOverride(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// 1Password's package writes its own repository file at install, with
	// the same baseurl under its own ID and DNF's default priority.
	f.Repositories.Value = append(f.Repositories.Value, inspect.Repository{ID: "1password", File: "1password.repo", Enabled: true, GPGCheck: "1", BaseURL: c.Definitions().Repositories["onepassword"].BaseURL, Options: map[string]string{"baseurl": c.Definitions().Repositories["onepassword"].BaseURL}})
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "repository:onepassword")
	if op == nil || op.Action != ActionRepair || !strings.Contains(op.Summary, "1password from 1password.repo also serves the declared baseurl") {
		t.Fatalf("duplicate = %+v", op)
	}
	if len(op.Steps) != 1 || strings.Join(op.Steps[0].Argv, " ") != "dnf5 config-manager setopt 1password.enabled=0" || !op.Steps[0].Privileged {
		t.Fatalf("the owned file is fine, so only the override step belongs here: %+v", op.Steps)
	}
	// Disabled through the override, the duplicate no longer counts.
	f.Repositories.Value[len(f.Repositories.Value)-1].Enabled = false
	if ready, repair, blocked := CheckRepository(c.Definitions(), "onepassword", f.Repositories.Value); !ready || repair != "" || blocked != "" {
		t.Fatalf("after the override: ready %v repair %q blocked %q", ready, repair, blocked)
	}
}

func TestReleasePackagesBelongToTheirRepository(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	f.Packages.Value = append(f.Packages.Value, inspect.Package{Name: "rpmfusion-free-release", Version: "44", Release: "3", Arch: "noarch", FromRepo: "@commandline", Reason: "user"})
	a := applied()
	a.Baseline = &state.Baseline{Schema: state.BaselineSchema, Packages: []string{"bash"}}
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: a, Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(p.Prune, func(pr Prune) bool { return pr.Name == "rpmfusion-free-release.noarch" }) {
		t.Fatal("the release package Nimbus installed is a prune candidate")
	}
	if !IsReleasePackage(c.Definitions(), "rpmfusion-free-release") || IsReleasePackage(c.Definitions(), "rpmfusion") {
		t.Fatal("release package recognition is wrong")
	}
	if got := ReleasePackageName("https://example.invalid/x/foo-bar-release-1.2-3.fc44.noarch.rpm"); got != "foo-bar-release" {
		t.Fatalf("name = %q", got)
	}
}

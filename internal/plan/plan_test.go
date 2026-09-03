package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

// repository loads and resolves the tracked desktop machine.
func repository(t *testing.T) (*definitions.Checkout, *definitions.Resolved) {
	t.Helper()
	c, err := definitions.Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if errs := definitions.Validate(c); len(errs) > 0 {
		t.Fatal(errs)
	}
	r, errs := definitions.Resolve(c, "desktop")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return c, r
}

// host builds facts from the recorded Fedora 44 fixture: Fedora, RPM Fusion,
// and Terra enabled, no Nimbus-written repositories, no Flatpak remote.
func host(t *testing.T) (*facts.FakeSource, *facts.Facts) {
	t.Helper()
	dir := filepath.Join("..", "facts", "testdata", "fedora44")
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	src := &facts.FakeSource{
		Commands: map[string][]byte{
			facts.Key("uname", "-m"):                                                                  []byte("x86_64\n"),
			facts.Key("dnf5", facts.PackageQueryArgs...):                                              read("repoquery-installed.txt"),
			facts.Key("flatpak", "remotes", "--system", "--columns=name,url"):                         []byte(""),
			facts.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin"): []byte(""),
			facts.Key("systemctl", "is-active", "firewalld"):                                          []byte("active\n"),
			facts.Key("dnf5", "--cacheonly", "check-upgrade"):                                         []byte("Repositories loaded.\nUpgrades\nlibrepo.x86_64 1.21.0-1.fc44 updates\n"),
		},
		Failures: map[string]string{facts.Key("dnf5", "--cacheonly", "check-upgrade"): "exit status 100"},
		Files:    map[string][]byte{facts.OSReleasePath: read("os-release"), facts.SELinuxPath: []byte("1\n")},
		Dirs:     map[string][]string{facts.RepoDir: {}},
		Paths:    map[string]string{},
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "yum.repos.d"))
	for _, e := range entries {
		src.Dirs[facts.RepoDir] = append(src.Dirs[facts.RepoDir], e.Name())
		src.Files[filepath.Join(facts.RepoDir, e.Name())] = read(filepath.Join("yum.repos.d", e.Name()))
	}
	for _, name := range facts.RequiredCommands {
		src.Paths[name] = "/usr/bin/" + name
	}
	return src, facts.Inspect(src, "")
}

// previewText renders a DNF5 preview table the way the fixture shows it.
func previewText(rows []TxPackage) []byte {
	var b strings.Builder
	b.WriteString("Updating and loading repositories:\nRepositories loaded.\nPackage Arch Version Repository Size\n")
	section := ""
	for _, r := range rows {
		if r.Section != section {
			section = r.Section
			fmt.Fprintf(&b, "%s:\n", strings.ToUpper(section[:1])+section[1:])
		}
		fmt.Fprintf(&b, " %s %s %s %s 1.0 KiB\n", r.Name, r.Arch, r.EVR, r.Repository)
	}
	b.WriteString("\nTransaction Summary:\n Installing: 1 package\n\nOperation aborted by the user.\n")
	return []byte(b.String())
}

// installArgs returns the exact preview argv the planner will run for the
// packages it can install now, so the fixture can answer it.
func installArgs(p *Plan) []string {
	for _, op := range p.Operations {
		if op.ID == "packages:install" {
			argv := op.Steps[0].Argv[2:] // drop dnf5 -y
			return append([]string{"--assumeno", "--cacheonly"}, argv...)
		}
	}
	return nil
}

func installNames(args []string) []string {
	var names []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && a != "install" {
			names = append(names, a)
		}
	}
	return names
}

func answerInstall(t *testing.T, src *facts.FakeSource, in Inputs, mutate func([]TxPackage) []TxPackage) *Plan {
	t.Helper()
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	args := installArgs(first)
	if args == nil {
		t.Fatal("no install transaction planned")
	}
	var rows []TxPackage
	for _, name := range installNames(args) {
		repo := "fedora"
		for _, p := range in.Resolved.Packages {
			if p.Name == name && p.Prefix != definitions.PrefixDNF {
				repo = DNFRepoIDs(p.Prefix, in.Root.Repositories[p.Prefix])[0]
			}
		}
		rows = append(rows, TxPackage{Name: name, Arch: "x86_64", EVR: "0:1-1.fc44", Repository: repo, Section: "installing"})
	}
	if mutate != nil {
		rows = mutate(rows)
	}
	src.Commands[facts.Key("dnf5", args...)] = previewText(rows)
	src.Failures[facts.Key("dnf5", args...)] = "exit status 1"
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func find(p *Plan, id string) *Operation {
	for i := range p.Operations {
		if p.Operations[i].ID == id {
			return &p.Operations[i]
		}
	}
	return nil
}

func TestPlanOnFreshFedora(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	p := answerInstall(t, src, in, nil)

	// Repositories: Terra and RPM Fusion are enabled on the fixture host;
	// the Nimbus-written ones, the COPR, and Flathub are not.
	for _, id := range []string{"repository:docker", "repository:onepassword", "repository:brave", "repository:vscodium", "repository:chatgpt", "repository:hyprland-copr", "flatpak-remote:flathub"} {
		if find(p, id) == nil {
			t.Errorf("missing operation %s", id)
		}
	}
	// RPM Fusion is enabled by its own release packages on this host but
	// without the declared priority, so it is repaired, not re-enabled.
	for _, id := range []string{"repository:rpmfusion-free", "repository:rpmfusion-nonfree"} {
		if op := find(p, id); op == nil || op.Action != ActionRepair || !strings.Contains(op.Summary, "priority") || op.Blocked != "" {
			t.Errorf("%s = %+v", id, op)
		}
	}
	// Terra is enabled through Terra's own file on this host, which Nimbus
	// does not own, so it is neither duplicated nor taken over.
	if op := find(p, "repository:terra"); op == nil || !strings.Contains(op.Blocked, "terra.repo, which Nimbus does not own") {
		t.Fatalf("terra = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || !strings.Contains(op.Blocked, "repository terra cannot be used") {
		t.Fatalf("ghostty = %+v", op)
	}
	docker := find(p, "repository:docker")
	if !strings.Contains(docker.Steps[3].Description, "nimbus-docker.repo") || !docker.Steps[2].Privileged || !strings.Contains(docker.Steps[0].Description, "060A61C51B558A7F742B77AAC52FEB6B621E9F35") {
		t.Fatalf("docker steps = %+v", docker.Steps)
	}
	rf := repositorySteps("rpmfusion-free", c.Definitions().Repositories["rpmfusion-free"])
	if !strings.Contains(rf[0].Description, "sha256") || rf[1].Argv[0] != "rpm2archive" || rf[4].Argv[1] != "install" {
		t.Fatalf("release package steps = %+v", rf)
	}

	// Packages: bash is installed and adopted; a Docker package waits for
	// its repository; everything installable is one transaction.
	if op := find(p, "package:dnf:dnf5-plugins"); op == nil || op.Action != ActionAdopt || !strings.Contains(op.Summary, "already installed") {
		t.Fatalf("dnf5-plugins = %+v", op)
	}
	// Docker's packages wait for the repository operation in this same plan
	// instead of blocking it, so a fresh host gets one applicable plan.
	if op := find(p, "package:docker:docker-ce"); op == nil || op.Blocked != "" || op.After != "repository:docker" {
		t.Fatalf("docker-ce = %+v", op)
	}
	inst := find(p, "packages:install")
	if inst.Blocked != "" || inst.Transaction == nil || !contains(inst.Steps[0].Argv, "--allowerasing") || !strings.Contains(inst.Summary, "packages through one DNF transaction") {
		t.Fatalf("install = %+v", inst)
	}
	if !strings.HasPrefix(strings.Join(inst.Steps[0].Argv, " "), "dnf5 -y install --allowerasing ") {
		t.Fatalf("argv = %v", inst.Steps[0].Argv)
	}
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || op.After != "flatpak-remote:flathub" || op.Blocked != "" {
		t.Fatalf("spotify = %+v", op)
	}
	if find(p, "packages:remove") != nil {
		t.Fatal("nothing declared as removed is installed, yet a removal was planned")
	}
	if p.Complete {
		t.Fatal("the foreign Terra file blocks, so the plan must be incomplete")
	}
	if p.Definitions != c.Digest() || !strings.HasPrefix(p.Definitions, "sha256:") {
		t.Fatalf("plan definitions = %s", p.Definitions)
	}
	if len(p.Updates.Available) != 1 || p.Updates.Available[0].Name != "librepo" {
		t.Fatalf("updates = %+v", p.Updates)
	}
	names := map[string]bool{}
	for _, pr := range p.Prune {
		names[pr.Name] = true
	}
	// A desired package is never a candidate, and the Fedora base is desired
	// through fedora-base; a user-installed package nothing selects is.
	if names["dnf5-plugins"] || names["bash"] || names["bzip2"] || !names["gzip"] {
		t.Fatalf("prune = %+v", p.Prune)
	}
	// Repositories and remotes come before every package operation.
	lastRepo, firstPkg := -1, len(p.Operations)
	for i, op := range p.Operations {
		switch op.Kind {
		case KindRepository, KindFlatpakRemote:
			lastRepo = i
		default:
			if i < firstPkg {
				firstPkg = i
			}
		}
	}
	if lastRepo > firstPkg {
		t.Fatalf("repository operation after a package operation: %d > %d", lastRepo, firstPkg)
	}
	if !sort.SliceIsSorted(p.Prune, func(i, j int) bool { return p.Prune[i].Name < p.Prune[j].Name }) {
		t.Fatal("prune candidates are not sorted")
	}
}

func TestPlanRefusesTransactionsBeyondTheDefinitions(t *testing.T) {
	c, r := repository(t)
	cases := []struct {
		name   string
		mutate func([]TxPackage) []TxPackage
		want   string
	}{
		{"undeclared install", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "surprise", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing"})
		}, "would install surprise"},
		{"undeclared removal", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "bzip2", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"})
		}, "would remove bzip2"},
		{"wrong repository", func(rows []TxPackage) []TxPackage {
			rows[0].Repository = "terra"
			return rows
		}, "would come from repository terra"},
		{"upgrade smuggled in", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "bash", Arch: "x86_64", EVR: "0:6-1", Repository: "updates", Section: "upgrading"})
		}, "run nimbus upgrade first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, f := host(t)
			p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, tc.mutate)
			op := find(p, "packages:install")
			if op.Blocked == "" || !strings.Contains(op.Blocked, tc.want) {
				t.Fatalf("blocked = %q", op.Blocked)
			}
		})
	}
	t.Run("dependencies are allowed", func(t *testing.T) {
		src, f := host(t)
		p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "libfoo", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing dependencies"})
		})
		if op := find(p, "packages:install"); op.Blocked != "" {
			t.Fatalf("dependency refused: %s", op.Blocked)
		}
	})
	t.Run("preview failure blocks with the reason", func(t *testing.T) {
		src, f := host(t)
		p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
		if err != nil {
			t.Fatal(err)
		}
		if op := find(p, "packages:install"); op.Blocked == "" || !strings.Contains(op.Blocked, "not recorded") {
			t.Fatalf("unrecorded preview = %+v", op)
		}
	})
}

func TestPlanRemovesDeclaredPackages(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	// ffmpeg-free is installed on this host, so media-codecs must swap it.
	f.Packages.Value = append(f.Packages.Value, facts.Package{Name: "ffmpeg-free", Epoch: "0", Version: "8.0.1", Release: "6.fc44", Arch: "x86_64", FromRepo: "fedora", Reason: "user"})
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	key := facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "ffmpeg-free")
	src.Commands[key] = previewText([]TxPackage{{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"}})
	src.Failures[key] = "exit status 1"
	p := answerInstall(t, src, in, nil)
	op := find(p, "packages:remove")
	if op == nil || op.Blocked != "" || op.Risk != RiskMedium || strings.Join(op.Steps[0].Argv, " ") != "dnf5 -y remove ffmpeg-free" {
		t.Fatalf("remove = %+v", op)
	}
	if find(p, "package:dnf:ffmpeg-free") != nil {
		t.Fatal("a removed package must not be adopted")
	}
	src.Commands[key] = previewText([]TxPackage{
		{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"},
		{Name: "libavcodec-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing unused dependencies"},
	})
	p, _ = Build(in)
	if op := find(p, "packages:remove"); op == nil || op.Blocked == "" || !strings.Contains(op.Blocked, "libavcodec-free") {
		t.Fatalf("extra removal accepted: %+v", op)
	}
	// When the install transaction already erases it, no second removal.
	erased := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows, TxPackage{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"})
	})
	if find(erased, "packages:remove") != nil || find(erased, "packages:install").Blocked != "" {
		t.Fatalf("erased removal planned twice: %+v", find(erased, "packages:remove"))
	}
}

func TestPlanDigestCoversOnlyTheApplySection(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	a := answerInstall(t, src, in, nil)
	b, _ := Build(in)
	if a.Digest != b.Digest || !strings.HasPrefix(a.Digest, "sha256:") {
		t.Fatalf("digest unstable: %s %s", a.Digest, b.Digest)
	}
	src.Commands[facts.Key("dnf5", "--cacheonly", "check-upgrade")] = []byte("Repositories loaded.\n")
	delete(src.Failures, facts.Key("dnf5", "--cacheonly", "check-upgrade"))
	c2, _ := Build(in)
	if c2.Digest != a.Digest || len(c2.Updates.Available) != 0 {
		t.Fatal("update information must not change the plan digest")
	}
	f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/"}}
	d, _ := Build(in)
	if d.Digest == a.Digest || find(d, "flatpak-remote:flathub") != nil || find(d, "flatpak:com.spotify.Client").Blocked != "" {
		t.Fatal("a present remote must change the operations and the digest")
	}
}

func TestUpdatesUnavailableIsReportedNotGuessed(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	key := facts.Key("dnf5", "--cacheonly", "check-upgrade")
	delete(src.Commands, key)
	src.Failures[key] = "Cache-only enabled but no cache for 'fedora'"
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	if p.Updates.Unavailable == "" || !strings.Contains(p.Updates.Unavailable, "nimbus refresh") {
		t.Fatalf("updates = %+v", p.Updates)
	}
	f.Packages = facts.Section[[]facts.Package]{Error: "dnf5 broken"}
	if _, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}); err == nil {
		t.Fatal("planning without installed packages must fail")
	}
}

func withoutTerraFile(f *facts.Facts) {
	var kept []facts.Repository
	for _, r := range f.Repositories.Value {
		if r.File != "terra.repo" {
			kept = append(kept, r)
		}
	}
	f.Repositories.Value = kept
}

func TestPlanIsCompleteOnAFreshHostWithoutForeignFiles(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, nil)
	for _, op := range p.Operations {
		if op.Blocked != "" {
			t.Fatalf("blocked on a fresh host: %s: %s", op.ID, op.Blocked)
		}
	}
	if !p.Complete {
		t.Fatal("a fresh host with pending operations must still produce a complete plan")
	}
	if op := find(p, "package:terra:ghostty"); op == nil || op.After != "repository:terra" {
		t.Fatalf("ghostty = %+v", op)
	}
}

func TestRepositoryStateIsVerifiedNotAssumed(t *testing.T) {
	c, r := repository(t)
	cases := []struct {
		name string
		repo facts.Repository
		want func(*Operation) bool
	}{
		{"correct nimbus file is ready", facts.Repository{ID: "nimbus-docker", File: "nimbus-docker.repo", Enabled: true, GPGCheck: "1", Priority: "100", BaseURL: c.Definitions().Repositories["docker"].BaseURL},
			func(op *Operation) bool { return op == nil }},
		{"gpgcheck off is repaired", facts.Repository{ID: "nimbus-docker", File: "nimbus-docker.repo", Enabled: true, GPGCheck: "0", Priority: "100", BaseURL: c.Definitions().Repositories["docker"].BaseURL},
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "gpgcheck=0") && op.Blocked == ""
			}},
		{"wrong baseurl is repaired", facts.Repository{ID: "nimbus-docker", File: "nimbus-docker.repo", Enabled: true, GPGCheck: "1", Priority: "100", BaseURL: "https://example.invalid/docker"},
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "baseurl")
			}},
		{"foreign file with our id blocks", facts.Repository{ID: "nimbus-docker", File: "docker-ce.repo", Enabled: true, GPGCheck: "1", Priority: "100", BaseURL: c.Definitions().Repositories["docker"].BaseURL},
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

func TestFlatpakRemoteMustMatchTheDeclaredURL(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: "flathub", URL: "https://example.invalid/not-flathub/"}}
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "flatpak-remote:flathub"); op == nil || !strings.Contains(op.Blocked, "points to https://example.invalid/not-flathub/") {
		t.Fatalf("mismatched remote = %+v", op)
	}
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || !strings.Contains(op.Blocked, "cannot be used") {
		t.Fatalf("spotify = %+v", op)
	}
	f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/"}}
	f.Flatpak.Value.Apps = []facts.FlatpakApp{{ID: "com.spotify.Client", Version: "1", Origin: "fedora"}}
	p, _ = Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || op.Action != ActionAdopt || !strings.Contains(op.Blocked, "remote fedora, not flathub") {
		t.Fatalf("foreign origin adopted: %+v", op)
	}
}

func TestAdoptionRefusesTheWrongSource(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	f.Packages.Value = append(f.Packages.Value,
		facts.Package{Name: "ripgrep", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		facts.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		facts.Package{Name: "zsh", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "copr:copr.fedorainfracloud.org:someone:zsh", Reason: "user"},
		facts.Package{Name: "git", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "anaconda", Reason: "user"},
	)
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "package:dnf:ripgrep"); op == nil || !strings.Contains(op.Blocked, "installed from repository terra (terra)") {
		t.Fatalf("ripgrep from terra = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || !strings.Contains(op.Blocked, "not from nimbus-terra") {
		t.Fatalf("ghostty from the maker's terra = %+v", op)
	}
	if op := find(p, "package:dnf:zsh"); op == nil || !strings.Contains(op.Blocked, "which the definitions do not declare") {
		t.Fatalf("zsh from an undeclared copr = %+v", op)
	}
	if op := find(p, "package:dnf:git"); op == nil || op.Blocked != "" || op.Action != ActionAdopt {
		t.Fatalf("git from the installer must adopt: %+v", op)
	}
	if op := find(p, "package:dnf:dnf5-plugins"); op == nil || op.Blocked != "" {
		t.Fatalf("a build-hash source must adopt: %+v", op)
	}
}

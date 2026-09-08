package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
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
	// These package-provider tests isolate resources covered by resources_test.go.
	r.Files = nil
	r.Services = nil
	r.Groups = nil
	r.DefaultTarget = ""
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
		if r.Section == SectionReplaced {
			// DNF prints the replaced version indented below the new one.
			fmt.Fprintf(&b, "   replacing %s %s %s %s 1.0 KiB\n", r.Name, r.Arch, r.EVR, r.Repository)
			continue
		}
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
			args := []string{"--assumeno", "--cacheonly"}
			argv := op.Steps[0].Argv[2:] // drop dnf5 -y
			for i := 0; i < len(argv); i++ {
				if argv[i] == "--store" {
					i++ // the stage path is apply's, not the preview's
					continue
				}
				args = append(args, argv[i])
			}
			return args
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
		native, arch := facts.SplitPackageRequest(name)
		if arch == "" {
			arch = "x86_64"
		}
		rows = append(rows, TxPackage{Name: native, Arch: arch, EVR: "0:1-1.fc44", Repository: repo, Section: "installing"})
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
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}

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
	if p.Complete {
		t.Fatal("the foreign Terra file blocks, so the plan must be incomplete")
	}
	docker := find(p, "repository:docker")
	if !strings.Contains(docker.Steps[3].Description, "nimbus-docker.repo") || !docker.Steps[2].Privileged || !strings.Contains(docker.Steps[0].Description, "060A61C51B558A7F742B77AAC52FEB6B621E9F35") {
		t.Fatalf("docker steps = %+v", docker.Steps)
	}
	rf := repositorySteps("rpmfusion-free", c.Definitions().Repositories["rpmfusion-free"])
	if !strings.Contains(rf[0].Description, "sha256") || rf[1].Argv[0] != "rpm2archive" || rf[5].Argv[1] != "install" {
		t.Fatalf("release package steps = %+v", rf)
	}

	// Packages: bash is installed and adopted; a Docker package waits for
	// its repository; the transaction of everything else waits for every
	// repository change of this round, since the download after them would
	// resolve against other metadata than a preview before them.
	if op := find(p, "package:dnf:dnf5-plugins"); op == nil || op.Action != ActionAdopt || !strings.Contains(op.Summary, "already installed") {
		t.Fatalf("dnf5-plugins = %+v", op)
	}
	if op := find(p, "package:docker:docker-ce"); op == nil || op.Blocked != "" || op.After != "repository:docker" {
		t.Fatalf("docker-ce = %+v", op)
	}
	if inst := find(p, "packages:install"); inst == nil || inst.Transaction != nil || !strings.Contains(inst.After, "repository:docker") || !strings.Contains(inst.After, "repository:rpmfusion-free") {
		t.Fatalf("install in a round with repository changes = %+v", inst)
	}

	// The next round, with every repository in place, previews the one
	// transaction and everything installable is in it.
	src, f = readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	p = answerInstall(t, src, in, nil)
	inst := find(p, "packages:install")
	if inst.Blocked != "" || inst.After != "" || inst.Transaction == nil || !contains(inst.Steps[0].Argv, "--allowerasing") || !contains(inst.Steps[0].Argv, "docker-ce") || !strings.Contains(inst.Summary, "packages through one DNF transaction") {
		t.Fatalf("install = %+v", inst)
	}
	if len(inst.Steps) != 1 || !strings.HasPrefix(strings.Join(inst.Steps[0].Argv, " "), "dnf5 -y install --allowerasing ") || !inst.Steps[0].Privileged {
		t.Fatalf("argv = %v", inst.Steps)
	}
	// The remote is added in this round and the application's command is
	// exact without a preview, so it runs in the same round.
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || op.After != "" || op.Blocked != "" || find(p, "flatpak-remote:flathub") == nil {
		t.Fatalf("spotify = %+v", op)
	}
	if find(p, "packages:remove") != nil {
		t.Fatal("nothing declared as removed is installed, yet a removal was planned")
	}
	if !p.Complete {
		t.Fatal("with every repository in place the plan must be complete")
	}
	if p.Definitions != c.Digest() || !strings.HasPrefix(p.Definitions, "sha256:") {
		t.Fatalf("plan definitions = %s", p.Definitions)
	}
	if len(p.Updates.Available) != 1 || p.Updates.Available[0].Name != "librepo" {
		t.Fatalf("updates = %+v", p.Updates)
	}
	// Before the first sync there is no baseline, so nothing can be told
	// apart from the base install: no candidates, and the plan says why.
	if len(p.Prune) != 0 || p.PruneUnavailable != "" {
		t.Fatalf("prune before a baseline = %+v %q", p.Prune, p.PruneUnavailable)
	}
	if withPrune, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Prune: true}); !strings.Contains(withPrune.PruneUnavailable, "baseline") {
		t.Fatalf("prune unavailable = %q", withPrune.PruneUnavailable)
	}
	// Repositories and remotes come before every package operation.
	lastRepo, firstPkg := -1, len(p.Operations)
	for i, op := range p.Operations {
		switch op.Kind {
		case KindDNFConfig, KindRepository, KindFlatpakRemote:
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

func TestPlanNotesWhatGoesBeyondTheDefinitions(t *testing.T) {
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
			return append(rows, TxPackage{Name: "bash", Arch: "x86_64", EVR: "0:6-1", Repository: "updates", Section: "downgrading"})
		}, "would downgrade bash"},
		{"obsoleting an undeclared package", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "old-tool", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: SectionReplaced})
		}, "would replace old-tool, which no component declares in removes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, f := readyHost(t, c)
			p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, tc.mutate)
			// DNF's resolution runs as it is; what goes beyond the
			// definitions is shown for review, not refused.
			op := find(p, "packages:install")
			if op.Blocked != "" || !strings.Contains(strings.Join(op.Notes, "\n"), tc.want) {
				t.Fatalf("blocked %q notes %v", op.Blocked, op.Notes)
			}
		})
	}
	t.Run("dependencies are allowed", func(t *testing.T) {
		src, f := readyHost(t, c)
		p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "libfoo", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing dependencies"})
		})
		if op := find(p, "packages:install"); op.Blocked != "" || len(op.Notes) != 0 {
			t.Fatalf("dependency noted: %s %v", op.Blocked, op.Notes)
		}
	})
	t.Run("preview failure blocks with the reason", func(t *testing.T) {
		src, f := readyHost(t, c)
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
	src, f := readyHost(t, c)
	// ffmpeg-free is installed on this host, so media-codecs must swap it.
	f.Packages.Value = append(f.Packages.Value, facts.Package{Name: "ffmpeg-free", Epoch: "0", Version: "8.0.1", Release: "6.fc44", Arch: "x86_64", FromRepo: "fedora", Reason: "user"})
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	key := facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "ffmpeg-free")
	src.Commands[key] = previewText([]TxPackage{{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"}})
	src.Failures[key] = "exit status 1"
	p := answerInstall(t, src, in, nil)
	op := find(p, "packages:remove")
	if op == nil || op.Blocked != "" || op.Risk != RiskMedium || strings.Join(op.Steps[0].Argv, " ") != "dnf5 -y remove --no-autoremove ffmpeg-free" {
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
	src, f := readyHost(t, c)
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
	f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/", GPGVerify: true, KeyFingerprints: []string{definitions.NormalizeFingerprint(c.Definitions().Repositories["flathub"].Key)}}}
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
	if p.Updates.Unavailable == "" || !strings.Contains(p.Updates.Unavailable, "sync refreshes it") {
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
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
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

// readyHost is the fixture host after a first apply round enabled every
// declared DNF and COPR repository as declared, so the next round can
// preview its transactions.
func readyHost(t *testing.T, c *definitions.Checkout) (*facts.FakeSource, *facts.Facts) {
	t.Helper()
	src, f := host(t)
	withoutTerraFile(f)
	for _, id := range sortedKeys(c.Definitions().Repositories) {
		r := c.Definitions().Repositories[id]
		switch {
		case r.Kind == "flatpak":
		case r.Kind == "dnf" && r.ReleasePackage == "":
			f.Repositories.Value = append(f.Repositories.Value, ownedRepoFile(c, id, nil))
		default:
			for _, host := range DNFRepoIDs(id, r) {
				found := false
				for i := range f.Repositories.Value {
					if f.Repositories.Value[i].ID == host {
						f.Repositories.Value[i].GPGCheck, f.Repositories.Value[i].Priority, found = "1", strconv.Itoa(*r.Priority), true
						f.Repositories.Value[i].KeyFingerprints = []string{definitions.NormalizeFingerprint(r.Key)}
						f.Repositories.Value[i].KeyError = ""
						f.Repositories.Value[i].GPGKey = "file://" + KeyPath(id)
					}
				}
				if !found {
					f.Repositories.Value = append(f.Repositories.Value, facts.Repository{ID: host, File: "_" + host + ".repo", Enabled: true, GPGCheck: "1", Priority: strconv.Itoa(*r.Priority), GPGKey: "file://" + KeyPath(id), KeyFingerprints: []string{definitions.NormalizeFingerprint(r.Key)}})
				}
			}
		}
	}
	return src, f
}

// ownedRepoFile is the section of nimbus-<id>.repo exactly as Nimbus writes
// it, as facts would observe it; edit changes it the way drift would.
func ownedRepoFile(c *definitions.Checkout, id string, edit func(*facts.Repository)) facts.Repository {
	have := facts.Repository{ID: "nimbus-" + id, File: "nimbus-" + id + ".repo", Enabled: true, Options: map[string]string{}}
	for _, o := range OwnedRepoOptions(id, c.Definitions().Repositories[id]) {
		have.Options[o.Key] = o.Value
	}
	have.GPGCheck, have.Priority, have.BaseURL = have.Options["gpgcheck"], have.Options["priority"], have.Options["baseurl"]
	have.GPGKey = have.Options["gpgkey"]
	have.KeyFingerprints = []string{definitions.NormalizeFingerprint(c.Definitions().Repositories[id].Key)}
	if edit != nil {
		edit(&have)
	}
	return have
}

func TestRepositoryStateIsVerifiedNotAssumed(t *testing.T) {
	c, r := repository(t)
	cases := []struct {
		name string
		repo facts.Repository
		want func(*Operation) bool
	}{
		{"correct nimbus file is ready", ownedRepoFile(c, "docker", nil),
			func(op *Operation) bool { return op == nil }},
		{"gpgcheck off is repaired", ownedRepoFile(c, "docker", func(h *facts.Repository) { h.GPGCheck, h.Options["gpgcheck"] = "0", "0" }),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "gpgcheck=0") && op.Blocked == ""
			}},
		{"wrong baseurl is repaired", ownedRepoFile(c, "docker", func(h *facts.Repository) {
			h.BaseURL, h.Options["baseurl"] = "https://example.invalid/docker", "https://example.invalid/docker"
		}),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "baseurl")
			}},
		// The first VM apply wrote repo_gpgcheck=1, which DNF could not
		// satisfy; a key Nimbus no longer writes must be repaired away.
		{"a stray key is repaired", ownedRepoFile(c, "docker", func(h *facts.Repository) { h.Options["repo_gpgcheck"] = "1" }),
			func(op *Operation) bool {
				return op != nil && op.Action == ActionRepair && strings.Contains(op.Summary, "repo_gpgcheck=1, which Nimbus does not write") && op.Steps[0].Argv[2] == "addrepo" && slices.Contains(op.Steps[0].Argv, "--overwrite")
			}},
		{"foreign file with our id blocks", ownedRepoFile(c, "docker", func(h *facts.Repository) { h.File = "docker-ce.repo" }),
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
	f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/", GPGVerify: true, KeyFingerprints: []string{definitions.NormalizeFingerprint(c.Definitions().Repositories["flathub"].Key)}}}
	f.Flatpak.Value.Apps = []facts.FlatpakApp{{ID: "com.spotify.Client", Version: "1", Origin: "fedora"}}
	p, _ = Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || op.Action != ActionAdopt || op.Blocked != "" || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "remote fedora, not flathub") {
		t.Fatalf("foreign origin must be adopted with a note: %+v", op)
	}
}

func TestAdoptionTakesWhatIsInstalled(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	// Whatever source a desired package came from, it is installed and
	// desired: Nimbus adopts it and records the source in the receipt.
	f.Packages.Value = append(f.Packages.Value,
		facts.Package{Name: "ripgrep", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		facts.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		facts.Package{Name: "git", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "anaconda", Reason: "user"},
	)
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	for _, id := range []string{"package:dnf:ripgrep", "package:terra:ghostty", "package:dnf:git"} {
		if op := find(p, id); op == nil || op.Blocked != "" || op.Action != ActionAdopt {
			t.Fatalf("%s = %+v", id, op)
		}
	}
}

func applied(receipts ...string) *state.Applied {
	a := &state.Applied{Present: true, Receipts: map[string]state.Receipt{}}
	for _, r := range receipts {
		provider := "dnf"
		if strings.HasPrefix(r, "flatpak:") {
			provider = "flatpak"
		}
		a.Receipts[r] = state.Receipt{Schema: state.Schema, Resource: r, Provider: provider, Package: facts.PackageID(PackageName(r), "x86_64"), Operation: "install", Verified: true}
	}
	return a
}

func TestAppliedStateShapesThePlan(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	a := applied("package:dnf:dnf5-plugins", "package:dnf:no-longer-wanted")
	// Everything on the fixture host existed before Nimbus took over.
	var baseline []string
	for _, p := range f.Packages.Value {
		baseline = append(baseline, p.Name)
	}
	sort.Strings(baseline)
	a.Baseline = &state.Baseline{Schema: state.Schema, Packages: baseline}
	f.Packages.Value = append(f.Packages.Value,
		facts.Package{Name: "no-longer-wanted", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "fedora", Reason: "user"},
		facts.Package{Name: "hand-installed", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "fedora", Reason: "user"},
	)
	key := facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "no-longer-wanted.x86_64")
	src.Commands[key] = previewText([]TxPackage{{Name: "no-longer-wanted", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"}})
	src.Failures[key] = "exit status 1"
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Applied: a, Source: src}
	p := answerInstall(t, src, in, nil)

	if op := find(p, "package:dnf:dnf5-plugins"); op == nil || op.Action != ActionKeep {
		t.Fatalf("managed package = %+v", op)
	}
	owned := find(p, "packages:remove-owned")
	if owned == nil || owned.Blocked != "" || strings.Join(owned.Steps[0].Argv, " ") != "dnf5 -y remove --no-autoremove no-longer-wanted.x86_64" || !contains(owned.Paths, "package:dnf:no-longer-wanted") {
		t.Fatalf("owned removal = %+v", owned)
	}
	names := map[string]bool{}
	for _, pr := range p.Prune {
		names[pr.Name] = true
	}
	if names["gzip.x86_64"] || names["no-longer-wanted.x86_64"] || !names["hand-installed.x86_64"] {
		t.Fatalf("prune buckets wrong: %+v", p.Prune)
	}
	if find(p, "packages:prune") != nil {
		t.Fatal("prune transaction planned without Prune")
	}

	pruneKey := facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "hand-installed.x86_64")
	src.Commands[pruneKey] = previewText([]TxPackage{{Name: "hand-installed", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"}})
	src.Failures[pruneKey] = "exit status 1"
	in.Prune = true
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if op := find(p, "packages:prune"); op == nil || op.Action != ActionPrune || op.Blocked != "" || !contains(op.Paths, "unmanaged:hand-installed.x86_64") {
		t.Fatalf("prune transaction = %+v", op)
	}
	if find(p, "packages:remove-owned") == nil {
		t.Fatal("owned removal lost with Prune")
	}
}

func TestReceiptOfAnAbsentPackageIsRetired(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	a := applied("package:dnf:vanished")
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Applied: a, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "package:dnf:vanished")
	if op == nil || op.Action != ActionRetire || len(op.Steps) != 0 || op.Blocked != "" {
		t.Fatalf("retire = %+v", op)
	}
	if find(p, "packages:remove-owned") != nil {
		t.Fatal("an absent package must not be removed")
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

func repositoryOnly(t *testing.T) *definitions.Checkout {
	t.Helper()
	c, _ := repository(t)
	return c
}

func TestPreviewFailureReportsTheResolutionProblem(t *testing.T) {
	stderr := errors.New("dnf5 --assumeno --cacheonly install foo: Updating and loading repositories:\nRepositories loaded.\nFailed to resolve the transaction:\nNo match for argument: foo\nYou can try to add to command line:\n  --skip-unavailable to skip unavailable packages")
	got := previewFailure(nil, stderr, errors.New("no transaction table in dnf5 output"))
	if got != "dnf5 could not resolve the transaction: No match for argument: foo" {
		t.Fatalf("reason = %q", got)
	}
	got = previewFailure(nil, errors.New("dnf5: something else\nlast line"), errors.New("no table"))
	if got != "dnf5 preview failed: last line" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestFlatpakWaitsForItsOwnInstallation(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// A fresh base install: no flatpak binary, so its state is unknown,
	// but common installs flatpak in this plan.
	f.Commands["flatpak"] = ""
	f.Flatpak = facts.Section[facts.Flatpak]{Error: `flatpak remotes: exec: "flatpak": executable file not found in $PATH`}
	p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, nil)
	remote := find(p, "flatpak-remote:flathub")
	if remote == nil || remote.Blocked != "" || remote.After != "packages:install" {
		t.Fatalf("remote = %+v", remote)
	}
	if app := find(p, "flatpak:com.spotify.Client"); app == nil || app.Blocked != "" || app.After != "flatpak-remote:flathub" {
		t.Fatalf("app = %+v", app)
	}
	if !p.Complete {
		t.Fatal("a fresh host must still get a complete plan")
	}
}

func TestARequestedProvideMayResolveToAnotherName(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// DNF resolves the first requested name to a differently named package,
	// the way pipewire-pulse resolved to pipewire-pulseaudio on Fedora 44.
	var requested string
	p := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		requested = rows[0].Name
		rows[0].Name = requested + "-real"
		return rows
	})
	op := find(p, "packages:install")
	if op.Blocked != "" || len(op.Notes) != 1 || op.Notes[0] != requested+" resolves to the package "+requested+"-real" {
		t.Fatalf("substitution = blocked %q notes %v", op.Blocked, op.Notes)
	}
	p = answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows, TxPackage{Name: "extra", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing"})
	})
	if op := find(p, "packages:install"); op.Blocked != "" || !strings.Contains(strings.Join(op.Notes, "\n"), "would install extra") {
		t.Fatalf("an extra row without an unmatched request must be noted: %q %v", op.Blocked, op.Notes)
	}
}

func TestNoMatchFromAnEnabledRepositoryAsksForARefresh(t *testing.T) {
	c, r := repository(t)
	// Every repository was enabled by an apply that stopped before its
	// refresh: the files are correct, so their packages join the preview,
	// but the local cache has never held Docker metadata. DNF matches
	// nothing from it, and one Fedora name is wrong as well.
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	args := installArgs(first)
	if args == nil || !slices.Contains(args, "docker-ce") {
		t.Fatalf("docker-ce is not in the preview: %v", args)
	}
	src.Commands[facts.Key("dnf5", args...)] = nil
	src.Failures[facts.Key("dnf5", args...)] = "dnf5 --assumeno --cacheonly install ...: Updating and loading repositories:\nRepositories loaded.\nFailed to resolve the transaction:\nNo match for argument: docker-ce\nNo match for argument: docker-ce-cli\nNo match for argument: no-such-fedora-package\nYou can try to add to command line:\n  --skip-unavailable to skip unavailable packages"
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "packages:install")
	if op == nil || op.Blocked == "" {
		t.Fatalf("install = %+v", op)
	}
	if !strings.HasPrefix(op.Blocked, "the enabled repositories docker have no cached metadata; sync again to refresh it (") || !strings.Contains(op.Blocked, "No match for argument: no-such-fedora-package") {
		t.Fatalf("blocked = %q", op.Blocked)
	}
}

func TestNeededUpgradesAreAcceptedAndNoted(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// A requested package needs a newer openssl-libs than the installed
	// one, so DNF upgrades it and lists the old version below it.
	p := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows,
			TxPackage{Name: "openssl-libs", Arch: "x86_64", EVR: "1:3.5.8-1.fc44", Repository: "updates", Section: "upgrading"},
			TxPackage{Name: "openssl-libs", Arch: "x86_64", EVR: "1:3.5.7-2.fc44", Repository: "updates", Section: SectionReplaced})
	})
	op := find(p, "packages:install")
	if op.Blocked != "" || len(op.Notes) != 1 || op.Notes[0] != "1 installed packages are upgraded because the requested packages need the newer versions: openssl-libs" {
		t.Fatalf("needed upgrade = blocked %q notes %v", op.Blocked, op.Notes)
	}
}

func TestDNFDropInIsPlannedFirstAndVerifiedWhole(t *testing.T) {
	c, r := repository(t)
	rendered := DNFDropIn(c.Definitions())
	if !strings.HasPrefix(rendered, "# Written by Nimbus") || !strings.Contains(rendered, "[main]\ndefaultyes=True\nfastestmirror=True\nmax_parallel_downloads=20\n") {
		t.Fatalf("rendered drop-in:\n%s", rendered)
	}
	build := func(have string, managed bool) *Operation {
		src, f := host(t)
		if have != "" {
			f.DNFDropIn.Value = have
		}
		in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
		if managed {
			in.Applied = applied("dnf:config")
		}
		p, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		if p.Operations[0].ID != "dnf:config" && find(p, "dnf:config") != nil {
			t.Fatalf("the drop-in is not the first operation: %s", p.Operations[0].ID)
		}
		return find(p, "dnf:config")
	}
	if op := build("", false); op == nil || op.Action != ActionInstall || op.Steps[len(op.Steps)-1].Argv[0] != "install" || !slices.Contains(op.Steps[len(op.Steps)-1].Argv, facts.DNFDropInPath) || op.Steps[0].Description != "set defaultyes=True" {
		t.Fatalf("absent = %+v", op)
	}
	if op := build("[main]\nmax_parallel_downloads=3\n", true); op == nil || op.Action != ActionRepair {
		t.Fatalf("differs = %+v", op)
	}
	if op := build(rendered, false); op == nil || op.Action != ActionAdopt {
		t.Fatalf("as declared without receipt = %+v", op)
	}
	if op := build(rendered, true); op == nil || op.Action != ActionKeep {
		t.Fatalf("managed = %+v", op)
	}
	// Nothing declared: a managed file is removed, a foreign one is left.
	root := c.Definitions()
	root.DNF = nil
	src, f := host(t)
	f.DNFDropIn.Value = rendered
	p, _ := Build(Inputs{Resolved: r, Root: root, Definitions: c.Digest(), Facts: f, Source: src, Applied: applied("dnf:config")})
	if op := find(p, "dnf:config"); op == nil || op.Action != ActionRemove || op.Steps[0].Argv[0] != "rm" {
		t.Fatalf("undeclared and managed = %+v", op)
	}
	p, _ = Build(Inputs{Resolved: r, Root: root, Definitions: c.Digest(), Facts: f, Source: src})
	if find(p, "dnf:config") != nil {
		t.Fatal("a drop-in Nimbus never wrote was planned for removal")
	}
}

func TestADuplicateMakerRepositoryIsDisabledByOverride(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// 1Password's package writes its own repository file at install, with
	// the same baseurl under its own ID and DNF's default priority.
	f.Repositories.Value = append(f.Repositories.Value, facts.Repository{ID: "1password", File: "1password.repo", Enabled: true, GPGCheck: "1", BaseURL: c.Definitions().Repositories["onepassword"].BaseURL, Options: map[string]string{"baseurl": c.Definitions().Repositories["onepassword"].BaseURL}})
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
	f.Packages.Value = append(f.Packages.Value, facts.Package{Name: "rpmfusion-free-release", Version: "44", Release: "3", Arch: "noarch", FromRepo: "@commandline", Reason: "user"})
	a := applied()
	a.Baseline = &state.Baseline{Schema: state.Schema, Packages: []string{"bash"}}
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: a, Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range p.Prune {
		if pr.Name == "rpmfusion-free-release" {
			t.Fatal("the release package Nimbus installed is a prune candidate")
		}
	}
	if !IsReleasePackage(c.Definitions(), "rpmfusion-free-release") || IsReleasePackage(c.Definitions(), "rpmfusion") {
		t.Fatal("release package recognition is wrong")
	}
	if got := ReleasePackageName("https://example.invalid/x/foo-bar-release-1.2-3.fc44.noarch.rpm"); got != "foo-bar-release" {
		t.Fatalf("name = %q", got)
	}
}

func TestAPrefixChangeRetiresTheOldReceiptAndAdoptsTheNew(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// ghostty was recorded under dnf: and is now selected as terra:ghostty;
	// the package stays, the old receipt goes, the new identity is adopted.
	f.Packages.Value = append(f.Packages.Value, facts.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "nimbus-terra", Reason: "user"})
	a := applied("package:dnf:ghostty")
	a.Baseline = &state.Baseline{Schema: state.Schema, Packages: []string{"bash"}}
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: a})
	if err != nil {
		t.Fatal(err)
	}
	if op := find(p, "package:dnf:ghostty"); op == nil || op.Action != ActionRetire {
		t.Fatalf("old receipt = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || op.Action != ActionAdopt || len(op.Notes) != 0 {
		t.Fatalf("new identity = %+v", op)
	}
	if op := find(p, "packages:remove-owned"); op != nil {
		t.Fatalf("the package must not be removed: %+v", op)
	}
}

func TestAnotherSourceIsNotedOnAdoptionAndKeep(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	f.Packages.Value = append(f.Packages.Value,
		facts.Package{Name: "ripgrep", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "nimbus-terra", Reason: "user"},
		facts.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "copr:copr.fedorainfracloud.org:someone:ghostty", Reason: "user"},
		facts.Package{Name: "git", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "anaconda", Reason: "user"},
	)
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: applied("package:dnf:ripgrep")})
	if op := find(p, "package:dnf:ripgrep"); op == nil || op.Action != ActionKeep || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "installed from nimbus-terra, not from fedora") {
		t.Fatalf("kept from another source = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || op.Action != ActionAdopt || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "not from nimbus-terra") {
		t.Fatalf("adopted from another source = %+v", op)
	}
	if op := find(p, "package:dnf:git"); op == nil || len(op.Notes) != 0 {
		t.Fatalf("the installer's source needs no note: %+v", op)
	}
	if steps := repositorySteps("hyprland-copr", c.Definitions().Repositories["hyprland-copr"]); steps[2].Argv[0] != "rpm" || steps[2].Argv[1] != "--import" || !steps[2].Privileged {
		t.Fatalf("the COPR key must be imported before the enable: %+v", steps)
	}
}

func TestMiseBootstrapLeavesConfiguredToolsToChezmoi(t *testing.T) {
	c, _ := repository(t)
	// The global dotfiles apply must also work without development selected.
	c.Machines["common-only"] = &definitions.Machine{Schema: 1, ID: "common-only", Profiles: []string{"common"}}
	r, errs := definitions.Resolve(c, "common-only")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	src, f := readyHost(t, c)
	home := "/home/tester"
	f.User = facts.Section[facts.User]{Value: facts.User{Home: home, Cargo: false, Crates: []string{}}}
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// A fresh home: Nimbus installs Mise itself. Chezmoi owns its tools.
	p := answerInstall(t, src, in, nil)
	if op := find(p, "user:mise"); op == nil || op.Action != ActionInstall || op.Steps[1].Argv[0] != "sh" || op.Steps[1].Argv[1] != InstallerScript || op.Steps[1].Privileged {
		t.Fatalf("mise installer = %+v", op)
	}
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not install the Chezmoi-owned tools: %+v", op)
	}
	// Mise present, config not written yet: no runtime operation is planned.
	src.Dirs[home+"/.local/bin"] = []string{"mise"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:mise"); op == nil || op.Action != ActionKeep {
		t.Fatalf("mise present = %+v", op)
	}
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not wait for the Mise config: %+v", op)
	}
	// Even with applied configuration and missing Rust, sync leaves the
	// tool installation to Chezmoi.
	src.Dirs[home+"/.config/mise"] = []string{"config.toml"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not duplicate the Chezmoi install script: %+v", op)
	}
	for _, op := range p.Operations {
		if strings.HasPrefix(op.ID, "package:cargo:") {
			t.Fatalf("Mise-owned Cargo tool must not also be installed directly: %+v", op)
		}
		if op.Kind == KindUser {
			for _, st := range op.Steps {
				if st.Privileged {
					t.Fatalf("user-scope step marked privileged: %+v", op)
				}
			}
		}
	}
	// Unknown user-scope state blocks instead of dropping the tools.
	f.User = facts.Section[facts.User]{Error: "cargo install --list: broken"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:tools"); op == nil || op.Blocked == "" || !strings.Contains(op.Blocked, "broken") || p.Complete {
		t.Fatalf("unknown user state = %+v complete %v", op, p.Complete)
	}
	if find(p, "user:mise") != nil || find(p, "package:cargo:sheldon") != nil {
		t.Fatal("user tools planned without their facts")
	}
}

package plan

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/rpm"
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
	r.Shell = ""
	r.Components = slices.DeleteFunc(r.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "snapper" })
	r.Files = nil
	r.Constraints = nil
	r.Services = nil
	r.Groups = nil
	r.DefaultTarget = ""
	return c, r
}

// host builds facts from the recorded Fedora 44 fixture: Fedora, RPM Fusion,
// and Terra enabled, no Nimbus-written repositories, no Flatpak remote.
func host(t *testing.T) (*nativetest.FakeSource, *inspect.Facts) {
	t.Helper()
	dir := filepath.Join("..", "inspect", "testdata", "fedora44")
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	src := &nativetest.FakeSource{
		Commands: map[string][]byte{
			nativetest.Key("uname", "-m"):                                                                  []byte("x86_64\n"),
			nativetest.Key("dnf5", inspect.PackageQueryArgs...):                                            read("repoquery-installed.txt"),
			nativetest.Key("flatpak", "remotes", "--system", "--columns=name,url"):                         []byte(""),
			nativetest.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin"): []byte(""),
			nativetest.Key("systemctl", "is-active", "firewalld"):                                          []byte("active\n"),
			nativetest.Key("dnf5", "--cacheonly", "check-upgrade"):                                         []byte("Repositories loaded.\nUpgrades\nlibrepo.x86_64 1.21.0-1.fc44 updates\n"),
		},
		Failures: map[string]string{nativetest.Key("dnf5", "--cacheonly", "check-upgrade"): "exit status 100"},
		Files:    map[string][]byte{inspect.OSReleasePath: read("os-release"), inspect.SELinuxPath: []byte("1\n")},
		Dirs:     map[string][]string{inspect.RepoDir: {}},
		Paths:    map[string]string{},
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "yum.repos.d"))
	for _, e := range entries {
		src.Dirs[inspect.RepoDir] = append(src.Dirs[inspect.RepoDir], e.Name())
		src.Files[filepath.Join(inspect.RepoDir, e.Name())] = read(filepath.Join("yum.repos.d", e.Name()))
	}
	for _, name := range inspect.RequiredCommands {
		src.Paths[name] = "/usr/bin/" + name
	}
	return src, inspect.Inspect(src, "")
}

// previewText renders a DNF5 preview table the way the fixture shows it.
func previewText(rows []TxPackage) []byte {
	var b bytes.Buffer
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
	return b.Bytes()
}

// installArgs returns the exact preview argv the planner will run for the
// packages it can install now, so the fixture can answer it.
func installArgs(p *Plan) []string {
	op := find(p, "packages:install")
	if op == nil {
		return nil
	}
	args := []string{"--assumeno", "--cacheonly"}
	return append(args, op.Steps[0].Argv[2:]...) // drop dnf5 -y
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

func answerInstall(t *testing.T, src *nativetest.FakeSource, in Inputs, mutate func([]TxPackage) []TxPackage) *Plan {
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
		native, arch := rpm.SplitRequest(name)
		arch = cmp.Or(arch, "x86_64")
		rows = append(rows, TxPackage{Name: native, Arch: arch, EVR: "0:1-1.fc44", Repository: repo, Section: "installing"})
	}
	if mutate != nil {
		rows = mutate(rows)
	}
	src.Commands[nativetest.Key("dnf5", args...)] = previewText(rows)
	src.Failures[nativetest.Key("dnf5", args...)] = "exit status 1"
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func find(p *Plan, id string) *Operation {
	if i := slices.IndexFunc(p.Operations, func(op Operation) bool { return op.ID == id }); i >= 0 {
		return &p.Operations[i]
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
	if inst.Blocked != "" || inst.After != "" || inst.Transaction == nil || !slices.Contains(inst.Steps[0].Argv, "--allowerasing") || !slices.Contains(inst.Steps[0].Argv, "docker-ce") || !strings.Contains(inst.Summary, "packages through one DNF transaction") {
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
			firstPkg = min(firstPkg, i)
		}
	}
	if lastRepo > firstPkg {
		t.Fatalf("repository operation after a package operation: %d > %d", lastRepo, firstPkg)
	}
	if !slices.IsSortedFunc(p.Prune, func(a, b Prune) int { return cmp.Compare(a.Name, b.Name) }) {
		t.Fatal("prune candidates are not sorted")
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
	src.Commands[nativetest.Key("dnf5", "--cacheonly", "check-upgrade")] = []byte("Repositories loaded.\n")
	delete(src.Failures, nativetest.Key("dnf5", "--cacheonly", "check-upgrade"))
	c2, _ := Build(in)
	if c2.Digest != a.Digest || len(c2.Updates.Available) != 0 {
		t.Fatal("update information must not change the plan digest")
	}
	f.Flatpak.Value.Remotes = []inspect.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/", GPGVerify: true, KeyFingerprints: []string{definitions.NormalizeFingerprint(c.Definitions().Repositories["flathub"].Key)}}}
	d, _ := Build(in)
	if d.Digest == a.Digest || find(d, "flatpak-remote:flathub") != nil || find(d, "flatpak:com.spotify.Client").Blocked != "" {
		t.Fatal("a present remote must change the operations and the digest")
	}
}

func TestUpdatesUnavailableIsReportedNotGuessed(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	key := nativetest.Key("dnf5", "--cacheonly", "check-upgrade")
	delete(src.Commands, key)
	src.Failures[key] = "Cache-only enabled but no cache for 'fedora'"
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	if p.Updates.Unavailable == "" || !strings.Contains(p.Updates.Unavailable, "sync refreshes it") {
		t.Fatalf("updates = %+v", p.Updates)
	}
	f.Packages = inspect.Section[[]inspect.Package]{Error: "dnf5 broken"}
	if _, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}); err == nil {
		t.Fatal("planning without installed packages must fail")
	}
}

func withoutTerraFile(f *inspect.Facts) {
	var kept []inspect.Repository
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
	if i := slices.IndexFunc(p.Operations, func(op Operation) bool { return op.Blocked != "" }); i >= 0 {
		t.Fatalf("blocked on a fresh host: %s: %s", p.Operations[i].ID, p.Operations[i].Blocked)
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
func readyHost(t *testing.T, c *definitions.Checkout) (*nativetest.FakeSource, *inspect.Facts) {
	t.Helper()
	src, f := host(t)
	f.DNFDropIn.Value = DNFDropIn(c.Definitions())
	withoutTerraFile(f)
	for _, id := range slices.Sorted(maps.Keys(c.Definitions().Repositories)) {
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
					f.Repositories.Value = append(f.Repositories.Value, inspect.Repository{ID: host, File: "_" + host + ".repo", Enabled: true, GPGCheck: "1", Priority: strconv.Itoa(*r.Priority), GPGKey: "file://" + KeyPath(id), KeyFingerprints: []string{definitions.NormalizeFingerprint(r.Key)}})
				}
			}
		}
	}
	return src, f
}

// ownedRepoFile is the section of nimbus-<id>.repo exactly as Nimbus writes
// it, as facts would observe it; edit changes it the way drift would.
func ownedRepoFile(c *definitions.Checkout, id string, edit func(*inspect.Repository)) inspect.Repository {
	have := inspect.Repository{ID: "nimbus-" + id, File: "nimbus-" + id + ".repo", Enabled: true, Options: map[string]string{}}
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

func applied(receipts ...string) *state.Applied {
	a := &state.Applied{Present: true, Receipts: map[string]state.Receipt{}}
	for _, r := range receipts {
		provider := "dnf"
		if strings.HasPrefix(r, "flatpak:") {
			provider = "flatpak"
		}
		a.Receipts[r] = state.Receipt{Schema: state.ReceiptSchema, Resource: r, Provider: provider, Package: inspect.PackageID(PackageName(r), "x86_64"), Operation: "install", Verified: true}
	}
	return a
}

func repositoryOnly(t *testing.T) *definitions.Checkout {
	t.Helper()
	c, _ := repository(t)
	return c
}

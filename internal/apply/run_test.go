package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// scripted is a Source whose responses change as commands run, so the
// executor's verification sees the effect of what it ran.
type scripted struct {
	facts.FakeSource
	installed []string // package names the fake host has
	remotes   []string
	apps      []string
	repoIDs   []string
	log       []string
	fail      map[string]string // command prefix -> error
	stored    string            // transaction.json content
	fpr       string            // fingerprint gpg reports
}

func newScripted() *scripted {
	s := &scripted{fail: map[string]string{}, fpr: "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F"}
	s.Commands = map[string][]byte{}
	s.Failures = map[string]string{}
	s.Files = map[string][]byte{}
	s.Dirs = map[string][]string{facts.RepoDir: {}}
	s.Paths = map[string]string{}
	s.installed = []string{"bash", "coreutils"}
	return s
}

func (s *scripted) Run(name string, args ...string) ([]byte, error) {
	key := facts.Key(name, args...)
	s.log = append(s.log, key)
	for prefix, msg := range s.fail {
		if strings.HasPrefix(key, prefix) {
			return nil, errors.New(msg)
		}
	}
	switch {
	case name == "dnf5" && len(args) > 2 && args[2] == "repoquery":
		var b strings.Builder
		for _, n := range s.installed {
			fmt.Fprintf(&b, "%s|0|1|1.fc44|x86_64|fedora|User\n", n)
		}
		return []byte(b.String()), nil
	case name == "uname":
		return []byte("x86_64\n"), nil
	case name == "gpg":
		return []byte("fpr:::::::::" + s.fpr + ":\n"), nil
	case name == "flatpak" && args[0] == "remotes":
		var b strings.Builder
		for _, r := range s.remotes {
			fmt.Fprintf(&b, "%s\thttps://dl.flathub.org/repo/\n", r)
		}
		return []byte(b.String()), nil
	case name == "flatpak" && args[0] == "list":
		var b strings.Builder
		for _, a := range s.apps {
			fmt.Fprintf(&b, "%s\t1.0\tflathub\n", a)
		}
		return []byte(b.String()), nil
	case name == "systemctl":
		return []byte("active\n"), nil
	case name == "sudo":
		return s.privileged(args)
	}
	return nil, fmt.Errorf("%s: %w", key, facts.ErrNotRecorded)
}

func (s *scripted) privileged(argv []string) ([]byte, error) {
	switch {
	case argv[0] == "dnf5" && argv[1] == "-y" && argv[2] == "install" && argv[3] == "--store":
		s.Files[filepath.Join(argv[4], "transaction.json")] = []byte(s.stored)
	case argv[0] == "dnf5" && argv[1] == "-y" && argv[2] == "replay":
		for _, n := range []string{"ripgrep", "libfoo"} {
			s.installed = append(s.installed, n)
		}
	case argv[0] == "dnf5" && argv[1] == "-y" && argv[2] == "remove":
		var kept []string
		for _, n := range s.installed {
			if !contains(argv[3:], n) {
				kept = append(kept, n)
			}
		}
		s.installed = kept
	case argv[0] == "dnf5" && argv[1] == "config-manager" && argv[2] == "addrepo":
		s.repoIDs = append(s.repoIDs, strings.TrimPrefix(argv[3], "--id="))
		s.Dirs[facts.RepoDir] = []string{"nimbus-terra.repo"}
		s.Files[filepath.Join(facts.RepoDir, "nimbus-terra.repo")] = []byte("[nimbus-terra]\nenabled=1\ngpgcheck=1\npriority=100\n")
	case argv[0] == "flatpak" && argv[1] == "remote-add":
		s.remotes = append(s.remotes, argv[5])
	case argv[0] == "flatpak" && argv[1] == "install":
		s.apps = append(s.apps, argv[5])
	}
	return nil, nil
}

func (s *scripted) ReadFile(path string) ([]byte, error) {
	if data, ok := s.Files[path]; ok {
		return data, nil
	}
	if path == facts.OSReleasePath {
		return []byte("ID=fedora\nVERSION_ID=44\n"), nil
	}
	return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
}

func (s *scripted) ran(prefix string) bool {
	for _, l := range s.log {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func terra() definitions.Repository {
	prio := 100
	return definitions.Repository{Kind: "dnf", BaseURL: "https://repos.fyralabs.com/terra44", KeyURL: "https://repos.fyralabs.com/terra44/key.asc",
		Key: "AE09 157A 4DE8 8B49 7EA1 D5D3 00CD AB43 DE22 6D6F", Priority: &prio}
}

func samplePlan(t *testing.T) *plan.Plan {
	t.Helper()
	tx := &plan.Transaction{Packages: []plan.TxPackage{
		{Name: "ripgrep", Arch: "x86_64", EVR: "0:15.2.0-1.fc44", Repository: "updates", Section: "installing"},
		{Name: "libfoo", Arch: "x86_64", EVR: "0:1-1.fc44", Repository: "fedora", Section: "installing dependencies"},
	}}
	stage := plan.StageRoot + "/packages-install"
	p := &plan.Plan{Machine: "desktop", Definitions: "sha256:defs", Complete: true, Digest: "sha256:plan", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable, Risk: plan.RiskMedium, Summary: "enable terra"},
		{ID: "flatpak-remote:flathub", Kind: plan.KindFlatpakRemote, Action: plan.ActionEnable, Summary: "add flathub"},
		{ID: "package:dnf:bash", Kind: plan.KindPackage, Action: plan.ActionAdopt, Summary: "adopt bash", Paths: []string{"profile:common"}},
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install 1", Paths: []string{"dnf:ripgrep"}, Transaction: tx,
			Steps: []plan.Step{
				{Argv: []string{"dnf5", "-y", "install", "--store", stage, "ripgrep"}, Privileged: true},
				{Argv: []string{"dnf5", "-y", "replay", stage}, Privileged: true},
				{Argv: []string{"rm", "-rf", stage}, Privileged: true},
			}},
		{ID: "package:dnf:ripgrep", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install ripgrep", Paths: []string{"profile:common"}, After: "packages:install"},
		{ID: "flatpak:com.spotify.Client", Kind: plan.KindFlatpak, Action: plan.ActionInstall, Summary: "install spotify", Paths: []string{"profile:hyprland-noctalia"},
			Steps: []plan.Step{{Argv: []string{"flatpak", "install", "--system", "--noninteractive", "flathub", "com.spotify.Client"}, Privileged: true}}},
	}}
	return p
}

func options(t *testing.T, src *scripted, root string) Options {
	t.Helper()
	fetched := map[string][]byte{
		"https://repos.fyralabs.com/terra44/key.asc":      []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nterra\n"),
		"https://dl.flathub.org/repo/flathub.flatpakrepo": []byte("[Flatpak Repo]\nUrl=https://dl.flathub.org/repo/\nGPGKey=a2V5\n"),
	}
	flathub := definitions.Repository{Kind: "flatpak", URL: "https://dl.flathub.org/repo/flathub.flatpakrepo", Key: "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F"}
	return Options{
		Source: src,
		Fetch: func(url string) ([]byte, error) {
			data, ok := fetched[url]
			if !ok {
				return nil, fmt.Errorf("unexpected download %s", url)
			}
			return data, nil
		},
		Record:     func(digest string, st *state.Stage) error { return state.Record(root, digest, st) },
		Keys:       func(string) (map[string][]byte, error) { return nil, errors.New("no release packages in this test") },
		Stage:      filepath.Join(t.TempDir(), "stage"),
		Root:       definitions.Root{Repositories: map[string]definitions.Repository{"terra": terra(), "flathub": flathub}},
		FirstApply: true, Engine: "test", Definitions: state.Definitions{Digest: "sha256:defs"},
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func TestRunExecutesVerifiesAndRecords(t *testing.T) {
	src := newScripted()
	src.stored = `{"rpms":[{"nevra":"ripgrep-15.2.0-1.fc44.x86_64","action":"Install"},{"nevra":"libfoo-1-1.fc44.x86_64","action":"Install"}],"version":"1.0"}`
	root := t.TempDir()
	opts := options(t, src, root)
	p := samplePlan(t)
	r := Run(p, opts)
	if r.Error != "" || r.Failed != "" {
		t.Fatalf("run failed: %+v\n%s", r, strings.Join(src.log, "\n"))
	}
	if strings.Join(r.Executed, ",") != "repository:terra,flatpak-remote:flathub,package:dnf:bash,packages:install,flatpak:com.spotify.Client" || strings.Join(r.Pending, ",") != "package:dnf:ripgrep" {
		t.Fatalf("executed %v pending %v", r.Executed, r.Pending)
	}
	for _, want := range []string{
		"sudo install -m 0644 ", "sudo rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus-terra", "sudo dnf5 config-manager addrepo --id=nimbus-terra",
		"sudo flatpak remote-add --if-not-exists --system --from flathub ", "sudo dnf5 -y install --store", "sudo dnf5 -y replay", "sudo rm -rf",
		"sudo flatpak install --system --noninteractive flathub com.spotify.Client", "gpg --batch --show-keys --with-colons ",
	} {
		if !src.ran(want) {
			t.Errorf("did not run %q\n%s", want, strings.Join(src.log, "\n"))
		}
	}
	a, err := state.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"repository:terra", "flatpak-remote:flathub", "package:dnf:bash", "package:dnf:ripgrep", "flatpak:com.spotify.Client"} {
		rc, ok := a.Receipts[id]
		if !ok || !rc.Verified || rc.PlanDigest != "sha256:plan" || rc.Definitions.Digest != "sha256:defs" {
			t.Errorf("receipt %s = %+v", id, rc)
		}
	}
	if a.Receipts["package:dnf:bash"].Operation != "adopt" || a.Receipts["package:dnf:ripgrep"].Operation != "install" || !strings.HasPrefix(a.Receipts["package:dnf:ripgrep"].Intended, "installed ") {
		t.Fatalf("operations = %+v", a.Receipts)
	}
	if a.Baseline == nil || !a.InBaseline("coreutils") || a.InBaseline("ripgrep") {
		t.Fatalf("baseline = %+v", a.Baseline)
	}
}

func TestRunStopsAtTheFirstFailureAndKeepsEarlierReceipts(t *testing.T) {
	src := newScripted()
	src.stored = `{"rpms":[{"nevra":"ripgrep-15.2.0-1.fc44.x86_64","action":"Install"},{"nevra":"surprise-1-1.fc44.x86_64","action":"Install"}],"version":"1.0"}`
	root := t.TempDir()
	r := Run(samplePlan(t), options(t, src, root))
	if r.Failed != "packages:install" || !strings.Contains(r.Error, "surprise appeared in the stored transaction without review") {
		t.Fatalf("result = %+v", r)
	}
	if src.ran("sudo dnf5 -y replay") || !src.ran("sudo rm -rf") {
		t.Fatalf("replay must not run and the stage must be cleaned:\n%s", strings.Join(src.log, "\n"))
	}
	a, _ := state.Read(root)
	if _, ok := a.Receipts["repository:terra"]; !ok || len(a.Receipts) != 3 {
		t.Fatalf("earlier receipts lost: %v", a.Receipts)
	}
	if _, ok := a.Receipts["package:dnf:ripgrep"]; ok {
		t.Fatal("a failed operation got a receipt")
	}
}

func TestKeyFingerprintMismatchStopsBeforeAnyPrivilegedCommand(t *testing.T) {
	src := newScripted()
	src.fpr = "0000000000000000000000000000000000000000"
	root := t.TempDir()
	r := Run(samplePlan(t), options(t, src, root))
	if r.Failed != "repository:terra" || !strings.Contains(r.Error, "does not match the declared") {
		t.Fatalf("result = %+v", r)
	}
	if src.ran("sudo ") {
		t.Fatalf("a privileged command ran after a key mismatch:\n%s", strings.Join(src.log, "\n"))
	}
	if _, err := os.Stat(filepath.Join(root, state.SchemaFile)); err == nil {
		t.Fatal("state written although nothing succeeded")
	}
}

func TestVerificationFailureGetsNoReceipt(t *testing.T) {
	src := newScripted()
	src.stored = `{"rpms":[{"nevra":"ripgrep-15.2.0-1.fc44.x86_64","action":"Install"},{"nevra":"libfoo-1-1.fc44.x86_64","action":"Install"}],"version":"1.0"}`
	src.fail["sudo flatpak install"] = "" // succeeds silently but installs nothing
	delete(src.fail, "sudo flatpak install")
	root := t.TempDir()
	opts := options(t, src, root)
	// Make the Flatpak install a no-op so verification must catch it.
	orig := src.privileged
	_ = orig
	p := samplePlan(t)
	p.Operations[5].Steps[0].Argv = []string{"flatpak", "install", "--system", "--noninteractive", "flathub", "org.example.Other"}
	r := Run(p, opts)
	if r.Failed != "flatpak:com.spotify.Client" || !strings.Contains(r.Error, "not installed after") {
		t.Fatalf("result = %+v", r)
	}
	a, _ := state.Read(root)
	if _, ok := a.Receipts["flatpak:com.spotify.Client"]; ok {
		t.Fatal("unverified operation got a receipt")
	}
}

func TestIncompletePlanIsRefusedAndOwnedRemovalRetiresReceipts(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	p := &plan.Plan{Machine: "desktop", Complete: false, Digest: "sha256:x", Operations: []plan.Operation{{ID: "repository:terra", Blocked: "foreign"}}}
	if r := Run(p, opts); r.Error == "" || len(r.Executed) != 0 {
		t.Fatalf("incomplete plan ran: %+v", r)
	}

	src.installed = append(src.installed, "old")
	if err := state.Record(root, "sha256:earlier", &state.Stage{Schema: state.Schema, PlanDigest: "sha256:earlier", Receipts: []state.Receipt{{Schema: state.Schema, Resource: "package:dnf:old", Provider: "dnf", Operation: "install", PlanDigest: "sha256:earlier", Verified: true}}}); err != nil {
		t.Fatal(err)
	}
	opts.FirstApply = false
	p = &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:rm", Operations: []plan.Operation{
		{ID: "packages:remove-owned", Kind: plan.KindPackage, Action: plan.ActionRemove, Summary: "remove old", Paths: []string{"package:dnf:old"},
			Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "remove", "old"}, Privileged: true}}},
	}}
	r := Run(p, opts)
	if r.Error != "" {
		t.Fatalf("removal failed: %+v", r)
	}
	a, _ := state.Read(root)
	if _, ok := a.Receipts["package:dnf:old"]; ok {
		t.Fatal("receipt not retired after owned removal")
	}
}

func TestNevraAndActionHelpers(t *testing.T) {
	if nevraName("xorg-x11-drv-nvidia-libs-3:610.57.04-1.fc44.i686") != "xorg-x11-drv-nvidia-libs" || nevraName("git-core-doc-2.55.0-1.fc44.noarch") != "git-core-doc" {
		t.Fatal(nevraName("git-core-doc-2.55.0-1.fc44.noarch"))
	}
	if actionOf("installing weak dependencies") != "install" || actionOf("removing unused dependencies") != "remove" || actionOf("upgrading") != "upgrade" {
		t.Fatal("action mapping")
	}
	tx := &plan.Transaction{Packages: []plan.TxPackage{{Name: "a", Section: "installing"}, {Name: "old", Section: "removing"}}}
	if err := compareStored(tx, storedTransaction{RPMs: []struct {
		NEVRA  string `json:"nevra"`
		Action string `json:"action"`
	}{{NEVRA: "a-1-1.fc44.x86_64", Action: "Install"}, {NEVRA: "old-1-1.fc44.x86_64", Action: "Remove"}}}); err != nil {
		t.Fatal(err)
	}
}

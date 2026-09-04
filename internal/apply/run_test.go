package apply

import (
	"errors"
	"fmt"
	"io"
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
	installs  []string          // what dnf5 install adds
	fpr       string            // fingerprint gpg reports
	// privilegedNoop makes every sudo command succeed without effect.
	privilegedNoop bool
}

func newScripted() *scripted {
	s := &scripted{fail: map[string]string{}, fpr: "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F", installs: []string{"ripgrep", "libfoo"}}
	s.Commands = map[string][]byte{}
	s.Failures = map[string]string{}
	s.Files = map[string][]byte{}
	s.Dirs = map[string][]string{facts.RepoDir: {}}
	s.Paths = map[string]string{}
	s.installed = []string{"bash", "coreutils"}
	return s
}

func (s *scripted) Stream(_, _ io.Writer, name string, args ...string) error {
	_, err := s.Run(name, args...)
	return err
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
	if s.privilegedNoop {
		return nil, nil
	}
	switch {
	case argv[0] == "dnf5" && argv[1] == "-y" && argv[2] == "install":
		// DNF resolves again at install time; the fake installs what the
		// test scripted, which may differ from the preview.
		s.installed = append(s.installed, s.installs...)
	case argv[0] == "dnf5" && argv[1] == "config-manager" && argv[2] == "setopt":
		var override string
		for _, a := range argv[3:] {
			if id, value, ok := strings.Cut(a, ".enabled="); ok {
				override += "[" + id + "]\nenabled=" + value + "\n"
			}
		}
		if override != "" {
			s.Dirs[facts.RepoOverride] = []string{"99-config_manager.repo"}
			s.Files[filepath.Join(facts.RepoOverride, "99-config_manager.repo")] = []byte(override)
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
		file := "[nimbus-terra]\nenabled=1\n"
		for _, a := range argv[4:] {
			if strings.HasPrefix(a, "--set=") {
				file += strings.TrimPrefix(a, "--set=") + "\n"
			}
		}
		s.Dirs[facts.RepoDir] = []string{"nimbus-terra.repo"}
		s.Files[filepath.Join(facts.RepoDir, "nimbus-terra.repo")] = []byte(file)
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
	p := &plan.Plan{Machine: "desktop", Definitions: "sha256:defs", Complete: true, Digest: "sha256:plan", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable, Risk: plan.RiskMedium, Summary: "enable terra"},
		{ID: "flatpak-remote:flathub", Kind: plan.KindFlatpakRemote, Action: plan.ActionEnable, Summary: "add flathub"},
		{ID: "package:dnf:bash", Kind: plan.KindPackage, Action: plan.ActionAdopt, Summary: "adopt bash", Paths: []string{"profile:common"}},
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install 1", Paths: []string{"profile:common"}, Items: []string{"dnf:ripgrep"}, Transaction: tx,
			Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "install", "ripgrep"}, Privileged: true}}},
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
		"sudo flatpak remote-add --if-not-exists --system --from flathub ", "sudo dnf5 -y install ripgrep",
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

func TestDifferencesFromThePreviewAreReportedNotRefused(t *testing.T) {
	src := newScripted()
	// DNF resolved again at install time and brought a package the preview
	// did not show; the run continues and says so.
	src.installs = []string{"ripgrep", "libfoo", "surprise"}
	root := t.TempDir()
	r := Run(samplePlan(t), options(t, src, root))
	// The fake reports every installed package as 1-1.fc44, so ripgrep's
	// version differs from its preview as well; both are reported.
	if r.Error != "" || len(r.Differences) != 2 || r.Differences[0] != "DNF also installed surprise 1-1.fc44 (fedora)" || !strings.Contains(r.Differences[1], "ripgrep was installed as 1-1.fc44, the preview showed 0:15.2.0-1.fc44") {
		t.Fatalf("result = %+v", r)
	}
	src = newScripted()
	src.installs = []string{"libfoo"}
	r = Run(samplePlan(t), options(t, src, t.TempDir()))
	if r.Failed != "packages:install" || !strings.Contains(r.Error, "ripgrep is not installed after the transaction") {
		t.Fatalf("a requested package that did not arrive must fail verification: %+v", r)
	}
}

func TestRunStopsAtTheFirstFailureAndKeepsEarlierReceipts(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	src.fail["sudo dnf5 -y install"] = "exit status 1"
	r := Run(samplePlan(t), options(t, src, root))
	if r.Failed != "packages:install" || !strings.Contains(r.Error, "exit status 1") {
		t.Fatalf("result = %+v", r)
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

func TestRetireRemovesOnlyTheReceipt(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	opts.FirstApply = false
	if err := state.Record(root, "sha256:earlier", &state.Stage{Schema: state.Schema, PlanDigest: "sha256:earlier", Receipts: []state.Receipt{{Schema: state.Schema, Resource: "package:dnf:gone", Provider: "dnf", Operation: "install", PlanDigest: "sha256:earlier", Verified: true}}}); err != nil {
		t.Fatal(err)
	}
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:retire", Operations: []plan.Operation{
		{ID: "package:dnf:gone", Kind: plan.KindPackage, Action: plan.ActionRetire, Summary: "retire gone"},
	}}
	r := Run(p, opts)
	if r.Error != "" || src.ran("sudo ") {
		t.Fatalf("retire ran a command or failed: %+v\n%s", r, strings.Join(src.log, "\n"))
	}
	a, _ := state.Read(root)
	if _, ok := a.Receipts["package:dnf:gone"]; ok {
		t.Fatal("receipt not retired")
	}
}

func TestRepositoryRepairIsVerifiedAsDeclared(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	// The scripted addrepo honours every --set, so the enabled repository
	// verifies as declared; drop the priority from what it writes and the
	// verification must fail.
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:repo", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable, Summary: "enable terra"},
	}}
	if r := Run(p, opts); r.Error != "" {
		t.Fatalf("enable failed: %+v", r)
	}
	src2 := newScripted()
	src2.Files = map[string][]byte{}
	opts2 := options(t, src2, t.TempDir())
	src2.fail["sudo dnf5 config-manager addrepo"] = ""
	delete(src2.fail, "sudo dnf5 config-manager addrepo")
	// Pre-write a file with gpgcheck off that addrepo will not replace.
	src2.Dirs[facts.RepoDir] = []string{"nimbus-terra.repo"}
	src2.Files[filepath.Join(facts.RepoDir, "nimbus-terra.repo")] = []byte("[nimbus-terra]\nenabled=1\ngpgcheck=0\nbaseurl=https://repos.fyralabs.com/terra44\npriority=100\n")
	p2 := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:repo2", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionRepair, Summary: "repair terra"},
	}}
	src2.privilegedNoop = true
	if r := Run(p2, opts2); r.Error == "" || !strings.Contains(r.Error, "gpgcheck") {
		t.Fatalf("repair that left gpgcheck off was verified: %+v", r)
	}
}

func TestDNFDropInIsWrittenThenReadBack(t *testing.T) {
	src := newScripted()
	src.privilegedNoop = true
	root := t.TempDir()
	opts := options(t, src, root)
	opts.Root.DNF = map[string]any{"max_parallel_downloads": int64(10), "fastestmirror": true}
	want := plan.DNFDropIn(opts.Root)
	op := plan.Operation{ID: "dnf:config", Kind: plan.KindDNFConfig, Action: plan.ActionInstall, Summary: "configure DNF",
		Steps: []plan.Step{{Description: "set fastestmirror=True"}, {Argv: []string{"install", "-m", "0644", plan.DNFDropInPlaceholder, facts.DNFDropInPath}, Privileged: true}}}
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:dnf", Operations: []plan.Operation{op}}
	// The privileged install is a no-op in the fake, so the file never
	// appears: verification must refuse the receipt.
	if r := Run(p, opts); r.Error == "" || !strings.Contains(r.Error, "does not hold the rendered drop-in") {
		t.Fatalf("unwritten drop-in verified: %+v", r)
	}
	staged, err := os.ReadFile(filepath.Join(opts.Stage, "dnf-drop-in.conf"))
	if err != nil || string(staged) != want {
		t.Fatalf("staged content = %q, %v", staged, err)
	}
	for _, l := range src.log {
		if strings.HasPrefix(l, "sudo install") && (strings.Contains(l, plan.DNFDropInPlaceholder) || !strings.Contains(l, opts.Stage)) {
			t.Fatalf("placeholder not filled: %s", l)
		}
	}
	src.Files[facts.DNFDropInPath] = []byte(want)
	r := Run(p, opts)
	if r.Error != "" || len(r.Executed) != 1 {
		t.Fatalf("written drop-in: %+v", r)
	}
	a, err := state.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if rc, ok := a.Receipts["dnf:config"]; !ok || rc.Provider != "dnf-config" || rc.Previous != "absent" {
		t.Fatalf("receipt = %+v", rc)
	}
}

func TestADuplicateRepositoryIsDisabledAndVerified(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	src.Dirs[facts.RepoDir] = []string{"nimbus-terra.repo", "terra-maker.repo"}
	owned := "[nimbus-terra]\n"
	for _, o := range plan.OwnedRepoOptions("terra", opts.Root.Repositories["terra"]) {
		owned += o.Key + "=" + o.Value + "\n"
	}
	src.Files[filepath.Join(facts.RepoDir, "nimbus-terra.repo")] = []byte(owned)
	src.Files[filepath.Join(facts.RepoDir, "terra-maker.repo")] = []byte("[terra-maker]\nname=Terra\nbaseurl=" + opts.Root.Repositories["terra"].BaseURL + "\nenabled=1\ngpgcheck=1\n")
	src.privilegedNoop = false
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:dup", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionRepair, Summary: "repair terra",
			Steps: []plan.Step{{Description: plan.DisableDuplicateDescription, Argv: []string{"dnf5", "config-manager", "setopt", "terra-maker.enabled=0"}, Privileged: true}}},
	}}
	r := Run(p, opts)
	if r.Error != "" || !src.ran("sudo dnf5 config-manager setopt terra-maker.enabled=0") {
		t.Fatalf("duplicate not disabled: %+v\n%s", r, strings.Join(src.log, "\n"))
	}
	if src.ran("sudo dnf5 config-manager addrepo") {
		t.Fatal("the owned file was rewritten although only the duplicate drifted")
	}
}

func TestUpgradeRunsTheNativeUpdatersWithVisibleOutput(t *testing.T) {
	src := newScripted()
	src.privilegedNoop = true
	opts := options(t, src, t.TempDir())
	if err := Upgrade(opts, opts.Root); err != nil {
		t.Fatal(err)
	}
	if !src.ran("sudo dnf5 -y upgrade") || src.ran("sudo flatpak update") {
		t.Fatalf("without flatpak on PATH only DNF upgrades:\n%s", strings.Join(src.log, "\n"))
	}
	src.Paths["flatpak"] = "/usr/bin/flatpak"
	if err := Upgrade(opts, opts.Root); err != nil {
		t.Fatal(err)
	}
	if !src.ran("sudo flatpak update --system --noninteractive") {
		t.Fatalf("flatpak update missing:\n%s", strings.Join(src.log, "\n"))
	}
	src.fail["sudo dnf5 -y upgrade"] = "exit status 1"
	if err := Upgrade(opts, opts.Root); err == nil {
		t.Fatal("a failed dnf5 upgrade was not reported")
	}
}

func TestBaselineIsTheSnapshotBeforeTheRunNotAfter(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	// The first receipt of a first run comes from the package transaction
	// itself when the sources already exist: the baseline must still be the
	// set that was installed before the run.
	tx := &plan.Transaction{Packages: []plan.TxPackage{{Name: "ripgrep", Arch: "x86_64", EVR: "0:15.2.0-1.fc44", Repository: "updates", Section: "installing"}}}
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:first", Operations: []plan.Operation{
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install 1", Items: []string{"dnf:ripgrep"}, Transaction: tx,
			Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "install", "ripgrep"}, Privileged: true}}},
	}}
	if r := Run(p, options(t, src, root)); r.Error != "" {
		t.Fatal(r.Error)
	}
	a, err := state.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if a.Baseline == nil || !a.InBaseline("bash") {
		t.Fatalf("baseline = %+v", a.Baseline)
	}
	if a.InBaseline("ripgrep") || a.InBaseline("libfoo") {
		t.Fatalf("packages installed by the run are in the baseline: %v", a.Baseline.Packages)
	}
}

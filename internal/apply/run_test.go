package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// scripted is a Source whose responses change as commands run, so the
// executor's verification sees the effect of what it ran.
type scripted struct {
	nativetest.FakeSource
	installed []string // package names the fake host has
	remotes   []string
	apps      []string
	log       []string
	fail      map[string]string // command prefix -> error
	installs  []string          // what dnf5 install adds
	// installerLeavesNothing makes sh leave no binary behind.
	installerLeavesNothing bool
	fpr                    string // fingerprint gpg reports
	// privilegedNoop makes every sudo command succeed without effect.
	privilegedNoop bool
}

func newScripted() *scripted {
	s := &scripted{fail: map[string]string{}, fpr: "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F", installs: []string{"ripgrep", "libfoo"}}
	s.Commands = map[string][]byte{}
	s.Failures = map[string]string{}
	s.Files = map[string][]byte{}
	s.Dirs = map[string][]string{inspect.RepoDir: {}}
	s.Paths = map[string]string{}
	s.installed = []string{"bash", "coreutils"}
	return s
}

func (s *scripted) Stream(_, _ io.Writer, name string, args ...string) error {
	_, err := s.Run(name, args...)
	return err
}

func (s *scripted) Run(name string, args ...string) ([]byte, error) {
	key := nativetest.Key(name, args...)
	s.log = append(s.log, key)
	for prefix, msg := range s.fail {
		if strings.HasPrefix(key, prefix) {
			return nil, errors.New(msg)
		}
	}
	switch {
	case name == "dnf5" && len(args) > 2 && args[2] == "repoquery":
		b := []byte{}
		for _, n := range s.installed {
			b = fmt.Appendf(b, "%s|0|1|1.fc44|x86_64|fedora|User\n", n)
		}
		return b, nil
	case name == "uname":
		return []byte("x86_64\n"), nil
	case name == "gpg":
		return []byte("pub:::::::::\nfpr:::::::::" + s.fpr + ":\n"), nil
	case name == "flatpak" && args[0] == "remotes":
		b := []byte{}
		for _, r := range s.remotes {
			b = fmt.Appendf(b, "%s\thttps://dl.flathub.org/repo/\n", r)
		}
		return b, nil
	case name == "flatpak" && args[0] == "list":
		b := []byte{}
		for _, a := range s.apps {
			b = fmt.Appendf(b, "%s\t1.0\tflathub\n", a)
		}
		return b, nil
	case name == "systemctl":
		return []byte("active\n"), nil
	case name == "sudo":
		return s.privileged(args)
	case name == "sh":
		// The maker's installer leaves its binary below the home directory,
		// unless the test says it leaves nothing.
		if !s.installerLeavesNothing {
			home, _ := os.UserHomeDir()
			s.Dirs[filepath.Join(home, ".local", "bin")] = []string{"mise"}
		}
		return nil, nil
	case name == "env":
		return nil, nil
	}
	return nil, fmt.Errorf("%s: %w", key, nativetest.ErrNotRecorded)
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
		var override bytes.Buffer
		for _, a := range argv[3:] {
			option, value, ok := strings.Cut(a, "=")
			if dot := strings.LastIndexByte(option, '.'); ok && dot > 0 {
				override.WriteString("[" + option[:dot] + "]\n" + option[dot+1:] + "=" + value + "\n")
			}
		}
		if override.Len() != 0 {
			s.Dirs[inspect.RepoOverride] = []string{"99-config_manager.repo"}
			s.Files[filepath.Join(inspect.RepoOverride, "99-config_manager.repo")] = override.Bytes()
		}
	case argv[0] == "dnf5" && argv[1] == "-y" && argv[2] == "remove":
		var kept []string
		for _, n := range s.installed {
			if !slices.Contains(argv[4:], n) {
				kept = append(kept, n)
			}
		}
		s.installed = kept
	case argv[0] == "dnf5" && argv[1] == "config-manager" && argv[2] == "addrepo":
		var file bytes.Buffer
		file.WriteString("[nimbus-terra]\nenabled=1\n")
		for _, a := range argv[4:] {
			if value, ok := strings.CutPrefix(a, "--set="); ok {
				file.WriteString(value + "\n")
			}
		}
		s.Dirs[inspect.RepoDir] = []string{"nimbus-terra.repo"}
		s.Files[filepath.Join(inspect.RepoDir, "nimbus-terra.repo")] = file.Bytes()
	case argv[0] == "flatpak" && argv[1] == "remote-add":
		s.remotes = append(s.remotes, argv[5])
		s.Files[filepath.Join(inspect.FlatpakRepoPath, "config")] = []byte("[remote \"" + argv[5] + "\"]\ngpg-verify=true\n")
	case argv[0] == "flatpak" && argv[1] == "install":
		s.apps = append(s.apps, argv[5])
	}
	return nil, nil
}

func (s *scripted) ReadFile(path string) ([]byte, error) {
	if data, ok := s.Files[path]; ok {
		return data, nil
	}
	if path == inspect.OSReleasePath {
		return []byte("ID=fedora\nVERSION_ID=44\n"), nil
	}
	return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
}

func (s *scripted) ran(prefix string) bool {
	return slices.ContainsFunc(s.log, func(l string) bool { return strings.HasPrefix(l, prefix) })
}

func terra() definitions.Repository {
	return definitions.Repository{Kind: "dnf", BaseURL: "https://repos.fyralabs.com/terra44", KeyURL: "https://repos.fyralabs.com/terra44/key.asc",
		Key: "AE09 157A 4DE8 8B49 7EA1 D5D3 00CD AB43 DE22 6D6F", Priority: new(100)}
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
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install 1", Paths: []string{"profile:common"}, Items: []string{"dnf:ripgrep"}, ItemPaths: map[string][]string{"dnf:ripgrep": {"profile:common"}}, Transaction: tx,
			Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "install", "ripgrep"}, Privileged: true}}},
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
	if strings.Join(r.Executed, ",") != "repository:terra,flatpak-remote:flathub,package:dnf:bash,packages:install,flatpak:com.spotify.Client" || len(r.Pending) != 0 {
		t.Fatalf("executed %v pending %v", r.Executed, r.Pending)
	}
	for _, want := range []string{
		"sudo install -m 0644 ", "sudo rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus-terra", "sudo dnf5 config-manager addrepo --id=nimbus-terra",
		"sudo flatpak remote-add --if-not-exists --system --from flathub ", "sudo dnf5 -y install ripgrep",
		"sudo flatpak install --system --noninteractive flathub com.spotify.Client", "gpg --no-options --homedir /dev/null ",
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
	if a.Baseline == nil || !a.InBaseline("coreutils.x86_64") || a.InBaseline("ripgrep.x86_64") {
		t.Fatalf("baseline = %+v", a.Baseline)
	}
}

func TestRunCancellationKeepsCompletedReceiptsAndStopsFurtherOperations(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts.Context = ctx
	record := opts.Record
	opts.Record = func(digest string, st *state.Stage) error {
		err := record(digest, st)
		cancel()
		return err
	}
	r := Run(samplePlan(t), opts)
	if r.Error != context.Canceled.Error() || !slices.Equal(r.Executed, []string{"repository:terra"}) || src.ran("sudo flatpak") {
		t.Fatalf("canceled run continued: %+v; commands %v", r, src.log)
	}
	a, err := state.Read(root)
	if err != nil || len(a.Receipts) != 1 || !a.Receipts["repository:terra"].Verified {
		t.Fatalf("completed receipt lost: %+v, %v", a, err)
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

func TestIncompletePlanIsRefusedAndOwnedRemovalRetiresReceipts(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	p := &plan.Plan{Machine: "desktop", Complete: false, Digest: "sha256:x", Operations: []plan.Operation{{ID: "repository:terra", Blocked: "foreign"}}}
	if r := Run(p, opts); r.Error == "" || len(r.Executed) != 0 {
		t.Fatalf("incomplete plan ran: %+v", r)
	}

	src.installed = append(src.installed, "old")
	if err := state.Record(root, "sha256:earlier", &state.Stage{Schema: state.Schema, PlanDigest: "sha256:earlier", Receipts: []state.Receipt{{Schema: state.ReceiptSchema, Resource: "package:dnf:old", Provider: "dnf", Operation: "install", PlanDigest: "sha256:earlier", Verified: true}}}); err != nil {
		t.Fatal(err)
	}
	opts.FirstApply = false
	p = &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:rm", Operations: []plan.Operation{
		{ID: "packages:remove-owned", Kind: plan.KindPackage, Action: plan.ActionRemove, Summary: "remove old", Paths: []string{"package:dnf:old"},
			Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "remove", "--no-autoremove", "old"}, Privileged: true}}},
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
	for _, kind := range []string{plan.KindPackage, plan.KindFlatpak} {
		t.Run(kind, func(t *testing.T) {
			id, provider := "package:dnf:gone", "dnf"
			if kind == plan.KindFlatpak {
				id, provider = "flatpak:org.example.Gone", "flatpak"
			}
			src := newScripted()
			root := t.TempDir()
			opts := options(t, src, root)
			opts.FirstApply = false
			if err := state.Record(root, "sha256:earlier", &state.Stage{Schema: state.Schema, PlanDigest: "sha256:earlier", Receipts: []state.Receipt{{Schema: state.ReceiptSchema, Resource: id, Provider: provider, Operation: "install", PlanDigest: "sha256:earlier", Verified: true}}}); err != nil {
				t.Fatal(err)
			}
			p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:retire", Operations: []plan.Operation{
				{ID: id, Kind: kind, Action: plan.ActionRetire, Summary: "retire gone"},
			}}
			r := Run(p, opts)
			if r.Error != "" || src.ran("sudo ") {
				t.Fatalf("retire ran a command or failed: %+v\n%s", r, strings.Join(src.log, "\n"))
			}
			a, _ := state.Read(root)
			if _, ok := a.Receipts[id]; ok {
				t.Fatal("receipt not retired")
			}
		})
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
	if a.Baseline == nil || !a.InBaseline("bash.x86_64") {
		t.Fatalf("baseline = %+v", a.Baseline)
	}
	if a.InBaseline("ripgrep.x86_64") || a.InBaseline("libfoo.x86_64") {
		t.Fatalf("packages installed by the run are in the baseline: %v", a.Baseline.Packages)
	}
}

type failingProgressWriter struct {
	prefix string
	err    error
}

func (w failingProgressWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), w.prefix) {
		return 0, w.err
	}
	return len(p), nil
}

func TestProgressFailureStopsBeforeNativeExecution(t *testing.T) {
	for _, kind := range []string{"operation", "privileged command", "user command", "installer digest"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			src := newScripted()
			opts := options(t, src, t.TempDir())
			opts.FirstApply = false
			cause := errors.New("output unavailable")
			prefix := "-> "
			op := plan.Operation{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Items: []string{"dnf:ripgrep"},
				Transaction: &plan.Transaction{Packages: []plan.TxPackage{{Name: "ripgrep", Arch: "x86_64", EVR: "1-1.fc44", Repository: "fedora", Section: "installing"}}},
				Steps:       []plan.Step{{Argv: []string{"dnf5", "-y", "install", "ripgrep"}}}}
			switch kind {
			case "privileged command":
				prefix = "   $ sudo "
			case "user command":
				prefix = "   $ env "
				op = plan.Operation{ID: "user:tools", Kind: plan.KindUser, Action: plan.ActionInstall, Steps: []plan.Step{{Argv: []string{"env", "tool", "install"}}}}
			case "installer digest":
				prefix = "   downloaded "
				op = plan.Operation{ID: "user:mise", Kind: plan.KindUser, Action: plan.ActionInstall, Steps: []plan.Step{{Description: "download https://mise.run to the stage directory and show its sha256"}, {Argv: []string{"sh", plan.InstallerScript}}}}
				opts.Fetch = func(string) ([]byte, error) { return []byte("#!/bin/sh\n"), nil }
			}
			opts.Out = failingProgressWriter{prefix: prefix, err: cause}
			recorded := false
			opts.Record = func(string, *state.Stage) error { recorded = true; return nil }
			result := Run(&plan.Plan{Complete: true, Operations: []plan.Operation{op}}, opts)
			if !strings.Contains(result.Error, cause.Error()) || result.Failed != op.ID || len(result.Failures) != 1 || len(result.Executed) != 0 || recorded {
				t.Fatalf("output failure lost or recorded: %+v recorded=%t", result, recorded)
			}
			if src.ran("sudo ") || src.ran("env ") || src.ran("sh ") {
				t.Fatalf("native execution after output failure: %v", src.log)
			}
		})
	}
}

func TestDiagnosticWriteFailurePreservesPrimaryOperationError(t *testing.T) {
	for _, kind := range []string{"execution", "record"} {
		t.Run(kind, func(t *testing.T) {
			src := newScripted()
			opts := options(t, src, t.TempDir())
			opts.FirstApply = false
			prefix := "   failed: "
			const primary = "primary operation failure"
			if kind == "execution" {
				src.fail["sudo dnf5"] = primary
			} else {
				prefix = "   operation applied and verified"
				opts.Record = func(string, *state.Stage) error { return errors.New(primary) }
			}
			opts.Out = failingProgressWriter{prefix: prefix, err: errors.New("secondary output failure")}
			op := plan.Operation{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Items: []string{"dnf:ripgrep"},
				Transaction: &plan.Transaction{Packages: []plan.TxPackage{{Name: "ripgrep", Arch: "x86_64", EVR: "1-1.fc44", Repository: "fedora", Section: "installing"}}},
				Steps:       []plan.Step{{Argv: []string{"dnf5", "-y", "install", "ripgrep"}}}}
			result := Run(&plan.Plan{Complete: true, Operations: []plan.Operation{op}}, opts)
			if !strings.Contains(result.Error, primary) || len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Error, primary) || len(result.Executed) != 0 {
				t.Fatalf("primary failure lost: %+v", result)
			}
		})
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type previewErrorWriter struct {
	after int
	err   error
}

type streamErrorSource struct {
	facts.Source
	err error
}

type resultErrorWriter struct {
	match string
	err   error
	out   io.Writer
}

func (w resultErrorWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.match) {
		return 0, w.err
	}
	if w.out != nil {
		return w.out.Write(p)
	}
	return len(p), nil
}

func TestSyncStopsWhenUpgradeAnnouncementFails(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=[]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			failedOutput := resultErrorWriter{match: "-> upgrade the system", err: syscall.ENOSPC, out: &report}
			cmd := newSync(&options{json: asJSON})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"--checkout", root, "--machine", "vm", "--yes"})
			cmd.SetOut(failedOutput)
			cmd.SetErr(io.Discard)
			if asJSON {
				cmd.SetOut(&report)
				cmd.SetErr(resultErrorWriter{match: "-> upgrade the system", err: syscall.ENOSPC})
			}
			if err := cmd.Execute(); err == nil {
				t.Error("upgrade announcement failure was ignored")
			}
			if len(src.calls) != 0 {
				t.Errorf("native mutation after failed upgrade announcement: %v", src.calls)
			}
			if asJSON {
				var env struct{ Data syncResult }
				if err := json.Unmarshal(report.Bytes(), &env); err != nil {
					t.Fatal(err)
				}
				if env.Data.Upgraded || env.Data.Failed != "upgrade" || !strings.Contains(env.Data.Error, syscall.ENOSPC.Error()) {
					t.Errorf("incorrect upgrade result: %+v", env.Data)
				}
			} else if !strings.Contains(report.String(), "failed     upgrade") || !strings.Contains(report.String(), syscall.ENOSPC.Error()) {
				t.Errorf("upgrade output failure missing from closing report: %s", &report)
			}
		})
	}
}

func TestSyncReportsFailedReadOnlyResults(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		flags        syncFlags
	}{
		{"plan", "plan for vm", syncFlags{plan: true}},
		{"cache note", "from the local metadata cache", syncFlags{plan: true}},
		{"unchanged", "nothing to do;", syncFlags{noUpgrade: true}},
		{"waiting plan", "plan for vm", syncFlags{noUpgrade: true}},
		{"waiting reason", "operations wait for", syncFlags{noUpgrade: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			if !strings.HasPrefix(tc.name, "waiting ") {
				if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\ncomponents=[\"runtime\"]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "components/runtime.toml"), []byte("schema=1\nid=\"runtime\"\n[installer]\nurl=\"https://example.invalid/install.sh\"\nbinary=\".local/bin/runtime\"\nconfig=\".config/runtime.toml\"\ninstall=[\"<home>/.local/bin/runtime\",\"install\"]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				src.Dirs[filepath.Join(os.Getenv("HOME"), ".local/bin")] = []string{"runtime"}
			}
			var report bytes.Buffer
			cmd := New()
			cmd.SetOut(resultErrorWriter{match: tc.output, err: syscall.ENOSPC, out: &report})
			cmd.SetErr(io.Discard)
			err := runSync(cmd, &options{}, machineFlags{checkout: root, machine: "vm"}, tc.flags)
			if err == nil || tc.flags.plan && !errors.Is(err, syscall.ENOSPC) || !tc.flags.plan && !strings.Contains(report.String(), syscall.ENOSPC.Error()) {
				t.Fatalf("result output failure not reported: %v\n%s", err, &report)
			}
			if len(src.calls) != 0 {
				t.Fatalf("read-only result ran native mutations: %v", src.calls)
			}
			if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read-only result wrote state: %v", err)
			}
		})
	}
}

func (s streamErrorSource) Stream(io.Writer, io.Writer, string, ...string) error {
	return s.err
}

func (w *previewErrorWriter) Write(p []byte) (int, error) {
	if w.after == 0 {
		return 0, w.err
	}
	if w.after > 0 {
		w.after--
	}
	return len(p), nil
}

func TestApprovalRejectsFailedPromptWithoutReading(t *testing.T) {
	in := strings.NewReader("yes\n")
	if approver(in, &previewErrorWriter{err: syscall.ENOSPC}, "") {
		t.Fatal("approved after the prompt could not be shown")
	}
	if in.Len() != len("yes\n") {
		t.Fatal("read an answer after the prompt failed")
	}
}

func TestSyncRejectsFailedPreview(t *testing.T) {
	for _, mode := range []string{"interactive", "yes", "prompt"} {
		t.Run(mode, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := newSync(&options{})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			args := []string{"--checkout", root, "--machine", "vm"}
			if mode == "yes" {
				args = append(args, "--yes")
			}
			out := &previewErrorWriter{err: syscall.ENOSPC}
			if mode == "prompt" {
				out.after = 1
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader("yes\n"))
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("output failure = %v", err)
			}
			if len(src.calls) != 0 {
				t.Fatalf("ran native mutations after failed preview: %v", src.calls)
			}
			if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("state written after failed preview: %v", err)
			}
		})
	}
}

func TestSyncStopsWhenReplannedOutputFails(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			jsonOutput := mode == "json"
			root, src := installerFixture(t)
			config := filepath.Join(root, "nimbus.toml")
			data, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, append(data, []byte("\n[dnf]\ndefaultyes=true\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[\"demo\"]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			checkout, err := loadCheckout(root)
			if err != nil {
				t.Fatal(err)
			}
			src.Files[facts.DNFDropInPath] = []byte(plan.DNFDropIn(checkout.Definitions()))
			src.Commands["dnf5 --assumeno --cacheonly install demo"] = []byte("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n demo x86_64 1-1 fedora 1 KiB\n\nTransaction Summary:\n")
			out := &previewErrorWriter{after: -1, err: syscall.ENOSPC}
			saved := newRecorder
			t.Cleanup(func() { newRecorder = saved })
			newRecorder = func(source facts.Source, stage string) func(string, *state.Stage) error {
				record := saved(source, stage)
				return func(digest string, st *state.Stage) error {
					if err := record(digest, st); err != nil {
						return err
					}
					out.after = 0
					return nil
				}
			}
			var report bytes.Buffer
			cmd := newSync(&options{json: jsonOutput})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"--checkout", root, "--machine", "vm", "--no-upgrade", "--yes"})
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			if jsonOutput {
				cmd.SetOut(&report)
				cmd.SetErr(out)
			}
			err = cmd.Execute()
			if jsonOutput {
				if !errors.Is(err, reported{}) || !strings.Contains(report.String(), "show updated plan") || !strings.Contains(report.String(), `"dnf:config"`) {
					t.Fatalf("completed operation or preview failure missing: %v %s", err, &report)
				}
			} else if !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("output failure = %v", err)
			}
			if slices.Contains(src.calls, "sudo dnf5 -y install demo") {
				t.Fatalf("installed packages after failed replan output: %v", src.calls)
			}
			applied, err := state.Read(stateRoot)
			if err != nil || len(applied.Receipts) != 1 {
				t.Fatalf("expected completed source operation receipt: %+v %v", applied, err)
			}
		})
	}
}

func TestSyncPreservesFailureWhenReportCannotBeWritten(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			withSource(t, streamErrorSource{src, errors.New("native upgrade failed")})
			cmd := newSync(&options{json: mode == "json"})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"--checkout", root, "--machine", "vm", "--yes"})
			cmd.SetOut(resultErrorWriter{match: "\nsync summary:\n", err: syscall.ENOSPC})
			cmd.SetErr(io.Discard)
			if mode == "json" {
				cmd.SetOut(&previewErrorWriter{err: syscall.ENOSPC})
			}
			err := cmd.Execute()
			if !errors.Is(err, syscall.ENOSPC) || errors.Is(err, reported{}) || !strings.Contains(err.Error(), "native upgrade failed") {
				t.Fatalf("native or report failure lost: %v", err)
			}
		})
	}
}

func applyEnv(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("NIMBUS_INSTALL_LOG_DIR", "")
	saved := stateRoot
	stateRoot = filepath.Join(t.TempDir(), "state")
	t.Cleanup(func() { stateRoot = saved })
	savedRec := newRecorder
	newRecorder = func(facts.Source, string) func(string, *state.Stage) error {
		return func(d string, st *state.Stage) error { return state.Record(stateRoot, d, st) }
	}
	t.Cleanup(func() { newRecorder = savedRec })
	savedSudo := sudoKeepalive
	sudoKeepalive = func(facts.Source, io.Writer, io.Writer) (func(), error) { return func() {}, nil }
	t.Cleanup(func() { sudoKeepalive = savedSudo })
	savedFetch := newFetcher
	newFetcher = func() func(string) ([]byte, error) {
		return func(url string) ([]byte, error) { return nil, errors.New("network is not available in tests: " + url) }
	}
	t.Cleanup(func() { newFetcher = savedFetch })
	return root
}

func TestSyncAsksOnceAndDeclinesCleanly(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	readyRepositories(t, src, root)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	saved := approver
	approver = func(_ io.Reader, _ io.Writer, _ string) bool { return false }
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "sync", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "not applied") {
		t.Fatalf("declined prompt: %d %q", code, errOut)
	}
	// The plan is on screen before the question, in installer terms.
	for _, want := range []string{"plan for laptop", "install ", "packages"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state changed although the question was declined: %v", err)
	}
}

func TestSyncIncompletePlanIsRefused(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withForeignTerra(t, src) // Terra's own file is not Nimbus's, so its repository blocks
	withSource(t, src)
	code, out, errOut := run(t, "sync", "-y", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "problems") || !strings.Contains(out, "problems:") || !strings.Contains(out, "terra.repo") {
		t.Fatalf("incomplete: %d %q\n%s", code, errOut, out)
	}
}

func TestSyncStopsAtTheFirstFailedOperation(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	answerLaptopInstall(t, src, root)
	key := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "stage", "key-brave.asc")
	if src.Failures == nil {
		src.Failures = map[string]string{}
	}
	src.Failures[facts.Key("gpg", facts.KeyInspectArgs(key)...)] = "key inspection failed"
	withSource(t, src)
	code, out, _ := run(t, "sync", "-y", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "plan for laptop") || !strings.Contains(out, "failed     repository:brave") || !strings.Contains(out, "key inspection failed") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// The drop-in was adopted before brave failed, so state exists; the
	// failed repository must have no receipt.
	applied, err := state.Read(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := applied.Receipts["repository:brave"]; ok {
		t.Fatal("a receipt was written for the failed operation")
	}
	if !applied.Receipts["dnf:config"].Verified {
		t.Fatal("the completed drop-in adoption has no verified receipt")
	}
	lock, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"))
	if !strings.Contains(string(lock), `"command":"sync"`) {
		t.Fatalf("lock content = %q", lock)
	}
}

func TestSyncJSONReportsFailureWithExitOne(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	code, out, _ := run(t, "sync", "-n", "--checkout", root, "--machine", "laptop", "--json")
	if code != ExitFailure || !strings.Contains(out, `"failed": "repository:brave"`) {
		t.Fatalf("json failure: %d\n%s", code, out)
	}
}

func TestSourceOperationsComeFirstAndOnlyRepositoriesNeedARefresh(t *testing.T) {
	p := &plan.Plan{Operations: []plan.Operation{
		{ID: "dnf:config", Kind: plan.KindDNFConfig, Action: plan.ActionInstall},
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable},
		{ID: "repository:docker", Kind: plan.KindRepository, Action: plan.ActionRepair, Blocked: "foreign file"},
		{ID: "flatpak-remote:flathub", Kind: plan.KindFlatpakRemote, Action: plan.ActionEnable, After: "packages:install"},
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Items: []string{"dnf:bat"}, After: "repository:terra"},
		{ID: "dnf:other", Kind: plan.KindDNFConfig, Action: plan.ActionKeep},
	}}
	var ids []string
	for _, op := range sourceOperations(p) {
		ids = append(ids, op.ID)
	}
	if strings.Join(ids, " ") != "dnf:config repository:terra" {
		t.Fatalf("source operations = %v", ids)
	}
	if changedRepositories([]string{"dnf:config", "packages:install"}) || !changedRepositories([]string{"repository:terra"}) {
		t.Fatal("repository change detection is wrong")
	}
}

func TestSyncStopsWhenThePlanChangesWhileTheQuestionIsOpen(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	readyRepositories(t, src, root)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	saved := approver
	approver = func(_ io.Reader, _ io.Writer, _ string) bool {
		// Another run finished meanwhile: a desired package is now installed.
		src.Commands[facts.Key("dnf5", facts.PackageQueryArgs...)] = append(src.Commands[facts.Key("dnf5", facts.PackageQueryArgs...)], []byte("ripgrep|0|15.2.0|1.fc44|x86_64|updates|User\n")...)
		return true
	}
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "sync", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "changed while the question was open") {
		t.Fatalf("stale plan ran: %d %q", code, errOut)
	}
}

func TestSelectionCommandsKeepJSONOnStdout(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	readyRepositories(t, src, root)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	code, out, errOut := run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop", "--json")
	if code != ExitFailure || !strings.HasPrefix(strings.TrimSpace(out), "{") || !strings.Contains(out, `"failed"`) {
		t.Fatalf("stdout is not one envelope: %d\n%s", code, out)
	}
	if !strings.Contains(errOut, "components add: add components docker") || !strings.Contains(errOut, "plan for laptop") {
		t.Fatalf("the review text must go to stderr:\n%s", errOut)
	}
}

func TestSyncShowsNewlyResolvedErasureBeforeExecuting(t *testing.T) {
	root, src := installerFixture(t)
	config := filepath.Join(root, "nimbus.toml")
	data, _ := os.ReadFile(config)
	if err := os.WriteFile(config, append(data, []byte("\n[dnf]\ndefaultyes=true\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[\"demo\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	checkout, err := loadCheckout(root)
	if err != nil {
		t.Fatal(err)
	}
	src.Files[facts.DNFDropInPath] = []byte(plan.DNFDropIn(checkout.Definitions()))
	preview := "dnf5 --assumeno --cacheonly install demo"
	initial := "Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n demo x86_64 1-1 fedora 1 KiB\n"
	src.Commands[preview] = []byte(initial + "\nTransaction Summary:\n")
	saved := newRecorder
	t.Cleanup(func() { newRecorder = saved })
	newRecorder = func(source facts.Source, stage string) func(string, *state.Stage) error {
		record := saved(source, stage)
		return func(digest string, st *state.Stage) error {
			if err := record(digest, st); err != nil {
				return err
			}
			src.Commands[preview] = []byte(initial + "Removing:\n unexpected-app x86_64 1-1 @System 1 KiB\n\nTransaction Summary:\n")
			return nil
		}
	}
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "-n", "-y")
	// The fake rejects the eventual package mutation; the post-preparation plan
	// must already have exposed the newly resolved removal and retained its note.
	if code != ExitFailure || !strings.Contains(out, "updated plan after completed operations:") || !strings.Contains(out, "unexpected-app") || !strings.Contains(out, "replanned packages:install:") {
		t.Fatalf("unseen replan: %d %s%s", code, out, errOut)
	}
	if strings.Index(out, "unexpected-app") > strings.Index(out, "$ sudo dnf5 -y install demo") {
		t.Fatal("erasure was shown only after execution")
	}
}

func TestSyncTracksCheckoutIdentityAcrossApprovalAndReplanning(t *testing.T) {
	for _, phase := range []string{"approval", "replan"} {
		for _, field := range []string{"commit", "origin", "dirty"} {
			t.Run(phase+"/"+field, func(t *testing.T) {
				root, src := installerFixture(t)
				rootFile := filepath.Join(root, "nimbus.toml")
				data, err := os.ReadFile(rootFile)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(rootFile, append(data, []byte("\n[dnf]\nmax_parallel_downloads = 10\n")...), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema = 1\nid = \"common\"\npackages = []\n"), 0644); err != nil {
					t.Fatal(err)
				}
				selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				src.Files[facts.DNFDropInPath] = []byte(plan.DNFDropIn(selected.Checkout.Definitions()))
				change := func() {
					switch field {
					case "commit":
						src.Commands[facts.Key("git", facts.GitArgs(root, "rev-parse", "HEAD")...)] = []byte("changed-head\n")
					case "origin":
						src.Files[filepath.Join(root, ".git/config")] = []byte("[remote \"origin\"]\nurl=https://example.invalid/other\n")
					case "dirty":
						src.Commands[facts.Key("git", facts.GitArgs(root, "status", "--porcelain")...)] = []byte(" M README.md\n")
					}
				}
				saved := approver
				t.Cleanup(func() { approver = saved })
				asked := false
				approver = func(io.Reader, io.Writer, string) bool {
					asked = true
					if phase == "approval" {
						change()
					}
					return true
				}
				if phase == "replan" {
					record := newRecorder
					newRecorder = func(source facts.Source, stage string) func(string, *state.Stage) error {
						save := record(source, stage)
						return func(digest string, st *state.Stage) error { err := save(digest, st); change(); return err }
					}
				}
				code, out, errOut := run(t, "sync", "-n", "--checkout", root, "--machine", "vm")
				if !asked {
					t.Fatal("approval not reached")
				}
				if field == "dirty" {
					if code != ExitOK {
						t.Fatalf("dirty metadata prevented sync: %d %s%s", code, out, errOut)
					}
					if phase == "approval" {
						applied, err := state.Read(stateRoot)
						if err != nil || !applied.Receipts["dnf:config"].Definitions.Dirty {
							t.Fatalf("stale dirty state: %+v %v", applied, err)
						}
					}
				} else {
					if code != ExitFailure || !strings.Contains(out, "checkout identity changed") || len(src.calls) > 0 {
						t.Fatalf("identity change accepted: %d %s%s calls=%v", code, out, errOut, src.calls)
					}
					if phase == "approval" {
						if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("state written before identity check: %v", err)
						}
					}
				}
			})
		}
	}
}

func TestSyncIncompleteJSONNamesBlockedOperation(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withForeignTerra(t, src)
	withSource(t, src)
	code, out, errOut := run(t, "sync", "-n", "--json", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "terra.repo") {
		t.Fatalf("blocked reason missing: %d %s%s", code, out, errOut)
	}
}

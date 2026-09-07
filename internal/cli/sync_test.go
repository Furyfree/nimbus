package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

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
	withoutTerra(src)
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
	if _, err := os.Stat(filepath.Join(stateRoot, "receipts", "package_dnf_ripgrep.json")); err == nil {
		t.Fatal("a package was recorded although the question was declined")
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
	withoutTerra(src)
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
	if _, err := os.Stat(filepath.Join(stateRoot, "receipts", "repository_brave.json")); err == nil {
		t.Fatal("a receipt was written for the failed operation")
	}
	lock, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"))
	if !strings.Contains(string(lock), `"command":"sync"`) {
		t.Fatalf("lock content = %q", lock)
	}
}

func TestSyncJSONReportsFailureWithExitOne(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withoutTerra(src)
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

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
	withSource(t, src)
	code, out, _ := run(t, "sync", "-y", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "plan for laptop") || !strings.Contains(out, "failed     repository:brave") || !strings.Contains(out, "network is not available") {
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

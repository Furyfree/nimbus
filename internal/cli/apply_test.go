package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
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
	savedFetch := newFetcher
	newFetcher = func() func(string) ([]byte, error) {
		return func(url string) ([]byte, error) { return nil, errors.New("network is not available in tests: " + url) }
	}
	t.Cleanup(func() { newFetcher = savedFetch })
	return root
}

func currentDigest(t *testing.T, root string) string {
	t.Helper()
	_, out, _ := run(t, "plan", "--checkout", root, "--machine", "laptop", "--json")
	var env struct {
		Data struct {
			Digest string `json:"digest"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data.Digest
}

func TestApplyRefusesWithoutApprovalOrWithTheWrongDigest(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withoutTerra(src)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	if code, _, errOut := run(t, "apply", "--checkout", root, "--machine", "laptop", "--json"); code != ExitUsage || !strings.Contains(errOut, "--approve") {
		t.Fatalf("json without approve: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "apply", "--checkout", root, "--machine", "laptop", "--approve", "sha256:wrong"); code != ExitFailure || !strings.Contains(errOut, "does not match the current plan") {
		t.Fatalf("wrong digest: %d %q", code, errOut)
	}
	saved := approver
	approver = func(_ io.Reader, _ io.Writer, _ string) bool { return false }
	t.Cleanup(func() { approver = saved })
	if code, _, errOut := run(t, "apply", "--checkout", root, "--machine", "laptop"); code != ExitFailure || !strings.Contains(errOut, "not approved") {
		t.Fatalf("declined prompt: %d %q", code, errOut)
	}
	if _, err := os.Stat(stateRoot); err == nil {
		t.Fatal("state written without approval")
	}
}

func TestApplyIncompletePlanIsRefused(t *testing.T) {
	root := applyEnv(t)
	withSource(t, fixtureSource(t, root)) // the install preview is not recorded, so the plan blocks
	code, out, errOut := run(t, "apply", "--checkout", root, "--machine", "laptop", "--approve", "sha256:any")
	if code != ExitFailure || !strings.Contains(errOut, "incomplete") || !strings.Contains(out, "blocked:") {
		t.Fatalf("incomplete: %d %q\n%s", code, errOut, out)
	}
}

func TestApplyStopsAtTheFirstFailedOperation(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withoutTerra(src)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	digest := currentDigest(t, root)
	code, out, _ := run(t, "apply", "--checkout", root, "--machine", "laptop", "--approve", digest)
	if code != ExitFailure || !strings.Contains(out, "apply stopped at repository:brave") || !strings.Contains(out, "network is not available") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, state.SchemaFile)); err == nil {
		t.Fatal("state written although the first operation failed")
	}
	lock, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"))
	if !strings.Contains(string(lock), `"command":"apply"`) {
		t.Fatalf("lock content = %q", lock)
	}
}

func TestApplyJSONReportsFailureWithExitOne(t *testing.T) {
	root := applyEnv(t)
	src := fixtureSource(t, root)
	withoutTerra(src)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	digest := currentDigest(t, root)
	code, out, _ := run(t, "apply", "--checkout", root, "--machine", "laptop", "--approve", digest, "--json")
	if code != ExitFailure || !strings.Contains(out, `"failed": "repository:brave"`) {
		t.Fatalf("json failure: %d\n%s", code, out)
	}
}

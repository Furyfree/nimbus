package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every CLI test, including delegated-command tests, must start outside the
// workstation's home and XDG state. Individual fixtures can further isolate it.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "nimbus-cli-tests-")
	if err != nil {
		panic(err)
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		path := filepath.Join(root, key)
		if err := os.Mkdir(path, 0700); err != nil {
			panic(err)
		}
		if err := os.Setenv(key, path); err != nil {
			panic(err)
		}
	}
	for _, key := range []string{"NIMBUS_INSTALL_LOG_DIR", maintenanceReport, upgradeActive, engineRestart, "NIMBUS_UPGRADE_YES", "NIMBUS_UPGRADE_VERBOSE"} {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	if err := os.RemoveAll(root); err != nil {
		panic(err)
	}
	os.Exit(code)
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRootHelpListsOnlyDeliveredCommands(t *testing.T) {
	code, out, _ := run(t)
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if code, flagHelp, errOut := run(t, "--help"); code != ExitOK || flagHelp != out {
		t.Fatalf("--help differs from bare help: exit %d\n%s%s", code, flagHelp, errOut)
	}
	for _, want := range []string{"doctor", "init", "launch", "postinstall", "status", "sync", "upgrade", "validate", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %s:\n%s", want, out)
		}
	}
	_, commands, found := strings.Cut(out, "Available Commands:")
	if !found {
		t.Fatalf("help lacks an Available Commands section:\n%s", out)
	}
	commands, _, _ = strings.Cut(commands, "\n\n")
	for _, stub := range []string{"help", "completion", "internal"} {
		if strings.Contains(commands, "  "+stub+" ") {
			t.Errorf("help exposes %s:\n%s", stub, out)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version", "--json")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.OutputSchema != 1 || env.Engine == "" {
		t.Fatalf("envelope = %+v", env)
	}
	if code, out, _ := run(t, "--version"); code != ExitOK || !strings.HasPrefix(out, "nimbus ") {
		t.Fatalf("--version: %d %q", code, out)
	}
}

func TestValidateRepositoryCheckout(t *testing.T) {
	root := filepath.Join("..", "..")
	code, out, errOut := run(t, "validate", "--checkout", root)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "machine desktop:") || !strings.HasSuffix(out, "ok\n") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	code, out, _ = run(t, "validate", "--checkout", root, "--json")
	if code != ExitOK {
		t.Fatalf("json exit %d", code)
	}
	var env struct {
		OutputSchema int `json:"output_schema"`
		Data         struct {
			Digest   string `json:"digest"`
			Machines []struct {
				Machine  string   `json:"machine"`
				Profiles []string `json:"profiles"`
			} `json:"machines"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(env.Data.Digest, "sha256:") || len(env.Data.Machines) != 3 || env.Data.Machines[0].Profiles[0] != "common" {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestValidateReportsErrorsWithExitOne(t *testing.T) {
	code, out, _ := run(t, "validate", "--checkout", t.TempDir())
	if code != ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "nimbus.toml: missing") {
		t.Fatalf("output:\n%s", out)
	}
	if code, _, errOut := run(t, "validate", "--bogus"); code != ExitUsage || !strings.Contains(errOut, "bogus") {
		t.Fatalf("usage error: %d %q", code, errOut)
	}
}

func TestUsageAndFailureExitCodes(t *testing.T) {
	if code, _, errOut := run(t, "nonsense"); code != ExitUsage || !strings.Contains(errOut, "nonsense") {
		t.Fatalf("unknown argument: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "version", "extra"); code != ExitUsage {
		t.Fatalf("version with an argument exited %d", code)
	}
	code, out, _ := run(t, "validate", "--checkout", filepath.Join(t.TempDir(), "absent"), "--json")
	if code != ExitFailure {
		t.Fatalf("missing checkout exited %d", code)
	}
	var env struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Errors) != 1 || !strings.Contains(env.Errors[0].Message, "resolve checkout") {
		t.Fatalf("json failure envelope: %v %s", err, out)
	}
}

func TestVersionFlagMatchesVersionCommand(t *testing.T) {
	_, a, _ := run(t, "version")
	_, b, _ := run(t, "--version")
	if a != b {
		t.Fatalf("--version differs from version:\n%s\n%s", a, b)
	}
	_, a, _ = run(t, "version", "--json")
	_, b, _ = run(t, "--version", "--json")
	if a != b {
		t.Fatalf("--version --json differs from version --json:\n%s\n%s", a, b)
	}
}

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/agentproxy"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

func agentProxyFixture(t *testing.T) string {
	t.Helper()
	root, _ := postinstallFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\ncomponents=['github-copilot']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentProxyPreviewNeverRunsAdapter(t *testing.T) {
	for _, mode := range []string{"help", "plan", "json", "nonterminal"} {
		t.Run(mode, func(t *testing.T) {
			root := agentProxyFixture(t)
			old := runAgentProxy
			t.Cleanup(func() { runAgentProxy = old })
			runAgentProxy = func(context.Context, string, string, io.Writer) (agentproxy.Result, error) {
				t.Fatal("preview invoked adapter")
				return agentproxy.Result{}, nil
			}
			args := []string{"agent-proxy", "--plan"}
			if mode == "help" {
				args = []string{"agent-proxy", "--help"}
			}
			if mode == "nonterminal" {
				args = []string{"agent-proxy"}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err := cmd.Execute()
			if mode == "nonterminal" {
				if err == nil {
					t.Fatal("nonterminal approved installation")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Copilot") {
				t.Fatal(out.String())
			}
		})
	}
}
func TestModelRefreshRequiresOptInAndPreservesDeferredResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _ = postinstallFixture(t)
	src := &nativetest.FakeSource{Files: map[string][]byte{}, Paths: map[string]string{"mise": "mise", "herdr": "herdr"}}
	old := runAgentProxy
	t.Cleanup(func() { runAgentProxy = old })
	calls := 0
	runAgentProxy = func(_ context.Context, mode, machine string, _ io.Writer) (agentproxy.Result, error) {
		calls++
		if mode != "refresh" || machine != "vm" {
			t.Fatal(mode, machine)
		}
		return agentproxy.Result{Status: "deferred", Detail: "Copilot is closed"}, nil
	}
	cmd, _ := postinstallCommand("", false, "status")
	var result syncResult
	if err := refreshAgentProxy(cmd, src, "vm", io.Discard, &result); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(result.Steps) != 0 {
		t.Fatal("unconfigured integration refreshed")
	}
	p, _ := agentproxy.StatePath()
	src.Files[p] = []byte(`{"version":1,"machine":"vm","configured":true,"providerId":"test"}`)
	src.Files[filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agent-proxy/config.yaml")] = []byte("fixture")
	src.Files[filepath.Join(os.Getenv("XDG_DATA_HOME"), "agent-proxy/current/VERSION")] = []byte("1.3.0-nimbus.2-source")
	if err := refreshAgentProxy(cmd, src, "vm", io.Discard, &result); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Steps) != 1 || result.Steps[0].Status != "deferred" {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestAgentProxyChoosesOnlyNeededOperations(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	key := nativetest.Key("chezmoi", "--skip-secrets", "status", "--exclude=scripts", filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agent-proxy/config.yaml"))
	for _, tt := range []struct {
		name, status string
		code         int
		reset        bool
		want         string
		fail         bool
	}{
		{name: "first setup", status: "not-configured", want: "setup"},
		{name: "configured", status: "configured", want: "refresh"},
		{name: "config changed", status: "configured", code: 1, want: "setup"},
		{name: "inspection failed", status: "configured", code: 2, fail: true},
		{name: "reset", status: "configured", reset: true, want: "disable-refresh"},
		{name: "bad state", status: "blocked", fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := &nativetest.FakeSource{Commands: map[string][]byte{key: nil}, ExitCodes: map[string]int{key: tt.code}}
			if tt.name == "config changed" {
				src.ExitCodes[key] = 0
				src.Commands[key] = []byte(" M .config/agent-proxy/config.yaml\n")
			}
			preview, mode, err := agentProxyOperation(src, agentproxy.Result{Status: tt.status, Detail: "unreadable state"}, tt.reset, false)
			if (err != nil) != tt.fail || mode != tt.want {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
			if mode == "refresh" {
				if !strings.Contains(preview, "already configured") || strings.Contains(preview, "install") || strings.Contains(preview, "Recovery") {
					t.Fatal(preview)
				}
			}
		})
	}
}

func TestAgentProxyResultsSeparateSkippedProvidersFromFailures(t *testing.T) {
	var out strings.Builder
	result := agentproxy.Result{Status: "succeeded", Detail: "13 models up to date. No changes needed.", Providers: []agentproxy.ProviderResult{
		{Provider: "codex", Status: "verified", Count: 6}, {Provider: "claude", Status: "verified", Count: 5}, {Provider: "grok", Status: "verified", Count: 2},
		{Provider: "agy", Status: "skipped", Reason: "not signed in"},
	}}
	if err := renderAgentProxy(&out, result); err != nil {
		t.Fatal(err)
	}
	want := "✓ Codex: 6 models\n✓ Claude: 5 models\n✓ Grok: 2 models\n- Antigravity: skipped - not signed in\n\n13 models up to date. No changes needed.\n"
	if out.String() != want {
		t.Fatalf("got:\n%s", out.String())
	}
	out.Reset()
	result.Status = "warning"
	result.Providers[3].Status = "failed"
	result.Providers[3].Reason = "could not read model list; existing entries preserved"
	if err := renderAgentProxy(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Warning: Antigravity") {
		t.Fatal(out.String())
	}
}

func TestAgentProxyUninstallChecksBeforeApproval(t *testing.T) {
	root := agentProxyFixture(t)
	oldCheck, oldTerminal, oldApprover, oldRun := checkAgentProxyUninstall, postinstallTerminal, approver, runAgentProxy
	t.Cleanup(func() {
		checkAgentProxyUninstall, postinstallTerminal, approver, runAgentProxy = oldCheck, oldTerminal, oldApprover, oldRun
	})
	checkAgentProxyUninstall = func(native.Source) error { return errors.New("unrecognized override: /tmp/foreign.conf") }
	postinstallTerminal = func(io.Reader) bool { return true }
	approver = func(io.Reader, io.Writer, string) bool { t.Fatal("asked before ownership was checked"); return false }
	runAgentProxy = func(context.Context, string, string, io.Writer) (agentproxy.Result, error) {
		t.Fatal("unsafe removal started")
		return agentproxy.Result{}, nil
	}
	for _, args := range [][]string{{"agent-proxy", "--uninstall"}, {"agent-proxy", "--uninstall", "--plan"}} {
		cmd, _ := postinstallCommand(root, false, args...)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "/tmp/foreign.conf") {
			t.Fatal(err)
		}
	}
}

func TestAgentProxyLiveStatusSurvivesDeselection(t *testing.T) {
	_, src := postinstallFixture(t)
	p, _ := agentproxy.StatePath()
	src.Files[p] = []byte(`{"version":1,"machine":"vm","configured":true,"providerId":"test"}`)
	src.Files[filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agent-proxy/config.yaml")] = []byte("fixture")
	src.Files[filepath.Join(os.Getenv("XDG_DATA_HOME"), "agent-proxy/current/VERSION")] = []byte("1.3.0-nimbus.2-source")
	src.Paths["mise"], src.Paths["herdr"] = "mise", "herdr"
	proxyShow := strings.Replace(zeronShow, "zeron.service", "agent-proxy.service", 1)
	herdrShow := strings.Replace(zeronShow, "zeron.service", "herdr.service", 1)
	absent := []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\nFragmentPath=\n")
	running := []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n")
	src.Commands[herdrShow] = running
	for _, test := range []struct{ state, label string }{
		{string(running), "Running"},
		{"LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled\n", "Stopped"},
		{"LoadState=loaded\nActiveState=failed\nUnitFileState=enabled\n", "Failed"},
		{string(absent), "Absent"},
	} {
		src.Commands[proxyShow] = []byte(test.state)
		task, present := inspectAgentProxyTask(src, "vm", false)
		if !present || task.CurrentState != test.label || !strings.Contains(task.Detail, "Copilot is not selected") {
			t.Fatalf("deselected installation hidden: %+v", task)
		}
		if (task.Status == postinstall.Complete) != (test.label == "Running") {
			t.Fatalf("registration masked current state: %+v", task)
		}
	}
	delete(src.Commands, proxyShow)
	task, present := inspectAgentProxyTask(src, "vm", false)
	if !present || task.Status != postinstall.Unknown || task.CurrentState != "" {
		t.Fatalf("unavailable service query trusted registration: %+v", task)
	}
	// A leftover native unit is visible even when registration has gone.
	delete(src.Files, p)
	src.Commands[proxyShow] = running
	if task, present := inspectAgentProxyTask(src, "vm", false); !present || task.Status == postinstall.Complete {
		t.Fatalf("unregistered service hidden or treated as owned: %+v", task)
	}
	src.Commands[proxyShow], src.Commands[herdrShow] = absent, absent
	if _, present := inspectAgentProxyTask(src, "vm", false); present {
		t.Fatal("absent deselected proxy exposed a task")
	}
	if len(src.streams) != 0 {
		t.Fatal("status changed native state", src.streams)
	}
}

func TestAgentProxyDeselectedUninstallKeepsOwnershipChecks(t *testing.T) {
	root, _ := postinstallFixture(t)
	oldCheck, oldRun := checkAgentProxyUninstall, runAgentProxy
	t.Cleanup(func() { checkAgentProxyUninstall, runAgentProxy = oldCheck, oldRun })
	checked, ran := 0, 0
	checkAgentProxyUninstall = func(native.Source) error { checked++; return nil }
	runAgentProxy = func(_ context.Context, mode, machine string, _ io.Writer) (agentproxy.Result, error) {
		ran++
		if mode != "uninstall" || machine != "vm" {
			t.Fatal(mode, machine)
		}
		return agentproxy.Result{Status: "succeeded", Detail: "Removed owned proxy"}, nil
	}
	for _, args := range [][]string{{"agent-proxy", "--uninstall", "--plan"}, {"agent-proxy", "--uninstall", "--yes"}} {
		cmd, _ := postinstallCommand(root, false, args...)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	if checked != 2 || ran != 1 {
		t.Fatalf("ownership checks=%d runs=%d", checked, ran)
	}
	checkAgentProxyUninstall = func(native.Source) error { return errors.New("foreign unit") }
	cmd, _ := postinstallCommand(root, false, "agent-proxy", "--uninstall", "--yes")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "foreign unit") || ran != 1 {
		t.Fatalf("ownership bypass: %v, runs=%d", err, ran)
	}
	cmd, _ = postinstallCommand(root, false, "agent-proxy", "--yes")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "requires the selected") || ran != 1 {
		t.Fatalf("deselected setup allowed: %v, runs=%d", err, ran)
	}
}

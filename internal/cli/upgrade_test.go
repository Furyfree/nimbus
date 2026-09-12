package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
)

func fakeTopgrade(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PATH", home)
	t.Setenv(upgradeActive, "")
	if err := os.WriteFile(filepath.Join(home, "topgrade"), []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeDelegatesInputOutputAndArguments(t *testing.T) {
	fakeTopgrade(t, `test "$NIMBUS_UPGRADE_ACTIVE" = 1 || exit 99
read -r answer
printf 'answer=%s\n' "$answer"
printf '<%s>\n' "$@"
printf 'native stderr\n' >&2
`)
	cmd := New()
	cmd.SetArgs([]string{"upgrade", "--", "--config", "/tmp/config with spaces", "$(touch bad)"})
	cmd.SetIn(strings.NewReader("yes please\n"))
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "answer=yes please\n<--config>\n</tmp/config with spaces>\n<$(touch bad)>\n" || errOut.String() != "native stderr\n" {
		t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
	}
}

func TestUpgradePreservesNativeFailuresAndSignals(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
	}{
		{"exit 0", 0}, {"exit 23", 23}, {"kill -INT $$", 130},
	} {
		t.Run(tc.body, func(t *testing.T) {
			fakeTopgrade(t, tc.body)
			if code, _, _ := run(t, "upgrade"); code != tc.code {
				t.Fatalf("exit %d, want %d", code, tc.code)
			}
		})
	}
}

func TestUpgradeRefusesRecursionAndJSONBeforeLaunching(t *testing.T) {
	fakeTopgrade(t, "printf 'unexpected launch'")
	for _, args := range [][]string{
		{"upgrade", "--json"},
		{"sync", "--upgrade", "--json"},
		{"sync", "--upgrade", "--json", "--yes"},
		{"sync", "--upgrade", "--json", "--plan"},
	} {
		if code, out, errOut := run(t, args...); code != ExitUsage || out != "" || !strings.Contains(errOut, "does not support --json") {
			t.Fatalf("JSON %v: exit %d, %q, %q", args, code, out, errOut)
		}
	}
	t.Setenv(upgradeActive, "1")
	if code, out, errOut := run(t, "upgrade"); code != ExitFailure || out != "" || !strings.Contains(errOut, "recursive") {
		t.Fatalf("recursion: exit %d, %q, %q", code, out, errOut)
	}
}

func TestUpgradeMissingTopgrade(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(upgradeActive, "")
	if code, _, errOut := run(t, "upgrade"); code != ExitFailure || !strings.Contains(errOut, "install it") {
		t.Fatalf("exit %d, %q", code, errOut)
	}
}

func TestSyncUpgradeComposition(t *testing.T) {
	for _, mode := range []string{"sync", "combined", "preview", "dirty-repo", "recursive"} {
		t.Run(mode, func(t *testing.T) {
			root, src := installerFixture(t)
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			t.Setenv(upgradeActive, "")
			if err := os.WriteFile(filepath.Join(bin, "topgrade"), []byte("#!/bin/sh\nprintf 'TOPGRADE:%s:%s\\n' \"$NIMBUS_UPGRADE_CHECKOUT\" \"$NIMBUS_UPGRADE_MACHINE\"\nexit 23\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			args := []string{"sync", "--checkout", root, "--machine", "vm", "--yes"}
			if mode != "sync" {
				args = append(args, "--upgrade")
			}
			if mode == "preview" {
				args = append(args, "--plan")
			}
			if mode == "dirty-repo" {
				src.Commands[nativetest.Key("git", inspect.GitArgs(root, "status", "--porcelain=v1", "--untracked-files=all")...)] = []byte(" M profiles/common.toml")
			}
			if mode == "recursive" {
				t.Setenv(upgradeActive, "1")
			}
			code, out, errOut := run(t, args...)
			want := ExitOK
			switch mode {
			case "combined":
				want = 23
			case "dirty-repo", "recursive":
				want = ExitFailure
			}
			if code != want {
				t.Fatalf("%d want %d: %s%s", code, want, out, errOut)
			}
			if strings.Contains(out, "TOPGRADE:") != (mode == "combined") {
				t.Fatal(out)
			}
			if mode == "combined" && !strings.Contains(out, "TOPGRADE:"+root+":vm") {
				t.Fatal("lost explicit selection: " + out)
			}
			if mode == "preview" && (!strings.Contains(out, "Run Topgrade") || slices.Contains(src.reads, "dnf5 makecache")) {
				t.Fatalf("preview: %s; %v", out, src.reads)
			}
			var wantCalls []string
			if mode == "sync" || mode == "combined" {
				wantCalls = []string{"chezmoi apply"}
			}
			if !slices.Equal(src.calls, wantCalls) {
				t.Fatalf("unexpected sync calls: %v", src.calls)
			}
		})
	}
}

func TestSystemUpgradeDoesNotReconcileUnrelatedDrift(t *testing.T) {
	root, src := installerFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['missing']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(upgradeActive, "1")
	t.Setenv("NIMBUS_UPGRADE_CHECKOUT", root)
	t.Setenv("NIMBUS_UPGRADE_MACHINE", "vm")
	code, out, errOut := run(t, "upgrade", "--system", "--yes")
	if code != ExitOK || !slices.Equal(src.calls, []string{"sudo dnf5 -y upgrade"}) {
		t.Fatalf("%d: %s%s; %v", code, out, errOut, src.calls)
	}
	if strings.Contains(out, "install missing") {
		t.Fatal(out)
	}
}

func TestSystemUpgradeRequiresReadySources(t *testing.T) {
	for _, kind := range []string{plan.KindRepository, plan.KindDNFConfig, plan.KindFlatpakRemote} {
		p := systemUpgradePlan(&plan.Plan{Complete: true, Operations: []plan.Operation{{Kind: kind, Action: plan.ActionRepair, Summary: "repair source"}}})
		if p.Complete || len(p.Operations) != 1 || !strings.Contains(p.Operations[0].Blocked, "run nimbus sync first") {
			t.Fatalf("%+v", p)
		}
	}
}

func TestUpgradePreviewDoesNotLaunchTopgrade(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(upgradeActive, "")
	if code, out, errOut := run(t, "upgrade", "--plan"); code != ExitOK || !strings.Contains(out, "Run Topgrade") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
}

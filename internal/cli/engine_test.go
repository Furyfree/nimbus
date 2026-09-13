package cli

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestEnginePreflightStopsBeforeNewDefinitionsAndFetch(t *testing.T) {
	for _, mode := range []string{"newer", "refresh failure", "query failure", "ambiguous", "no installed RPM", "disabled source", "untrusted source", "source inspection failure"} {
		t.Run(mode, func(t *testing.T) {
			root, base := installerFixture(t)
			// An old engine cannot decode these definitions. It must still check RPMs.
			if err := os.WriteFile(filepath.Join(root, "nimbus.toml"), []byte("unknown_future_field=true\n"), 0644); err != nil {
				t.Fatal(err)
			}
			src := &maintenanceSource{Source: base}
			withSource(t, src)
			base.Commands[nativetest.Key("sudo", engineQueryArgs...)] = []byte("nimbus-0:0.4.7-1.fc44.x86_64")
			switch mode {
			case "refresh failure":
				base.Failures[nativetest.Key("sudo", engineRefreshArgs...)] = "offline"
			case "query failure":
				base.Failures[nativetest.Key("sudo", engineQueryArgs...)] = "bad metadata"
			case "ambiguous":
				base.Commands[nativetest.Key("sudo", engineQueryArgs...)] = []byte("unexpected")
			case "disabled source":
				base.Commands[nativetest.Key("sudo", engineConfigArgs...)] = []byte("======== config\nenabled = 0\npkg_gpgcheck = 1\nsslverify = 1\n")
			case "untrusted source":
				base.Commands[nativetest.Key("sudo", engineConfigArgs...)] = []byte("======== config\nenabled = 1\npkg_gpgcheck = 0\nsslverify = 1\n")
			case "source inspection failure":
				base.Failures[nativetest.Key("sudo", engineConfigArgs...)] = "private proxy password"
			case "no installed RPM":
				delete(base.Commands, nativetest.Key("rpm", engineInstalledArgs...))
			}
			code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
			if code != ExitFailure || strings.Contains(out+errOut, "definition error") {
				t.Fatalf("preflight: %d %s%s", code, out, errOut)
			}
			if slices.ContainsFunc(src.events, func(s string) bool { return strings.Contains(s, " fetch ") || strings.Contains(s, " merge ") }) {
				t.Fatal(src.events)
			}
			if strings.Contains(out+errOut, "private proxy password") {
				t.Fatal("configuration error leaked", out, errOut)
			}
			if mode == "newer" && !strings.Contains(out+errOut, "nimbus sync --upgrade") {
				t.Fatal(out, errOut)
			}
		})
	}
}
func TestEngineRestartPreservesSelectionAndFlags(t *testing.T) {
	root, base := installerFixture(t)
	bin := t.TempDir()
	executable := filepath.Join(bin, "nimbus")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'RESTART:%s\\n' \"$NIMBUS_ENGINE_RESTART\"\nprintf '<%s>\\n' \"$@\"\nexit 17\n"), 0755); err != nil {
		t.Fatal(err)
	}
	saved := syncExecutable
	syncExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { syncExecutable = saved })
	candidate := "nimbus-0:0.4.7-1.fc44.x86_64"
	base.Commands[nativetest.Key("sudo", engineQueryArgs...)] = []byte(candidate)
	base.Commands[nativetest.Key("rpm", "-qf", "--qf", "%{NAME}", executable)] = []byte("nimbus")
	base.Commands[nativetest.Key(executable, "version", "--json")] = []byte(`{"engine":"0.4.7"}`)
	withSource(t, handoffOutputSource{Source: base.FakeSource, afterStream: func(name string, args []string) {
		if name == "sudo" && slices.Contains(args, "upgrade") {
			base.Commands[nativetest.Key("rpm", engineInstalledArgs...)] = []byte(candidate)
		}
	}})
	base.Commands["sudo dnf5 --setopt=cacheonly=metadata --setopt=nimbus-engine.gpgcheck=1 --setopt=nimbus-engine.skip_if_unavailable=0 upgrade --from-repo=nimbus-engine "+candidate+" --assumeyes"] = nil
	code, out, errOut := run(t, "sync", "--upgrade", "--yes", "--prune", "--checkout", root, "--machine", "vm")
	if code != 17 || !strings.Contains(out, "RESTART:"+candidate) || !strings.Contains(out, "<--upgrade>\n<--checkout>\n<"+root+">\n<--machine>\n<vm>\n<--yes>\n<--prune>") {
		t.Fatalf("restart: %d %s%s", code, out, errOut)
	}
}
func TestCombinedMaintenanceAppliesAndUpgradesOnce(t *testing.T) {
	root, base := installerFixture(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "topgrade"), []byte("#!/bin/sh\nprintf 'TOPGRADE-RAN\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	src := &maintenanceSource{Source: base}
	withSource(t, src)
	code, out, errOut := run(t, "sync", "--upgrade", "--yes", "--checkout", root, "--machine", "vm")
	if code != 0 || strings.Count(out, "TOPGRADE-RAN") != 1 || strings.Count(out, "sync summary:") != 1 {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	fetches, applies := 0, 0
	for _, event := range src.events {
		if strings.Contains(event, " fetch ") {
			fetches++
		}
		if event == "chezmoi apply" {
			applies++
		}
	}
	if fetches != 2 || applies != 1 {
		t.Fatal(src.events)
	}
}
func TestManualConfirmationCannotBeAssumed(t *testing.T) {
	root, src := postinstallFixture(t)
	cmd, _ := postinstallCommand(root, false, "onepassword", "--yes")
	if err := cmd.Execute(); err == nil {
		t.Fatal("--yes confirmed GUI")
	}
	if slices.Contains(src.reads, "op whoami --format=json") || len(src.streams) != 0 {
		t.Fatal(src.reads, src.streams)
	}
	saved := postinstallTerminal
	postinstallTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { postinstallTerminal = saved })
	cmd, _ = postinstallCommand(root, false, "onepassword", "--mark-done")
	cmd.SetIn(strings.NewReader("yes\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(src.reads, "op whoami --format=json") {
		t.Fatal("explicit completion did not verify CLI access")
	}
	snapshot, err := inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := selectedTask(snapshot.view, "onepassword")
	if err != nil || task.Status != "complete" {
		t.Fatal(task, err)
	}
	cmd, _ = postinstallCommand(root, false, "onepassword", "--yes")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = selectedTask(snapshot.view, "onepassword")
	if err != nil || task.Status != "complete" {
		t.Fatal(task, err)
	}
	delete(src.Paths, "op")
	snapshot, err = inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = selectedTask(snapshot.view, "onepassword")
	if err != nil || task.Status == "complete" {
		t.Fatal("stored completion hid missing CLI", task, err)
	}
}

func TestEnablingSSHRequiresNewManualConfirmation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := confirmPasswordState("vm", false); err != nil {
		t.Fatal(err)
	}
	if !passwordConfirmed("vm", false) || passwordConfirmed("vm", true) {
		t.Fatal("SSH inherited a different GUI acknowledgment")
	}
}
func TestRepositoryConfigurationDumpIsNotLoggable(t *testing.T) {
	if publicInstallCommand("sudo", engineConfigArgs) {
		t.Fatal("repository credentials could enter installation log")
	}
}
func TestExplicitUpgradeStopsWhenFreshMetadataFails(t *testing.T) {
	root, src := installerFixture(t)
	src.Failures["sudo dnf5 --refresh --setopt=*.skip_if_unavailable=0 makecache"] = "offline"
	code, out, errOut := run(t, "upgrade", "--system", "--yes", "--checkout", root, "--machine", "vm")
	if code == 0 || !strings.Contains(out+errOut, "refresh") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if slices.ContainsFunc(src.calls, func(call string) bool { return strings.Contains(call, " -y upgrade") }) {
		t.Fatal(src.calls)
	}
}

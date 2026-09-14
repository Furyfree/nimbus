package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestApprovedGreeterInspectionIsNarrowAndPreservesFailure(t *testing.T) {
	key := "sudo -- " + inspect.GreeterBinary + " passwordless-sync status test"
	src := &nativetest.FakeSource{Failures: map[string]string{key: "failed to open the Polkit rules directory: Permission denied"}}
	approved := approvedGreeterSource{Source: src, user: "test"}
	_, err := approved.Run(inspect.GreeterBinary, "passwordless-sync", "status", "test")
	if !errors.Is(err, inspect.ErrGreeterAdminStatus) {
		t.Fatalf("privileged failure lost: %v", err)
	}
	for _, args := range [][]string{{"passwordless-sync", "enable", "test"}, {"passwordless-sync", "status", "other"}} {
		_, err := approved.Run(inspect.GreeterBinary, args...)
		if !errors.Is(err, nativetest.ErrNotRecorded) || errors.Is(err, inspect.ErrGreeterAdminStatus) {
			t.Fatalf("unapproved command elevated: %v", err)
		}
	}
	for _, args := range [][]string{{"--sync"}, {"--legacy"}, {"--supports", "other"}} {
		if publicInstallCommand(inspect.GreeterHelper, args) {
			t.Fatalf("arbitrary helper command considered transcript-safe: %v", args)
		}
	}
}

func TestGreeterAuthorizationUsesNormalSyncAndInitApproval(t *testing.T) {
	for _, command := range []string{"sync", "init"} {
		t.Run(command, func(t *testing.T) {
			root, src := installerFixture(t)
			for file, data := range map[string]string{
				"profiles/common.toml":    "schema=1\nid='common'\ncomponents=['greeter']\n",
				"components/greeter.toml": "schema=1\nid='greeter'\npackages=['noctalia','noctalia-greeter']\ngreeter_passwordless_sync=true\n",
			} {
				if err := os.WriteFile(filepath.Join(root, file), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			src.Commands[key] = append(src.Commands[key], []byte("noctalia|0|5.1.0|1.fc44|x86_64|updates|User\nnoctalia-greeter|0|1.5.0|1.fc44|x86_64|updates|User\n")...)
			src.Commands["getent --service=files passwd test"] = []byte("test:x:1000:1000::/home/test:/bin/bash\n")
			src.Commands["rpm -q --queryformat %{VERSION} noctalia"] = []byte("5.1.0")
			src.Commands[inspect.GreeterHelper+" --supports secure-sync-v1"] = []byte("secure-sync-v1\n")
			src.Files[inspect.GreeterPolicy] = []byte(`<policyconfig><action id="org.noctalia.greeter.sync-appearance"><annotate key="org.freedesktop.policykit.exec.path">/usr/bin/noctalia-greeter-apply-appearance</annotate><annotate key="org.freedesktop.policykit.exec.argv1">--sync</annotate></action></policyconfig>`)
			status := inspect.GreeterBinary + " passwordless-sync status test"
			src.Failures[status] = "failed to open the Polkit rules directory: Permission denied"
			adminStatus := "sudo -- " + status
			src.Commands[adminStatus] = []byte("test: not enabled by the noctalia-greeter managed rule\nOther administrator-authored Polkit rules are not included in this status.\n")
			enable := "sudo -- " + inspect.GreeterBinary + " passwordless-sync enable test"
			src.Commands[enable] = nil
			withSource(t, handoffOutputSource{Source: src, afterStream: func(name string, args []string) {
				if nativetest.Key(name, args...) == enable {
					src.Commands[adminStatus] = []byte(strings.Replace(string(src.Commands[adminStatus]), ": not enabled", ": enabled", 1))
				}
			}})
			args := []string{command, "--checkout", root, "--machine", "vm"}
			if command == "sync" {
				args = append(args, "--no-upgrade")
			}
			code, out, errOut := run(t, append(slices.Clone(args), "--plan")...)
			if code != ExitOK || slices.Contains(src.reads, adminStatus) || len(src.calls) != 0 {
				t.Fatalf("preview: %d %s%s reads=%v calls=%v", code, out, errOut, src.reads, src.calls)
			}
			saved := approver
			t.Cleanup(func() { approver = saved })
			approver = func(io.Reader, io.Writer, string) bool { return false }
			_, _, _ = run(t, args...)
			if slices.Contains(src.reads, adminStatus) || slices.Contains(src.calls, enable) {
				t.Fatal("greeter access attempted after refusal")
			}
			approver = saved
			code, out, errOut = run(t, append(slices.Clone(args), "--yes")...)
			if code != ExitOK || !slices.Contains(src.calls, enable) {
				t.Fatalf("apply: %d %s%s calls=%v", code, out, errOut, src.calls)
			}
			src.calls, src.reads = nil, nil
			code, out, errOut = run(t, append(slices.Clone(args), "--yes")...)
			if code != ExitOK || slices.Contains(src.calls, enable) || !slices.Contains(src.reads, adminStatus) {
				t.Fatalf("repeat: %d %s%s calls=%v reads=%v", code, out, errOut, src.calls, src.reads)
			}
			src.calls, src.reads = nil, nil
			statusArgs := []string{"status", "--checkout", root, "--machine", "vm"}
			code, out, errOut = run(t, statusArgs...)
			if code != ExitOK || !strings.Contains(out, "to install 0") || !strings.Contains(out, "pending 1, blocked 0") {
				t.Fatalf("greeter check reported as installation: %d %s%s", code, out, errOut)
			}
			code, out, errOut = run(t, append(statusArgs, "--json")...)
			var result struct {
				Data statusResult `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || code != ExitOK || result.Data.ToInstall != 0 || result.Data.Pending != 1 || !result.Data.Complete {
				t.Fatalf("JSON greeter status: %d %s%s: %v", code, out, errOut, err)
			}
			if slices.Contains(src.reads, adminStatus) || len(src.calls) != 0 {
				t.Fatalf("status authenticated or mutated state: reads=%v calls=%v", src.reads, src.calls)
			}
		})
	}
}

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
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/state"
)

type postinstallSource struct {
	*nativetest.FakeSource
	reads, streams, files []string
	streamErr             error
	onStream              func(string)
}

func (s *postinstallSource) Run(name string, args ...string) ([]byte, error) {
	s.reads = append(s.reads, nativetest.Key(name, args...))
	return s.FakeSource.Run(name, args...)
}

func (s *postinstallSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	if s.onStream != nil {
		s.onStream(nativetest.Key(name, args...))
	}
	return s.streamErr
}

func (s *postinstallSource) ReadFile(path string) ([]byte, error) {
	s.files = append(s.files, path)
	return s.FakeSource.ReadFile(path)
}

func postinstallFixture(t *testing.T) (string, *postinstallSource) {
	t.Helper()
	root := applyEnv(t)
	src := &postinstallSource{FakeSource: fixtureSource(t, root)}
	key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	src.Commands[key] = append(src.Commands[key], []byte("1password|0|8.10.1|1|x86_64|onepassword|User\n")...)
	src.Paths["1password"] = "/usr/bin/1password"
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:onepassword:1password", Provider: "dnf", Package: "1password.x86_64", Verified: true, Operation: "install", PlanDigest: "fixture", Timestamp: time.Unix(100, 0)}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}
	withSource(t, src)
	return root, src
}

func postinstallCommand(root string, jsonOutput bool, args ...string) (*cobra.Command, *bytes.Buffer) {
	cmd := newPostinstall(&options{json: jsonOutput})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append([]string{"--checkout", root, "--machine", "vm"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	return cmd, &out
}

func TestPostinstallListingStaysReadOnlyAndAvoidsUserData(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			root, src := postinstallFixture(t)
			t.Setenv("XDG_RUNTIME_DIR", "")
			cmd, out := postinstallCommand(root, jsonOutput)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(src.streams) != 0 {
				t.Fatalf("listing executed %v", src.streams)
			}
			allowed := []string{"uname -m", nativetest.Key("dnf5", inspect.PackageQueryArgs...), "id -un"}
			for _, command := range src.reads {
				if !slices.Contains(allowed, command) {
					t.Fatalf("unnecessary inspection: %s", command)
				}
			}
			for _, path := range src.files {
				if path != inspect.OSReleasePath && path != inspect.SecureBootPath {
					t.Fatalf("unexpected private/configuration read: %s", path)
				}
			}
			if jsonOutput {
				var envelope struct{ Data postinstallView }
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				i := slices.IndexFunc(envelope.Data.Tasks, func(task postinstall.Task) bool { return task.ID == "onepassword" })
				if i < 0 || envelope.Data.Tasks[i].Status != postinstall.Unknown {
					t.Fatalf("sign-in readiness was misreported: %s", out)
				}
			} else if !strings.Contains(out.String(), "onepassword [unknown]") || !strings.Contains(out.String(), "Native action: 1password") {
				t.Fatalf("missing status or direct action: %s", out)
			}
		})
	}
}

func TestPostinstallJSONCannotSelectOrApprove(t *testing.T) {
	for _, args := range [][]string{{"onepassword"}, {"onepassword", "--yes"}, {"--yes"}} {
		root, src := postinstallFixture(t)
		cmd, _ := postinstallCommand(root, true, args...)
		if err := cmd.Execute(); err == nil || len(src.reads) != 0 || len(src.streams) != 0 {
			t.Fatalf("JSON selected a task: %v reads=%v actions=%v", err, src.reads, src.streams)
		}
	}
}

func TestPostinstallSelectionPreviewAndCancellation(t *testing.T) {
	for _, mode := range []string{"preview", "cancel", "nonterminal", "output failure"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			savedTerminal, savedApprover := postinstallTerminal, approver
			t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
			postinstallTerminal = func(io.Reader) bool { return mode != "nonterminal" }
			approver = func(io.Reader, io.Writer, string) bool { return false }
			args := []string{"onepassword"}
			if mode == "preview" {
				args = append(args, "--plan")
			}
			cmd, _ := postinstallCommand(root, false, args...)
			if mode == "output failure" {
				cmd.SetOut(&previewErrorWriter{err: syscall.ENOSPC})
			}
			err := cmd.Execute()
			if (err == nil) != (mode == "preview") || len(src.streams) != 0 {
				t.Fatalf("%v actions=%v", err, src.streams)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read-only/cancelled action touched lock state: %v", err)
			}
		})
	}
}

func TestPostinstallRechecksOwnershipAndDefinitionsAfterApproval(t *testing.T) {
	for _, change := range []string{"receipt", "definitions", "installed version"} {
		t.Run(change, func(t *testing.T) {
			root, src := postinstallFixture(t)
			savedTerminal, savedApprover := postinstallTerminal, approver
			t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
			postinstallTerminal = func(io.Reader) bool { return true }
			approver = func(io.Reader, io.Writer, string) bool {
				switch change {
				case "receipt":
					if err := os.Remove(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("package:onepassword:1password"))); err != nil {
						t.Fatal(err)
					}
				case "definitions":
					path := manifestPath(root, "vm")
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, append(data, []byte("\n# changed during review\n")...), 0o644); err != nil {
						t.Fatal(err)
					}
				case "installed version":
					key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
					src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("1password|0|8.10.1|"), []byte("1password|0|8.10.2|"))
				}
				return true
			}
			cmd, _ := postinstallCommand(root, false, "onepassword")
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "changed after approval") || len(src.streams) != 0 {
				t.Fatalf("stale approval accepted: %v, %v", err, src.streams)
			}
		})
	}
}

func TestPostinstallActionDoesNotConvertExitZeroToAccountReadiness(t *testing.T) {
	root, src := postinstallFixture(t)
	path := filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("package:onepassword:1password"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cmd, out := postinstallCommand(root, false, "onepassword", "--yes")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(src.streams, []string{"1password"}) || !strings.Contains(out.String(), "After action: onepassword: unknown") {
		t.Fatalf("native success became signed-in state: %s actions=%v", out, src.streams)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("postinstall wrote a completion receipt: %v", err)
	}
}

func TestPostinstallLockAndNativeFailure(t *testing.T) {
	for _, locked := range []bool{false, true} {
		root, src := postinstallFixture(t)
		src.streamErr = errors.New("native launch failed")
		if locked {
			path, err := apply.LockPath()
			if err != nil {
				t.Fatal(err)
			}
			lock, err := apply.Acquire(path, apply.LockInfo{Command: "other"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = lock.Release() })
		}
		cmd, out := postinstallCommand(root, false, "onepassword", "--yes")
		err := cmd.Execute()
		if err == nil || (len(src.streams) == 0) != locked {
			t.Fatalf("locked=%t: %v streams=%v", locked, err, src.streams)
		}
		if !locked && (!strings.Contains(err.Error(), "native launch failed") || !strings.Contains(out.String(), "After action: onepassword: unknown")) {
			t.Fatalf("native failure/current status lost: %v %s", err, out)
		}
	}
}

func TestPostinstallRebootRemainsInstructionOnly(t *testing.T) {
	root, src := postinstallFixture(t)
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "target:default", Provider: "target", Verified: true, Reboot: true, Operation: "install", PlanDigest: "fixture", Timestamp: time.Unix(100, 0)}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}
	src.Files["/proc/stat"] = []byte("btime 50\n")
	t.Setenv("XDG_RUNTIME_DIR", "")
	cmd, out := postinstallCommand(root, false, "reboot", "--yes")
	if err := cmd.Execute(); err != nil || len(src.streams) != 0 || !strings.Contains(out.String(), "reboot [pending]") {
		t.Fatalf("instruction-only task mutated: %v %s streams=%v", err, out, src.streams)
	}
}

func TestPostinstallRejectsForgedNativeActions(t *testing.T) {
	for _, task := range []postinstall.Task{
		{ID: "onepassword", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.OpenApplication, Argv: []string{"sh", "-c", "unexpected"}}},
		{ID: "onepassword", Status: postinstall.Blocked, Action: &postinstall.Action{Kind: postinstall.OpenApplication, Argv: []string{"1password"}}},
		{ID: "other", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.EnrollFingerprint, Argv: []string{"fprintd-enroll"}}},
		{ID: "copilot", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"sudo", "--", "/tmp/github-copilot-installer", "install"}}},
		{ID: "proton-cachyos", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"protonplus", "update", "all"}}},
		{ID: "proton-cachyos", Status: postinstall.Blocked, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"protonplus", "install", "steam-system", "proton-cachyos", "latest"}}},
	} {
		if _, err := postinstallArgv(task); err == nil {
			t.Fatalf("untyped action accepted: %+v", task)
		}
	}
}

func TestPostinstallCopilotUsesNativeInstallWithoutAppReceipts(t *testing.T) {
	for _, mode := range []string{"preview", "install", "failure"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['copilot-installer:github-copilot-installer']\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			src.Commands[key] = append(src.Commands[key], []byte("github-copilot-installer|0|0.2.0|1|x86_64|copilot-installer|User\n")...)
			src.Paths["/usr/bin/github-copilot-installer"] = "/usr/bin/github-copilot-installer"
			r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:copilot-installer:github-copilot-installer", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
			if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
				t.Fatal(err)
			}
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"copilot", "--yes"}
			if mode == "preview" {
				args = append(args, "--plan")
			}
			if mode == "failure" {
				src.streamErr = errors.New("helper rejected installation")
			}
			cmd, out := postinstallCommand(root, false, args...)
			err = cmd.Execute()
			if (err != nil) != (mode == "failure") {
				t.Fatalf("%v: %s", err, out)
			}
			if mode == "preview" {
				if len(src.streams) != 0 || !strings.Contains(out.String(), "Native action: sudo -- /usr/bin/github-copilot-installer install") {
					t.Fatalf("preview executed or hid action: %s %v", out, src.streams)
				}
			} else if !slices.Equal(src.streams, []string{"sudo -- /usr/bin/github-copilot-installer install"}) || !strings.Contains(out.String(), "After action: copilot: unknown") {
				t.Fatalf("helper prompts or ownership boundary lost: %s %v", out, src.streams)
			}
			after, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("native action changed Nimbus receipts")
			}
		})
	}
}

func TestPostinstallProtonCachyOSPreservesApprovalAndNativeOwnership(t *testing.T) {
	for _, mode := range []string{"preview", "cancel", "install", "failure"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['terra:protonplus','rpmfusion-nonfree:steam']\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			for _, pkg := range []struct{ name, prefix string }{{"protonplus", "terra"}, {"steam", "rpmfusion-nonfree"}} {
				src.Commands[key] = append(src.Commands[key], []byte(pkg.name+"|0|1.0|1|x86_64|"+pkg.prefix+"|User\n")...)
				src.Paths[pkg.name] = "/usr/bin/" + pkg.name
				r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:" + pkg.prefix + ":" + pkg.name, Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
				if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"proton-cachyos", "--yes"}
			if mode == "preview" {
				args = append(args, "--plan")
			} else if mode == "cancel" {
				args = []string{"proton-cachyos"}
				savedTerminal, savedApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool { return false }
			} else if mode == "failure" {
				src.streamErr = errors.New("ProtonPlus download failed")
			}
			cmd, out := postinstallCommand(root, false, args...)
			err = cmd.Execute()
			if (err != nil) != (mode == "cancel" || mode == "failure") {
				t.Fatalf("%v: %s", err, out)
			}
			want := "protonplus install steam-system proton-cachyos latest"
			if !strings.Contains(out.String(), "Native action: "+want) {
				t.Fatalf("missing action preview: %s", out)
			}
			if mode == "preview" || mode == "cancel" {
				if len(src.streams) != 0 {
					t.Fatalf("unapproved download: %v", src.streams)
				}
			} else if !slices.Equal(src.streams, []string{want}) || !strings.Contains(out.String(), "After action: proton-cachyos: unknown") {
				t.Fatalf("native command or unknown completion lost: %s %v", out, src.streams)
			}
			after, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("ProtonPlus action changed Nimbus receipts")
			}
		})
	}
}

func TestPostinstallNoctaliaPreviewCancellationAndFailure(t *testing.T) {
	for _, mode := range []string{"preview", "cancel", "stale", "failure", "success"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['noctalia']\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			src.Commands[key] = append(src.Commands[key], []byte("noctalia|0|5.0.1|1|x86_64|fedora|User\n")...)
			src.Paths["noctalia"] = "/usr/bin/noctalia"
			src.Commands["noctalia config export full"] = []byte("[plugins]\nenabled=['noctalia/timer']\n[[plugins.source]]\nname='official'\nkind='git'\nenabled=true\n")
			src.Commands["noctalia msg plugins list"] = []byte("noctalia/timer [official] 1.2.1 enabled\n")
			r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:noctalia", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
			if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
				t.Fatal(err)
			}
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"noctalia-plugins", "--yes"}
			if mode == "preview" {
				args = append(args, "--plan")
			} else if mode == "cancel" || mode == "stale" {
				args = []string{"noctalia-plugins"}
				savedTerminal, savedApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "stale" {
						src.Commands["noctalia config export full"] = []byte("[plugins]\nenabled=[]")
						return true
					}
					return false
				}
			} else if mode == "failure" {
				src.streamErr = errors.New("native export failed")
			} else {
				src.onStream = func(key string) {
					if key == "noctalia msg plugins update official" {
						path := filepath.Join(os.Getenv("XDG_STATE_HOME"), "noctalia/plugins/materialized/official/timer")
						src.Files[filepath.Join(path, "plugin.toml")] = []byte("id='noctalia/timer'\n[[widget]]\nentry='bar.luau'\n")
						src.Files[filepath.Join(path, "bar.luau")] = []byte("return {}")
					}
				}
			}
			cmd, out := postinstallCommand(root, false, args...)
			err = cmd.Execute()
			if (err != nil) != (mode == "cancel" || mode == "stale" || mode == "failure") {
				t.Fatalf("%v: %s", err, out)
			}
			for _, want := range []string{"Native action: noctalia msg plugins update official"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing preview: %s", out)
				}
			}
			if mode == "preview" || mode == "cancel" || mode == "stale" {
				if len(src.streams) != 0 {
					t.Fatalf("unapproved mutation: %v", src.streams)
				}
			} else if mode == "success" {
				if !slices.Equal(src.streams, []string{"noctalia msg plugins update official"}) || !strings.Contains(out.String(), "After action: noctalia-plugins: complete") {
					t.Fatalf("%s %v", out, src.streams)
				}
			} else if !strings.Contains(out.String(), "After action: noctalia-plugins: pending") {
				t.Fatalf("lost failure status: %s", out)
			}
			after, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("plugin action changed receipts")
			}
		})
	}
}

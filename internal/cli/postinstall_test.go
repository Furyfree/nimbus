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
	src.Paths["op"] = "/usr/bin/op"
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"]}`)
	src.Commands["op whoami --format=json"] = []byte("{}")
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
			cmd, out := postinstallCommand(root, jsonOutput, "status")
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(src.streams) != 0 {
				t.Fatalf("listing executed %v", src.streams)
			}
			allowed := []string{"uname -m", nativetest.Key("dnf5", inspect.PackageQueryArgs...), "id -un", nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)}
			for _, command := range src.reads {
				if !slices.Contains(allowed, command) {
					t.Fatalf("unnecessary inspection: %s", command)
				}
			}
			for _, path := range src.files {
				if path != inspect.OSReleasePath && path != inspect.SecureBootPath && path != "/proc/mounts" && path != filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "agent-proxy.json") {
					t.Fatalf("unexpected private/configuration read: %s", path)
				}
			}
			if jsonOutput {
				var envelope struct{ Data postinstallView }
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				i := slices.IndexFunc(envelope.Data.Tasks, func(task postinstall.Task) bool { return task.ID == "onepassword" })
				if i < 0 || envelope.Data.Tasks[i].Status != postinstall.Pending {
					t.Fatalf("sign-in readiness was misreported: %s", out)
				}
			} else if !strings.Contains(out.String(), "onepassword") || !strings.Contains(out.String(), "Pending") {
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
			cmd.SetIn(strings.NewReader("no\n"))
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "changed after approval") || len(src.streams) != 0 {
				t.Fatalf("stale approval accepted: %v, %v", err, src.streams)
			}
		})
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
	cmd, out := postinstallCommand(root, false, "status")
	if err := cmd.Execute(); err != nil || len(src.streams) != 0 || !strings.Contains(out.String(), "Notice: reboot:") {
		t.Fatalf("instruction-only task mutated: %v %s streams=%v", err, out, src.streams)
	}
}

func TestPostinstallRejectsForgedNativeActions(t *testing.T) {
	for _, task := range []postinstall.Task{
		{ID: "nvidia-mok", Status: postinstall.Blocked, Action: &postinstall.Action{Kind: postinstall.SetupNVIDIA}},
		{ID: "nvidia-mok", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.SetupNVIDIA, Argv: []string{"sudo", "sh"}}},
		{ID: "onepassword", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.OpenApplication, Argv: []string{"sh", "-c", "unexpected"}}},
		{ID: "onepassword", Status: postinstall.Blocked, Action: &postinstall.Action{Kind: postinstall.OpenApplication, Argv: []string{"1password"}}},
		{ID: "other", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.EnrollFingerprint, Argv: []string{"fprintd-enroll"}}},
		{ID: "copilot", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"sudo", "--", "/tmp/github-copilot-installer", "install"}}},
		{ID: "proton-cachyos", Status: postinstall.Unknown, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"protonplus", "update", "all"}}},
		{ID: "proton-cachyos", Status: postinstall.Blocked, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"protonplus", "install", "steam-system", "proton-cachyos", "latest"}}},
		{ID: "wowup", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.InstallApplication, Argv: []string{"sudo", "--", "/tmp/wowup-cf-installer", "install", "--assumeyes"}}},
		{ID: "voxtype", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.SetupVoxtype, Commands: [][]string{{"sh", "-c", "voxtype setup"}, {"systemctl", "--user", "enable", "--now", "voxtype.service"}}}},
		{ID: "hostname", Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.SetHostname, Hostname: "nimbus-laptop", Argv: []string{"sudo", "--", "/usr/bin/hostnamectl", "set-hostname", "other"}}},
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
			src.Commands["/usr/bin/github-copilot-installer status"] = []byte("Installed GitHub Copilot: not installed\n")
			src.onStream = func(string) {
				if mode == "install" {
					src.Commands["/usr/bin/github-copilot-installer status"] = []byte("Installed GitHub Copilot: 1.2.3-1\n")
				}
			}
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
			} else if !slices.Equal(src.streams, []string{"sudo -- /usr/bin/github-copilot-installer install"}) || (mode == "install" && !strings.Contains(out.String(), "1.2.3-1 is installed")) {
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
			src.Commands["protonplus list steam-system"] = []byte("Installed runners for Steam:\nNo runners installed\n")
			src.onStream = func(string) {
				if mode == "install" {
					src.Commands["protonplus list steam-system"] = []byte("Installed runners for Steam:\n  Proton-CachyOS Latest\n")
				}
			}
			oldTerminal, oldApprover := postinstallTerminal, approver
			t.Cleanup(func() { postinstallTerminal, approver = oldTerminal, oldApprover })
			postinstallTerminal = func(io.Reader) bool { return true }
			approver = func(io.Reader, io.Writer, string) bool { return mode != "cancel" }
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
			} else if !slices.Equal(src.streams, []string{want}) || (mode == "install" && !strings.Contains(out.String(), "ProtonPlus lists")) {
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
				want := "\u2713 All enabled Noctalia plugins have their required runtime files.\n"
				if !slices.Equal(src.streams, []string{"noctalia msg plugins update official"}) || !strings.HasSuffix(out.String(), want) {
					t.Fatalf("%s %v", out, src.streams)
				}
				// Completed selection and preview show only the result and never
				// offer or repeat the update, even with explicit approval.
				for _, flag := range []string{"--plan", "--yes"} {
					complete, result := postinstallCommand(root, false, "noctalia-plugins", flag)
					if err := complete.Execute(); err != nil || result.String() != want || len(src.streams) != 1 {
						t.Fatalf("completed task: %v %q %v", err, result.String(), src.streams)
					}
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

func TestPostinstallVoxtypePreviewAndApproval(t *testing.T) {
	root, src := postinstallFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['voxtype:voxtype']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if src.ExitCodes == nil {
		src.ExitCodes = map[string]int{}
	}
	key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	src.Commands[key] = append(src.Commands[key], []byte("voxtype|0|1.0.1|0.3.fc44|x86_64|voxtype|User\n")...)
	src.Paths["voxtype"] = "/usr/bin/voxtype"
	src.Paths["systemctl"] = "/usr/bin/systemctl"
	src.Commands[nativetest.Key("voxtype", "info", "models", "--json", "--engine", "whisper")] = []byte(
		`{"engines":{"whisper":{"models":[{"name":"small","installed":false},{"name":"medium","installed":false}]}}}`)
	unit := func(verb, state string, code int) {
		k := nativetest.Key("systemctl", "--user", verb, "voxtype.service")
		src.Commands[k] = []byte(state + "\n")
		if code != 0 {
			src.ExitCodes[k] = code
		}
	}
	unit("is-enabled", "disabled", 1)
	unit("is-active", "inactive", 3)
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:voxtype:voxtype", Provider: "dnf", Package: "voxtype.x86_64", Verified: true, Operation: "install", PlanDigest: "fixture", Timestamp: time.Unix(100, 0)}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}

	cmd, out := postinstallCommand(root, false, "voxtype", "--plan")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	for _, want := range []string{"voxtype setup --download --model small", "systemctl --user enable --now voxtype.service"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview lacks %q:\n%s", want, out)
		}
	}
	if len(src.streams) != 0 {
		t.Fatalf("preview ran native commands: %v", src.streams)
	}

	savedTerminal, savedApprover := postinstallTerminal, approver
	t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
	postinstallTerminal = func(io.Reader) bool { return true }
	approver = func(io.Reader, io.Writer, string) bool { return true }
	downloadKey := nativetest.Key("voxtype", "setup", "--download", "--model", "small")
	enableKey := nativetest.Key("systemctl", "--user", "enable", "--now", "voxtype.service")
	src.Commands[downloadKey] = nil
	src.Commands[enableKey] = nil
	src.onStream = func(key string) {
		if key == enableKey {
			src.Commands[nativetest.Key("voxtype", "info", "models", "--json", "--engine", "whisper")] = []byte(
				`{"engines":{"whisper":{"models":[{"name":"small","installed":true}]}}}`)
			unit("is-enabled", "enabled", 0)
			unit("is-active", "active", 0)
		}
	}
	cmd, out = postinstallCommand(root, false, "voxtype")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("approved setup failed: %v", err)
	}
	if !slices.Equal(src.streams, []string{downloadKey, enableKey}) {
		t.Fatalf("native workflow mismatch: %v", src.streams)
	}
	if !strings.Contains(out.String(), "voxtype.service is enabled") {
		t.Fatalf("completion was not reported:\n%s", out)
	}
}

func TestPostinstallNVIDIAMOKApprovalBoundary(t *testing.T) {
	for _, mode := range []string{"plan", "cancel", "nonterminal", "json", "approved", "changed ownership"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\ncomponents=['nvidia']\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "components/nvidia.toml"), []byte("schema=1\nid='nvidia'\npackages=['akmod-nvidia','akmods','mokutil']\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			var receipts []state.Receipt
			for _, name := range []string{"akmod-nvidia", "akmods", "mokutil"} {
				key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
				src.Commands[key] = append(src.Commands[key], []byte(name+"|0|1|1|x86_64|fedora|User\n")...)
				receipts = append(receipts, state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:" + name, Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"})
			}
			if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: receipts}); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"sudo", "kmodgenca", "akmods", "dracut", "mokutil", "modinfo", "nvidia-smi"} {
				src.Paths[name] = "/usr/bin/" + name
			}
			src.Files[inspect.SecureBootPath] = []byte{0, 0, 0, 0, 1}
			src.Files[postinstall.MOKCertificate] = []byte("fixture certificate")
			src.Commands["mokutil --sb-state"] = []byte("SecureBoot enabled")
			src.Commands["uname -r"] = []byte("test-kernel")
			src.streamErr = errors.New("authentication stopped")
			savedTerminal, savedApprover := postinstallTerminal, approver
			t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
			postinstallTerminal = func(io.Reader) bool { return mode != "nonterminal" }
			approver = func(io.Reader, io.Writer, string) bool {
				if mode == "changed ownership" {
					if err := os.Remove(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("package:dnf:akmods"))); err != nil {
						t.Fatal(err)
					}
					return true
				}
				return false
			}
			args := []string{"nvidia-mok"}
			if mode == "plan" {
				args = append(args, "--plan", "--yes")
			} else if mode == "approved" {
				args = append(args, "--yes")
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err := cmd.Execute()
			if (err == nil) != (mode == "plan") {
				t.Fatalf("%v: %s", err, out)
			}
			if mode == "approved" {
				if !strings.Contains(err.Error(), "authentication stopped") || !slices.Equal(src.streams, []string{"sudo --validate"}) {
					t.Fatalf("approved setup not dispatched: %v %v", err, src.streams)
				}
			} else {
				if len(src.streams) != 0 {
					t.Fatalf("unapproved native actions: %v", src.streams)
				}
				for _, command := range src.reads {
					if strings.HasPrefix(command, "sudo ") {
						t.Fatalf("unapproved privilege escalation: %s", command)
					}
				}
			}
			if mode == "plan" {
				for _, want := range []string{"kmodgenca -a", "akmods --force --rebuild", "dracut --force", "mokutil --import", "US/QWERTY"} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("preview omits %q: %s", want, out)
					}
				}
			}
			if mode == "changed ownership" && !strings.Contains(err.Error(), "changed after approval") {
				t.Fatalf("stale ownership accepted: %v", err)
			}
		})
	}
}

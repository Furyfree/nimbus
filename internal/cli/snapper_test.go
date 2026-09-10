package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/snapper"
	"github.com/Furyfree/nimbus/internal/state"
)

const snapshotCommand = "sudo -- snapper --config nimbus "
const beforeSnapshot = snapshotCommand + "create --type pre --print-number --cleanup-algorithm number --description Nimbus system changes"
const afterSnapshot = snapshotCommand + "create --type post --print-number --cleanup-algorithm number --description Nimbus system changes --pre-number 11"
const cleanupSnapshots = snapshotCommand + "cleanup number"

type snapshotSource struct {
	*installerSource
	mutations []string
}

func (s *snapshotSource) Run(name string, args ...string) ([]byte, error) {
	key := nativetest.Key(name, args...)
	if strings.HasPrefix(key, snapshotCommand) {
		s.mutations = append(s.mutations, key)
		if s.Failures[key] != "" {
			return s.installerSource.Run(name, args...)
		}
		switch {
		case strings.Contains(key, "create-config"):
			s.Files[snapper.Config] = slices.Clone(s.Files[snapper.Template])
			s.Dirs["/etc/snapper/configs"] = []string{"nimbus"}
			s.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=\"nimbus\"\n")
		case strings.Contains(key, "set-config"):
			s.Files[snapper.Config] = []byte(strings.ReplaceAll(string(s.Files[snapper.Config]), `NUMBER_LIMIT="50"`, `NUMBER_LIMIT="6"`))
		}
	}
	return s.installerSource.Run(name, args...)
}

func (s *snapshotSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	s.mutations = append(s.mutations, key)
	err := s.installerSource.Stream(out, errOut, name, args...)
	if err == nil && key == "sudo systemctl set-default graphical.target" {
		s.Commands["systemctl get-default"] = []byte("graphical.target\n")
	}
	return err
}

func snapshotFixture(t *testing.T) (string, *snapshotSource) {
	t.Helper()
	root, base := installerFixture(t)
	template, err := os.ReadFile("../../system/root" + snapper.Template)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		"profiles/common.toml":           []byte("schema=1\nid='common'\ncomponents=['snapper']\n"),
		"components/snapper.toml":        []byte("schema=1\nid='snapper'\n[[files]]\nsource='etc/snapper/config-templates/nimbus'\nowner='root'\ngroup='root'\nmode='0644'\n"),
		"system/root" + snapper.Template: template,
	} {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &snapshotSource{installerSource: base}
	src.Dirs["/etc"] = append(src.Dirs["/etc"], "snapper")
	for path, children := range map[string][]string{
		"/etc/snapper":                  {"configs", "config-templates"},
		"/etc/snapper/configs":          {"nimbus"},
		"/etc/snapper/config-templates": {"nimbus"},
	} {
		src.Dirs[path] = children
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+path] = []byte("directory|root|root|755|1\n")
	}
	for _, path := range []string{snapper.Config, snapper.Template} {
		src.Files[path] = slices.Clone(template)
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+path] = []byte("regular file|root|root|644|1\n")
	}
	src.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=\"nimbus\"\n")
	src.Commands[beforeSnapshot] = []byte("11\n")
	src.Commands[afterSnapshot] = []byte("12\n")
	src.Commands[cleanupSnapshots] = nil
	src.Commands[snapshotCommand+"create-config --fstype btrfs --template nimbus /"] = nil
	src.Commands[snapshotCommand+"set-config NUMBER_LIMIT=6"] = nil
	src.Commands["sudo systemctl set-default graphical.target"] = nil
	s, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	intended, _ := json.Marshal(inspect.SystemFile{Exists: true, Content: template, Owner: "root", Group: "root", Mode: "0644"})
	receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: "file:" + snapper.Template,
		Provider: plan.KindFile, Machine: "vm", Verified: true, PlanDigest: "earlier",
		Operation: plan.ActionInstall, Intended: string(intended), Previous: `{"exists":false}`,
		Definitions: state.Definitions{Digest: s.Checkout.Digest()}}
	if err := state.Record(stateRoot, "earlier", &state.Stage{Schema: state.Schema, PlanDigest: "earlier", Receipts: []state.Receipt{receipt}}); err != nil {
		t.Fatal(err)
	}
	withSource(t, src)
	return root, src
}

func TestSnapshotBoundaries(t *testing.T) {
	for _, mode := range []string{"upgrade", "sync", "unchanged", "preview", "declined", "pre failure", "update failure", "post failure", "cleanup failure", "both failures", "settings drift", "setup", "upgrade before setup"} {
		t.Run(mode, func(t *testing.T) {
			root, src := snapshotFixture(t)
			args := []string{"upgrade", "--system", "--checkout", root, "--machine", "vm", "--yes", "--json"}
			want := []string{beforeSnapshot, "sudo dnf5 -y upgrade", afterSnapshot, cleanupSnapshots}
			codeWant := ExitOK
			switch mode {
			case "sync":
				path := filepath.Join(root, "components/snapper.toml")
				data, _ := os.ReadFile(path)
				data = []byte(strings.Replace(string(data), "[[files]]", "default_target='graphical.target'\n[[files]]", 1))
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
				want[1] = "sudo systemctl set-default graphical.target"
			case "unchanged", "declined":
				want = nil
			case "preview":
				args = append(args, "--plan")
				want = nil
			case "pre failure":
				src.Failures[beforeSnapshot] = "snapshot disk full"
				want = want[:1]
				codeWant = ExitFailure
			case "update failure":
				src.Failures[want[1]] = "update failed"
				codeWant = ExitFailure
			case "post failure":
				src.Failures[afterSnapshot] = "post failed"
				codeWant = ExitFailure
			case "cleanup failure":
				src.Failures[cleanupSnapshots] = "cleanup failed"
				codeWant = ExitFailure
			case "both failures":
				src.Failures[want[1]] = "update failed"
				src.Failures[afterSnapshot] = "post failed"
				codeWant = ExitFailure
			case "settings drift":
				src.Files[snapper.Config] = []byte(strings.ReplaceAll(string(src.Files[snapper.Config]), `NUMBER_LIMIT="6"`, `NUMBER_LIMIT="50"`))
				want[1] = snapshotCommand + "set-config NUMBER_LIMIT=6"
			case "setup", "upgrade before setup":
				delete(src.Files, snapper.Config)
				src.Dirs["/etc/snapper/configs"] = nil
				delete(src.Files, "/etc/sysconfig/snapper")
				want = []string{snapshotCommand + "create-config --fstype btrfs --template nimbus /"}
				if mode == "upgrade before setup" {
					want = nil
					codeWant = ExitFailure
				}
			}
			if slices.Contains([]string{"sync", "unchanged", "settings drift", "setup"}, mode) {
				args = append([]string{"sync"}, args[2:]...)
			}
			if mode == "declined" {
				args = slices.DeleteFunc(args, func(arg string) bool { return arg == "--yes" || arg == "--json" })
				old := approver
				approver = func(io.Reader, io.Writer, string) bool { return false }
				t.Cleanup(func() { approver = old })
				codeWant = ExitFailure
			}
			code, out, errOut := run(t, args...)
			if code != codeWant || !slices.Equal(src.mutations, want) {
				t.Fatalf("exit %d want %d; mutations %v want %v\n%s%s", code, codeWant, src.mutations, want, out, errOut)
			}
			if mode == "both failures" && (!strings.Contains(out, "update failed") || !strings.Contains(out, "post failed")) {
				t.Fatal(out)
			}
			if mode == "preview" && !strings.Contains(out, `"snapshots"`) {
				t.Fatal("preview omitted snapshot policy")
			}
		})
	}
}

func TestSnapshotConfigurationChangesInvalidateApproval(t *testing.T) {
	root, src := snapshotFixture(t)
	old := approver
	approver = func(io.Reader, io.Writer, string) bool {
		src.Files[snapper.Config] = []byte(strings.ReplaceAll(string(src.Files[snapper.Config]), `NUMBER_LIMIT="6"`, `NUMBER_LIMIT="50"`))
		return true
	}
	t.Cleanup(func() { approver = old })
	code, out, errOut := run(t, "upgrade", "--system", "--checkout", root, "--machine", "vm")
	if code != ExitFailure || len(src.mutations) != 0 || !strings.Contains(out, "changed while the question was open") {
		t.Fatalf("%d %s%s %v", code, out, errOut, src.mutations)
	}
}

type interruptedSnapshotSource struct {
	*snapshotSource
	command *cobra.Command
}

func (s *interruptedSnapshotSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	if key != "sudo dnf5 -y upgrade" && key != "sudo systemctl set-default graphical.target" {
		return s.snapshotSource.Stream(out, errOut, name, args...)
	}
	s.mutations = append(s.mutations, key)
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := self.Signal(os.Interrupt); err != nil {
		return err
	}
	<-s.command.Context().Done()
	s.mutations = append(s.mutations, "native command stopped")
	return errors.New("native command interrupted")
}

func TestSnapshotInterrupt(t *testing.T) {
	if mode := os.Getenv("NIMBUS_TEST_SNAPSHOT_INTERRUPT"); mode != "" {
		root, src := snapshotFixture(t)
		args := []string{"upgrade", "--system", "--checkout", root, "--machine", "vm", "--yes"}
		nativeCommand := "sudo dnf5 -y upgrade"
		if mode == "sync" {
			path := filepath.Join(root, "components/snapper.toml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), "[[files]]", "default_target='graphical.target'\n[[files]]", 1))
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			args = append([]string{"sync"}, args[2:]...)
			nativeCommand = "sudo systemctl set-default graphical.target"
		}
		if mode == "post failure" {
			src.Failures[afterSnapshot] = "post snapshot failed"
		}
		rootCmd, _ := newRoot()
		command, _, err := rootCmd.Find(args[:1])
		if err != nil {
			t.Fatal(err)
		}
		withSource(t, &interruptedSnapshotSource{snapshotSource: src, command: command})
		var output strings.Builder
		rootCmd.SetOut(&output)
		rootCmd.SetErr(&output)
		rootCmd.SetArgs(args)
		err = rootCmd.Execute()
		want := []string{beforeSnapshot, nativeCommand, "native command stopped", afterSnapshot, cleanupSnapshots}
		if err == nil || !slices.Equal(src.mutations, want) || !strings.Contains(output.String(), "native command interrupted") {
			t.Fatalf("error %v; mutations %v; output %s", err, src.mutations, &output)
		}
		if mode == "post failure" && !strings.Contains(output.String(), "post snapshot failed") {
			t.Fatal(output.String())
		}
		return
	}
	for _, mode := range []string{"sync", "upgrade", "post failure"} {
		t.Run(mode, func(t *testing.T) {
			// A regression must interrupt only this test's subprocess.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSnapshotInterrupt$")
			child.Env = append(os.Environ(), "NIMBUS_TEST_SNAPSHOT_INTERRUPT="+mode)
			if out, err := child.CombinedOutput(); err != nil {
				t.Fatalf("interrupted %s: %v\n%s", mode, err, out)
			}
		})
	}
}

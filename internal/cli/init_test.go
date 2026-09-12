package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/selector"
)

type handoffOutputSource struct {
	native.Source
	afterStream func(string, []string)
}

func (s handoffOutputSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	err := s.Source.Stream(out, errOut, name, args...)
	s.afterStream(name, args)
	return err
}

func TestInitStopsBeforeSelectionWhenStartupOutputFails(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		reuse        bool
	}{
		{"log path", "Installation logs:", false},
		{"hardware", "hardware:", false},
		{"selector reuse", "the selector already names", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			withHardware(src.FakeSource)
			path, err := selector.DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"init", "-y", "--checkout", root, "--machine", "vm"}
			var before []byte
			if tc.reuse {
				if err := selector.Write(path, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: "vm", Origin: "github.com/Furyfree/nimbus"}); err != nil {
					t.Fatal(err)
				}
				before, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				args = []string{"init", "-y", "--checkout", root}
			}
			cmd := New()
			cmd.SetArgs(args)
			cmd.SetOut(resultErrorWriter{match: tc.output, err: syscall.ENOSPC})
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) {
				t.Errorf("startup output failure lost: %v", err)
			}
			if len(src.calls) != 0 {
				t.Errorf("native mutation after failed startup output: %v", src.calls)
			}
			after, err := os.ReadFile(path)
			if tc.reuse {
				if err != nil || string(after) != string(before) {
					t.Errorf("existing selector changed: %q, %v", after, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("selector written after failed startup output: %q, %v", after, err)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed startup output reached mutation lock: %v", err)
			}
			runs, err := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "install", "run-*"))
			if tc.name != "log path" {
				if err != nil || len(runs) != 0 {
					t.Fatalf("preview wrote installation logs: %v, %v", runs, err)
				}
				return
			}
			if err != nil || len(runs) != 1 {
				t.Fatalf("installation log missing: %v, %v", runs, err)
			}
			finished, err := os.ReadFile(filepath.Join(runs[0], ".finished"))
			if err != nil || strings.TrimSpace(string(finished)) != "failed" {
				t.Errorf("failed initialization log was not finished: %q, %v", finished, err)
			}
			if _, err := os.Stat(filepath.Join(runs[0], ".active")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed initialization log remains active: %v", err)
			}
		})
	}
}

func TestInitReportsResultWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		newMachine   bool
	}{
		{"selector", "selected vm; selector written to", false},
		{"manifest", "; the Git change is yours to commit", true},
		{"footer", "Installation time:", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"init", "-y", "--checkout", root, "--machine", "vm"}
			if tc.newMachine {
				saved := pickerFn
				t.Cleanup(func() { pickerFn = saved })
				pickerFn = func(title string, _ []pickItem) ([]string, error) {
					if strings.HasPrefix(title, "profiles") {
						return []string{"common"}, nil
					}
					return nil, nil
				}
				args = []string{"init", "-y", "--checkout", root, "--new", "newbox", "--no-dotfiles"}
			}
			cmd := New()
			cmd.SetArgs(args)
			cmd.SetOut(resultErrorWriter{match: tc.output, err: syscall.ENOSPC})
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) || errors.Is(err, reported{}) {
				t.Fatalf("result output error = %v", err)
			}
			if tc.name != "footer" && len(src.calls) != 0 {
				t.Fatalf("mutated after a failed selection report: %v", src.calls)
			}
			if tc.newMachine {
				if _, err := os.Stat(manifestPath(root, "newbox")); err != nil {
					t.Fatalf("already written manifest lost: %v", err)
				}
			} else {
				path, err := selector.DefaultPath()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := selector.Load(path); err != nil {
					t.Fatalf("already written selector lost: %v", err)
				}
			}
		})
	}
}

func TestInitPromptPreservesEnteredAnswersAndReportsReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   io.Reader
		want string
		err  error
	}{
		{"blank answer", strings.NewReader("\n"), "default", nil},
		{"answer", strings.NewReader(" custom \n"), "custom", nil},
		{"final answer", strings.NewReader("custom"), "custom", nil},
		{"empty EOF", strings.NewReader(""), "", io.EOF},
		{"blank EOF", strings.NewReader("  "), "", io.EOF},
		{"read error", iotest.ErrReader(syscall.EIO), "", syscall.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := promptLineFn(tc.in, io.Discard, "repository", "default")
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("answer = %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestInitPromptFailureStopsBeforeReadingOrWritingSelection(t *testing.T) {
	root, src := installerFixture(t)
	in := strings.NewReader("https://example.invalid/dotfiles.git\n")
	out := &previewErrorWriter{after: -1, err: syscall.ENOSPC}
	saved := pickerFn
	pickerFn = func(title string, _ []pickItem) ([]string, error) {
		if strings.HasPrefix(title, "profiles") {
			return []string{"common"}, nil
		}
		out.after = 0
		return nil, nil
	}
	t.Cleanup(func() { pickerFn = saved })
	cmd := New()
	cmd.SetArgs([]string{"init", "-y", "--checkout", root, "--new", "newbox"})
	cmd.SetIn(in)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("init error = %v", err)
	}
	if in.Len() != len("https://example.invalid/dotfiles.git\n") || len(src.calls) != 0 {
		t.Fatalf("failed prompt consumed input or ran commands: unread=%d, calls=%v", in.Len(), src.calls)
	}
	path, err := selector.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, manifestPath(root, "newbox")} {
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("selection written after prompt failure: %s: %v", target, err)
		}
	}
}

func TestChezmoiHandoffRequiresEveryDisclosureBeforeMutation(t *testing.T) {
	for _, after := range []int{0, 1, 2} {
		t.Run([]string{"initialize", "setup note", "apply"}[after], func(t *testing.T) {
			_, src := installerFixture(t)
			if after > 0 {
				src.Dirs[filepath.Join(os.Getenv("HOME"), ".local", "share", "chezmoi")] = []string{".git"}
			}
			calls := 0
			tracked := handoffOutputSource{Source: src.FakeSource, afterStream: func(string, []string) { calls++ }}
			err := chezmoiHandoff(tracked, &previewErrorWriter{after: after, err: syscall.ENOSPC}, "vm", []string{"common"}, &definitions.Dotfiles{Repo: "https://github.com/Furyfree/dotfiles.git"}, false)
			if !errors.Is(err, syscall.ENOSPC) || calls != 0 {
				t.Fatalf("handoff error = %v, mutations = %d", err, calls)
			}
		})
	}
}

type unreadableHandoffSource struct {
	native.Source
	err error
}

func (s unreadableHandoffSource) ReadDir(path string) ([]string, error) {
	return nil, &os.PathError{Op: "readdir", Path: path, Err: s.err}
}

func TestChezmoiHandoffStopsWhenSourceCannotBeInspected(t *testing.T) {
	for _, readErr := range []error{os.ErrPermission, syscall.EIO} {
		t.Run(readErr.Error(), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			calls := 0
			src := handoffOutputSource{
				Source:      unreadableHandoffSource{Source: &nativetest.FakeSource{Paths: map[string]string{"chezmoi": "/usr/bin/chezmoi"}}, err: readErr},
				afterStream: func(string, []string) { calls++ },
			}
			err := chezmoiHandoff(src, io.Discard, "vm", []string{"common"}, &definitions.Dotfiles{Repo: "https://example.invalid/dotfiles.git"}, false)
			if !errors.Is(err, readErr) || calls != 0 {
				t.Fatalf("source inspection error = %v, mutation attempts = %d", err, calls)
			}
			if !strings.Contains(err.Error(), filepath.Join(home, ".local", "share", "chezmoi")) {
				t.Fatalf("source inspection error lacks its path: %v", err)
			}
		})
	}
}

func TestInitDisclosesDotfilesBeforeSystemWork(t *testing.T) {
	root, src := installerFixture(t)
	withHardware(src.FakeSource)
	cmd := New()
	cmd.SetArgs([]string{"init", "-y", "--checkout", root, "--machine", "vm"})
	cmd.SetOut(resultErrorWriter{match: "Initialize Chezmoi from", err: syscall.ENOSPC})
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("init error = %v", err)
	}
	if len(src.calls) != 0 || slices.Contains(src.reads, "dnf5 makecache") {
		t.Fatalf("system work preceded the failed disclosure: calls=%v, reads=%v", src.calls, src.reads)
	}
}

type initOutputWriter func([]byte) (int, error)

func (w initOutputWriter) Write(p []byte) (int, error) { return w(p) }

func TestInitChecksSelectedDefinitionsBeforeSystemSync(t *testing.T) {
	for _, tc := range []struct {
		name              string
		newMachine, drift bool
	}{
		{name: "tracked"},
		{name: "tracked with drift", drift: true},
		{name: "new", newMachine: true},
		{name: "new with drift", newMachine: true, drift: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=[]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			machine := "vm"
			args := []string{"init", "-y", "--checkout", root, "--machine", machine}
			if tc.newMachine {
				machine = "newbox"
				args = []string{"init", "-y", "--checkout", root, "--new", machine, "--no-dotfiles"}
				saved := pickerFn
				t.Cleanup(func() { pickerFn = saved })
				pickerFn = func(title string, _ []pickItem) ([]string, error) {
					if strings.HasPrefix(title, "profiles") {
						return []string{"common"}, nil
					}
					return nil, nil
				}
			}
			var out, errOut strings.Builder
			selected := false
			writer := initOutputWriter(func(p []byte) (int, error) {
				if !selected && strings.Contains(string(p), "selector written to") {
					selected = true
					if tc.drift {
						path := manifestPath(root, machine)
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, append(data, "# changed after selection\n"...), 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				return out.Write(p)
			})
			code := Execute(args, writer, &errOut)
			if !selected {
				t.Fatalf("selection boundary not reached: %d %s%s", code, &out, &errOut)
			}
			if tc.drift {
				if code != ExitFailure || !strings.Contains(out.String(), "definitions changed during initialization") {
					t.Fatalf("definition drift was not reported: %d %s%s", code, &out, &errOut)
				}
				if len(src.calls) != 0 || slices.Contains(src.reads, "dnf5 makecache") {
					t.Fatalf("definition drift reached system work: calls=%v, reads=%v", src.calls, src.reads)
				}
			} else if code != ExitOK || !strings.Contains(out.String(), "succeeded  system installation") {
				t.Fatalf("unchanged definitions did not reach system sync: %d %s%s; calls=%v", code, &out, &errOut, src.calls)
			}
		})
	}
}

func TestInitReturnsSummaryFailureAndPreservesNativeFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "native failure"}[failed], func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles", "common.toml"), []byte("schema = 1\nid = \"common\"\npackages = []\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if failed {
				src.Failures["chezmoi apply"] = "tool installation failed"
			}
			out := &previewErrorWriter{after: -1, err: syscall.ENOSPC}
			withSource(t, handoffOutputSource{Source: src, afterStream: func(name string, args []string) {
				if name == "chezmoi" && slices.Equal(args, []string{"apply"}) {
					out.after = 0
				}
			}})
			cmd := New()
			cmd.SetArgs([]string{"init", "-y", "--checkout", root, "--machine", "vm"})
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if !errors.Is(err, syscall.ENOSPC) || !slices.Contains(src.calls, "chezmoi apply") || failed && !strings.Contains(err.Error(), "chezmoi apply: chezmoi failed") {
				t.Fatalf("summary error = %v, calls = %v", err, src.calls)
			}
		})
	}
}

func withHardware(src *nativetest.FakeSource) {
	src.Files[filepath.Join(inspect.DMIDir, "product_name")] = []byte("HP EliteBook X G1a 14 inch Notebook Next Gen AI PC\n")
	src.Files[filepath.Join(inspect.DMIDir, "board_name")] = []byte("8CB1\n")
	src.Files[filepath.Join(inspect.DMIDir, "chassis_type")] = []byte("10\n")
	src.Dirs[inspect.PCIDir] = []string{"0000:c1:00.0"}
	src.Files[filepath.Join(inspect.PCIDir, "0000:c1:00.0", "class")] = []byte("0x030000\n")
	src.Files[filepath.Join(inspect.PCIDir, "0000:c1:00.0", "vendor")] = []byte("0x1002\n")
	src.Files[filepath.Join(inspect.PCIDir, "0000:c1:00.0", "device")] = []byte("0x150e\n")
}

func TestInitYesUsesExplicitMachineAndReusesSelectorWithoutConfirmation(t *testing.T) {
	root, src := installerFixture(t)
	saved := approver
	approver = func(io.Reader, io.Writer, string) bool {
		t.Fatal("init requested confirmation")
		return false
	}
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "init", "-y", "--checkout", root, "--machine", "vm")
	if code != ExitOK || !strings.Contains(out, "selected vm; selector written to") || !strings.Contains(out, "plan for vm") || !slices.Contains(src.calls, "chezmoi apply") {
		t.Fatalf("init: %d\n%s%s", code, out, errOut)
	}
	path, _ := selector.DefaultPath()
	sel, err := selector.Load(path)
	if err != nil || sel.Machine != "vm" || sel.Checkout != root || sel.Origin != "github.com/Furyfree/nimbus" {
		t.Fatalf("selector = %+v %v", sel, err)
	}
	// Explicitly approved reruns retain the selection.
	for _, extra := range [][]string{nil, {"--yes"}} {
		args := append([]string{"init", "-y", "--checkout", root}, extra...)
		if code, out, errOut := run(t, args...); code != ExitOK || !strings.Contains(out, "the selector already names vm") {
			t.Fatalf("rerun: %d\n%s%s", code, out, errOut)
		}
	}
}

func TestInitRequiresExplicitSelectionBeforeMutation(t *testing.T) {
	root, src := installerFixture(t)
	code, out, errOut := run(t, "init", "-y", "--checkout", root)
	if code != ExitUsage || !strings.Contains(out+errOut, "pass --machine ID (available: vm)") || len(src.calls) != 0 {
		t.Fatalf("missing selection: %d calls=%v\n%s%s", code, src.calls, out, errOut)
	}
	path, _ := selector.DefaultPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing selection wrote a selector: %v", err)
	}
}

func TestInitAsksForMachineBeforeApproval(t *testing.T) {
	for _, tc := range []struct {
		name, input, wantErr string
		plan, applied        bool
	}{
		{"approved", "vm\ny\n", "", false, true},
		{"preview", "vm\n", "", true, false},
		{"declined", "vm\nn\n", "not applied", false, false},
		{"approval EOF", "vm\n", "not applied", false, false},
		{"selection EOF", "", "choose machine: EOF", false, false},
		{"blank selection", "\n", "no machine selected", false, false},
		{"unknown machine", "unknown\n", "machine unknown is not tracked", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			manifest, err := os.ReadFile(manifestPath(root, "vm"))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"laptop", "desktop"} {
				if err := os.WriteFile(manifestPath(root, id), []byte(strings.Replace(string(manifest), `id = "vm"`, `id = "`+id+`"`, 1)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out strings.Builder
			cmd := New()
			args := []string{"init", "--checkout", root}
			if tc.plan {
				args = append(args, "--plan")
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader(tc.input))
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			err = cmd.Execute()
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("init: %v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), "Choose machine (desktop, laptop, vm):") {
				t.Fatalf("machine choices missing:\n%s", out.String())
			}
			path, _ := selector.DefaultPath()
			sel, err := selector.Load(path)
			if tc.applied {
				if err != nil || sel.Machine != "vm" || !slices.Contains(src.calls, "chezmoi apply") {
					t.Fatalf("selection = %+v, %v; calls = %v", sel, err, src.calls)
				}
			} else if !errors.Is(err, os.ErrNotExist) || len(src.calls) != 0 {
				t.Fatalf("unapproved selection changed state: %+v, %v; calls = %v", sel, err, src.calls)
			}
		})
	}
}

func TestInitDescribesANewMachineFromTheHardware(t *testing.T) {
	applyEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withHardware(src)
	withSource(t, src)
	var titles []string
	var componentItems []pickItem
	savedPick := pickerFn
	pickerFn = func(title string, items []pickItem) ([]string, error) {
		titles = append(titles, title)
		if strings.HasPrefix(title, "profiles") {
			return []string{"development", "hyprland-noctalia"}, nil
		}
		componentItems = items
		var chosen []string
		for _, it := range items {
			if it.Selected {
				chosen = append(chosen, it.ID)
			}
		}
		return chosen, nil
	}
	t.Cleanup(func() { pickerFn = savedPick })
	var asked string
	savedPrompt := promptLineFn
	promptLineFn = func(_ io.Reader, _ io.Writer, prompt, def string) (string, error) {
		asked = prompt + " [" + def + "]"
		return def, nil
	}
	t.Cleanup(func() { promptLineFn = savedPrompt })

	code, out, errOut := run(t, "init", "--plan", "--checkout", root, "--new", "mybox")
	if code != ExitOK || !strings.Contains(out, "Create machine manifest:") || !strings.Contains(out, "Select mybox") {
		t.Fatalf("init --new: %d %q\n%s", code, errOut, out)
	}
	if len(titles) != 2 || !strings.HasPrefix(titles[0], "profiles for mybox") || !strings.HasPrefix(titles[1], "components for mybox") {
		t.Fatalf("dialog order = %v", titles)
	}
	detected := map[string]bool{}
	for _, it := range componentItems {
		if it.Selected {
			detected[it.ID] = true
		}
	}
	if !detected["amd-graphics"] || !detected["laptop-power"] || detected["nvidia"] || detected["desktop-display"] {
		t.Fatalf("detected = %v", detected)
	}
	if asked != "dotfiles repository for Chezmoi (empty for none) [https://github.com/Furyfree/dotfiles.git]" {
		t.Fatalf("dotfiles prompt = %q", asked)
	}
	manifest := strings.ReplaceAll(out, "\n+", "\n")
	for _, want := range []string{"# mybox: HP EliteBook X G1a", "hardware = 'HP EliteBook X G1a 14 inch Notebook Next Gen AI PC'", "shell = 'bash'", "'common',\n  'development',\n  'hyprland-noctalia'", "'amd-graphics',\n  'laptop-power'", "[dotfiles]\nrepo = 'https://github.com/Furyfree/dotfiles.git'"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %q:\n%s", want, manifest)
		}
	}
	if _, err := os.Stat(manifestPath(root, "mybox")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote a new manifest: %v", err)
	}
	if code, _, errOut := run(t, "init", "--checkout", root, "--new", "Bad Name"); code != ExitFailure || !strings.Contains(errOut, "lowercase letters") {
		t.Fatalf("bad ID: %d %q", code, errOut)
	}
}

func TestChezmoiHandoffRunsOnceWithThePromptFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	var out strings.Builder
	dotfiles := &definitions.Dotfiles{Repo: "https://github.com/Furyfree/dotfiles.git"}
	// Without chezmoi the handoff waits for the next run.
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common", "development"}, dotfiles, false); err == nil || !strings.Contains(err.Error(), "chezmoi is not installed yet") {
		t.Fatalf("without chezmoi: %v\n%s", err, out.String())
	}
	src.Paths["chezmoi"] = "/usr/bin/chezmoi"
	key := nativetest.Key("chezmoi", "init", "--promptString", "Machine=laptop", "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=common/development", "--promptBool", "Enable 1Password SSH integration=false", "--", dotfiles.Repo)
	src.Commands[key] = nil
	src.Commands[nativetest.Key("chezmoi", "apply")] = nil
	src.Commands["chezmoi source-path"] = []byte(home)
	src.Commands[nativetest.Key("git", inspect.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte(dotfiles.Repo)
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"laptop","ManagedByNimbus":true,"Profiles":["common","development"],"profiles":["common","unix","linux","development"]}`)
	// The empty directory a failed clone left behind does not count as
	// initialized; the handoff runs again.
	src.Dirs[filepath.Join(home, ".local", "share", "chezmoi")] = []string{}
	out.Reset()
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common", "development"}, dotfiles, false); err != nil || !strings.Contains(out.String(), "$ chezmoi apply") {
		t.Fatalf("handoff: %v\n%s", err, out.String())
	}
	// Initialized already: only the refresh command is printed.
	src.Dirs[filepath.Join(home, ".local", "share", "chezmoi")] = []string{".git"}
	delete(src.Commands, key)
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"laptop","ManagedByNimbus":true,"Profiles":["common"],"profiles":["common","unix","linux"]}`)
	out.Reset()
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common"}, dotfiles, false); err != nil || !strings.Contains(out.String(), "chezmoi init --prompt --promptString Machine=laptop") {
		t.Fatalf("initialized: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := chezmoiHandoff(src, &out, "vm", nil, nil, false); err != nil || !strings.Contains(out.String(), "dotfiles skipped") {
		t.Fatalf("no dotfiles: %v\n%s", err, out.String())
	}
}

func TestInitOnePasswordSSHIsExplicitAndPreservesExistingSelection(t *testing.T) {
	root, src := installerFixture(t)
	initial := "chezmoi init --promptString Machine=vm --promptBool ManagedByNimbus=true --promptMultichoice Profiles=common --promptBool Enable 1Password SSH integration="
	delete(src.Commands, initial+"false -- https://github.com/Furyfree/dotfiles.git")
	src.Commands[initial+"true -- https://github.com/Furyfree/dotfiles.git"] = nil
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
	code, out, errOut := run(t, "init", "-y", "--checkout", root, "--machine", "vm", "--onepassword-ssh")
	if code != ExitOK || !slices.Contains(src.calls, initial+"true -- https://github.com/Furyfree/dotfiles.git") {
		t.Fatalf("opt-in failed: %d %s%s", code, out, errOut)
	}
	src.Dirs[filepath.Join(os.Getenv("HOME"), ".local", "share", "chezmoi")] = []string{".git"}
	src.calls = nil
	code, out, errOut = run(t, "init", "-y", "--checkout", root, "--machine", "vm")
	if code != ExitOK || !strings.Contains(out, "Enable 1Password SSH integration=true") {
		t.Fatalf("existing opt-in lost: %d %s%s", code, out, errOut)
	}
	if slices.ContainsFunc(src.calls, func(call string) bool { return strings.HasPrefix(call, "chezmoi init") }) {
		t.Fatal("existing source reinitialized")
	}
}

func TestInitRejectsOnePasswordWithoutDotfilesBeforeMutation(t *testing.T) {
	root, src := installerFixture(t)
	code, _, errOut := run(t, "init", "-y", "--checkout", root, "--new", "newbox", "--no-dotfiles", "--onepassword-ssh")
	if code != ExitUsage || !strings.Contains(errOut, "exclude each other") || len(src.calls) != 0 {
		t.Fatalf("conflicting flags reached mutation: %d %s %v", code, errOut, src.calls)
	}
}

func TestInitPreservesHardwareAndKeepsItsCommentOnOneLine(t *testing.T) {
	root, src := installerFixture(t)
	withHardware(src.FakeSource)
	const hardware = "fixture\a\v\x7f\nidentity"
	src.Files[filepath.Join(inspect.DMIDir, "product_name")] = []byte(hardware)
	src.Files[filepath.Join(inspect.DMIDir, "board_name")] = []byte("invalid\xffboard")
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=[]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := pickerFn
	t.Cleanup(func() { pickerFn = saved })
	pickerFn = func(title string, _ []pickItem) ([]string, error) {
		if strings.HasPrefix(title, "profiles") {
			return []string{"common"}, nil
		}
		return nil, nil
	}
	code, out, errOut := run(t, "init", "-y", "--checkout", root, "--new", "newbox", "--no-dotfiles")
	if code != ExitOK {
		t.Fatalf("init failed: %d %s%s", code, out, errOut)
	}
	checkout, err := loadCheckout(root)
	if err != nil || checkout.Machines["newbox"].Hardware != hardware {
		t.Fatalf("hardware did not survive initialization: %+v, %v", checkout, err)
	}
	data, err := os.ReadFile(manifestPath(root, "newbox"))
	if err != nil {
		t.Fatal(err)
	}
	comment, rest, _ := strings.Cut(string(data), "\n")
	if !strings.Contains(comment, "identity") || !strings.HasPrefix(rest, "schema = 1\n") {
		t.Fatalf("hardware comment is not one complete line: %q", data)
	}
}

func TestInitRejectsInvalidManifestTextBeforeSelectionWrites(t *testing.T) {
	for _, field := range []string{"hardware", "dotfiles"} {
		t.Run(field, func(t *testing.T) {
			root, src := installerFixture(t)
			withHardware(src.FakeSource)
			saved := pickerFn
			t.Cleanup(func() { pickerFn = saved })
			pickerFn = func(title string, _ []pickItem) ([]string, error) {
				if strings.HasPrefix(title, "profiles") {
					return []string{"common"}, nil
				}
				return nil, nil
			}
			args := []string{"init", "-y", "--checkout", root, "--new", "newbox"}
			if field == "hardware" {
				src.Files[filepath.Join(inspect.DMIDir, "product_name")] = []byte("invalid\xffidentity")
				args = append(args, "--no-dotfiles")
			} else {
				args = append(args, "--dotfiles", "git@example.invalid:owner/invalid\xffrepo")
			}
			code, out, errOut := run(t, args...)
			if code != ExitFailure || !strings.Contains(out+errOut, "invalid UTF-8") || len(src.calls) != 0 {
				t.Fatalf("invalid text reached installation: %d %s%s; calls=%v", code, out, errOut, src.calls)
			}
			path, err := selector.DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{path, manifestPath(root, "newbox")} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid text wrote selection: %s: %v", path, err)
				}
			}
		})
	}
}

func TestInitPreviewAndRefusalLeaveNoInstallationState(t *testing.T) {
	for _, mode := range []string{"plan", "declined", "EOF"} {
		for _, newMachine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/new=%t", mode, newMachine), func(t *testing.T) {
				root, src := installerFixture(t)
				var out, errOut strings.Builder
				cmd := New()
				args := []string{"init", "--checkout", root, "--machine", "vm"}
				if newMachine {
					saved := pickerFn
					t.Cleanup(func() { pickerFn = saved })
					pickerFn = func(title string, _ []pickItem) ([]string, error) {
						if strings.HasPrefix(title, "profiles") {
							return []string{"common"}, nil
						}
						return nil, nil
					}
					args = []string{"init", "--checkout", root, "--new", "newbox", "--dotfiles", "https://github.com/Furyfree/dotfiles.git"}
				}
				if mode == "plan" {
					args = append(args, "--plan")
				}
				cmd.SetArgs(args)
				cmd.SetOut(&out)
				cmd.SetErr(&errOut)
				input := strings.NewReader("")
				if mode != "EOF" {
					input = strings.NewReader("n\n")
				}
				cmd.SetIn(input)
				err := cmd.Execute()
				if mode == "plan" && err != nil || mode != "plan" && (err == nil || !strings.Contains(err.Error(), "not applied")) {
					t.Fatalf("init = %v; output=%s%s", err, &out, &errOut)
				}
				if !strings.Contains(out.String(), "plan for ") || !strings.Contains(out.String(), "Initialize Chezmoi from") || !strings.Contains(out.String(), "Select ") {
					t.Fatalf("incomplete installation preview: %s", &out)
				}
				if mode == "plan" && input.Len() != len("n\n") {
					t.Fatal("preview requested approval")
				}
				if len(src.calls) != 0 || slices.Contains(src.reads, "dnf5 makecache") {
					t.Fatalf("preview or refusal ran mutation: calls=%v; reads=%v", src.calls, src.reads)
				}
				selectorPath, err := selector.DefaultPath()
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{selectorPath, manifestPath(root, "newbox"), stateRoot,
					filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"),
					filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "install")} {
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("preview or refusal wrote %s: %v", path, err)
					}
				}
			})
		}
	}
}

func TestInitRechecksApprovalBeforeWritingSelector(t *testing.T) {
	for _, change := range []string{"none", "definitions", "origin", "commit", "system", "selector"} {
		t.Run(change, func(t *testing.T) {
			root, src := installerFixture(t)
			if change == "system" {
				if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['bash']\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out, errOut strings.Builder
			path, err := selector.DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			var selectorBefore []byte
			saved := approver
			t.Cleanup(func() { approver = saved })
			asked := 0
			approver = func(io.Reader, io.Writer, string) bool {
				asked++
				if !strings.Contains(out.String(), "plan for vm") || !strings.Contains(out.String(), "Initialize Chezmoi from") || len(src.calls) != 0 {
					t.Fatal("approval preceded the preview or followed mutation")
				}
				switch change {
				case "definitions":
					if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=[]\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				case "origin":
					if err := os.WriteFile(filepath.Join(root, ".git/config"), []byte("[remote \"origin\"]\nurl=https://github.com/changed/nimbus.git\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				case "commit":
					src.Commands[nativetest.Key("git", inspect.GitArgs(root, "rev-parse", "HEAD")...)] = []byte("changed\n")
				case "system":
					key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
					src.Commands[key] = []byte(strings.ReplaceAll(string(src.Commands[key]), "bash|0|", "bash|1|"))
				case "selector":
					if err := selector.Write(path, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: "other", Origin: "github.com/Furyfree/nimbus"}); err != nil {
						t.Fatal(err)
					}
					selectorBefore, err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				return true
			}
			code := Execute([]string{"init", "--checkout", root, "--machine", "vm"}, &out, &errOut)
			if asked != 1 {
				t.Fatalf("approval count = %d: %s%s", asked, &out, &errOut)
			}
			if change == "none" {
				if code != ExitOK || !slices.Contains(src.calls, "chezmoi apply") {
					t.Fatalf("approved installation failed: %d %s%s", code, &out, &errOut)
				}
				return
			}
			if code != ExitFailure || !strings.Contains(out.String()+errOut.String(), "changed during") || len(src.calls) != 0 || slices.Contains(src.reads, "dnf5 makecache") {
				t.Fatalf("approval change reached installation: %d %s%s; calls=%v", code, &out, &errOut, src.calls)
			}
			after, err := os.ReadFile(path)
			if change == "selector" {
				if err != nil || string(after) != string(selectorBefore) {
					t.Fatalf("changed selector overwritten: %q, %v", after, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("selector written after approval changed: %q, %v", after, err)
			}
		})
	}
}

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/selector"
)

type handoffOutputSource struct {
	facts.Source
	afterStream func(string, []string)
}

func (s handoffOutputSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	err := s.Source.Stream(out, errOut, name, args...)
	s.afterStream(name, args)
	return err
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
	cmd.SetArgs([]string{"init", "--checkout", root, "--new", "newbox"})
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

func TestInitDisclosesDotfilesBeforeSystemWork(t *testing.T) {
	root, src := installerFixture(t)
	withHardware(src.FakeSource)
	cmd := New()
	cmd.SetArgs([]string{"init", "--checkout", root, "--machine", "vm"})
	// The log location, hardware, and selected-machine lines precede the
	// dotfiles and tool-script disclosure.
	cmd.SetOut(&previewErrorWriter{after: 3, err: syscall.ENOSPC})
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("init error = %v", err)
	}
	if len(src.calls) != 0 || slices.Contains(src.reads, "dnf5 makecache") {
		t.Fatalf("system work preceded the failed disclosure: calls=%v, reads=%v", src.calls, src.reads)
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
			cmd.SetArgs([]string{"init", "--checkout", root, "--machine", "vm"})
			cmd.SetOut(out)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if !errors.Is(err, syscall.ENOSPC) || !slices.Contains(src.calls, "chezmoi apply") || failed && !strings.Contains(err.Error(), "chezmoi apply: chezmoi failed") {
				t.Fatalf("summary error = %v, calls = %v", err, src.calls)
			}
		})
	}
}

func withHardware(src *facts.FakeSource) {
	src.Files[filepath.Join(facts.DMIDir, "product_name")] = []byte("HP EliteBook X G1a 14 inch Notebook Next Gen AI PC\n")
	src.Files[filepath.Join(facts.DMIDir, "board_name")] = []byte("8CB1\n")
	src.Files[filepath.Join(facts.DMIDir, "chassis_type")] = []byte("10\n")
	src.Dirs[facts.PCIDir] = []string{"0000:c1:00.0"}
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "class")] = []byte("0x030000\n")
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "vendor")] = []byte("0x1002\n")
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "device")] = []byte("0x150e\n")
}

func TestInitUsesExplicitMachineAndReusesSelectorWithoutConfirmation(t *testing.T) {
	root, src := installerFixture(t)
	saved := approver
	approver = func(io.Reader, io.Writer, string) bool {
		t.Fatal("init requested confirmation")
		return false
	}
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm")
	if code != ExitOK || !strings.Contains(out, "selected vm; selector written to") || !strings.Contains(out, "plan for vm") || !slices.Contains(src.calls, "chezmoi apply") {
		t.Fatalf("init: %d\n%s%s", code, out, errOut)
	}
	path, _ := selector.DefaultPath()
	sel, err := selector.Load(path)
	if err != nil || sel.Machine != "vm" || sel.Checkout != root || sel.Origin != "github.com/Furyfree/nimbus" {
		t.Fatalf("selector = %+v %v", sel, err)
	}
	// Both default reruns and older commands with --yes retain the selection.
	for _, extra := range [][]string{nil, {"--yes"}} {
		args := append([]string{"init", "--checkout", root}, extra...)
		if code, out, errOut := run(t, args...); code != ExitOK || !strings.Contains(out, "the selector already names vm") {
			t.Fatalf("rerun: %d\n%s%s", code, out, errOut)
		}
	}
}

func TestInitRequiresExplicitSelectionBeforeMutation(t *testing.T) {
	root, src := installerFixture(t)
	code, out, errOut := run(t, "init", "--checkout", root)
	if code != ExitUsage || !strings.Contains(out+errOut, "pass --machine ID (available: vm)") || len(src.calls) != 0 {
		t.Fatalf("missing selection: %d calls=%v\n%s%s", code, src.calls, out, errOut)
	}
	path, _ := selector.DefaultPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing selection wrote a selector: %v", err)
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

	code, out, errOut := run(t, "init", "--checkout", root, "--new", "mybox", "-y")
	if code != ExitFailure || !strings.Contains(out, "wrote "+filepath.Join(root, "machines", "mybox.toml")) || !strings.Contains(out, "selected mybox") {
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
	data, _ := os.ReadFile(filepath.Join(root, "machines", "mybox.toml"))
	manifest := string(data)
	for _, want := range []string{"# mybox: HP EliteBook X G1a", `hardware = "HP EliteBook X G1a 14 inch Notebook Next Gen AI PC"`, "\"common\",\n  \"development\",\n  \"hyprland-noctalia\",", "\"amd-graphics\",\n  \"laptop-power\",", "[dotfiles]\nrepo = \"https://github.com/Furyfree/dotfiles.git\""} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %q:\n%s", want, manifest)
		}
	}
	c, err := definitions.Load(root)
	if err != nil || len(definitions.Validate(c)) > 0 {
		t.Fatalf("the new manifest must validate: %v", err)
	}
	if code, _, errOut := run(t, "init", "--checkout", root, "--new", "mybox", "--no-dotfiles"); code != ExitFailure || !strings.Contains(errOut, "already exists") {
		t.Fatalf("existing ID: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "init", "--checkout", root, "--new", "Bad Name"); code != ExitFailure || !strings.Contains(errOut, "lowercase letters") {
		t.Fatalf("bad ID: %d %q", code, errOut)
	}
}

func TestChezmoiHandoffRunsOnceWithThePromptFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := &facts.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	var out strings.Builder
	dotfiles := &definitions.Dotfiles{Repo: "https://github.com/Furyfree/dotfiles.git"}
	// Without chezmoi the handoff waits for the next run.
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common", "development"}, dotfiles, false); err == nil || !strings.Contains(err.Error(), "chezmoi is not installed yet") {
		t.Fatalf("without chezmoi: %v\n%s", err, out.String())
	}
	src.Paths["chezmoi"] = "/usr/bin/chezmoi"
	key := facts.Key("chezmoi", "init", "--promptString", "Machine=laptop", "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=common/development", "--promptBool", "Enable 1Password SSH integration=false", "--", dotfiles.Repo)
	src.Commands[key] = nil
	src.Commands[facts.Key("chezmoi", "apply")] = nil
	src.Commands["chezmoi source-path"] = []byte(home)
	src.Commands[facts.Key("git", facts.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte(dotfiles.Repo)
	src.Commands[facts.Key("chezmoi", facts.ChezmoiDataArgs...)] = []byte(`{"Machine":"laptop","ManagedByNimbus":true,"Profiles":["common","development"],"profiles":["common","unix","linux","development"]}`)
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
	src.Commands[facts.Key("chezmoi", facts.ChezmoiDataArgs...)] = []byte(`{"Machine":"laptop","ManagedByNimbus":true,"Profiles":["common"],"profiles":["common","unix","linux"]}`)
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
	src.Commands[facts.Key("chezmoi", facts.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "--onepassword-ssh", "-y")
	if code != ExitOK || !slices.Contains(src.calls, initial+"true -- https://github.com/Furyfree/dotfiles.git") {
		t.Fatalf("opt-in failed: %d %s%s", code, out, errOut)
	}
	src.Dirs[filepath.Join(os.Getenv("HOME"), ".local", "share", "chezmoi")] = []string{".git"}
	src.calls = nil
	code, out, errOut = run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK || !strings.Contains(out, "Enable 1Password SSH integration=true") {
		t.Fatalf("existing opt-in lost: %d %s%s", code, out, errOut)
	}
	if slices.ContainsFunc(src.calls, func(call string) bool { return strings.HasPrefix(call, "chezmoi init") }) {
		t.Fatal("existing source reinitialized")
	}
}

func TestInitRejectsOnePasswordWithoutDotfilesBeforeMutation(t *testing.T) {
	root, src := installerFixture(t)
	code, _, errOut := run(t, "init", "--checkout", root, "--new", "newbox", "--no-dotfiles", "--onepassword-ssh")
	if code != ExitUsage || !strings.Contains(errOut, "exclude each other") || len(src.calls) != 0 {
		t.Fatalf("conflicting flags reached mutation: %d %s %v", code, errOut, src.calls)
	}
}

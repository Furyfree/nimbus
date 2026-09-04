package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/selector"
)

func withHardware(src *facts.FakeSource) {
	src.Files[filepath.Join(facts.DMIDir, "product_name")] = []byte("HP EliteBook X G1a 14 inch Notebook Next Gen AI PC\n")
	src.Files[filepath.Join(facts.DMIDir, "board_name")] = []byte("8CB1\n")
	src.Files[filepath.Join(facts.DMIDir, "chassis_type")] = []byte("10\n")
	src.Dirs[facts.PCIDir] = []string{"0000:c1:00.0"}
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "class")] = []byte("0x030000\n")
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "vendor")] = []byte("0x1002\n")
	src.Files[filepath.Join(facts.PCIDir, "0000:c1:00.0", "device")] = []byte("0x150e\n")
}

func TestInitPicksTheMatchingMachineWritesTheSelectorAndSyncs(t *testing.T) {
	applyEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withHardware(src)
	withSource(t, src)
	var offered []pickItem
	saved := pickOneFn
	pickOneFn = func(title string, items []pickItem) (string, error) {
		offered = items
		for _, it := range items {
			if it.Selected {
				return it.ID, nil
			}
		}
		return "", nil
	}
	t.Cleanup(func() { pickOneFn = saved })
	code, out, _ := run(t, "init", "--checkout", root, "-y")
	// The sync that follows stops at the first repository, which needs the
	// network; everything before it must have happened.
	if code != ExitFailure || !strings.Contains(out, "hardware: HP EliteBook X G1a") || !strings.Contains(out, "selected laptop; selector written to") || !strings.Contains(out, "sync stopped at repository:brave") {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if len(offered) != 4 || offered[0].ID != "desktop" || offered[1].ID != "laptop" || !offered[1].Selected || offered[3].ID != "new" || offered[3].Selected {
		t.Fatalf("offered = %+v", offered)
	}
	path, _ := selector.DefaultPath()
	sel, err := selector.Load(path)
	if err != nil || sel.Machine != "laptop" || sel.Checkout != root || sel.Origin != "github.com/Furyfree/nimbus" {
		t.Fatalf("selector = %+v %v", sel, err)
	}
	// A rerun reuses the selector without asking.
	offered = nil
	if _, out, _ := run(t, "init", "--checkout", root, "-y"); !strings.Contains(out, "the selector already names laptop") || offered != nil {
		t.Fatalf("rerun asked again:\n%s", out)
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
	promptLineFn = func(_ io.Reader, _ io.Writer, prompt, def string) string {
		asked = prompt + " [" + def + "]"
		return def
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
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common", "development"}, dotfiles); err != nil || !strings.Contains(out.String(), "chezmoi is not installed yet") {
		t.Fatalf("without chezmoi: %v\n%s", err, out.String())
	}
	src.Paths["chezmoi"] = "/usr/bin/chezmoi"
	key := facts.Key("chezmoi", "init", "--promptString", "Machine=laptop", "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=common/development", dotfiles.Repo)
	src.Commands[key] = nil
	// The empty directory a failed clone left behind does not count as
	// initialized; the handoff runs again.
	src.Dirs[filepath.Join(home, ".local", "share", "chezmoi")] = []string{}
	out.Reset()
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common", "development"}, dotfiles); err != nil || !strings.Contains(out.String(), "chezmoi diff") {
		t.Fatalf("handoff: %v\n%s", err, out.String())
	}
	// Initialized already: only the refresh command is printed.
	src.Dirs[filepath.Join(home, ".local", "share", "chezmoi")] = []string{".git"}
	delete(src.Commands, key)
	out.Reset()
	if err := chezmoiHandoff(src, &out, "laptop", []string{"common"}, dotfiles); err != nil || !strings.Contains(out.String(), "chezmoi init --prompt --promptString Machine=laptop") {
		t.Fatalf("initialized: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := chezmoiHandoff(src, &out, "vm", nil, nil); err != nil || !strings.Contains(out.String(), "stays pending") {
		t.Fatalf("no dotfiles: %v\n%s", err, out.String())
	}
}

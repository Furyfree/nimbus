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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestSelectionStopsWhenReviewCannotBeWritten(t *testing.T) {
	for _, tc := range []struct {
		name        string
		yes, asJSON bool
		after       int
	}{
		{name: "interactive diff"},
		{name: "interactive plan", after: 1},
		{name: "interactive approval notice", after: 2},
		{name: "interactive prompt", after: 3},
		{name: "automatic diff", yes: true},
		{name: "automatic plan", yes: true, after: 1},
		{name: "JSON diff", asJSON: true},
		{name: "JSON plan", asJSON: true, after: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema = 1\nid = \"common\"\npackages = []\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "components/demo.toml"), []byte("schema = 1\nid = \"demo\"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := manifestPath(root, "vm")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeErr := errors.New("review output unavailable")
			writer := &previewErrorWriter{after: tc.after, err: writeErr}
			cmd := newComponents(&options{json: tc.asJSON})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			args := []string{"add", "demo", "--checkout", root, "--machine", "vm"}
			if tc.yes {
				args = append(args, "--yes")
			}
			if tc.asJSON {
				args = append(args, "--yes")
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader("yes\n"))
			cmd.SetOut(writer)
			cmd.SetErr(io.Discard)
			if tc.asJSON {
				cmd.SetOut(io.Discard)
				cmd.SetErr(writer)
			}
			if err := cmd.Execute(); err == nil || tc.after < 3 && !errors.Is(err, writeErr) {
				t.Fatalf("review failure not reported: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("failed review changed manifest: %q, %v", after, err)
			}
			if len(src.calls) != 0 {
				t.Fatalf("failed review reached native mutation: %v", src.calls)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed review reached mutation lock: %v", err)
			}
		})
	}
}

func TestSelectionReportsUnchangedOutputFailure(t *testing.T) {
	root, src := installerFixture(t)
	path := manifestPath(root, "vm")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("result output unavailable")
	cmd := newProfiles(&options{})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"add", "common", "--checkout", root, "--machine", "vm"})
	cmd.SetOut(&previewErrorWriter{err: writeErr})
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !errors.Is(err, writeErr) {
		t.Fatalf("unchanged output failure not reported: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) || len(src.calls) != 0 || len(src.reads) != 0 {
		t.Fatalf("unchanged selection did work: manifest=%q, error=%v, calls=%v, reads=%v", after, err, src.calls, src.reads)
	}
}

func TestSelectionNoOpPreservesOriginalManifest(t *testing.T) {
	root, src := installerFixture(t)
	path := manifestPath(root, "vm")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "profiles", "add", "common", "--checkout", root, "--machine", "vm", "--yes")
	if code != ExitOK || !strings.Contains(out, "already says that") {
		t.Fatalf("unchanged selection failed: %d %s%s", code, out, errOut)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) || len(src.calls) != 0 || len(src.reads) != 0 {
		t.Fatalf("unchanged selection did work: manifest=%q, error=%v, calls=%v, reads=%v", after, err, src.calls, src.reads)
	}
}

func TestSelectionReportsWrittenManifestAndRefreshOutputFailures(t *testing.T) {
	for _, tc := range []struct {
		name, group, id string
		after           int
		initialized     bool
	}{
		{name: "manifest", group: "components", id: "demo", after: 2},
		{name: "profile refresh", group: "profiles", id: "extra", after: 3, initialized: true},
		{name: "unavailable selection", group: "profiles", id: "extra", after: 3},
	} {
		for _, asJSON := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
				root, src := installerFixture(t)
				for path, data := range map[string]string{
					"profiles/common.toml": "schema = 1\nid = \"common\"\npackages = []\n",
					"profiles/extra.toml":  "schema = 1\nid = \"extra\"\n",
					"components/demo.toml": "schema = 1\nid = \"demo\"\n",
				} {
					if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--no-upgrade", "--yes"); code != ExitOK {
					t.Fatalf("prepare already-managed fixture: %d %s%s", code, out, errOut)
				}
				src.calls = nil
				if tc.initialized {
					src.Dirs[filepath.Join(os.Getenv("HOME"), ".local/share/chezmoi")] = []string{".git"}
				}
				writeErr := errors.New("result output unavailable")
				writer := &previewErrorWriter{after: tc.after, err: writeErr}
				opts := &options{json: asJSON}
				cmd := newComponents(opts)
				if tc.group == "profiles" {
					cmd = newProfiles(opts)
				}
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				cmd.SetArgs([]string{"add", tc.id, "--checkout", root, "--machine", "vm", "--yes"})
				cmd.SetOut(writer)
				cmd.SetErr(io.Discard)
				if asJSON {
					cmd.SetOut(io.Discard)
					cmd.SetErr(writer)
				}
				if err := cmd.Execute(); !errors.Is(err, writeErr) {
					t.Fatalf("selection result output failure not reported: %v", err)
				}
				selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				m := selected.Checkout.Machines["vm"]
				if !slices.Contains(m.Components, tc.id) && !slices.Contains(m.Profiles, tc.id) {
					t.Fatal("report failure discarded the approved manifest edit")
				}
				p, err := buildPlan(selected, src)
				if err != nil || !nothingToRun(p) || len(src.calls) != 0 {
					t.Fatalf("expected a manifest-only change: plan=%+v, error=%v, calls=%v", p, err, src.calls)
				}
			})
		}
	}
}

func TestWriteManifestCleansTemporaryFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "machine.toml")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(destination, "preserve")
	if err := os.WriteFile(preserved, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeManifest(destination, []byte("schema = 1"))
	if renameErr, ok := errors.AsType[*os.LinkError](err); !ok || renameErr.Op != "rename" {
		t.Fatalf("expected rename failure, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "machine.toml" {
		t.Fatalf("failed replacement left temporary files: %v", entries)
	}
	data, err := os.ReadFile(preserved)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("destination changed: %q, %v", data, err)
	}
}

func TestRenderManifestIsCanonicalAndKeepsComments(t *testing.T) {
	existing := []byte("# Laptop.\n# Second line.\nschema = 1\nid = \"laptop\"\nprofiles = [\"common\"]\n")
	m := &definitions.Machine{Schema: 1, ID: "laptop", Shell: "zsh", Profiles: []string{"common", "development"}, Components: []string{"amd-graphics"},
		Packages: []string{"gimp"}, Dotfiles: &definitions.Dotfiles{Repo: "https://example.invalid/dotfiles.git"}}
	data, err := renderManifest(existing, m)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	want := "# Laptop.\n# Second line.\nschema = 1\nid = 'laptop'\nshell = 'zsh'\nprofiles = [\n  'common',\n  'development'\n]\ncomponents = [\n  'amd-graphics'\n]\npackages = [\n  'gimp'\n]\npackage_exclusions = []\n\n[dotfiles]\nrepo = 'https://example.invalid/dotfiles.git'"
	if out != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", out, want)
	}
	// The rendered form must decode back to the same manifest.
	var back definitions.Machine
	decoder := toml.NewDecoder(strings.NewReader(out))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&back); err != nil {
		t.Fatalf("decode rendered manifest: %v", err)
	}
	if back.Schema != m.Schema || back.ID != m.ID || back.Hardware != m.Hardware || back.Shell != m.Shell ||
		!slices.Equal(back.Profiles, m.Profiles) || !slices.Equal(back.Components, m.Components) ||
		!slices.Equal(back.Packages, m.Packages) || !slices.Equal(back.PackageExclusions, m.PackageExclusions) ||
		back.Dotfiles == nil || *back.Dotfiles != *m.Dotfiles {
		t.Fatalf("decoded manifest = %+v, want %+v", back, *m)
	}
	diff := unifiedDiff("machines/laptop.toml", existing, []byte(out+"\n"))
	if !strings.Contains(diff, "+  'development'") || !strings.Contains(diff, "-profiles = [\"common\"]") {
		t.Fatalf("diff:\n%s", diff)
	}
}

func TestSelectionPreservesManifestStringValues(t *testing.T) {
	for _, field := range []string{"hardware", "dotfiles"} {
		t.Run(field, func(t *testing.T) {
			root, _ := installerFixture(t)
			for path, data := range map[string]string{
				"profiles/common.toml": "schema=1\nid='common'\npackages=[]\n",
				"components/demo.toml": "schema=1\nid='demo'\n",
			} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			m := &definitions.Machine{Schema: 1, ID: "vm", Profiles: []string{"common"}}
			const value = "fixture\a\v\x7fidentity"
			if field == "hardware" {
				m.Hardware = value
			} else {
				m.Dotfiles = &definitions.Dotfiles{Repo: "git@example.invalid:owner/" + value}
			}
			data, err := toml.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			const comment = "# Retained machine description.\n"
			path := manifestPath(root, "vm")
			if err := os.WriteFile(path, append([]byte(comment), data...), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadCheckout(root); err != nil {
				t.Fatalf("input manifest is invalid: %v", err)
			}
			code, out, errOut := run(t, "components", "add", "demo", "--checkout", root, "--machine", "vm", "--yes")
			if code != ExitOK {
				t.Fatalf("selection failed: %d %s%s", code, out, errOut)
			}
			checkout, err := loadCheckout(root)
			if err != nil {
				t.Fatalf("selection wrote an invalid manifest: %v", err)
			}
			got := checkout.Machines["vm"]
			if got.Hardware != m.Hardware || field == "dotfiles" && (got.Dotfiles == nil || *got.Dotfiles != *m.Dotfiles) || !slices.Equal(got.Components, []string{"demo"}) {
				t.Fatalf("selection changed unrelated values: %+v", got)
			}
			data, err = os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(data), comment) {
				t.Fatalf("leading comment lost: %q, %v", data, err)
			}
		})
	}
}

// editableCheckout copies the repository definitions into a temporary
// directory so a selection command can write a manifest without touching
// the real checkout.
func editableCheckout(t *testing.T) string {
	t.Helper()
	src := repoRoot(t)
	dst := t.TempDir()
	for _, dir := range []string{"machines", "profiles", "components", "system"} {
		if err := filepath.WalkDir(filepath.Join(src, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(dst, rel), 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if dir == "machines" {
				// These package/selection fixtures predate native constraints.
				// Constraint tests exercise the full definitions separately.
				var manifest map[string]any
				if err := toml.Unmarshal(data, &manifest); err != nil {
					return err
				}
				delete(manifest, "package_constraints")
				data, err = toml.Marshal(manifest)
				if err != nil {
					return err
				}
			}
			return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(src, "nimbus.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "nimbus.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, ".git", "config"), []byte("[remote \"origin\"]\n\turl = https://github.com/Furyfree/nimbus.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dst)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestProfilesAddShowsDiffAndPlanThenWrites(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	readyRepositories(t, src, root)
	withSource(t, src)
	// Declined approval leaves the manifest untouched.
	saved := approver
	approver = func(_ io.Reader, _ io.Writer, _ string) bool { return false }
	t.Cleanup(func() { approver = saved })
	answerLaptopInstall(t, src, root)
	before, _ := os.ReadFile(manifestPath(root, "laptop"))
	// docker is already selected through development, so making it explicit
	// changes the manifest but not the plan; the recorded preview still fits.
	code, out, errOut := run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(errOut, "not applied") {
		t.Fatalf("declined: %d %q\n%s", code, errOut, out)
	}
	for _, want := range []string{"components add: add components docker", "+  'docker'", "plan for laptop"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	after, _ := os.ReadFile(manifestPath(root, "laptop"))
	if string(before) != string(after) {
		t.Fatal("manifest changed without approval")
	}
	if code, _, errOut := run(t, "profiles", "add", "nope", "--checkout", root, "--machine", "laptop"); code != ExitFailure || !strings.Contains(errOut, "unknown profile") {
		t.Fatalf("unknown profile: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "profiles", "remove", "common", "--checkout", root, "--machine", "laptop"); code != ExitFailure || !strings.Contains(errOut, "common cannot be removed") {
		t.Fatalf("remove common: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "profiles", "add", "gaming", "--checkout", root, "--machine", "laptop"); code != ExitFailure || !strings.Contains(errOut, "incomplete") {
		t.Fatalf("an edit whose plan is incomplete must stop before approval: %d %q", code, errOut)
	}
	if code, out, _ := run(t, "profiles", "add", "common", "--checkout", root, "--machine", "laptop"); code != ExitOK || !strings.Contains(out, "already says that") {
		t.Fatalf("no-op edit: %d\n%s", code, out)
	}
}

func TestPickerBackspaceRemovesLastRune(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"empty", "", ""},
		{"ASCII", "a", ""},
		{"two bytes", "co\u00e5", "co"},
		{"three bytes", "co\u754c", "co"},
		{"four bytes", "co\U0001f600", "co"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var model tea.Model = pickerModel{items: []pickItem{{ID: "common"}}}
			for _, r := range tc.input {
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
			model, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
			got := model.(pickerModel)
			if got.filter != tc.want || len(got.visible()) != 1 {
				t.Fatalf("typing %q then Backspace: filter = %q, visible = %v; want filter %q and common visible", tc.input, got.filter, got.visible(), tc.want)
			}
		})
	}
}

func TestPickerIsUsedWhenNoIDsAreGiven(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withSource(t, src)
	savedPick := pickerFn
	var offered []string
	pickerFn = func(title string, items []pickItem) ([]string, error) {
		for _, it := range items {
			offered = append(offered, it.ID)
		}
		return nil, nil
	}
	t.Cleanup(func() { pickerFn = savedPick })
	// Nothing chosen: the edit is empty and the manifest already says that.
	code, out, errOut := run(t, "components", "remove", "--checkout", root, "--machine", "laptop")
	if code != ExitOK || !strings.Contains(out, "already says that") {
		t.Fatalf("empty pick: %d %q\n%s", code, errOut, out)
	}
	if strings.Join(offered, ",") != "amd-graphics,laptop-power" {
		t.Fatalf("picker offered %v", offered)
	}
	if code, _, errOut := run(t, "components", "remove", "docker", "--checkout", root, "--machine", "laptop"); code != ExitFailure || !strings.Contains(errOut, "removed by removing the profile") {
		t.Fatalf("profile-selected component: %d %q", code, errOut)
	}
}

func TestPackageCommandsRejectExtraQueries(t *testing.T) {
	for _, action := range []string{"install", "remove"} {
		t.Run(action, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "missing")
			code, out, errOut := run(t, "packages", action, "bash", "zsh", "--checkout", missing, "--machine", "vm")
			if code != ExitUsage || !strings.Contains(errOut, "at most 1") {
				t.Fatalf("extra query was not rejected before loading the checkout: %d %s%s", code, out, errOut)
			}
		})
	}
}

func TestSelectionCommandsAcceptMultipleIDs(t *testing.T) {
	for _, tc := range []struct {
		group string
		ids   []string
	}{
		{"profiles", []string{"common", "development"}},
		{"components", []string{"amd-graphics", "laptop-power"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			root := editableCheckout(t)
			withSource(t, fixtureSource(t, root))
			args := append([]string{tc.group, "add"}, tc.ids...)
			args = append(args, "--checkout", root, "--machine", "laptop")
			if code, out, errOut := run(t, args...); code != ExitOK || !strings.Contains(out, "already says that") {
				t.Fatalf("multiple selected IDs rejected: %d %s%s", code, out, errOut)
			}
		})
	}
}

func TestPackagesInstallNeedsAQueryAndUsesTheCache(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	if code, _, errOut := run(t, "packages", "install", "--checkout", root, "--machine", "laptop"); code != ExitUsage || !strings.Contains(errOut, "needs a query") {
		t.Fatalf("no query: %d %q", code, errOut)
	}
	savedPick := pickerFn
	var offered []pickItem
	pickerFn = func(title string, items []pickItem) ([]string, error) {
		offered = items
		return []string{"terra:ghostty"}, nil
	}
	t.Cleanup(func() { pickerFn = savedPick })
	src.Commands["dnf5 -q --cacheonly repoquery --available --qf %{name}|%{evr}|%{reponame}\n *ghost*"] = []byte("ghostty|1.3.1-3.fc44|nimbus-terra\nghostscript|10.05.1-1.fc44|fedora\n")
	saved := approver
	approver = func(_ io.Reader, _ io.Writer, _ string) bool { return false }
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "packages", "install", "ghost", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(errOut, "not applied") {
		t.Fatalf("install flow: %d %q\n%s", code, errOut, out)
	}
	if len(offered) != 2 || offered[0].ID != "ghostscript" || offered[1].ID != "terra:ghostty" {
		t.Fatalf("offered = %+v", offered)
	}
	if !strings.Contains(out, "+  'terra:ghostty'") {
		t.Fatalf("diff lacks the new reference:\n%s", out)
	}
}

func TestSelectionEditDigestSurvivesTheManifestWrite(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	readyRepositories(t, src, root, "blesh") // Keep Brave as the deliberate first failure.
	answerLaptopInstall(t, src, root)
	withSource(t, src)
	saved := approver
	var seen string
	approver = func(_ io.Reader, _ io.Writer, digest string) bool { seen = digest; return false }
	t.Cleanup(func() { approver = saved })
	run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop")
	if seen == "" {
		t.Fatal("no digest offered for approval")
	}
	// Approving that exact digest must carry through the manifest write and
	// the reload; the run then stops at the first operation, which needs the
	// network, and that proves the digests matched.
	code, out, errOut := run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop", "-y")
	if code != ExitFailure || !strings.Contains(out, "wrote ") || !strings.Contains(out, "failed     repository:brave") {
		t.Fatalf("edit flow: %d %q\n%s", code, errOut, out)
	}
	data, _ := os.ReadFile(manifestPath(root, "laptop"))
	if !strings.Contains(string(data), "'docker'") {
		t.Fatal("manifest not written")
	}
	lock, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"))
	if !strings.Contains(string(lock), `"command":"components add"`) {
		t.Fatalf("lock was not taken by the edit: %q", lock)
	}
}

func TestPackageSelectionRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, exclusion string
		profile                   bool
	}{
		{name: "profile", profile: true},
		{name: "explicit and profile", explicit: "demo", profile: true},
		{name: "qualified explicit and profile", explicit: "dnf:demo", profile: true},
		{name: "explicit only", explicit: "demo"},
		{name: "bare exclusion", exclusion: "demo", profile: true},
		{name: "qualified exclusion", exclusion: "dnf:demo", profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, src := installerFixture(t)
			profile := "schema=1\nid='common'\npackages=['untouched'"
			if tc.profile {
				profile += ",'demo'"
			}
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte(profile+"]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			m := &definitions.Machine{Schema: 1, ID: "vm", Profiles: []string{"common"}, PackageExclusions: []string{"untouched"}}
			if tc.explicit != "" {
				m.Packages = []string{tc.explicit}
			}
			if tc.exclusion != "" {
				m.PackageExclusions = append(m.PackageExclusions, tc.exclusion)
			}
			data, err := renderManifest(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			path := manifestPath(root, "vm")
			if err := writeManifest(path, data); err != nil {
				t.Fatal(err)
			}
			inventory := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			installed := []byte("demo|0|1|1|x86_64|fedora|User\n")
			src.Commands[inventory] = nil
			if tc.exclusion == "" {
				src.Commands[inventory] = installed
				st := &state.Stage{Schema: state.Schema, PlanDigest: "earlier", Receipts: []state.Receipt{{
					Schema: state.ReceiptSchema, Resource: "package:dnf:demo", Provider: "dnf", Package: "demo.x86_64",
					Machine: "vm", Verified: true, Operation: "install", PlanDigest: "earlier",
				}}}
				if err := state.Record(stateRoot, st.PlanDigest, st); err != nil {
					t.Fatal(err)
				}
			}
			src.Commands["dnf5 -q --cacheonly repoquery --available --qf %{name}|%{evr}|%{reponame}\n *demo*"] = []byte("demo|1-1|fedora\n")
			src.Commands["dnf5 --assumeno --cacheonly install demo"] = []byte("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n demo x86_64 1-1 fedora 1 KiB\n\nTransaction Summary:\n")
			src.Commands["dnf5 --assumeno --cacheonly remove --no-autoremove demo.x86_64"] = []byte("Repositories loaded.\nPackage Arch Version Repository Size\nRemoving:\n demo x86_64 1-1 @System 1 KiB\n\nTransaction Summary:\n")
			const install = "sudo dnf5 -y install demo"
			const remove = "sudo dnf5 -y remove --no-autoremove demo.x86_64"
			src.Commands[install], src.Commands[remove] = nil, nil
			withSource(t, handoffOutputSource{Source: src, afterStream: func(name string, args []string) {
				switch nativetest.Key(name, args...) {
				case install:
					src.Commands[inventory] = installed
				case remove:
					src.Commands[inventory] = nil
				default:
					t.Fatalf("unexpected mutation: %s %v", name, args)
				}
			}})
			savedPick, savedApprover := pickerFn, approver
			t.Cleanup(func() { pickerFn, approver = savedPick, savedApprover })
			pickerFn = func(_ string, items []pickItem) ([]string, error) {
				for _, item := range items {
					if item.ID == "demo" || item.ID == "dnf:demo" {
						return []string{item.ID}, nil
					}
				}
				t.Fatalf("demo missing from picker: %+v", items)
				return nil, nil
			}
			approver = func(io.Reader, io.Writer, string) bool { return false }
			if tc.explicit != "" {
				code, out, errOut := run(t, "packages", "install", "demo", "--checkout", root, "--machine", "vm", "--yes")
				after, err := os.ReadFile(path)
				if code != ExitOK || !strings.Contains(out, "already says that") || err != nil || string(after) != string(append(data, '\n')) || len(src.calls) != 0 {
					t.Fatalf("equivalent explicit reference was not a no-op: %d %s%s; calls=%v, manifest=%q, error=%v", code, out, errOut, src.calls, after, err)
				}
			}
			actions, wantCalls := []string{"install", "remove"}, []string{install, remove}
			if tc.exclusion == "" {
				actions, wantCalls = append([]string{"remove"}, actions...), append([]string{remove}, wantCalls...)
			}
			for _, action := range actions {
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				calls := len(src.calls)
				code, out, errOut := run(t, "packages", action, "demo", "--checkout", root, "--machine", "vm")
				after, err := os.ReadFile(path)
				if code != ExitFailure || !strings.Contains(errOut, "not applied") || err != nil || string(after) != string(before) || len(src.calls) != calls {
					t.Fatalf("declined %s changed state: %d %s%s; calls=%v, manifest=%q, error=%v", action, code, out, errOut, src.calls, after, err)
				}
				code, out, errOut = run(t, "packages", action, "demo", "--checkout", root, "--machine", "vm", "--yes")
				if code != ExitOK || !strings.Contains(out, "plan for vm") || !strings.Contains(out, "wrote ") {
					t.Fatalf("%s failed: %d %s%s", action, code, out, errOut)
				}
				s, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				wantSelected := action == "install"
				if selected := slices.ContainsFunc(s.Resolved.Packages, func(p definitions.ResolvedPackage) bool { return p.Canonical == "dnf:demo" }); selected != wantSelected {
					t.Fatalf("%s left the wrong desired state: %+v", action, s.Resolved.Packages)
				}
				m := s.Checkout.Machines["vm"]
				wantExclusions := []string{"untouched"}
				if !wantSelected && tc.profile {
					wantExclusions = append(wantExclusions, "dnf:demo")
				}
				if !slices.Equal(m.PackageExclusions, wantExclusions) || !wantSelected && len(m.Packages) != 0 {
					t.Fatalf("%s changed unrelated exclusions or retained an explicit reference: %+v", action, m)
				}
				applied, err := state.Read(stateRoot)
				if err != nil {
					t.Fatal(err)
				}
				if _, owned := applied.Receipts["package:dnf:demo"]; owned != wantSelected {
					t.Fatalf("%s left the wrong receipt state: %+v", action, applied.Receipts)
				}
			}
			if !slices.Equal(src.calls, wantCalls) {
				t.Fatalf("native calls = %v, want %v", src.calls, wantCalls)
			}
		})
	}
}

func TestPackagesRemoveRefusesRequiredComponentPackage(t *testing.T) {
	root, src := installerFixture(t)
	manifest := "schema=1\nid='vm'\nprofiles=['common']\ncomponents=['consumer']\npackages=['dnf:demo']\n[package_constraints]\n'dnf:demo'='1.*'\n"
	for path, data := range map[string]string{
		"profiles/common.toml":     "schema=1\nid='common'\n",
		"components/runtime.toml":  "schema=1\nid='runtime'\npackages=['demo']\n",
		"components/consumer.toml": "schema=1\nid='consumer'\nrequires=['runtime']\n",
		"machines/vm.toml":         manifest,
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	savedPick, savedApprover := pickerFn, approver
	t.Cleanup(func() { pickerFn, approver = savedPick, savedApprover })
	pickerFn = func(_ string, items []pickItem) ([]string, error) {
		if len(items) != 1 || items[0].ID != "dnf:demo" {
			t.Fatalf("unexpected package choices: %+v", items)
		}
		return []string{items[0].ID}, nil
	}
	approver = func(io.Reader, io.Writer, string) bool {
		t.Fatal("required package removal reached approval")
		return true
	}
	code, out, errOut := run(t, "packages", "remove", "demo", "--checkout", root, "--machine", "vm")
	after, err := os.ReadFile(manifestPath(root, "vm"))
	if code != ExitFailure || !strings.Contains(errOut, "another selected component requires") || strings.Contains(out, "plan for") || err != nil || string(after) != manifest || len(src.calls) != 0 || len(src.reads) != 0 {
		t.Fatalf("required package removal was not refused before review: %d %s%s; manifest=%q, error=%v, calls=%v, reads=%v", code, out, errOut, after, err, src.calls, src.reads)
	}
}

func TestSelectionRemovesOnlyUnselectedPackageConstraints(t *testing.T) {
	for _, mode := range []string{"profile", "component", "explicit package", "excluded package", "shared component", "last constraint"} {
		t.Run(mode, func(t *testing.T) {
			root, src := installerFixture(t)
			for path, data := range map[string]string{
				"profiles/common.toml": "schema=1\nid='common'\npackages=['bash']\n",
				"profiles/extra.toml":  "schema=1\nid='extra'\ncomponents=['demo']\n",
				"components/demo.toml": "schema=1\nid='demo'\npackages=['demo']\n",
			} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			m := &definitions.Machine{Schema: 1, ID: "vm", Profiles: []string{"common"},
				PackageConstraints: map[string]string{"dnf:demo": "1.*", "dnf:bash": "5.*"}}
			args := []string{"packages", "remove", "demo"}
			switch mode {
			case "profile", "shared component", "last constraint":
				m.Profiles = append(m.Profiles, "extra")
				args = []string{"profiles", "remove", "extra"}
				if mode == "shared component" {
					m.Components = []string{"demo"}
				}
				if mode == "last constraint" {
					delete(m.PackageConstraints, "dnf:bash")
				}
			case "component":
				m.Components = []string{"demo"}
				args = []string{"components", "remove", "demo"}
			case "explicit package":
				m.Packages = []string{"demo"}
			case "excluded package":
				m.Profiles = append(m.Profiles, "extra")
			}
			before, err := renderManifest(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			path := manifestPath(root, "vm")
			if err := writeManifest(path, before); err != nil {
				t.Fatal(err)
			}
			savedPick, savedApprover := pickerFn, approver
			t.Cleanup(func() { pickerFn, approver = savedPick, savedApprover })
			pickerFn = func(_ string, items []pickItem) ([]string, error) { return []string{items[0].ID}, nil }
			approver = func(io.Reader, io.Writer, string) bool { return false }
			args = append(args, "--checkout", root, "--machine", "vm")
			code, out, errOut := run(t, append(slices.Clone(args), "--plan", "--json")...)
			var result struct {
				Data struct {
					Diff string     `json:"diff"`
					Plan *plan.Plan `json:"plan"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || code != ExitOK {
				t.Fatalf("selection preview: %d %s%s: %v", code, out, errOut, err)
			}
			retained := mode == "shared component"
			if removed := strings.Contains(result.Data.Diff, "-'dnf:demo' = '1.*'"); removed == retained ||
				mode != "last constraint" && !strings.Contains(result.Data.Diff, " 'dnf:bash' = '5.*'") {
				t.Fatalf("incorrect constraint edit:\n%s", result.Data.Diff)
			}
			var versionlock string
			for _, op := range result.Data.Plan.Operations {
				if plan.IsConstraintOperation(op) {
					versionlock = string(op.File.After.Content)
				}
			}
			if strings.Contains(versionlock, `name = "bash"`) != (mode != "last constraint") || strings.Contains(versionlock, `name = "demo"`) != retained {
				t.Fatalf("planned constraints do not match the selection: %s", versionlock)
			}
			code, out, errOut = run(t, args...)
			if code != ExitFailure || !strings.Contains(errOut, "not applied") {
				t.Fatalf("declined selection: %d %s%s", code, out, errOut)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before)+"\n" || len(src.calls) != 0 {
				t.Fatalf("preview or decline changed state: manifest=%q error=%v calls=%v", after, err, src.calls)
			}
			if mode == "last constraint" {
				code, out, errOut = run(t, append(args, "--yes")...)
				if code != ExitOK || !strings.Contains(out, "wrote ") {
					t.Fatalf("approved selection: %d %s%s", code, out, errOut)
				}
				selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
				if err != nil || len(selected.Checkout.Machines["vm"].PackageConstraints) != 0 || !slices.Equal(selected.Resolved.Profiles, []string{"common"}) {
					t.Fatalf("approved removal did not persist a valid selection: %+v, %v", selected, err)
				}
			}
		})
	}
}

func TestSelectionPreviewAndJSONNeverGrantApproval(t *testing.T) {
	for _, mode := range []string{"plan", "json-plan", "json"} {
		t.Run(mode, func(t *testing.T) {
			root, src := installerFixture(t)
			if err := os.WriteFile(filepath.Join(root, "components/demo.toml"), []byte("schema=1\nid='demo'\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := manifestPath(root, "vm")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"components", "add", "demo", "--checkout", root, "--machine", "vm"}
			if mode != "json" {
				args = append(args, "--plan")
			}
			if mode != "plan" {
				args = append(args, "--json")
			}
			code, out, errOut := run(t, args...)
			want := ExitOK
			if mode == "json" {
				want = ExitUsage
			}
			if code != want {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) || len(src.calls) > 0 || slices.Contains(src.reads, "dnf5 makecache") {
				t.Fatalf("preview/JSON mutated: %v %v %v", err, src.calls, src.reads)
			}
		})
	}
}

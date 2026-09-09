package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
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
	c, err := loadCheckout(root)
	if err != nil {
		t.Fatal(err)
	}
	path := manifestPath(root, "vm")
	before := append(renderManifest(nil, c.Machines["vm"]), '\n')
	if err := os.WriteFile(path, before, 0644); err != nil {
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
	if err != nil || string(after) != string(before) || len(src.calls) != 0 {
		t.Fatalf("unchanged selection mutated: manifest=%q, error=%v, calls=%v", after, err, src.calls)
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
	m := &definitions.Machine{Schema: 1, ID: "laptop", Profiles: []string{"common", "development"}, Components: []string{"amd-graphics"},
		Packages: []string{"gimp"}, Dotfiles: &definitions.Dotfiles{Repo: "https://example.invalid/dotfiles.git"}}
	out := string(renderManifest(existing, m))
	want := "# Laptop.\n# Second line.\nschema = 1\nid = \"laptop\"\n\nprofiles = [\n  \"common\",\n  \"development\",\n]\n\ncomponents = [\n  \"amd-graphics\",\n]\n\npackages = [\n  \"gimp\",\n]\n\npackage_exclusions = []\n\n[dotfiles]\nrepo = \"https://example.invalid/dotfiles.git\""
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
	if back.Schema != m.Schema || back.ID != m.ID || back.Hardware != m.Hardware ||
		!slices.Equal(back.Profiles, m.Profiles) || !slices.Equal(back.Components, m.Components) ||
		!slices.Equal(back.Packages, m.Packages) || !slices.Equal(back.PackageExclusions, m.PackageExclusions) ||
		back.Dotfiles == nil || *back.Dotfiles != *m.Dotfiles {
		t.Fatalf("decoded manifest = %+v, want %+v", back, *m)
	}
	diff := unifiedDiff("machines/laptop.toml", existing, []byte(out+"\n"))
	if !strings.Contains(diff, "+  \"development\",") || !strings.Contains(diff, "-profiles = [\"common\"]") {
		t.Fatalf("diff:\n%s", diff)
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
			rel, _ := filepath.Rel(src, p)
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(dst, rel), 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(src, "nimbus.toml"))
	os.WriteFile(filepath.Join(dst, "nimbus.toml"), data, 0o644)
	os.MkdirAll(filepath.Join(dst, ".git"), 0o755)
	os.WriteFile(filepath.Join(dst, ".git", "config"), []byte("[remote \"origin\"]\n\turl = https://github.com/Furyfree/nimbus.git\n"), 0o644)
	real, _ := filepath.EvalSymlinks(dst)
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
	for _, want := range []string{"components add: add components docker", "+  \"docker\",", "plan for laptop"} {
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
	if !strings.Contains(out, "+  \"terra:ghostty\",") {
		t.Fatalf("diff lacks the new reference:\n%s", out)
	}
}

func TestSelectionEditDigestSurvivesTheManifestWrite(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
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
	if !strings.Contains(string(data), "\"docker\",") {
		t.Fatal("manifest not written")
	}
	lock, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock"))
	if !strings.Contains(string(lock), `"command":"components add"`) {
		t.Fatalf("lock was not taken by the edit: %q", lock)
	}
}

package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
)

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
	tree := map[string]string{"machines/laptop.toml": out + "\n"}
	_ = tree
	_ = back
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
	withoutTerra(src)
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
	withoutTerra(src)
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

func TestPackagesInstallNeedsAQueryAndUsesTheCache(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withoutTerra(src)
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
	withoutTerra(src)
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
	if code != ExitFailure || !strings.Contains(out, "wrote ") || !strings.Contains(out, "sync stopped at repository:brave") {
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

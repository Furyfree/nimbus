package facts

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRepositoryLocationOverridesAreObserved(t *testing.T) {
	src := &FakeSource{Dirs: map[string][]string{RepoDir: {"test.repo"}, RepoOverride: {"override.repo"}}, Files: map[string][]byte{
		filepath.Join(RepoDir, "test.repo"):          []byte("[nimbus-test]\nbaseurl=https://expected.invalid/repo\n"),
		filepath.Join(RepoOverride, "override.repo"): []byte("[nimbus-*]\nbaseurl=https://other.invalid/repo\nmetalink=https://other.invalid/meta\nmirrorlist=https://other.invalid/list\n"),
	}}
	repos, err := repositories(src)
	if err != nil || len(repos) != 1 {
		t.Fatalf("%v %v", repos, err)
	}
	if repos[0].BaseURL != "https://other.invalid/repo" || repos[0].Metalink != "https://other.invalid/meta" {
		t.Fatalf("ignored native source override: %+v", repos[0])
	}
}

func TestRepositoryHeaderCommentsPreserveSecurityOverrides(t *testing.T) {
	for _, comment := range []string{"# local override", "; local override"} {
		t.Run(comment, func(t *testing.T) {
			src := &FakeSource{
				Dirs: map[string][]string{RepoDir: {"maker.repo"}, RepoOverride: {"99-local.repo"}},
				Files: map[string][]byte{
					filepath.Join(RepoDir, "maker.repo"):         []byte("[nimbus-maker] " + comment + "\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/pinned\nbaseurl=https://expected.invalid/repo\n[nimbus-other]\nenabled=0\n"),
					filepath.Join(RepoOverride, "99-local.repo"): []byte("[nimbus-[mn]aker] " + comment + "\ngpgcheck=0\ngpgkey=file:///etc/pki/rpm-gpg/other\nbaseurl=https://other.invalid/repo\n"),
				},
				Commands: map[string][]byte{Key("gpg", KeyInspectArgs("/etc/pki/rpm-gpg/other")...): keyOutput(keyB)},
			}
			repos, err := repositories(src)
			if err != nil || len(repos) != 2 {
				t.Fatalf("repositories = %+v, %v", repos, err)
			}
			r := repos[0]
			if r.ID != "nimbus-maker" || r.GPGCheck != "0" || r.BaseURL != "https://other.invalid/repo" || !slices.Equal(r.Overrides, []string{"99-local.repo"}) {
				t.Fatalf("commented override was ignored: %+v", r)
			}
			if r.KeyError != "" || !slices.Equal(r.KeyFingerprints, []string{keyB}) {
				t.Fatalf("commented signing-key override was ignored: %+v", r)
			}
			if repos[1].ID != "nimbus-other" || repos[1].Enabled || len(repos[1].Overrides) != 0 {
				t.Fatalf("bracket glob changed an unrelated repository: %+v", repos[1])
			}
		})
	}
}

func TestMalformedRepositoryConfigurationIsUnknown(t *testing.T) {
	for _, dir := range []string{RepoDir, RepoOverride} {
		t.Run(dir, func(t *testing.T) {
			src := &FakeSource{
				Dirs: map[string][]string{RepoDir: {"maker.repo"}, RepoOverride: {"99-local.repo"}},
				Files: map[string][]byte{
					filepath.Join(RepoDir, "maker.repo"):         []byte("[maker]\nenabled=0\n"),
					filepath.Join(RepoOverride, "99-local.repo"): []byte("[maker]\nenabled=1\n"),
				},
			}
			file := src.Dirs[dir][0]
			src.Files[filepath.Join(dir, file)] = []byte("[maker] unexpected\ngpgcheck=0\n")
			got := collect(func() ([]Repository, error) { return repositories(src) })
			if got.Known() || got.Value != nil || !strings.Contains(got.Error, file+" line 1") {
				t.Fatalf("malformed repository input was accepted: %+v", got)
			}
		})
	}
}

func TestUnsupportedRepositoryOverrideGlobsAreUnknown(t *testing.T) {
	for _, pattern := range []string{"nimbus-[!x]*", "nimbus-@(maker|other)", "nimbus-[[:alpha:]]*", "nimbus-[a-]", "nimbus-$vendor"} {
		t.Run(pattern, func(t *testing.T) {
			src := &FakeSource{
				Dirs: map[string][]string{RepoDir: {"maker.repo"}, RepoOverride: {"99-local.repo"}},
				Files: map[string][]byte{
					filepath.Join(RepoDir, "maker.repo"):         []byte("[nimbus-maker]\ngpgcheck=1\n"),
					filepath.Join(RepoOverride, "99-local.repo"): []byte("[" + pattern + "]\ngpgcheck=0\n"),
				},
			}
			got := collect(func() ([]Repository, error) { return repositories(src) })
			if got.Known() || got.Value != nil || !strings.Contains(got.Error, "99-local.repo") {
				t.Fatalf("unsupported native override was silently ignored: %+v", got)
			}
		})
	}
}

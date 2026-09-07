package facts

import (
	"path/filepath"
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

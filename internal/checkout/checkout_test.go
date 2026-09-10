package checkout

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
)

func TestFastForward(t *testing.T) {
	f := newRepository(t)
	r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	want := f.remoteCommit("new definitions")
	before := git(t, f.local, "rev-parse", "HEAD")
	if err := r.Prepare(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, f.local, "rev-parse", "HEAD"); got != before {
		t.Fatal("preparation changed the working tree")
	}
	if err := r.Update(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, f.local, "rev-parse", "HEAD"); got != want {
		t.Fatalf("updated HEAD = %s, want %s", got, want)
	}
	if got := git(t, f.local, "status", "--porcelain"); got != "" {
		t.Fatalf("update left local changes: %s", got)
	}
	// An already-current repository is a valid update too.
	r, err = Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Prepare(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceSubdirectoryChecksWholeRepository(t *testing.T) {
	f := newRepository(t)
	source := filepath.Join(f.local, "home")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Inspect(native.ExecSource{}, "dotfiles", source, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != f.local {
		t.Fatalf("repository path = %q, want %q", r.Path, f.local)
	}
	f.write(f.local, "outside-home.txt", "local work")
	if _, err := Inspect(native.ExecSource{}, "dotfiles", source, f.origin); err == nil || !strings.Contains(err.Error(), "outside-home.txt") {
		t.Fatalf("source subdirectory hid changes outside it: %v", err)
	}
}

func TestPreparedTargetIsPinned(t *testing.T) {
	f := newRepository(t)
	r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	want := f.remoteCommit("approved revision")
	if err := r.Prepare(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	f.remoteCommit("later revision")
	if err := r.Update(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, f.local, "rev-parse", "HEAD"); got != want {
		t.Fatal("update applied a revision fetched after preparation")
	}
}

func TestDeletedRemoteBranch(t *testing.T) {
	f := newRepository(t)
	r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	git(t, f.remote, "update-ref", "-d", "refs/heads/main")
	git(t, f.remote, "update-server-info")
	before := git(t, f.local, "rev-parse", "HEAD")
	if err := r.Prepare(native.ExecSource{}); err == nil {
		t.Fatal("preparation accepted a deleted remote branch")
	}
	if err := r.Update(native.ExecSource{}); err == nil {
		t.Fatal("update accepted failed preparation")
	}
	if got := git(t, f.local, "rev-parse", "HEAD"); got != before {
		t.Fatal("failed fetch changed HEAD")
	}
}

func TestUpdatePreservesIgnoredFiles(t *testing.T) {
	f := newRepository(t)
	f.write(f.local, ".git/info/exclude", "local.txt\n")
	f.write(f.local, "local.txt", "preserve local content")
	r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	f.write(f.writer, "local.txt", "new tracked file")
	f.remoteCommit("adds previously ignored file")
	if err := r.Prepare(native.ExecSource{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(native.ExecSource{}); err == nil {
		t.Fatal("update overwrote an ignored file")
	}
	data, err := os.ReadFile(filepath.Join(f.local, "local.txt"))
	if err != nil || string(data) != "preserve local content" {
		t.Fatalf("ignored file changed: %q, %v", data, err)
	}
}

func TestUnsafeCheckout(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func(*repositoryFixture)
	}{
		{"tracked edits", "definitions.txt", func(f *repositoryFixture) { f.write(f.local, "definitions.txt", "local edit") }},
		{"untracked file", "new.txt", func(f *repositoryFixture) { f.write(f.local, "new.txt", "local file") }},
		{"staged edit", "local changes", func(f *repositoryFixture) {
			f.write(f.local, "definitions.txt", "staged edit")
			git(t, f.local, "add", ".")
		}},
		{"detached", "no checked-out branch", func(f *repositoryFixture) { git(t, f.local, "checkout", "--detach") }},
		{"no upstream", "must track", func(f *repositoryFixture) { git(t, f.local, "branch", "--unset-upstream") }},
		{"other remote", "must track", func(f *repositoryFixture) {
			git(t, f.local, "remote", "rename", "origin", "other")
			git(t, f.local, "remote", "add", "origin", f.origin)
		}},
		{"wrong origin", "approved repository", func(f *repositoryFixture) {
			git(t, f.local, "remote", "set-url", "origin", "https://secret-token@example.com/other/repo")
		}},
		{"missing upstream", "upstream branch is missing", func(f *repositoryFixture) {
			git(t, f.local, "update-ref", "-d", "refs/remotes/origin/main")
		}},
		{"local commit", "local commits", func(f *repositoryFixture) {
			f.write(f.local, "local.txt", "local commit")
			git(t, f.local, "add", ".")
			git(t, f.local, "commit", "-m", "local commit")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepository(t)
			tc.change(f)
			_, err := Inspect(native.ExecSource{}, "dotfiles", f.local, f.origin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), f.local) || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("error must identify the directory without exposing credentials: %v", err)
			}
		})
	}
}

func TestFetchRejectsRewrittenHistory(t *testing.T) {
	f := newRepository(t)
	previous := f.remoteCommit("second commit")
	git(t, f.local, "pull", "--ff-only")
	r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
	if err != nil {
		t.Fatal(err)
	}
	git(t, f.writer, "reset", "--hard", "HEAD~1")
	f.remoteCommit("different history")
	if err := r.Prepare(native.ExecSource{}); err == nil || !strings.Contains(err.Error(), "cannot fast-forward") {
		t.Fatalf("error = %v, want fast-forward rejection", err)
	}
	if got := git(t, f.local, "rev-parse", "HEAD"); got != previous {
		t.Fatal("failed preparation changed the checkout")
	}
}

func TestChangesAfterPreparation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*repositoryFixture)
	}{
		{"dirty", func(f *repositoryFixture) { f.write(f.local, "new.txt", "preserve me") }},
		{"branch", func(f *repositoryFixture) { git(t, f.local, "checkout", "-b", "different", "--track", "origin/main") }},
		{"head", func(f *repositoryFixture) {
			git(t, f.local, "merge", "--ff-only", "origin/main")
		}},
		{"origin", func(f *repositoryFixture) {
			git(t, f.local, "remote", "set-url", "origin", "https://example.com/other/repo")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepository(t)
			r, err := Inspect(native.ExecSource{}, "Nimbus", f.local, f.origin)
			if err != nil {
				t.Fatal(err)
			}
			f.remoteCommit("new definitions")
			if err := r.Prepare(native.ExecSource{}); err != nil {
				t.Fatal(err)
			}
			tc.change(f)
			before := git(t, f.local, "rev-parse", "HEAD")
			if err := r.Update(native.ExecSource{}); err == nil {
				t.Fatal("update accepted a changed checkout")
			}
			if got := git(t, f.local, "rev-parse", "HEAD"); got != before {
				t.Fatal("rejected update changed HEAD")
			}
		})
	}
}

type repositoryFixture struct {
	t                             *testing.T
	local, writer, remote, origin string
}

func newRepository(t *testing.T) *repositoryFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	f := &repositoryFixture{t: t, local: filepath.Join(dir, "local"), writer: filepath.Join(dir, "writer"), remote: filepath.Join(dir, "remote.git")}
	git(t, dir, "init", "--bare", "--initial-branch=main", f.remote)
	git(t, dir, "init", "--initial-branch=main", f.writer)
	git(t, f.writer, "remote", "add", "origin", f.remote)
	f.remoteCommit("initial definitions")
	git(t, dir, "clone", f.remote, f.local)
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(server.Close)
	f.origin = server.URL + "/remote.git"
	git(t, f.local, "remote", "set-url", "origin", f.origin)
	return f
}

func (f *repositoryFixture) write(dir, name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *repositoryFixture) remoteCommit(content string) string {
	f.t.Helper()
	f.write(f.writer, "definitions.txt", content)
	git(f.t, f.writer, "add", ".")
	git(f.t, f.writer, "commit", "-m", content)
	git(f.t, f.writer, "push", "--force", "origin", "main")
	git(f.t, f.remote, "update-server-info")
	return git(f.t, f.writer, "rev-parse", "HEAD")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Nimbus test", "-c", "user.email=nimbus@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

package selector

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	want := "github.com/furyfree-org/nimbus"
	inputs := []string{
		"git@github.com:furyfree-org/nimbus.git",
		"git@GitHub.com:furyfree-org/nimbus",
		"https://github.com/furyfree-org/nimbus",
		"https://github.com/furyfree-org/nimbus.git",
		"https://github.com/furyfree-org/nimbus/",
		"https://user@github.com/furyfree-org/nimbus.git",
		"ssh://git@github.com/furyfree-org/nimbus.git",
		"github.com/furyfree-org/nimbus",
	}
	for _, in := range inputs {
		got, err := NormalizeOrigin(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "ftp://x/y", "nimbus", "file:///tmp/x"} {
		if _, err := NormalizeOrigin(in); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func writeSelector(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSelector(t *testing.T) {
	good := "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"github.com/furyfree-org/nimbus\"\n"
	if _, err := Load(writeSelector(t, good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"missing origin":     "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\n",
		"unnormalized":       "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"https://github.com/a/b.git\"\n",
		"unknown field":      good + "profiles = [\"common\"]\n",
		"unsupported schema": strings.Replace(good, "schema = 1", "schema = 2", 1),
	}
	for name, body := range bad {
		if _, err := Load(writeSelector(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestWriteRemovesTemporaryFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(path, "preserved")
	if err := os.WriteFile(preserved, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Write(path, &Selector{Schema: CurrentSchema, Checkout: "checkout", Machine: "desktop", Origin: "github.com/Furyfree/nimbus"})
	if rename, ok := errors.AsType[*os.LinkError](err); !ok || rename.Op != "rename" {
		t.Fatalf("expected rename failure, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("temporary file remains after failed rename: %v", entries)
	}
	content, err := os.ReadFile(preserved)
	if err != nil || string(content) != "keep" {
		t.Fatalf("destination changed: %q, %v", content, err)
	}
}

func gitCheckout(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const originConfig = "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = git@github.com:furyfree-org/nimbus.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

func TestVerify(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, gitCheckout(t, originConfig)); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(originConfig, "furyfree-org/nimbus", "someone/else", 1)
	if err := Verify(sel, gitCheckout(t, other)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch not reported: %v", err)
	}
	if err := Verify(sel, gitCheckout(t, "[core]\n")); err == nil {
		t.Fatal("missing origin not reported")
	}
	if err := Verify(sel, gitCheckout(t, "[include]\n\tpath = other\n"+originConfig)); err == nil || !strings.Contains(err.Error(), "include") {
		t.Fatalf("include not rejected: %v", err)
	}
	if err := Verify(sel, t.TempDir()); err == nil {
		t.Fatal("non-repository accepted")
	}
}

func TestVerifyWorktreePointer(t *testing.T) {
	main := gitCheckout(t, originConfig)
	wtDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := &Selector{Schema: 1, Checkout: wt, Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, wt); err != nil {
		t.Fatal(err)
	}
}

func TestSelectorMustBeARegularFile(t *testing.T) {
	good := "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"github.com/furyfree-org/nimbus\"\n"
	real := writeSelector(t, good)
	link := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked selector accepted: %v", err)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory accepted as selector")
	}
}

func TestOriginKeyIsCaseInsensitive(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	upper := strings.Replace(originConfig, "\turl =", "\tURL =", 1)
	if err := Verify(sel, gitCheckout(t, upper)); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteSubsectionIsCaseSensitive(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	cased := strings.Replace(originConfig, `[remote "origin"]`, `[remote "Origin"]`, 1)
	if err := Verify(sel, gitCheckout(t, cased)); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("differently cased remote accepted: %v", err)
	}
	upperSection := strings.Replace(originConfig, `[remote "origin"]`, `[Remote "origin"]`, 1)
	if err := Verify(sel, gitCheckout(t, upperSection)); err != nil {
		t.Fatalf("section name must be case-insensitive: %v", err)
	}
}

func TestUnreadableCommondirFails(t *testing.T) {
	main := gitCheckout(t, originConfig)
	wtDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(filepath.Join(wtDir, "commondir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "config"), []byte(originConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := &Selector{Schema: 1, Checkout: wt, Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, wt); err == nil || !strings.Contains(err.Error(), "commondir") {
		t.Fatalf("directory commondir fell back to the worktree config: %v", err)
	}
}

func TestOriginSectionSyntax(t *testing.T) {
	for _, header := range []string{
		`[remote "origin"] # origin comment`,
		"[Remote\t\"origin\"] ; origin comment",
		`[remote  "origin"]`,
		`[remote "or\igin"]`,
	} {
		t.Run(header, func(t *testing.T) {
			config := header + "\nurl=https://github.com/Furyfree/nimbus.git\n" +
				`[remote "other] # ; \" \\"] # outside comment` + "\nurl=https://github.com/other/repo.git\n"
			got, err := ParseOriginURL([]byte(config))
			if err != nil || got != "https://github.com/Furyfree/nimbus.git" {
				t.Fatalf("origin = %q, %v", got, err)
			}
		})
	}
}

func TestOriginRejectsIncludesAndMalformedHeaders(t *testing.T) {
	for _, header := range []string{
		`[include] # comment`,
		`[INCLUDE] ; comment`,
		"[include\t]#comment",
		"[includeIf\t\"gitdir:/tmp/path]with-bracket/\"] # comment",
		`[include "subsection"]`,
		`[includeIf.foo]`,
		`[includeIf.gitdir:/tmp/]`,
		`[include`,
		`[include] unexpected`,
		`[remote "unterminated]`,
		`[remote "origin" ]`,
		`[remote "origin"] unexpected`,
	} {
		t.Run(header, func(t *testing.T) {
			config := originConfig + header + "\npath=other\n"
			if origin, err := ParseOriginURL([]byte(config)); err == nil {
				t.Fatalf("accepted %q as origin %q", header, origin)
			}
		})
	}
}

func TestVerifyRejectsCommentedIncludeUsedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, comment := range []string{"# comment", "; comment"} {
		t.Run(comment, func(t *testing.T) {
			root := gitCheckout(t, originConfig+"[include] "+comment+"\npath=included\n")
			if err := os.WriteFile(filepath.Join(root, ".git", "included"), []byte("[remote \"origin\"]\nurl=https://github.com/other/repo.git\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.CommandContext(t.Context(), "git", "config", "--includes", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "https://github.com/other/repo.git" {
				t.Fatalf("native Git origin = %q, %v", out, err)
			}
			sel := &Selector{Origin: "github.com/furyfree-org/nimbus"}
			if err := Verify(sel, root); err == nil || !strings.Contains(err.Error(), "include directives") {
				t.Fatalf("included Git origin bypassed selector trust: %v", err)
			}
		})
	}
}

func TestVerifyRejectsDottedOriginUsedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, header := range []string{
		`[remote.origin]`,
		`[remote.Origin]`,
		`[Remote.ORIGIN]`,
		`[remote.origin] # comment`,
	} {
		t.Run(header, func(t *testing.T) {
			root := gitCheckout(t, originConfig+header+"\nurl=https://github.com/other/repo.git\n")
			out, err := exec.CommandContext(t.Context(), "git", "config", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "https://github.com/other/repo.git" {
				t.Fatalf("native Git origin = %q, %v", out, err)
			}
			sel := &Selector{Origin: "github.com/furyfree-org/nimbus"}
			if err := Verify(sel, root); err == nil {
				t.Fatal("dotted Git origin bypassed selector trust")
			}
		})
	}
}

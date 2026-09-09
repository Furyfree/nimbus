package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSource(t *testing.T) {
	script := filepath.Join(repoRoot(t), "tools/release/source.sh")
	for _, scenario := range []string{"success", "invalid tag", "missing tag", "unmerged tag", "existing output", "inside checkout", "vendor failure", "test failure", "module drift"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			repo := filepath.Join(root, "repo")
			bin := filepath.Join(root, "bin")
			for _, dir := range []string{repo, bin} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, text string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), mode); err != nil {
					t.Fatal(err)
				}
			}
			env := append(os.Environ(), "HOME="+root, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "PATH="+bin+":"+os.Getenv("PATH"), "SCENARIO="+scenario)
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir, cmd.Env = repo, env
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init")
			write(filepath.Join(repo, "LICENSE"), "fixture license\n", 0644)
			write(filepath.Join(repo, "go.mod"), "module example.invalid/fixture\n", 0644)
			write(filepath.Join(repo, "go.sum"), "fixture checksums\n", 0644)
			git("add", ".")
			git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
			commit := git("rev-parse", "HEAD")
			git("update-ref", "refs/remotes/origin/main", commit)
			if scenario == "unmerged tag" {
				git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "unmerged")
			}
			git("tag", "v0.1.0")
			// Neither untracked files nor edits after the tag may enter the bundle.
			write(filepath.Join(repo, "private-local"), "must not be exported", 0600)
			write(filepath.Join(repo, "LICENSE"), "unpublished edit", 0644)
			write(filepath.Join(bin, "go"), `#!/usr/bin/env bash
set -eu
case "$*" in
  'mod download'|'mod verify') ;;
  'mod vendor')
    [ "$SCENARIO" != 'vendor failure' ] || exit 1
    mkdir -p vendor/example
    printf notice > vendor/example/LICENSE
    if [ "$SCENARIO" = 'module drift' ]; then echo drift >> go.sum; fi ;;
  'env GOVERSION') echo go1.26.7 ;;
  'list -m -mod=readonly all') echo example.invalid/fixture ;;
  'test -mod=vendor ./...')
    [ "$GOPROXY" = off ] && [ "$GOSUMDB" = off ] && [ "$GOTOOLCHAIN" = local ]
    [ "$SCENARIO" != 'test failure' ] || exit 1
    echo generated > test-artifact ;;
  build*)
    while [ "$1" != -o ]; do shift; done
    printf '#!/bin/sh\nexit 0\n' > "$2"
    chmod 700 "$2" ;;
  *) exit 99 ;;
esac
`, 0700)
			tag := "v0.1.0"
			output := filepath.Join(root, "release")
			switch scenario {
			case "invalid tag":
				tag = "v0.1.0;exit 0"
			case "missing tag":
				tag = "v0.2.0"
			case "existing output":
				write(output, "preserve existing output", 0600)
			case "inside checkout":
				output = filepath.Join(repo, "release")
			}
			cmd := exec.Command("bash", script, tag, output)
			cmd.Dir, cmd.Env = repo, env
			out, err := cmd.CombinedOutput()
			if scenario != "success" {
				if err == nil {
					t.Fatalf("accepted %s: %s", scenario, out)
				}
				if scenario == "existing output" {
					data, _ := os.ReadFile(output)
					if string(data) != "preserve existing output" {
						t.Fatal("overwrote existing output")
					}
				} else if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatalf("failed build left output: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("release: %v %s", err, out)
			}
			data, err := os.ReadFile(filepath.Join(output, "nimbus-0.1.0-vendor.tar.gz"))
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			archive := tar.NewReader(gz)
			files := map[string]string{}
			for {
				header, err := archive.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(header.Name, "nimbus-0.1.0/") || strings.Contains(header.Name, ".git") || strings.Contains(header.Name, "private-local") || strings.Contains(header.Name, "test-artifact") {
					t.Fatalf("unexpected archive entry %q", header.Name)
				}
				content, err := io.ReadAll(archive)
				if err != nil {
					t.Fatal(err)
				}
				files[header.Name] = string(content)
			}
			if files["nimbus-0.1.0/LICENSE"] != "fixture license\n" || files["nimbus-0.1.0/vendor/example/LICENSE"] != "notice" {
				t.Fatal("tagged source or dependency notice missing")
			}
			provenance, _ := os.ReadFile(filepath.Join(output, "RELEASE-SOURCE.txt"))
			if !strings.Contains(string(provenance), "Commit: "+commit) {
				t.Fatal("release does not identify its source commit")
			}
			cmd = exec.Command("sha256sum", "--check", "SHA256SUMS")
			cmd.Dir = output
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("checksums: %v %s", err, out)
			}
		})
	}
}

func TestReleaseTagPushesOnlyRequestedTag(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+root, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--bare", remote)
	git("init", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("commit", "--allow-empty", "-m", "fixture")
	git("remote", "add", "origin", remote)
	git("push", "origin", "main")
	git("tag", "-a", "private-draft", "-m", "must stay local")
	git("config", "push.followTags", "true")
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "tools/release/tag.sh"), "v0.1.0")
	cmd.Dir, cmd.Env = repo, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tag: %v %s", err, out)
	}
	if got := git("--git-dir="+remote, "tag", "--list"); got != "v0.1.0" {
		t.Fatalf("unexpected published tags: %q", got)
	}
	if got := git("cat-file", "-t", "refs/tags/private-draft"); got != "tag" {
		t.Fatal("unrelated local tag was changed")
	}
}

package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestUserToolsRunAsTheUserAndAreVerifiedByPresence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	opts.Fetch = func(url string) ([]byte, error) {
		if url != "https://mise.run" {
			return nil, errors.New("unexpected " + url)
		}
		return []byte("#!/bin/sh\necho installer\n"), nil
	}
	var out strings.Builder
	opts.Out = &out
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:user", Operations: []plan.Operation{
		{ID: "user:mise", Kind: plan.KindUser, Action: plan.ActionInstall, Summary: "install mise",
			Steps: []plan.Step{{Description: "download https://mise.run to the stage directory and show its sha256"}, {Argv: []string{"sh", plan.InstallerScript}}, {Description: "verify ~/.local/bin/mise exists"}}},
	}}
	r := Run(p, opts)
	if r.Error != "" || strings.Join(r.Executed, ",") != "user:mise" {
		t.Fatalf("result = %+v\n%s", r, strings.Join(src.log, "\n"))
	}
	for _, want := range []string{"sha256 ", "sh " + filepath.Join(opts.Stage, "installer-mise.sh")} {
		if !src.ran(want) && !strings.Contains(out.String(), want) {
			t.Errorf("missing %q\n%s\n%s", want, strings.Join(src.log, "\n"), out.String())
		}
	}
	if i := slices.IndexFunc(src.log, func(l string) bool { return strings.HasPrefix(l, "sudo ") }); i >= 0 {
		t.Fatalf("a user-scope step went through sudo: %s", src.log[i])
	}
	if a, _ := state.Read(root); a != nil && len(a.Receipts) != 0 {
		t.Fatalf("user-scope steps must write no receipt: %v", a.Receipts)
	}
	// The installer left nothing: verification fails and the run stops.
	src2 := newScripted()
	src2.installerLeavesNothing = true
	opts2 := options(t, src2, t.TempDir())
	opts2.Fetch = opts.Fetch
	if r := Run(&plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:user2", Operations: p.Operations[:1]}, opts2); r.Error == "" || !strings.Contains(r.Error, "~/.local/bin/mise") || !strings.Contains(r.Error, os.ErrNotExist.Error()) {
		t.Fatalf("missing binary passed verification: %+v", r)
	}
}

type unreadableUserDirectory struct{ native.Source }

func (s unreadableUserDirectory) ReadDir(path string) ([]string, error) {
	return nil, fmt.Errorf("read %s: %w", path, os.ErrPermission)
}

func TestUserToolVerificationPreservesDirectoryReadError(t *testing.T) {
	ex := &executor{opts: Options{Source: unreadableUserDirectory{Source: &nativetest.FakeSource{}}}}
	op := plan.Operation{ID: "user:mise", Steps: []plan.Step{{Description: "verify ~/.local/bin/mise exists"}}}
	if err := ex.verifyUserTool(op, t.TempDir()); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("directory inspection cause lost: %v", err)
	}
}

func TestIndependentInstallersContinueAfterFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	src := newScripted()
	opts := options(t, src, t.TempDir())
	opts.FirstApply = false
	opts.Fetch = func(url string) ([]byte, error) {
		if strings.HasSuffix(url, "/broken") {
			return nil, errors.New("download failed")
		}
		return []byte("#!/bin/sh\n"), nil
	}
	var ops []plan.Operation
	for _, tool := range []string{"broken", "working"} {
		ops = append(ops, plan.Operation{ID: "user:" + tool, Kind: plan.KindUser, Action: plan.ActionInstall,
			Steps: []plan.Step{{Description: "download https://example.invalid/" + tool}, {Argv: []string{"sh", plan.InstallerScript}}, {Description: "verify ~/.local/bin/mise exists"}}})
	}
	r := Run(&plan.Plan{Complete: true, Operations: ops}, opts)
	if len(r.Failures) != 1 || r.Failures[0].ID != "user:broken" || len(r.Executed) != 1 || r.Executed[0] != "user:working" {
		t.Fatalf("result: %+v", r)
	}
}

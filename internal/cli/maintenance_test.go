package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/version"
)

func answerMaintenanceRepository(src *nativetest.FakeSource, root, origin string) {
	for _, answer := range []struct {
		args   []string
		output string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, root},
		{[]string{"remote", "get-url", "--all", "origin"}, origin},
		{[]string{"status", "--porcelain=v1", "--untracked-files=all"}, ""},
		{[]string{"symbolic-ref", "--quiet", "HEAD"}, "refs/heads/main"},
		{[]string{"for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/main"}, "origin\x00refs/heads/main"},
		{[]string{"rev-parse", "--verify", "HEAD"}, "abc123"},
		{[]string{"rev-parse", "--verify", "@{upstream}"}, "abc123"},
		{[]string{"merge-base", "--is-ancestor", "abc123", "@{upstream}"}, ""},
		{[]string{"fetch", "--no-tags", "--no-recurse-submodules", "origin", "refs/heads/main"}, ""},
		{[]string{"rev-parse", "--verify", "FETCH_HEAD^{commit}"}, "abc123"},
		{[]string{"merge-base", "--is-ancestor", "abc123", "abc123"}, ""},
		{[]string{"-c", "merge.autoStash=false", "merge", "--ff-only", "--no-overwrite-ignore", "--no-edit", "--no-stat", "abc123"}, ""},
	} {
		src.Commands[nativetest.Key("git", inspect.GitArgs(root, answer.args...)...)] = []byte(answer.output)
	}
}

type maintenanceSource struct {
	native.Source
	events   []string
	afterRun func(string)
}

func (s *maintenanceSource) Run(name string, args ...string) ([]byte, error) {
	key := nativetest.Key(name, args...)
	s.events = append(s.events, key)
	out, err := s.Source.Run(name, args...)
	if s.afterRun != nil {
		s.afterRun(key)
	}
	return out, err
}

func (s *maintenanceSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	s.events = append(s.events, nativetest.Key(name, args...))
	return s.Source.Stream(out, errOut, name, args...)
}

func TestMaintenanceStagesAndFailures(t *testing.T) {
	for _, mode := range []string{"success", "dirty Nimbus", "dirty dotfiles", "fetch failure", "system failure", "Chezmoi failure", "declined", "changed during approval", "preview"} {
		t.Run(mode, func(t *testing.T) {
			root, base := installerFixture(t)
			src := &maintenanceSource{Source: base}
			withSource(t, src)
			dotfiles := os.Getenv("HOME")
			args := []string{"sync", "--checkout", root, "--machine", "vm", "--json", "--yes"}
			if strings.HasPrefix(mode, "dirty ") {
				path := root
				if mode == "dirty dotfiles" {
					path = dotfiles
				}
				base.Commands[nativetest.Key("git", inspect.GitArgs(path, "status", "--porcelain=v1", "--untracked-files=all")...)] = []byte(" M config.toml\n?? notes.txt\n")
			}
			switch mode {
			case "fetch failure":
				base.Failures[nativetest.Key("git", inspect.GitArgs(dotfiles, "fetch", "--no-tags", "--no-recurse-submodules", "origin", "refs/heads/main")...)] = "network unavailable"
			case "system failure":
				base.Failures[nativetest.Key("dnf5", inspect.PackageQueryArgs...)] = "inspection failed"
			case "Chezmoi failure":
				base.failApply = true
			case "declined", "changed during approval":
				args = args[:len(args)-2]
				old := approver
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "declined" {
						return false
					}
					base.Commands[nativetest.Key("git", inspect.GitArgs(dotfiles, "rev-parse", "--verify", "HEAD")...)] = []byte("changed")
					base.Commands[nativetest.Key("git", inspect.GitArgs(dotfiles, "merge-base", "--is-ancestor", "changed", "@{upstream}")...)] = nil
					return true
				}
				t.Cleanup(func() { approver = old })
			case "preview":
				args = append(args, "--plan")
			}
			code, out, errOut := run(t, args...)
			wantOK := mode == "success" || mode == "preview"
			if (code == ExitOK) != wantOK {
				t.Fatalf("exit %d: %s%s", code, out, errOut)
			}
			if mode != "declined" && mode != "changed during approval" {
				var envelope Envelope
				if err := json.Unmarshal([]byte(out), &envelope); err != nil {
					t.Fatalf("not one JSON result: %v\n%s", err, out)
				}
			}
			applied := slices.Contains(src.events, "chezmoi apply")
			if applied != (mode == "success" || mode == "Chezmoi failure") {
				t.Fatalf("unexpected Chezmoi apply: %v", src.events)
			}
			if strings.HasPrefix(mode, "dirty ") || mode == "preview" {
				if slices.ContainsFunc(src.events, func(s string) bool {
					return strings.Contains(s, " fetch ") || strings.Contains(s, " merge --ff-only") || strings.HasPrefix(s, "sudo ")
				}) {
					t.Fatalf("preflight/preview mutated: %v", src.events)
				}
			}
			if mode == "fetch failure" && slices.ContainsFunc(src.events, func(s string) bool { return strings.Contains(s, " merge --ff-only") }) {
				t.Fatal("first tree updated before second fetch succeeded")
			}
			if mode == "success" {
				merge := slices.IndexFunc(src.events, func(s string) bool { return strings.Contains(s, " merge --ff-only") })
				metadata := slices.Index(src.events, "dnf5 makecache")
				if merge < 0 || metadata <= merge || slices.Index(src.events, "chezmoi apply") <= metadata {
					t.Fatalf("wrong phase order: %v", src.events)
				}
			}
			if mode == "Chezmoi failure" && (!strings.Contains(out, "system sync completed") || !strings.Contains(out, "partly updated")) {
				t.Fatal(out)
			}
		})
	}
}

func TestCombinedSyncStartsReplacementEngine(t *testing.T) {
	root, src := installerFixture(t)
	bin := t.TempDir()
	executable := filepath.Join(bin, "nimbus")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := syncExecutable
	syncExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { syncExecutable = old })
	t.Setenv("PATH", bin)
	t.Setenv("NIMBUS_TEST_EXECUTABLE", executable)
	t.Setenv(upgradeActive, "")
	body := `#!/bin/sh
printf '#!/bin/sh\nprintf "NEW-ENGINE\\n"\nprintf "<%%s>\\n" "$@"\nexit 17\n' > "$NIMBUS_TEST_EXECUTABLE"
`
	if err := os.WriteFile(filepath.Join(bin, "topgrade"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "sync", "--upgrade", "--checkout", root, "--machine", "vm", "--yes", "--prune")
	if code != 17 || !strings.Contains(out, "NEW-ENGINE") || !strings.Contains(out, "<sync>\n<--checkout>\n<"+root+">\n<--machine>\n<vm>\n<--yes>\n<--prune>") {
		t.Fatalf("replacement handoff: %d %s%s", code, out, errOut)
	}
	if len(src.calls) != 0 {
		t.Fatalf("old engine reconciled: %v", src.calls)
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{})
	if err != nil {
		t.Fatal("lock leaked: ", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncLoadsFetchedDefinitions(t *testing.T) {
	saved := version.Engine
	version.Engine = "0.3.1"
	t.Cleanup(func() { version.Engine = saved })
	root, base := installerFixture(t)
	src := &maintenanceSource{Source: base}
	withSource(t, src)
	src.afterRun = func(call string) {
		if strings.Contains(call, "-C "+root+" -c merge.autoStash=false merge") {
			if err := os.WriteFile(filepath.Join(root, "nimbus.toml"), []byte("schema=1\n[compatibility]\nfedora=['44']\nmin_engine='999.0.0'\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
	if code != ExitFailure || !strings.Contains(out, "999.0.0") || len(base.calls) != 0 {
		t.Fatalf("updated definitions not validated: %d %s%s %v", code, out, errOut, base.calls)
	}
}

func TestSyncRefreshesSharedProfilesWithoutChangingSSHChoice(t *testing.T) {
	root, base := installerFixture(t)
	dataKey := nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)
	base.Commands[dataKey] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common","old"],"onePasswordSsh":true}`)
	initArgs := []string{"init", "--prompt", "--promptString", "Machine=vm", "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=common", "--promptBool", "Enable 1Password SSH integration=true"}
	key := nativetest.Key("chezmoi", initArgs...)
	base.Commands[key] = nil
	withSource(t, handoffOutputSource{Source: base, afterStream: func(name string, args []string) {
		if nativetest.Key(name, args...) == key {
			base.Commands[dataKey] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
		}
	}})
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
	if code != ExitOK || !slices.Equal(base.calls, []string{key, "chezmoi apply"}) {
		t.Fatalf("%d %s%s calls=%v", code, out, errOut, base.calls)
	}
}

func TestSyncRejectsUnchangedChezmoiProfiles(t *testing.T) {
	root, base := installerFixture(t)
	base.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common","old"]}`)
	key := nativetest.Key("chezmoi", "init", "--prompt", "--promptString", "Machine=vm", "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=common", "--promptBool", "Enable 1Password SSH integration=false")
	base.Commands[key] = nil
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
	if code != ExitFailure || !strings.Contains(out, "did not retain the requested selection") || slices.Contains(base.calls, "chezmoi apply") {
		t.Fatalf("applied with stale selection: %d %s%s calls=%v", code, out, errOut, base.calls)
	}
}

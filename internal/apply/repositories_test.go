package apply

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestKeyFingerprintMismatchStopsBeforeAnyPrivilegedCommand(t *testing.T) {
	src := newScripted()
	src.fpr = "0000000000000000000000000000000000000000"
	root := t.TempDir()
	r := Run(samplePlan(t), options(t, src, root))
	if r.Failed != "repository:terra" || !strings.Contains(r.Error, "does not match the declared") {
		t.Fatalf("result = %+v", r)
	}
	if src.ran("sudo ") {
		t.Fatalf("a privileged command ran after a key mismatch:\n%s", strings.Join(src.log, "\n"))
	}
	if _, err := os.Stat(filepath.Join(root, state.SchemaFile)); err == nil {
		t.Fatal("state written although nothing succeeded")
	}
}

func TestRepositoryRepairIsVerifiedAsDeclared(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	// The scripted addrepo honours every --set, so the enabled repository
	// verifies as declared.
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:repo", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable, Summary: "enable terra"},
	}}
	if r := Run(p, opts); r.Error != "" {
		t.Fatalf("enable failed: %+v", r)
	}
	src2 := newScripted()
	opts2 := options(t, src2, t.TempDir())
	// Pre-write a file with gpgcheck off that addrepo will not replace.
	src2.Dirs[inspect.RepoDir] = []string{"nimbus-terra.repo"}
	src2.Files[filepath.Join(inspect.RepoDir, "nimbus-terra.repo")] = []byte("[nimbus-terra]\nenabled=1\ngpgcheck=0\nbaseurl=https://repos.fyralabs.com/terra44\npriority=100\n")
	p2 := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:repo2", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionRepair, Summary: "repair terra"},
	}}
	src2.privilegedNoop = true
	if r := Run(p2, opts2); r.Error == "" || !strings.Contains(r.Error, "gpgcheck") {
		t.Fatalf("repair that left gpgcheck off was verified: %+v", r)
	}
}

type dnfDropInSource struct {
	*nativetest.FakeSource
	readErr error
}

func (s dnfDropInSource) ReadFile(path string) ([]byte, error) {
	if s.readErr != nil {
		return nil, fmt.Errorf("read %s: %w", path, s.readErr)
	}
	return s.FakeSource.ReadFile(path)
}

func TestDNFDropInAdoptionVerifiesCurrentContent(t *testing.T) {
	root := definitions.Root{DNF: map[string]any{"fastestmirror": true}}
	for _, tc := range []struct {
		name    string
		content *string
		readErr error
		valid   bool
	}{
		{name: "matching", content: new(plan.DNFDropIn(root)), valid: true},
		{name: "changed", content: new("[main]\nfastestmirror=False\n")},
		{name: "empty", content: new("")},
		{name: "absent"},
		{name: "unreadable", readErr: os.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := dnfDropInSource{FakeSource: &nativetest.FakeSource{Files: map[string][]byte{}}, readErr: tc.readErr}
			if tc.content != nil {
				src.Files[inspect.DNFDropInPath] = []byte(*tc.content)
			}
			ex := &executor{p: &plan.Plan{}, opts: Options{Source: src, Root: root, Now: time.Now}}
			receipts, removed, err := ex.dnfConfig(plan.Operation{ID: "dnf:config", Kind: plan.KindDNFConfig, Action: plan.ActionAdopt})
			if (err == nil) != tc.valid || (len(receipts) == 1) != tc.valid || len(removed) != 0 {
				t.Fatalf("adoption: receipts=%v removed=%v err=%v; want valid=%t", receipts, removed, err, tc.valid)
			}
			if tc.readErr != nil && !errors.Is(err, tc.readErr) {
				t.Fatalf("inspection cause lost: %v", err)
			}
		})
	}
}

func TestDNFDropInRemovalRequiresVerifiedAbsence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content *string
		readErr error
		valid   bool
	}{
		{name: "absent", valid: true},
		{name: "present", content: new("[main]\nfastestmirror=True\n")},
		{name: "empty", content: new("")},
		{name: "unreadable", readErr: os.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := []string{"rm", "-f", inspect.DNFDropInPath}
			src := dnfDropInSource{FakeSource: &nativetest.FakeSource{Commands: map[string][]byte{nativetest.Key("sudo", argv...): nil}, Files: map[string][]byte{}}, readErr: tc.readErr}
			if tc.content != nil {
				src.Files[inspect.DNFDropInPath] = []byte(*tc.content)
			}
			ex := &executor{opts: Options{Source: src, Out: io.Discard}}
			op := plan.Operation{ID: "dnf:config", Kind: plan.KindDNFConfig, Action: plan.ActionRemove, Steps: []plan.Step{{Argv: argv}}}
			receipts, removed, err := ex.dnfConfig(op)
			if (err == nil) != tc.valid || (len(removed) == 1) != tc.valid || len(receipts) != 0 {
				t.Fatalf("removal: receipts=%v removed=%v err=%v; want valid=%t", receipts, removed, err, tc.valid)
			}
			if tc.valid && removed[0] != op.ID {
				t.Fatalf("retired unrelated receipt: %v", removed)
			}
			if tc.readErr != nil && !errors.Is(err, tc.readErr) {
				t.Fatalf("inspection cause lost: %v", err)
			}
		})
	}
}

func TestDNFDropInIsWrittenThenReadBack(t *testing.T) {
	src := newScripted()
	src.privilegedNoop = true
	root := t.TempDir()
	opts := options(t, src, root)
	opts.Root.DNF = map[string]any{"max_parallel_downloads": int64(10), "fastestmirror": true}
	want := plan.DNFDropIn(opts.Root)
	op := plan.Operation{ID: "dnf:config", Kind: plan.KindDNFConfig, Action: plan.ActionInstall, Summary: "configure DNF",
		Steps: []plan.Step{{Description: "set fastestmirror=True"}, {Argv: []string{"install", "-m", "0644", plan.DNFDropInPlaceholder, inspect.DNFDropInPath}, Privileged: true}}}
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:dnf", Operations: []plan.Operation{op}}
	// The privileged install is a no-op in the fake, so the file never
	// appears: verification must refuse the receipt.
	if r := Run(p, opts); r.Error == "" || !strings.Contains(r.Error, inspect.DNFDropInPath) || !strings.Contains(r.Error, os.ErrNotExist.Error()) {
		t.Fatalf("unwritten drop-in verified: %+v", r)
	}
	staged, err := os.ReadFile(filepath.Join(opts.Stage, "dnf-drop-in.conf"))
	if err != nil || string(staged) != want {
		t.Fatalf("staged content = %q, %v", staged, err)
	}
	if i := slices.IndexFunc(src.log, func(l string) bool {
		return strings.HasPrefix(l, "sudo install") && (strings.Contains(l, plan.DNFDropInPlaceholder) || !strings.Contains(l, opts.Stage))
	}); i >= 0 {
		t.Fatalf("placeholder not filled: %s", src.log[i])
	}
	src.Files[inspect.DNFDropInPath] = []byte(want)
	r := Run(p, opts)
	if r.Error != "" || len(r.Executed) != 1 {
		t.Fatalf("written drop-in: %+v", r)
	}
	a, err := state.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if rc, ok := a.Receipts["dnf:config"]; !ok || rc.Provider != "dnf-config" || rc.Previous != "absent" {
		t.Fatalf("receipt = %+v", rc)
	}
}

func TestADuplicateRepositoryIsDisabledAndVerified(t *testing.T) {
	src := newScripted()
	root := t.TempDir()
	opts := options(t, src, root)
	src.Dirs[inspect.RepoDir] = []string{"nimbus-terra.repo", "terra-maker.repo"}
	var owned bytes.Buffer
	owned.WriteString("[nimbus-terra]\n")
	for _, o := range plan.OwnedRepoOptions("terra", opts.Root.Repositories["terra"]) {
		owned.WriteString(o.Key + "=" + o.Value + "\n")
	}
	src.Files[filepath.Join(inspect.RepoDir, "nimbus-terra.repo")] = owned.Bytes()
	src.Files[filepath.Join(inspect.RepoDir, "terra-maker.repo")] = []byte("[terra-maker]\nname=Terra\nbaseurl=" + opts.Root.Repositories["terra"].BaseURL + "\nenabled=1\ngpgcheck=1\n")
	src.privilegedNoop = false
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:dup", Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionRepair, Summary: "repair terra",
			Steps: []plan.Step{{Description: plan.DisableDuplicateDescription, Argv: []string{"dnf5", "config-manager", "setopt", "terra-maker.enabled=0"}, Privileged: true}}},
	}}
	r := Run(p, opts)
	if r.Error != "" || !src.ran("sudo dnf5 config-manager setopt terra-maker.enabled=0") {
		t.Fatalf("duplicate not disabled: %+v\n%s", r, strings.Join(src.log, "\n"))
	}
	if src.ran("sudo dnf5 config-manager addrepo") {
		t.Fatal("the owned file was rewritten although only the duplicate drifted")
	}
}

func TestCOPRImportsTheVerifiedKeyBeforeEnabling(t *testing.T) {
	src := newScripted()
	src.privilegedNoop = true
	opts := options(t, src, t.TempDir())
	opts.Fetch = func(url string) ([]byte, error) { return []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\ncopr\n"), nil }
	src.fpr = "97E23476C89635135407C7D5E9BA41342C4B2995" // the declared COPR key
	op := plan.Operation{ID: "repository:hyprland-copr", Kind: plan.KindRepository, Action: plan.ActionEnable, Summary: "enable copr"}
	ex := &executor{p: &plan.Plan{}, opts: opts, seen: map[string]inspect.Package{}}
	ex.opts.Out = io.Discard
	copr := definitions.Repository{Kind: "copr", Project: "lionheartp/Hyprland", Key: "97E2 3476 C896 3513 5407 C7D5 E9BA 4134 2C4B 2995", Priority: new(130)}
	if err := ex.enableCOPR("hyprland-copr", copr, op); err != nil {
		t.Fatal(err)
	}
	var importAt, enableAt int
	for i, l := range src.log {
		if strings.HasPrefix(l, "sudo rpm --import ") {
			importAt = i + 1
		}
		if strings.HasPrefix(l, "sudo dnf5 copr enable -y") {
			enableAt = i + 1
		}
	}
	if importAt == 0 || enableAt == 0 || importAt > enableAt {
		t.Fatalf("import %d enable %d:\n%s", importAt, enableAt, strings.Join(src.log, "\n"))
	}
}

type keyScript struct {
	*scripted
	installedKey string
	leaveOldKey  bool
	extraKey     bool
}

func (s *keyScript) Run(name string, args ...string) ([]byte, error) {
	if name == "gpg" {
		key := s.fpr
		if args[len(args)-1] == plan.KeyPath("terra") || strings.HasSuffix(args[len(args)-1], ".trustedkeys.gpg") {
			key = s.installedKey
		}
		out := "pub:::::::::\nfpr:::::::::" + key + ":\n"
		if s.extraKey {
			out += "pub:::::::::\nfpr:::::::::" + strings.Repeat("3", 40) + ":\n"
		}
		return []byte(out), nil
	}
	if name == "sudo" && len(args) > 0 && args[0] == "install" && !s.leaveOldKey {
		s.installedKey = s.fpr
	}
	return s.scripted.Run(name, args...)
}

func (s *keyScript) Stream(_, _ io.Writer, name string, args ...string) error {
	_, err := s.Run(name, args...)
	return err
}

func TestRepositoryPinRepairVerifiesTheInstalledReplacement(t *testing.T) {
	for _, leaveOld := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement installed", true: "replacement missing"}[leaveOld], func(t *testing.T) {
			src := &keyScript{scripted: newScripted(), installedKey: strings.Repeat("1", 40), leaveOldKey: leaveOld}
			opts := options(t, src.scripted, t.TempDir())
			opts.Source = src
			src.Dirs[inspect.RepoOverride] = []string{"99-config_manager.repo"}
			src.Files[filepath.Join(inspect.RepoOverride, "99-config_manager.repo")] = []byte("[nimbus-terra]\ngpgkey=https://wrong.invalid/key\n")
			op := plan.Operation{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionRepair,
				Steps: []plan.Step{{Argv: []string{"gpg", "<verified key>"}}, plan.AddRepoStep("terra", terra(), true)}}
			p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:key", Operations: []plan.Operation{op}}
			result := Run(p, opts)
			if leaveOld {
				if result.Error == "" || !strings.Contains(result.Error, "trusted fingerprints") || len(result.Executed) != 0 {
					t.Fatalf("unchanged key accepted: %+v", result)
				}
			} else if result.Error != "" || len(result.Executed) != 1 {
				t.Fatalf("repair failed: %+v", result)
			}
		})
	}
}

func TestKeyBundleWithAdditionalPrimaryKeyIsRejectedBeforeImport(t *testing.T) {
	src := &keyScript{scripted: newScripted(), extraKey: true}
	opts := options(t, src.scripted, t.TempDir())
	opts.Source = src
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:key", Operations: []plan.Operation{{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable}}}
	result := Run(p, opts)
	if !strings.Contains(result.Error, "2 primary keys") || src.ran("sudo ") {
		t.Fatalf("unapproved bundled key imported: %+v\n%v", result, src.log)
	}
}

func TestFlatpakEnableVerifiesInstalledKeyInsteadOfOnlyDownloadedKey(t *testing.T) {
	src := &keyScript{scripted: newScripted(), installedKey: strings.Repeat("1", 40)}
	opts := options(t, src.scripted, t.TempDir())
	opts.Source = src
	p := &plan.Plan{Machine: "desktop", Complete: true, Digest: "sha256:remote", Operations: []plan.Operation{{ID: "flatpak-remote:flathub", Kind: plan.KindFlatpakRemote, Action: plan.ActionEnable}}}
	result := Run(p, opts)
	if !strings.Contains(result.Error, "trusted fingerprints") || len(result.Executed) != 0 {
		t.Fatalf("wrong installed remote key accepted: %+v", result)
	}
	if _, ok := src.Files[filepath.Join(inspect.FlatpakRepoPath, "config")]; !ok {
		t.Fatal("test never reached remote enable")
	}
}

type releaseKeyScript struct {
	*scripted
	failures []error
	checked  []string
}

func (s *releaseKeyScript) Run(name string, args ...string) ([]byte, error) {
	if name == "gpg" {
		i := len(s.checked)
		s.checked = append(s.checked, args[len(args)-1])
		if i < len(s.failures) {
			return nil, s.failures[i]
		}
	}
	return s.scripted.Run(name, args...)
}

func TestReleasePackageKeySelectionPreservesFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures []error
		mismatch bool
		empty    bool
		success  bool
	}{
		{name: "inspection failures", failures: []error{syscall.EIO, syscall.EACCES}},
		{name: "fingerprint mismatches", mismatch: true},
		{name: "empty archive", empty: true},
		{name: "matching key after failed inspection", failures: []error{syscall.EIO}, success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &releaseKeyScript{scripted: newScripted(), failures: tc.failures}
			r := terra()
			if tc.mismatch {
				src.fpr = strings.Repeat("1", 40)
			}
			data := []byte("fixture release RPM")
			r.ReleasePackage = "https://example.invalid/release.rpm"
			r.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
			keys := map[string][]byte{
				"etc/pki/rpm-gpg/first.asc":  []byte("first fixture key"),
				"etc/pki/rpm-gpg/second.asc": []byte("second fixture key"),
			}
			if tc.empty {
				keys = nil
			}
			ex := &executor{opts: Options{
				Source: src, Stage: t.TempDir(), Out: io.Discard,
				Fetch: func(string) ([]byte, error) { return data, nil },
				Keys:  func(string) (map[string][]byte, error) { return keys, nil },
			}}
			op := plan.Operation{Action: plan.ActionRepair, Steps: []plan.Step{{Argv: []string{"gpg"}}}}
			err := ex.enableReleasePackage("terra", r, op)
			if tc.success {
				if err != nil || len(src.checked) != 2 || !src.ran("sudo rpm --import ") {
					t.Fatalf("matching key was not imported after an inspection failure: %v; checked=%v, calls=%v", err, src.checked, src.log)
				}
				if !src.ran("sudo install -m 0644 " + src.checked[1] + " " + plan.KeyPath("terra")) {
					t.Fatalf("did not install the matching key: %v", src.log)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "no key in "+r.ReleasePackage+" has the declared fingerprint") || src.ran("sudo ") {
				t.Fatalf("unverified release key accepted or failure lost: %v; calls=%v", err, src.log)
			}
			if tc.empty && err.Error() != fmt.Sprintf("no key in %s has the declared fingerprint %s", r.ReleasePackage, src.fpr) {
				t.Fatalf("empty archive diagnostic changed: %v", err)
			}
			for _, cause := range tc.failures {
				if !errors.Is(err, cause) {
					t.Errorf("inspection cause %v lost: %v", cause, err)
				}
			}
			for name := range maps.Keys(keys) {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("key context %s lost: %v", name, err)
				}
			}
			if tc.mismatch && !strings.Contains(err.Error(), "key fingerprint "+src.fpr+" does not match") {
				t.Fatalf("fingerprint mismatch lost: %v", err)
			}
		})
	}
}

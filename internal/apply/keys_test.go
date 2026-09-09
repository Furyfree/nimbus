package apply

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

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
			src.Dirs[facts.RepoOverride] = []string{"99-config_manager.repo"}
			src.Files[filepath.Join(facts.RepoOverride, "99-config_manager.repo")] = []byte("[nimbus-terra]\ngpgkey=https://wrong.invalid/key\n")
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
	if _, ok := src.Files[filepath.Join(facts.FlatpakRepoPath, "config")]; !ok {
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

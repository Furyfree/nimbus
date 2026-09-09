package facts

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const keyA = "1111111111111111111111111111111111111111"
const keyB = "2222222222222222222222222222222222222222"

func keyOutput(keys ...string) []byte {
	out := []byte{}
	for _, key := range keys {
		out = fmt.Appendf(out, "pub:::::::::\nfpr:::::::::%s:\n", key)
	}
	return out
}

func TestKeyFingerprintsDistinguishPrimaryKeysFromSubkeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.asc")
	out := append(keyOutput(keyA), []byte("sub:::::::::\nfpr:::::::::"+keyB+":\n")...)
	src := &FakeSource{Commands: map[string][]byte{Key("gpg", KeyInspectArgs(path)...): out}}
	got, err := KeyFingerprints(src, path)
	if err != nil || !slices.Equal(got, []string{keyA}) {
		t.Fatalf("primary with subkey = %v, %v", got, err)
	}
	src.Commands[Key("gpg", KeyInspectArgs(path)...)] = keyOutput(keyA, keyB)
	got, err = KeyFingerprints(src, path)
	if err != nil || !slices.Equal(got, []string{keyA, keyB}) {
		t.Fatalf("two primary keys = %v, %v", got, err)
	}
}

func TestRepeatedPrimaryKeysDoNotHideOtherSigners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.asc")
	src := &FakeSource{Commands: map[string][]byte{Key("gpg", KeyInspectArgs(path)...): keyOutput(keyB, keyA, keyB, keyA)}}
	got, err := KeyFingerprints(src, path)
	if err != nil || !slices.Equal(got, []string{keyA, keyB}) {
		t.Fatalf("repeated primary keys = %v, %v", got, err)
	}
	src.Commands[Key("gpg", KeyInspectArgs(path)...)] = keyOutput(keyA, keyA, "invalid")
	if _, err := KeyFingerprints(src, path); err == nil {
		t.Fatal("repeated valid keys hid an invalid primary fingerprint")
	}
}

func TestRepositoryKeysDeduplicateFilesWithoutLosingSigners(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.asc")
	second := filepath.Join(t.TempDir(), "second.asc")
	src := &FakeSource{Commands: map[string][]byte{
		Key("gpg", KeyInspectArgs(first)...):  keyOutput(keyA, keyA),
		Key("gpg", KeyInspectArgs(second)...): keyOutput(keyB, keyA),
	}}
	urls := "file://" + first + " file://" + second + " file://" + first
	got, problem := repositoryKeys(src, urls)
	if problem != "" || !slices.Equal(got, []string{keyA, keyB}) {
		t.Fatalf("repeated configured keys = %v, %s", got, problem)
	}
	delete(src.Commands, Key("gpg", KeyInspectArgs(second)...))
	if got, problem := repositoryKeys(src, urls); problem == "" || got != nil {
		t.Fatalf("unreadable key was hidden by repeated known keys: %v, %s", got, problem)
	}
}

func TestRepositoryKeyObservationUsesTheEffectiveOverride(t *testing.T) {
	src := &FakeSource{
		Dirs: map[string][]string{RepoDir: {"maker.repo"}, RepoOverride: {"99-config_manager.repo"}},
		Files: map[string][]byte{
			filepath.Join(RepoDir, "maker.repo"):                  []byte("[maker]\ngpgkey=https://maker.invalid/key.asc\ngpgcheck=1\n"),
			filepath.Join(RepoOverride, "99-config_manager.repo"): []byte("[make*]\ngpgkey=file:///etc/pki/rpm-gpg/pinned\n"),
		},
		Commands: map[string][]byte{Key("gpg", KeyInspectArgs("/etc/pki/rpm-gpg/pinned")...): keyOutput(keyA)},
	}
	repos, err := repositories(src)
	if err != nil || len(repos) != 1 || repos[0].KeyError != "" || !slices.Equal(repos[0].KeyFingerprints, []string{keyA}) {
		t.Fatalf("effective key = %+v, %v", repos, err)
	}
	src.Dirs[RepoOverride] = nil
	repos, err = repositories(src)
	if err != nil || !strings.Contains(repos[0].KeyError, "not an explicit local file") {
		t.Fatalf("remote key was treated as verified: %+v, %v", repos, err)
	}
}

func TestRepositoryContinuedKeysRetainEverySigner(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprintf("override=%v", override), func(t *testing.T) {
			keyFiles := "file:///etc/pki/rpm-gpg/pinned\n  file:///etc/pki/rpm-gpg/extra"
			src := &FakeSource{
				Dirs:  map[string][]string{RepoDir: {"maker.repo"}},
				Files: map[string][]byte{},
				Commands: map[string][]byte{
					Key("gpg", KeyInspectArgs("/etc/pki/rpm-gpg/pinned")...): keyOutput(keyA),
					Key("gpg", KeyInspectArgs("/etc/pki/rpm-gpg/extra")...):  keyOutput(keyB),
				},
			}
			if override {
				src.Files[filepath.Join(RepoDir, "maker.repo")] = []byte("[maker]\ngpgkey=file:///etc/pki/rpm-gpg/pinned\n")
				src.Dirs[RepoOverride] = []string{"99-local.repo"}
				src.Files[filepath.Join(RepoOverride, "99-local.repo")] = []byte("[maker]\ngpgkey=" + keyFiles + "\n")
			} else {
				src.Files[filepath.Join(RepoDir, "maker.repo")] = []byte("[maker]\ngpgkey=" + keyFiles + "\n")
			}
			repos, err := repositories(src)
			if err != nil || len(repos) != 1 {
				t.Fatalf("repositories = %+v, %v", repos, err)
			}
			if repos[0].KeyError != "" || !slices.Equal(repos[0].KeyFingerprints, []string{keyA, keyB}) {
				t.Fatalf("continued signing key was ignored: %+v", repos[0])
			}
			want := "file:///etc/pki/rpm-gpg/pinned\nfile:///etc/pki/rpm-gpg/extra"
			options := repos[0].Options
			if override {
				options = repos[0].OverrideOptions
			}
			if repos[0].GPGKey != want || options["gpgkey"] != want {
				t.Fatalf("effective and recorded key options differ: %+v", repos[0])
			}
			delete(src.Commands, Key("gpg", KeyInspectArgs("/etc/pki/rpm-gpg/extra")...))
			repos, err = repositories(src)
			if err != nil || len(repos) != 1 || repos[0].KeyError == "" || len(repos[0].KeyFingerprints) != 0 {
				t.Fatalf("unreadable continued key was treated as verified: %+v, %v", repos, err)
			}
		})
	}
}

func TestFlatpakMalformedConfigurationLeavesTrustUnknown(t *testing.T) {
	src := &FakeSource{
		Files: map[string][]byte{filepath.Join(FlatpakRepoPath, "config"): []byte("[remote \"flathub\"]\ngpg-verify=true\n[broken\ngpg-verify=false\n")},
	}
	remote := FlatpakRemote{Name: "flathub"}
	inspectRemoteTrust(src, &remote)
	if remote.KeyError == "" || remote.GPGVerify || len(remote.KeyFingerprints) != 0 {
		t.Fatalf("malformed config was treated as verified: %+v", remote)
	}
}

func TestFlatpakTrustUsesNativeConfigurationSyntax(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		verify bool
	}{
		{"indented assignments", "  [remote \"flathub\"] \t\r\n  # policy\r\n\t gpg-verify = true \t\r\n  xa.disable = false\r\n", true},
		{"numeric booleans", "[remote \"flathub\"]\ngpg-verify=1\nxa.disable=0\n", true},
		{"duplicate groups and keys", "[remote \"flathub\"]\ngpg-verify=true\n[core]\nrepo_version=1\n[remote \"flathub\"]\ngpg-verify=1\ngpg-verify=false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &FakeSource{
				Files:    map[string][]byte{filepath.Join(FlatpakRepoPath, "config"): []byte(tc.config)},
				Commands: map[string][]byte{Key("gpg", KeyInspectArgs(filepath.Join(FlatpakRepoPath, "flathub.trustedkeys.gpg"))...): keyOutput(keyA)},
			}
			remote := FlatpakRemote{Name: "flathub"}
			inspectRemoteTrust(src, &remote)
			if remote.KeyError != "" || remote.GPGVerify != tc.verify || !slices.Equal(remote.KeyFingerprints, []string{keyA}) {
				t.Fatalf("native configuration trust = %+v", remote)
			}
		})
	}
}

func TestFlatpakUnsupportedTrustConfigurationFailsClosed(t *testing.T) {
	for _, config := range []string{
		"[remote \"flathub\"]\ngpg-verify=\"true\"\n",
		"[remote \"flathub\"]\ngpg-verify='true'\n",
		"[remote \"flathub\"]\ngpg-verify=TRUE\n",
		"[remote \"flathub\"]\ngpg-verify=yes\n",
		"[remote \"flathub\"]\ngpg-verify=on\n",
		"[remote \"flathub\"]\ngpg-verify=true\\s\n",
		"[remote \"flathub\"]\ngpg-verify=true\nxa.disable=\"false\"\n",
		"[remote \"flathub\"]\ngpg-verify=true\nxa.disable=true\n",
		"[remote \"flathub\"]\ngpg-verify=true\ngpgkeypath=\\s\n",
		"[remote \"flathub\"] # policy\ngpg-verify=true\n",
		"[remote \"flathub\"]\n; policy\ngpg-verify=true\n",
		"[remote \"flathub\"]\ngpg-verify=true\n  false\n",
		"[remote \"flathub\"]\ngpg-verify=true\ngpg-verify[C]=false\n",
		"[remote \"flathub\"]\ngpg-verify=true\n[broken\tgroup]\n",
		"[remote \"flathub\"]\ngpg-verify=true\nEncoding=latin1\n",
		"[remote \"flathub\"]\ngpg-verify=true\x00\n",
		"[remote \"flathub\"]\ngpg-verify=true\n#\xff\n",
	} {
		t.Run(config, func(t *testing.T) {
			src := &FakeSource{
				Files:    map[string][]byte{filepath.Join(FlatpakRepoPath, "config"): []byte(config)},
				Commands: map[string][]byte{Key("gpg", KeyInspectArgs(filepath.Join(FlatpakRepoPath, "flathub.trustedkeys.gpg"))...): keyOutput(keyA)},
			}
			remote := FlatpakRemote{Name: "flathub"}
			inspectRemoteTrust(src, &remote)
			if remote.KeyError == "" || remote.GPGVerify || len(remote.KeyFingerprints) != 0 {
				t.Fatalf("unsupported configuration was treated as verified: %+v", remote)
			}
		})
	}
}

func TestFlatpakTrustObservesInstalledKeyAndSignaturePolicy(t *testing.T) {
	src := &FakeSource{
		Files:    map[string][]byte{filepath.Join(FlatpakRepoPath, "config"): []byte("[remote \"flathub\"]\ngpg-verify=false\n")},
		Commands: map[string][]byte{Key("gpg", KeyInspectArgs(filepath.Join(FlatpakRepoPath, "flathub.trustedkeys.gpg"))...): keyOutput(keyA, keyB)},
	}
	remote := FlatpakRemote{Name: "flathub"}
	inspectRemoteTrust(src, &remote)
	if remote.GPGVerify || remote.KeyError != "" || len(remote.KeyFingerprints) != 2 {
		t.Fatalf("remote trust = %+v", remote)
	}
	delete(src.Commands, Key("gpg", KeyInspectArgs(filepath.Join(FlatpakRepoPath, "flathub.trustedkeys.gpg"))...))
	remote = FlatpakRemote{Name: "flathub"}
	inspectRemoteTrust(src, &remote)
	if remote.KeyError == "" {
		t.Fatal("missing key observation was accepted")
	}
}

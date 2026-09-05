package facts

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const keyA = "1111111111111111111111111111111111111111"
const keyB = "2222222222222222222222222222222222222222"

func keyOutput(keys ...string) []byte {
	var out string
	for _, key := range keys {
		out += "pub:::::::::\nfpr:::::::::" + key + ":\n"
	}
	return []byte(out)
}

func TestKeyFingerprintsDistinguishPrimaryKeysFromSubkeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.asc")
	out := append(keyOutput(keyA), []byte("sub:::::::::\nfpr:::::::::"+keyB+":\n")...)
	src := &FakeSource{Commands: map[string][]byte{Key("gpg", KeyInspectArgs(path)...): out}}
	got, err := KeyFingerprints(src, path)
	if err != nil || !reflect.DeepEqual(got, []string{keyA}) {
		t.Fatalf("primary with subkey = %v, %v", got, err)
	}
	src.Commands[Key("gpg", KeyInspectArgs(path)...)] = keyOutput(keyA, keyB)
	got, err = KeyFingerprints(src, path)
	if err != nil || !reflect.DeepEqual(got, []string{keyA, keyB}) {
		t.Fatalf("two primary keys = %v, %v", got, err)
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
	if err != nil || len(repos) != 1 || repos[0].KeyError != "" || !reflect.DeepEqual(repos[0].KeyFingerprints, []string{keyA}) {
		t.Fatalf("effective key = %+v, %v", repos, err)
	}
	src.Dirs[RepoOverride] = nil
	repos, err = repositories(src)
	if err != nil || !strings.Contains(repos[0].KeyError, "not an explicit local file") {
		t.Fatalf("remote key was treated as verified: %+v, %v", repos, err)
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

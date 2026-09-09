package facts

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// KeyInspectArgs examines a public key without creating a GnuPG home or
// consulting the user's keyring, configuration, or trust database.
func KeyInspectArgs(path string) []string {
	return []string{"--no-options", "--homedir", "/dev/null", "--batch", "--no-keyring", "--no-auto-check-trustdb", "--trust-model", "always", "--lock-never", "--with-colons", "--show-keys", path}
}

// KeyFingerprints returns every primary key; subkey fingerprints do not
// introduce another trusted signer. A bundle with extra primary keys must
// never pass a check against just its first fingerprint.
func KeyFingerprints(src Source, path string) ([]string, error) {
	out, err := src.Run("gpg", KeyInspectArgs(path)...)
	if err != nil {
		return nil, err
	}
	var result []string
	primary := false
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Split(line, ":")
		switch fields[0] {
		case "pub":
			primary = true
		case "sub":
			primary = false
		case "fpr":
			if primary && len(fields) > 9 {
				fp := strings.ToUpper(fields[9])
				if _, err := hex.DecodeString(fp); err != nil || len(fp) != 40 && len(fp) != 64 {
					return nil, fmt.Errorf("invalid primary fingerprint in %s", path)
				}
				if !slices.Contains(result, fp) {
					result = append(result, fp)
				}
				primary = false
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no primary key fingerprint in %s", path)
	}
	slices.Sort(result)
	return result, nil
}

func repositoryKeys(src Source, urls string) ([]string, string) {
	var result []string
	for uri := range strings.FieldsSeq(urls) {
		path, local := strings.CutPrefix(uri, "file://")
		if !local || !filepath.IsAbs(path) || strings.Contains(path, "$") {
			return nil, "configured signing key is not an explicit local file: " + uri
		}
		keys, err := KeyFingerprints(src, path)
		if err != nil {
			return nil, err.Error()
		}
		for _, key := range keys {
			if !slices.Contains(result, key) {
				result = append(result, key)
			}
		}
	}
	if len(result) == 0 {
		return nil, "repository has no configured signing key"
	}
	slices.Sort(result)
	return result, ""
}

const FlatpakRepoPath = "/var/lib/flatpak/repo"

// Flatpak uses the OSTree remote configuration and per-remote keyring.
// Read those files locally; remote-info can fetch metadata.
func inspectRemoteTrust(src Source, remote *FlatpakRemote) {
	if remote.Name == "" || strings.ContainsAny(remote.Name, "/\\") || remote.Name == "." || remote.Name == ".." {
		remote.KeyError = "invalid Flatpak remote name"
		return
	}
	data, err := src.ReadFile(filepath.Join(FlatpakRepoPath, "config"))
	if err != nil {
		remote.KeyError = err.Error()
		return
	}
	found := false
	for _, section := range parseRepoFile("config", data) {
		if section.ID == `remote "`+remote.Name+`"` {
			found = true
			remote.GPGVerify = normalizeBool(section.Options["gpg-verify"]) == "1"
			if normalizeBool(section.Options["xa.disable"]) == "1" || section.Options["gpgkeypath"] != "" {
				remote.KeyError = "remote is disabled or uses an additional gpgkeypath"
				return
			}
		}
	}
	if !found {
		remote.KeyError = "remote configuration is missing"
		return
	}
	remote.KeyFingerprints, err = KeyFingerprints(src, filepath.Join(FlatpakRepoPath, remote.Name+".trustedkeys.gpg"))
	if err != nil {
		remote.KeyError = err.Error()
	}
}

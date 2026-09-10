package inspect

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Furyfree/nimbus/internal/native"
)

// KeyInspectArgs examines a public key without creating a GnuPG home or
// consulting the user's keyring, configuration, or trust database.
func KeyInspectArgs(path string) []string {
	return []string{"--no-options", "--homedir", "/dev/null", "--batch", "--no-keyring", "--no-auto-check-trustdb", "--trust-model", "always", "--lock-never", "--with-colons", "--show-keys", path}
}

// KeyFingerprints returns every primary key; subkey fingerprints do not
// introduce another trusted signer. A bundle with extra primary keys must
// never pass a check against just its first fingerprint.
func KeyFingerprints(src native.Source, path string) ([]string, error) {
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
				result = append(result, fp)
				primary = false
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no primary key fingerprint in %s", path)
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func repositoryKeys(src native.Source, urls string) ([]string, string) {
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
		result = append(result, keys...)
	}
	if len(result) == 0 {
		return nil, "repository has no configured signing key"
	}
	slices.Sort(result)
	return slices.Compact(result), ""
}

const FlatpakRepoPath = "/var/lib/flatpak/repo"

// Flatpak uses the OSTree remote configuration and per-remote keyring.
// Read those files locally; remote-info can fetch metadata.
func inspectRemoteTrust(src native.Source, remote *FlatpakRemote) {
	if remote.Name == "" || strings.ContainsAny(remote.Name, "/\\") || remote.Name == "." || remote.Name == ".." {
		remote.KeyError = "invalid Flatpak remote name"
		return
	}
	data, err := src.ReadFile(filepath.Join(FlatpakRepoPath, "config"))
	if err != nil {
		remote.KeyError = err.Error()
		return
	}
	options, err := remoteTrustOptions(data, remote.Name)
	if err != nil {
		remote.KeyError = err.Error()
		return
	}
	if options["xa.disable"] == "true" || options["gpgkeypath"] != "" {
		remote.KeyError = "remote is disabled or uses an additional gpgkeypath"
		return
	}
	remote.GPGVerify = options["gpg-verify"] == "true"
	remote.KeyFingerprints, err = KeyFingerprints(src, filepath.Join(FlatpakRepoPath, remote.Name+".trustedkeys.gpg"))
	if err != nil {
		remote.KeyError = err.Error()
	}
}

// remoteTrustOptions reads only the signature policy from OSTree's GLib key
// file. DNF's quoting, continuation, and boolean rules do not apply here.
func remoteTrustOptions(data []byte, name string) (map[string]string, error) {
	contents := string(data)
	if !utf8.ValidString(contents) || strings.ContainsRune(contents, 0) {
		return nil, fmt.Errorf("remote configuration is not valid UTF-8 text")
	}
	const whitespace = " \t\r\v\f"
	want := `remote "` + name + `"`
	group := ""
	found := false
	options := map[string]string{}
	lineNumber := 0
	for raw := range strings.SplitSeq(contents, "\n") {
		lineNumber++
		line := strings.TrimLeft(strings.TrimSuffix(raw, "\r"), whitespace)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			var ok bool
			group, ok = strings.CutSuffix(strings.TrimRight(line[1:], " \t"), "]")
			if !ok || group == "" || strings.ContainsAny(group, "[]") || strings.IndexFunc(group, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
				return nil, fmt.Errorf("remote configuration line %d: invalid group", lineNumber)
			}
			found = found || group == want
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimRight(key, whitespace)
		if !ok || group == "" || key == "" || strings.ContainsAny(key, "[]") {
			return nil, fmt.Errorf("remote configuration line %d: invalid or unsupported key", lineNumber)
		}
		value = strings.TrimLeft(value, whitespace)
		if key == "Encoding" && !strings.EqualFold(value, "UTF-8") {
			return nil, fmt.Errorf("remote configuration line %d: unsupported encoding", lineNumber)
		}
		if group == want {
			switch key {
			case "gpg-verify", "xa.disable", "gpgkeypath":
				options[key] = value
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("remote configuration is missing")
	}
	for _, key := range []string{"gpg-verify", "xa.disable"} {
		value, ok := options[key]
		if !ok {
			continue
		}
		switch strings.TrimRight(value, whitespace) {
		case "true", "1":
			options[key] = "true"
		case "false", "0":
			options[key] = "false"
		default:
			return nil, fmt.Errorf("remote configuration has an invalid %s boolean", key)
		}
	}
	return options, nil
}

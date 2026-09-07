package plan

import (
	"fmt"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

func keyDrift(r definitions.Repository, fingerprints []string, keyError string) string {
	if keyError != "" {
		return "signing key is unverified: " + keyError
	}
	want := definitions.NormalizeFingerprint(r.Key)
	if len(fingerprints) != 1 || fingerprints[0] != want {
		return fmt.Sprintf("trusted fingerprints [%s] differ from declared %s", strings.Join(fingerprints, ", "), want)
	}
	return ""
}

// RepositoryKeyDrift compares the active local key material to the pin.
func RepositoryKeyDrift(id string, r definitions.Repository, have facts.Repository) string {
	if have.GPGKey != "file://"+KeyPath(id) {
		return "configured signing key must use the verified local pin at " + KeyPath(id)
	}
	return keyDrift(r, have.KeyFingerprints, have.KeyError)
}

// FlatpakKeyDrift also requires signature checking on the existing remote.
func FlatpakKeyDrift(r definitions.Repository, have facts.FlatpakRemote) string {
	if !have.GPGVerify {
		return "GPG verification is disabled or unknown"
	}
	return keyDrift(r, have.KeyFingerprints, have.KeyError)
}

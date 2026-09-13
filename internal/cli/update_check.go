package cli

import (
	"fmt"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
)

// Check the full native transaction, including obsoletes and dependencies.
// An empty upgrade list alone does not prove that DNF has no work.
func systemUpdatesPending(src native.Source, root definitions.Root) (bool, error) {
	out, runErr := src.Run("dnf5", "--cacheonly", "--assumeno", "upgrade")
	tx, err := plan.ParsePreview(out)
	if err != nil {
		return false, fmt.Errorf("DNF update check failed: %w", err)
	}
	if runErr != nil && len(tx.Packages) == 0 {
		return false, fmt.Errorf("DNF update check failed: %w", runErr)
	}
	if !tx.NothingToDo && len(tx.Packages) == 0 {
		return false, fmt.Errorf("DNF update check returned no usable transaction")
	}
	pending := len(tx.Packages) > 0
	for _, repo := range root.Repositories {
		if repo.Kind != "flatpak" {
			continue
		}
		remotes, err := src.Run("flatpak", "remotes", "--system", "--columns=name")
		if err != nil {
			return false, fmt.Errorf("Flatpak update check failed: %w", err)
		}
		// No --cached: inspect fresh remote metadata for all system remotes,
		// including runtime/locale refs and sources added outside Nimbus.
		for remote := range strings.Lines(string(remotes)) {
			remote = strings.TrimSpace(remote)
			if remote == "" {
				continue
			}
			refs, err := src.Run("flatpak", "remote-ls", "--system", "--updates", "--all", "--columns=ref", "--", remote)
			if err != nil {
				return false, fmt.Errorf("Flatpak update check for %s failed: %w", remote, err)
			}
			pending = pending || strings.TrimSpace(string(refs)) != ""
		}
		break
	}
	return pending, nil
}

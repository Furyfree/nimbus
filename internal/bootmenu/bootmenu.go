// Package bootmenu maintains the Nimbus BLS mirror that lets GRUB show one
// default kernel plus a "Previous kernels" submenu. The mirror holds a copy
// of exactly the current Fedora default entry; the grub.d payload lists it
// with `blscfg <entry>` and the rest with an unfiltered `blscfg`.
package bootmenu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

const (
	// Marker activates the grouped kernel menu; the boot-theme component owns it.
	Marker = "/etc/nimbus/boot-theme.enabled"
	// EntriesDir is Fedora's BLS entry directory.
	EntriesDir = "/boot/loader/entries"
	// Grubenv holds saved_entry, the id Fedora installed last.
	Grubenv = "/boot/grub2/grubenv"
	// MirrorDir is the Nimbus-owned copy of the single default entry.
	MirrorDir = "/boot/loader/entries-nimbus"
)

// Mirror rebuilds the mirror for the current default entry and returns its
// entry id (the BLS filename without .conf). A missing or stale saved_entry
// falls back to the newest installed entry.
func Mirror(src native.Source, entriesDir, grubenv, mirrorDir string) (string, error) {
	names, err := src.ReadDir(entriesDir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", entriesDir, err)
	}
	var ids []string
	for _, name := range names {
		stem, ok := strings.CutSuffix(name, ".conf")
		if !ok || stem == "" || strings.HasPrefix(stem, ".") {
			continue
		}
		ids = append(ids, stem)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no BLS entries in %s", entriesDir)
	}
	saved, err := grubenvValue(src, grubenv, "saved_entry")
	if err != nil {
		return "", err
	}
	chosen := ""
	switch {
	// A rescue entry never leads the grouped menu; it belongs under the
	// Previous kernels submenu even if Fedora's saved_entry names it.
	case saved != "" && !strings.Contains(saved, "-0-rescue") && slices.Contains(ids, saved):
		chosen = saved
	default:
		chosen = newestKernel(ids)
	}
	if err := rebuild(entriesDir, mirrorDir, chosen); err != nil {
		return "", err
	}
	return chosen, nil
}

// newestKernel picks the newest non-rescue, non-debug entry, matching
// Fedora's default selection. Rescue never leads the grouped menu, and debug
// entries only lead when they are the only kernels installed.
func newestKernel(ids []string) string {
	kernels := make([]string, 0, len(ids))
	for _, id := range ids {
		if !strings.Contains(id, "-0-rescue") {
			kernels = append(kernels, id)
		}
	}
	if len(kernels) == 0 {
		return slices.MaxFunc(ids, compareVersions)
	}
	usable := make([]string, 0, len(kernels))
	for _, id := range kernels {
		if !strings.Contains(id, "debug") {
			usable = append(usable, id)
		}
	}
	if len(usable) == 0 {
		usable = kernels
	}
	return slices.MaxFunc(usable, compareVersions)
}

// grubenvValue reads one key from the GRUB environment block. A missing file
// or key is not an error: the caller falls back to the newest entry.
func grubenvValue(src native.Source, path, key string) (string, error) {
	data, err := src.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), key+"="); ok {
			return value, nil
		}
	}
	return "", nil
}

// rebuild replaces the mirror contents with a copy of one BLS entry. The
// entry is written through a temporary file, synced and renamed into place
// before any stale copy is removed, so a failure never leaves grub.cfg
// pointing at a missing or truncated mirror. Only *.conf files created by
// Nimbus and its own temporary files are removed.
func rebuild(entriesDir, mirrorDir, chosen string) error {
	data, err := os.ReadFile(filepath.Join(entriesDir, chosen+".conf"))
	if err != nil {
		return err
	}
	data = plainTitle(data)
	if err := os.MkdirAll(mirrorDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(mirrorDir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(mirrorDir, chosen+".conf")
	tmp, err := os.CreateTemp(mirrorDir, ".nimbus-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	names, err := os.ReadDir(mirrorDir)
	if err != nil {
		return err
	}
	for _, entry := range names {
		name := entry.Name()
		if strings.HasPrefix(name, ".nimbus-") && strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(mirrorDir, name)); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(name, ".conf") || name == chosen+".conf" {
			continue
		}
		if err := os.Remove(filepath.Join(mirrorDir, name)); err != nil {
			return err
		}
	}
	return nil
}

// plainTitle shortens the mirrored entry's title to the text before its first
// parenthesis, so the top-level entry reads "Fedora Linux". Only the Nimbus
// copy changes: Fedora's own entry keeps the full title and lists the kernel
// version under Previous kernels, and GRUB's default is chosen by entry id.
func plainTitle(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, "title ")
		if !ok {
			continue
		}
		if name, _, cut := strings.Cut(rest, " ("); cut && strings.TrimSpace(name) != "" {
			lines[i] = "title " + strings.TrimSpace(name)
		}
		break
	}
	return []byte(strings.Join(lines, "\n"))
}

// compareVersions orders BLS ids by their Fedora version suffix: numeric
// segments compare as numbers, alpha segments bytewise, and a numeric segment
// outranks an alpha one, so "0-rescue-…" sorts before a kernel release.
func compareVersions(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		digitA, digitB := ra[i] >= '0' && ra[i] <= '9', rb[j] >= '0' && rb[j] <= '9'
		switch {
		case digitA && !digitB:
			return 1
		case !digitA && digitB:
			return -1
		case digitA:
			endA, endB := i, j
			for endA < len(ra) && ra[endA] >= '0' && ra[endA] <= '9' {
				endA++
			}
			for endB < len(rb) && rb[endB] >= '0' && rb[endB] <= '9' {
				endB++
			}
			segA := strings.TrimLeft(string(ra[i:endA]), "0")
			segB := strings.TrimLeft(string(rb[j:endB]), "0")
			if len(segA) != len(segB) {
				return compareInts(len(segA), len(segB))
			}
			if c := strings.Compare(segA, segB); c != 0 {
				return c
			}
			i, j = endA, endB
		default:
			if ra[i] != rb[j] {
				return compareInts(int(ra[i]), int(rb[j]))
			}
			i++
			j++
		}
	}
	return compareInts(len(ra)-i, len(rb)-j)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

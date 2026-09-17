// Package bootmenu maintains the Nimbus BLS mirror that lets GRUB show one
// default kernel plus a "Previous kernels" submenu. The mirror holds a copy
// of exactly the current Fedora default entry; the grub.d payload lists it
// with `blscfg` and the rest through `blscfg non-default`.
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
	case slices.Contains(ids, saved):
		chosen = saved
	default:
		chosen = slices.MaxFunc(ids, compareVersions)
	}
	if err := rebuild(entriesDir, mirrorDir, chosen); err != nil {
		return "", err
	}
	return chosen, nil
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

// rebuild replaces the mirror contents with a copy of one BLS entry. Only
// *.conf files created by Nimbus are removed.
func rebuild(entriesDir, mirrorDir, chosen string) error {
	if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
		return err
	}
	names, err := os.ReadDir(mirrorDir)
	if err != nil {
		return err
	}
	for _, entry := range names {
		name := entry.Name()
		if !strings.HasSuffix(name, ".conf") || name == chosen+".conf" {
			continue
		}
		if err := os.Remove(filepath.Join(mirrorDir, name)); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(entriesDir, chosen+".conf"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(mirrorDir, chosen+".conf"), data, 0o644)
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

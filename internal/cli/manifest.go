package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
)

// renderManifest writes a machine manifest in the canonical layout. Leading
// comment lines of the existing file are kept; everything else is derived
// from the manifest so the file always says exactly what it means.
func renderManifest(existing []byte, m *definitions.Machine) ([]byte, error) {
	if !utf8.ValidString(m.Hardware) {
		return nil, fmt.Errorf("machine hardware contains invalid UTF-8")
	}
	if m.Dotfiles != nil && !utf8.ValidString(m.Dotfiles.Repo) {
		return nil, fmt.Errorf("machine dotfiles.repo contains invalid UTF-8")
	}
	var b bytes.Buffer
	for line := range strings.SplitSeq(string(existing), "\n") {
		if strings.HasPrefix(line, "#") {
			b.WriteString(line + "\n")
			continue
		}
		break
	}
	if err := toml.NewEncoder(&b).SetArraysMultiline(true).SetIndentSymbol("  ").Encode(m); err != nil {
		return nil, fmt.Errorf("encode machine manifest: %w", err)
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// manifestPath is the tracked manifest file of a machine.
func manifestPath(root, machine string) string {
	return filepath.Join(root, "machines", machine+".toml")
}

// writeManifest replaces the manifest atomically as the normal user and
// leaves the Git change to the user.
func writeManifest(path string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nimbus-manifest-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	defer func() { _ = tmp.Close() }()
	if _, err := tmp.Write(append(content, '\n')); err != nil {
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// unifiedDiff is a small line diff for the manifest review; the files are a
// few dozen lines, so a full LCS is affordable.
func unifiedDiff(path string, before, after []byte) string {
	a := strings.Split(strings.TrimRight(string(before), "\n"), "\n")
	b := strings.Split(strings.TrimRight(string(after), "\n"), "\n")
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", path, path)
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			fmt.Fprintf(&out, " %s\n", a[i])
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			fmt.Fprintf(&out, "+%s\n", b[j])
			j++
		default:
			fmt.Fprintf(&out, "-%s\n", a[i])
			i++
		}
	}
	return out.String()
}

func addUnique(list []string, items ...string) []string {
	seen := map[string]bool{}
	for _, v := range list {
		seen[v] = true
	}
	for _, it := range items {
		if !seen[it] {
			list = append(list, it)
			seen[it] = true
		}
	}
	return list
}

func removeAll(list []string, items ...string) []string {
	drop := map[string]bool{}
	for _, it := range items {
		drop[it] = true
	}
	var out []string
	for _, v := range list {
		if !drop[v] {
			out = append(out, v)
		}
	}
	return out
}

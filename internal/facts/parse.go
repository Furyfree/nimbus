package facts

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// parseOSRelease reads the KEY=value pairs of /etc/os-release.
func parseOSRelease(data []byte) map[string]string {
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}

// PackageQueryFormat is the DNF5 query format the inspector requests. DNF5
// prints "\t" literally, so fields are separated by "|".
const PackageQueryFormat = "%{name}|%{epoch}|%{version}|%{release}|%{arch}|%{from_repo}|%{reason}\n"

// parsePackages reads the repoquery output produced by PackageQueryFormat.
func parsePackages(data []byte) ([]Package, error) {
	var pkgs []Package
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 7 {
			return nil, fmt.Errorf("line %d: expected 7 fields, got %d: %q", n, len(fields), line)
		}
		if fields[0] == "" {
			return nil, fmt.Errorf("line %d: empty package name", n)
		}
		pkgs = append(pkgs, Package{
			Name: fields[0], Epoch: fields[1], Version: fields[2], Release: fields[3],
			Arch: fields[4], FromRepo: fields[5], Reason: normalizeReason(fields[6]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func normalizeReason(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "user":
		return "user"
	case "dependency":
		return "dependency"
	case "weak dependency", "weak-dependency":
		return "weak-dependency"
	case "group":
		return "group"
	case "external":
		return "external"
	default:
		return "unknown"
	}
}

// parseRepoFile reads every [section] of one .repo file. Only the keys the
// engine needs are kept; enabled defaults to true as DNF does.
func parseRepoFile(file string, data []byte) []Repository {
	var repos []Repository
	var current *Repository
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			repos = append(repos, Repository{ID: line[1 : len(line)-1], File: filepath.Base(file), Enabled: true})
			current = &repos[len(repos)-1]
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "name":
			current.Name = value
		case "enabled":
			current.Enabled = value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
		case "gpgcheck":
			current.GPGCheck = normalizeBool(value)
		case "gpgkey":
			current.GPGKey = value
		case "priority":
			current.Priority = value
		}
	}
	return repos
}

func normalizeBool(v string) string {
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return "1"
	case "0", "false", "no":
		return "0"
	}
	return v
}

// parseColumns reads flatpak --columns output: tab-separated rows, no
// header.
func parseColumns(data []byte, want int) ([][]string, error) {
	var rows [][]string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < want {
			return nil, fmt.Errorf("line %d: expected %d columns, got %d: %q", n, want, len(fields), line)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields[:want])
	}
	return rows, scanner.Err()
}

package facts

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// parseOSRelease reads the KEY=value pairs of /etc/os-release.
func parseOSRelease(data []byte) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
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
			Arch: fields[4], FromRepo: normalizeRepo(fields[5]), Reason: normalizeReason(fields[6]),
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

// parseRepoFile keeps DNF's continued values and section comments intact.
// Typed observations derive from the complete, effective options.
func parseRepoFile(file string, data []byte) ([]Repository, error) {
	var repos []Repository
	var current *Repository
	sections := map[string]int{}
	key := ""
	lineNumber := 0
	for raw := range strings.SplitSeq(string(data), "\n") {
		lineNumber++
		raw = strings.TrimRight(raw, "\r")
		if lineNumber == 1 {
			raw = strings.TrimPrefix(raw, "\uFEFF")
		}
		if strings.ContainsRune(raw, '\x00') {
			return nil, fmt.Errorf("%s line %d: NUL in configuration", file, lineNumber)
		}
		if raw == "" || raw[0] == '#' || raw[0] == ';' {
			key = ""
			continue
		}
		line := strings.Trim(raw, " \t\r")
		if line == "" {
			if key != "" {
				current.Options[key] += "\n"
			}
			continue
		}
		if line[0] == '[' {
			id, err := repoSection(line)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", file, lineNumber, err)
			}
			i, ok := sections[id]
			if !ok {
				i = len(repos)
				sections[id] = i
				repos = append(repos, Repository{ID: id, File: filepath.Base(file), Options: map[string]string{}})
			}
			current, key = &repos[i], ""
			continue
		}
		if current == nil {
			return nil, fmt.Errorf("%s line %d: option without a section", file, lineNumber)
		}
		if raw[0] == ' ' || raw[0] == '\t' || raw[0] == '\r' {
			if key == "" {
				return nil, fmt.Errorf("%s line %d: continuation without an option", file, lineNumber)
			}
			current.Options[key] += "\n" + line
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		key = strings.Trim(name, " \t")
		if !ok || key == "" {
			return nil, fmt.Errorf("%s line %d: expected option=value", file, lineNumber)
		}
		current.Options[key] = strings.TrimLeft(value, " \t")
	}
	for i := range repos {
		r := &repos[i]
		for key, value := range r.Options {
			value = strings.TrimRight(value, "\n")
			if len(value) > 1 && value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
				value = value[1 : len(value)-1]
			}
			r.Options[key] = value
		}
		for _, key := range []string{"enabled", "gpgcheck"} {
			if value, ok := r.Options[key]; ok && normalizeBool(value) != "0" && normalizeBool(value) != "1" {
				return nil, fmt.Errorf("%s section %q: invalid %s value %q", file, r.ID, key, value)
			}
		}
		r.Name = r.Options["name"]
		r.Enabled = true
		if value, ok := r.Options["enabled"]; ok {
			r.Enabled = normalizeBool(value) == "1"
		}
		r.GPGCheck = normalizeBool(r.Options["gpgcheck"])
		r.GPGKey = r.Options["gpgkey"]
		r.Priority = r.Options["priority"]
		r.BaseURL = r.Options["baseurl"]
		r.Metalink = r.Options["metalink"]
		r.Mirrorlist = r.Options["mirrorlist"]
	}
	return repos, nil
}

func repoSection(line string) (string, error) {
	bracketExpression := false
	for i := 1; i < len(line); i++ {
		switch line[i] {
		case '\r':
			return "", fmt.Errorf("invalid repository section %q", line)
		case '[':
			bracketExpression = true
		case ']':
			if bracketExpression {
				bracketExpression = false
				continue
			}
			rest := strings.Trim(line[i+1:], " \t\r")
			if i == 1 || (rest != "" && rest[0] != '#' && rest[0] != ';') {
				return "", fmt.Errorf("invalid repository section %q", line)
			}
			return line[1:i], nil
		}
	}
	return "", fmt.Errorf("unclosed repository section %q", line)
}

// normalizeRepo returns the repository a package came from. A package
// installed by dnf5 replay is recorded as @stored_transaction(<repo>); the
// repository inside is the source that matters for ownership.
func normalizeRepo(v string) string {
	if inner, ok := strings.CutPrefix(v, "@stored_transaction("); ok {
		return strings.TrimSuffix(inner, ")")
	}
	return v
}

// parseCargoList reads cargo install --list: a crate heads each block as
// "name vX.Y.Z:" at the start of a line, its binaries indented below.
func parseCargoList(out []byte) []string {
	var crates []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		name, _, _ := strings.Cut(line, " ")
		crates = append(crates, name)
	}
	slices.Sort(crates)
	return crates
}

// ParseChezmoiData reads the handoff's exact keys. "Profiles" holds the
// machine selection; "profiles" is a separate list derived by dotfiles.
func ParseChezmoiData(out []byte) (Chezmoi, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		return Chezmoi{Initialized: true}, fmt.Errorf("chezmoi data: %w", err)
	}
	data := Chezmoi{Initialized: true}
	for _, field := range []struct {
		name   string
		target any
	}{
		{"Machine", &data.Machine},
		{"ManagedByNimbus", &data.ManagedByNimbus},
		{"Profiles", &data.Profiles},
		{"onePasswordSsh", &data.OnePasswordSSH},
	} {
		if raw, ok := fields[field.name]; ok {
			if err := json.Unmarshal(raw, field.target); err != nil {
				return Chezmoi{Initialized: true}, fmt.Errorf("chezmoi data %s: %w", field.name, err)
			}
		}
	}
	return data, nil
}

func normalizeBool(v string) string {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return "1"
	case "0", "false", "no", "off":
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

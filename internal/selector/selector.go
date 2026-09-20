// Package selector reads the local selector and checks that the selected
// checkout has the approved Git origin. It runs no command and uses no
// network.
package selector

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// CurrentSchema is the selector schema this engine reads. Schema 2 adds the
// required channel; schema 1 selectors are read as stable and are recorded as
// schema 2 by the next selector-writing action.
const CurrentSchema = 2

// Channel names the engine track a selector follows.
const (
	ChannelStable  = "stable"
	ChannelDevelop = "develop"
)

// Selector is ~/.config/nimbus/config.toml.
type Selector struct {
	Schema   int    `toml:"schema"`
	Checkout string `toml:"checkout"`
	Machine  string `toml:"machine"`
	Origin   string `toml:"origin"`
	Channel  string `toml:"channel"`
}

// DefaultPath is the selector location for the current user.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nimbus", "config.toml"), nil
}

// Load reads and checks one selector file. The selector must be a regular
// file, not a symlink, and the file read is the file that was inspected.
func Load(path string) (*Selector, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read selector: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("selector %s must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("selector %s must be a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read selector: %w", err)
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read selector: %w", err)
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("selector %s changed while it was being read", path)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read selector: %w", err)
	}
	var s Selector
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			return nil, fmt.Errorf("selector %s: unknown field: %s", path, strings.TrimSpace(strict.String()))
		}
		return nil, fmt.Errorf("selector %s: %w", path, err)
	}
	switch s.Schema {
	case 1:
		// The legacy schema predates the channel; it means stable and is
		// recorded as schema 2 by the next selector-writing action.
		if s.Channel != "" {
			return nil, fmt.Errorf("selector %s: schema 1 must not set channel; run init to record schema 2", path)
		}
		s.Channel = ChannelStable
	case CurrentSchema:
		switch s.Channel {
		case ChannelStable, ChannelDevelop:
		case "":
			return nil, fmt.Errorf("selector %s: channel is required", path)
		default:
			return nil, fmt.Errorf("selector %s: unsupported channel %q", path, s.Channel)
		}
	default:
		return nil, fmt.Errorf("selector %s: schema %d is not supported", path, s.Schema)
	}
	switch {
	case s.Checkout == "":
		return nil, fmt.Errorf("selector %s: checkout is required", path)
	case s.Machine == "":
		return nil, fmt.Errorf("selector %s: machine is required", path)
	case s.Origin == "":
		return nil, fmt.Errorf("selector %s: origin is required", path)
	}
	normalized, err := NormalizeOrigin(s.Origin)
	if err != nil {
		return nil, fmt.Errorf("selector %s: origin: %w", path, err)
	}
	if normalized != s.Origin {
		return nil, fmt.Errorf("selector %s: origin must be the normalized identity %q", path, normalized)
	}
	return &s, nil
}

// SetChannel rewrites the selector's channel, migrating schema 1 to schema 2
// while preserving checkout, machine and origin. It is the narrow action the
// checkout bootstrap uses after a channel switch.
func SetChannel(path, channel string) error {
	if channel != ChannelStable && channel != ChannelDevelop {
		return fmt.Errorf("unsupported channel %q", channel)
	}
	sel, err := Load(path)
	if err != nil {
		return err
	}
	sel.Schema = CurrentSchema
	sel.Channel = channel
	return Write(path, sel)
}

// Write stores the selector: the directory with mode 0700 when it is
// missing, the file written beside its target and renamed into place, so a
// reader never sees a partial file. The origin must already be normalized.
func Write(path string, s *Selector) error {
	if s.Schema != CurrentSchema || s.Checkout == "" || s.Machine == "" || s.Origin == "" ||
		(s.Channel != ChannelStable && s.Channel != ChannelDevelop) {
		return errors.New("selector: schema, checkout, machine, origin, and a valid channel are required")
	}
	if !utf8.ValidString(s.Checkout) || !utf8.ValidString(s.Machine) || !utf8.ValidString(s.Origin) ||
		!utf8.ValidString(s.Channel) {
		return errors.New("selector: checkout, machine, origin, and channel must be valid UTF-8")
	}
	if normalized, err := NormalizeOrigin(s.Origin); err != nil || normalized != s.Origin {
		return fmt.Errorf("selector: origin %q is not a normalized identity", s.Origin)
	}
	content, err := toml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode selector: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("selector %s must not be a symlink", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(content); err != nil {
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var scpLikeRe = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// NormalizeOrigin reduces a Git locator to its repository identity: transport
// and user removed, host lowercased, trailing slashes and one .git removed.
func NormalizeOrigin(locator string) (string, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return "", fmt.Errorf("empty locator")
	}
	var host, p string
	if strings.Contains(locator, "://") {
		u, err := url.Parse(locator)
		if err != nil {
			return "", err
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git":
		default:
			return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
		}
		host, p = u.Host, u.Path
	} else if m := scpLikeRe.FindStringSubmatch(locator); m != nil {
		host, p = m[1], m[2]
	} else if !strings.Contains(locator, ":") && strings.Count(locator, "/") >= 1 {
		// Already an identity such as github.com/owner/repo.
		host, p, _ = strings.Cut(locator, "/")
	} else {
		return "", fmt.Errorf("unsupported locator %q", locator)
	}
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	host = strings.ToLower(host)
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.TrimRight(p, "/")
	if host == "" || p == "" {
		return "", fmt.Errorf("locator %q has no host and path", locator)
	}
	return host + "/" + p, nil
}

// worktreeGitDir resolves the Git directory of this worktree without running
// Git: .git itself, or the directory a linked worktree's .git file points at.
// HEAD lives here, per worktree.
func worktreeGitDir(root string) (string, error) {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return "", fmt.Errorf("%s is not a Git checkout", root)
	}
	dir := gitPath
	if info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("%s must not be a symlink", gitPath)
	}
	if info.Mode().IsRegular() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		target, ok := strings.CutPrefix(line, "gitdir: ")
		if !ok {
			return "", fmt.Errorf("%s is not a worktree pointer", gitPath)
		}
		dir = target
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
	}
	return dir, nil
}

// gitDir resolves the directory holding a checkout's shared Git metadata,
// such as its configuration: the worktree's Git directory, or the commondir
// a linked worktree names.
func gitDir(root string) (string, error) {
	dir, err := worktreeGitDir(root)
	if err != nil {
		return "", err
	}
	common, err := os.ReadFile(filepath.Join(dir, "commondir"))
	switch {
	case err == nil:
		value := strings.TrimSpace(string(common))
		if value == "" {
			return "", fmt.Errorf("%s: commondir is empty", dir)
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(dir, value)
		}
		return value, nil
	case errors.Is(err, fs.ErrNotExist):
		return dir, nil
	default:
		return "", fmt.Errorf("read %s: %w", filepath.Join(dir, "commondir"), err)
	}
}

// CheckoutOrigin reads remote.origin.url from the checkout's local Git
// configuration without running Git. It follows a worktree .git file and
// rejects configuration that uses include directives.
func CheckoutOrigin(root string) (string, error) {
	dir, err := gitDir(root)
	if err != nil {
		return "", err
	}
	configPath := filepath.Join(dir, "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", configPath, err)
	}
	origin, err := ParseOriginURL(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", configPath, err)
	}
	return origin, nil
}

// CheckoutBranch reads the checkout's attached local branch without running
// Git. A detached HEAD or a foreign HEAD line is an error.
func CheckoutBranch(root string) (string, error) {
	dir, err := worktreeGitDir(root)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Join(dir, "HEAD"), err)
	}
	line := strings.TrimSpace(string(data))
	if branch, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok && branch != "" {
		return branch, nil
	}
	return "", fmt.Errorf("%s is not an attached local branch", root)
}

// ParseOriginURL extracts remote.origin.url from Git configuration text.
// Include directives and multiline values are not supported.
func ParseOriginURL(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	section, subsection := "", ""
	origin := ""
	foundOrigin := false
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			var err error
			section, subsection, err = parseSection(line)
			if err != nil {
				return "", err
			}
			if section == "include" || section == "includeif" {
				return "", fmt.Errorf("include directives are not supported; set remote.origin.url directly")
			}
			continue
		}
		key, rawValue, assigned := strings.Cut(raw, "=")
		if !assigned {
			rawValue = raw
		}
		value, err := parseGitValue(rawValue)
		if err != nil {
			return "", fmt.Errorf("parse Git configuration line %d: %w", lineNumber, err)
		}
		if section != "remote" || subsection != "origin" || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		if foundOrigin {
			return "", fmt.Errorf("remote.origin.url is set more than once")
		}
		if !assigned {
			value = ""
		}
		origin, foundOrigin = value, true
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("unreadable configuration: %w", err)
	}
	if origin == "" {
		return "", fmt.Errorf("remote.origin.url is not set")
	}
	return origin, nil
}

// parseGitValue decodes Git's quoted segments, escapes, whitespace, and
// comments while rejecting line continuations.
func parseGitValue(raw string) (string, error) {
	value := make([]byte, 0, len(raw))
	end := 0
	quoted, escaped := false, false
	for i := range len(raw) {
		char := raw[i]
		if escaped {
			switch char {
			case 'n':
				char = '\n'
			case 't':
				char = '\t'
			case 'b':
				char = '\b'
			case '\\', '"':
			default:
				return "", fmt.Errorf("invalid escape \\%c", char)
			}
			escaped = false
		} else {
			switch char {
			case '\\':
				escaped = true
				continue
			case '"':
				quoted = !quoted
				end = len(value)
				continue
			case '#', ';':
				if !quoted {
					return string(value[:end]), nil
				}
			case ' ', '\t', '\r', '\v', '\f':
				if !quoted {
					if len(value) > 0 {
						value = append(value, char)
					}
					continue
				}
			}
		}
		value = append(value, char)
		end = len(value)
	}
	if escaped {
		return "", fmt.Errorf("line continuations are not supported; write each value on one line")
	}
	if quoted {
		return "", fmt.Errorf("unterminated quoted value")
	}
	return string(value[:end]), nil
}

var sectionHeaderRe = regexp.MustCompile(`^\[([A-Za-z0-9.-]+)(?:[ \t]+"((?:[^"\\]|\\.)*)")?\][ \t]*(?:[#;].*)?$`)

// parseSection keeps comments and brackets inside quoted subsections intact.
// Section names are case-insensitive; subsection names are not.
func parseSection(header string) (string, string, error) {
	match := sectionHeaderRe.FindStringSubmatch(header)
	if match == nil {
		return "", "", fmt.Errorf("invalid Git configuration section %q", header)
	}
	if strings.Contains(match[1], ".") {
		return "", "", fmt.Errorf("legacy dotted Git configuration section %q is not supported; use a quoted subsection", header)
	}
	var sub strings.Builder
	for i := 0; i < len(match[2]); i++ {
		// Git removes the backslash before any escaped subsection byte.
		if match[2][i] == '\\' {
			i++
		}
		sub.WriteByte(match[2][i])
	}
	return strings.ToLower(match[1]), sub.String(), nil
}

// Verify checks that the checkout's origin matches the selector.
func Verify(s *Selector, checkoutRoot string) error {
	raw, err := CheckoutOrigin(checkoutRoot)
	if err != nil {
		return err
	}
	got, err := NormalizeOrigin(raw)
	if err != nil {
		return fmt.Errorf("checkout origin %q: %w", raw, err)
	}
	if got != s.Origin {
		return fmt.Errorf("checkout origin %q does not match the approved origin %q", got, s.Origin)
	}
	return nil
}

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

	"github.com/pelletier/go-toml/v2"
)

// CurrentSchema is the selector schema this engine reads.
const CurrentSchema = 1

// Selector is ~/.config/nimbus/config.toml.
type Selector struct {
	Schema   int    `toml:"schema"`
	Checkout string `toml:"checkout"`
	Machine  string `toml:"machine"`
	Origin   string `toml:"origin"`
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
	defer f.Close()
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
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("selector %s: unknown field: %s", path, strings.TrimSpace(strict.String()))
		}
		return nil, fmt.Errorf("selector %s: %w", path, err)
	}
	switch {
	case s.Schema != CurrentSchema:
		return nil, fmt.Errorf("selector %s: schema %d is not supported", path, s.Schema)
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
		i := strings.IndexByte(locator, '/')
		host, p = locator[:i], locator[i:]
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

// CheckoutOrigin reads remote.origin.url from the checkout's local Git
// configuration without running Git. It follows a worktree .git file and
// rejects configuration that uses include directives.
func CheckoutOrigin(root string) (string, error) {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return "", fmt.Errorf("%s is not a Git checkout", root)
	}
	gitDir := gitPath
	if info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("%s must not be a symlink", gitPath)
	}
	if info.Mode().IsRegular() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "gitdir: ") {
			return "", fmt.Errorf("%s is not a worktree pointer", gitPath)
		}
		gitDir = strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
	}
	configPath := filepath.Join(gitDir, "config")
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		dir := strings.TrimSpace(string(common))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(gitDir, dir)
		}
		configPath = filepath.Join(dir, "config")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", configPath, err)
	}
	origin, err := parseOriginURL(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", configPath, err)
	}
	return origin, nil
}

func parseOriginURL(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	section := ""
	origin := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			section = strings.ToLower(line)
			if strings.HasPrefix(section, "[include]") || strings.HasPrefix(section, "[includeif") {
				return "", fmt.Errorf("include directives are not supported; set remote.origin.url directly")
			}
			continue
		}
		if section != `[remote "origin"]` {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		if origin != "" {
			return "", fmt.Errorf("remote.origin.url is set more than once")
		}
		origin = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("unreadable configuration: %w", err)
	}
	if origin == "" {
		return "", fmt.Errorf("remote.origin.url is not set")
	}
	return origin, nil
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

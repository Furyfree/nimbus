package facts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Furyfree/nimbus/internal/selector"
)

// Paths the inspector reads.
const (
	OSReleasePath  = "/etc/os-release"
	RepoDir        = "/etc/yum.repos.d"
	SecureBootPath = "/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
	SELinuxPath    = "/sys/fs/selinux/enforce"
)

// Inspect collects every fact family. checkoutRoot may be empty when no
// checkout is selected; the checkout section then records that.
func Inspect(src Source, checkoutRoot string) *Facts {
	f := &Facts{Commands: map[string]string{}}
	for _, name := range RequiredCommands {
		if p, err := src.LookPath(name); err == nil {
			f.Commands[name] = p
		} else {
			f.Commands[name] = ""
		}
	}
	f.Platform = collect(func() (Platform, error) { return platform(src) })
	f.Packages = collect(func() ([]Package, error) { return packages(src) })
	f.Repositories = collect(func() ([]Repository, error) { return repositories(src) })
	f.Flatpak = collect(func() (Flatpak, error) { return flatpak(src) })
	f.SecureBoot = collect(func() (string, error) { return secureBoot(src) })
	f.SELinux = collect(func() (string, error) { return selinux(src) })
	f.Firewalld = collect(func() (string, error) { return firewalld(src) })
	f.Checkout = collect(func() (Checkout, error) { return checkout(src, checkoutRoot) })
	return f
}

func collect[T any](fn func() (T, error)) Section[T] {
	v, err := fn()
	if err != nil {
		var zero T
		return Section[T]{Value: zero, Error: err.Error()}
	}
	return Section[T]{Value: v}
}

func platform(src Source) (Platform, error) {
	data, err := src.ReadFile(OSReleasePath)
	if err != nil {
		return Platform{}, err
	}
	values := parseOSRelease(data)
	arch, err := src.Run("uname", "-m")
	if err != nil {
		return Platform{}, err
	}
	p := Platform{ID: values["ID"], VersionID: values["VERSION_ID"], PrettyName: values["PRETTY_NAME"], Arch: strings.TrimSpace(string(arch))}
	if p.ID == "" || p.VersionID == "" {
		return Platform{}, errors.New(OSReleasePath + " lacks ID or VERSION_ID")
	}
	return p, nil
}

// PackageQueryArgs are the DNF5 arguments for the installed-package query.
// --cacheonly keeps it off the network; the installed set needs no metadata.
var PackageQueryArgs = []string{"-q", "--cacheonly", "repoquery", "--installed", "--qf", PackageQueryFormat}

func packages(src Source) ([]Package, error) {
	out, err := src.Run("dnf5", PackageQueryArgs...)
	if err != nil {
		return nil, err
	}
	return parsePackages(out)
}

func repositories(src Source) ([]Repository, error) {
	names, err := src.ReadDir(RepoDir)
	if err != nil {
		return nil, err
	}
	var repos []Repository
	for _, name := range names {
		if !strings.HasSuffix(name, ".repo") {
			continue
		}
		data, err := src.ReadFile(filepath.Join(RepoDir, name))
		if err != nil {
			return nil, err
		}
		repos = append(repos, parseRepoFile(name, data)...)
	}
	return repos, nil
}

func flatpak(src Source) (Flatpak, error) {
	var f Flatpak
	out, err := src.Run("flatpak", "remotes", "--system", "--columns=name,url")
	if err != nil {
		return f, err
	}
	rows, err := parseColumns(out, 2)
	if err != nil {
		return f, err
	}
	for _, r := range rows {
		f.Remotes = append(f.Remotes, FlatpakRemote{Name: r[0], URL: r[1]})
	}
	out, err = src.Run("flatpak", "list", "--system", "--app", "--columns=application,version,origin")
	if err != nil {
		return f, err
	}
	rows, err = parseColumns(out, 3)
	if err != nil {
		return f, err
	}
	for _, r := range rows {
		f.Apps = append(f.Apps, FlatpakApp{ID: r[0], Version: r[1], Origin: r[2]})
	}
	return f, nil
}

// secureBoot reads the EFI variable: four bytes of attributes then the
// value byte.
func secureBoot(src Source) (string, error) {
	data, err := src.ReadFile(SecureBootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SecureBootUnavailable, nil
		}
		return "", err
	}
	if len(data) < 5 {
		return "", errors.New(SecureBootPath + " is shorter than five bytes")
	}
	if data[4] == 1 {
		return SecureBootEnabled, nil
	}
	return SecureBootDisabled, nil
}

func selinux(src Source) (string, error) {
	data, err := src.ReadFile(SELinuxPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SELinuxDisabled, nil
		}
		return "", err
	}
	switch strings.TrimSpace(string(data)) {
	case "1":
		return SELinuxEnforcing, nil
	case "0":
		return SELinuxPermissive, nil
	}
	return "", errors.New(SELinuxPath + " holds an unexpected value")
}

// firewalld returns systemctl's unit state, such as active or inactive.
// systemctl exits non-zero for every state but active, so the output is
// read even when the command reports failure.
func firewalld(src Source) (string, error) {
	out, err := src.Run("systemctl", "is-active", "firewalld")
	state := strings.TrimSpace(string(out))
	if state == "" {
		if err != nil {
			return "", err
		}
		return "", errors.New("systemctl reported no state")
	}
	return state, nil
}

// GitArgs prefixes every Git invocation. --no-optional-locks stops status
// from refreshing and rewriting the index, so inspection never mutates the
// checkout.
func GitArgs(root string, args ...string) []string {
	return append([]string{"--no-optional-locks", "-C", root}, args...)
}

func checkout(src Source, root string) (Checkout, error) {
	if root == "" {
		return Checkout{}, errors.New("no checkout selected")
	}
	origin, err := checkoutOrigin(src, root)
	if err != nil {
		return Checkout{}, err
	}
	normalized, err := selector.NormalizeOrigin(origin)
	if err != nil {
		return Checkout{}, err
	}
	commit, err := src.Run("git", GitArgs(root, "rev-parse", "HEAD")...)
	if err != nil {
		return Checkout{}, err
	}
	status, err := src.Run("git", GitArgs(root, "status", "--porcelain")...)
	if err != nil {
		return Checkout{}, err
	}
	return Checkout{Root: root, Origin: normalized, Commit: strings.TrimSpace(string(commit)), Dirty: strings.TrimSpace(string(status)) != ""}, nil
}

// checkoutOrigin reads remote.origin.url through the source, following a
// worktree pointer and its commondir the way selector.CheckoutOrigin does.
func checkoutOrigin(src Source, root string) (string, error) {
	gitPath := filepath.Join(root, ".git")
	gitDir := gitPath
	data, err := src.ReadFile(gitPath)
	switch {
	case err == nil:
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "gitdir: ") {
			return "", fmt.Errorf("%s is not a worktree pointer", gitPath)
		}
		gitDir = strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
	case IsDirectoryError(err):
	case errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("%s is not a Git checkout", root)
	default:
		return "", err
	}
	configPath := filepath.Join(gitDir, "config")
	common, err := src.ReadFile(filepath.Join(gitDir, "commondir"))
	switch {
	case err == nil:
		dir := strings.TrimSpace(string(common))
		if dir == "" {
			return "", fmt.Errorf("%s: commondir is empty", gitDir)
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(gitDir, dir)
		}
		configPath = filepath.Join(dir, "config")
	case errors.Is(err, os.ErrNotExist):
	default:
		return "", fmt.Errorf("read %s: %w", filepath.Join(gitDir, "commondir"), err)
	}
	config, err := src.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", configPath, err)
	}
	origin, err := selector.ParseOriginURL(config)
	if err != nil {
		return "", fmt.Errorf("%s: %w", configPath, err)
	}
	return origin, nil
}

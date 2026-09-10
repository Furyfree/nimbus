package snapper

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

type Storage struct {
	Identity   string `json:"identity"`
	Filesystem string `json:"filesystem"`
	Subvolume  string `json:"subvolume"`
}

func sameStorage(a, b *Storage) bool {
	return a == b || a != nil && b != nil && *a == *b
}

// Only an empty, separately mounted subvolume from the root filesystem can
// be adopted. Mount identity and permissions participate in plan approval.
func inspectStorage(src native.Source, dir inspect.Directory) (*Storage, error) {
	mode, err := strconv.ParseUint(dir.Mode, 8, 32)
	if err != nil || dir.Owner != "root" || dir.Group != "root" || mode&0022 != 0 {
		return nil, errors.New("snapshot storage must be root-owned and not writable by others")
	}
	entries, err := src.ReadDir("/.snapshots")
	if err != nil {
		return nil, fmt.Errorf("cannot establish that snapshot storage is empty: %w", err)
	}
	if len(entries) != 0 {
		return nil, errors.New("snapshot storage is not empty; migrate it explicitly, preserving its contents")
	}
	var mounts []string
	for _, target := range []string{"/", "/.snapshots"} {
		out, err := src.Run("findmnt", "--raw", "--noheadings", "--output", "FSTYPE,UUID,FSROOT,OPTIONS,ID", "--mountpoint", target)
		if err != nil {
			return nil, fmt.Errorf("inspect exact mount %s: %w", target, err)
		}
		fields := strings.Fields(string(out))
		if len(fields) != 5 || fields[0] != "btrfs" || !slices.Contains(strings.Split(fields[3], ","), "rw") {
			return nil, fmt.Errorf("%s must be one writable Btrfs mount with a filesystem UUID", target)
		}
		mounts = append(mounts, strings.Join(fields, " "))
	}
	root, storage := strings.Fields(mounts[0]), strings.Fields(mounts[1])
	if root[1] != storage[1] || root[2] == storage[2] || storage[2] == "/" {
		return nil, errors.New("snapshot storage must be a separate subvolume on the root filesystem")
	}
	out, err := src.Run("stat", "--format=%i", "--", "/.snapshots")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(out)) != "256" {
		return nil, errors.New("snapshot mount must point to a Btrfs subvolume root")
	}
	return &Storage{
		Identity:   fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(mounts, "\n")+"\n"+dir.Mode))),
		Filesystem: storage[1], Subvolume: storage[2],
	}, nil
}

func (p *Plan) adoptionDigest() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(p.Reuse.Identity+"\n"+strings.Join(p.Settings, "\n"))))
}

// Adopt registers existing storage without changing its mount or contents.
// The privileged caller supplies the existing atomic, ownership-checked
// system-file writer. Interrupted registration can be retried with the exact
// marked configuration; foreign or changed files are never adopted this way.
func Adopt(src native.Source, expected string, write func(string, inspect.SystemFile, inspect.SystemFile) error) error {
	template, err := inspect.ObserveFile(src, Template)
	if err != nil {
		return err
	}
	mode, err := strconv.ParseUint(template.Mode, 8, 32)
	if err != nil || !template.Exists || template.Owner != "root" || mode&0022 != 0 {
		return errors.New("Snapper template must be root-owned and not writable by others")
	}
	p, err := Inspect(src, template.Content)
	if err != nil {
		return err
	}
	if !p.Setup || expected == "" || p.Reuse == nil || p.adoptionDigest() != expected {
		return errors.New("Snapper storage changed since approval; run sync again")
	}
	registration, err := inspect.ObserveFile(src, registry)
	if err != nil {
		return err
	}
	if registration.Exists {
		mode, err := strconv.ParseUint(registration.Mode, 8, 32)
		if err != nil || registration.Owner != "root" || mode&0022 != 0 {
			return errors.New("Snapper registration must be root-owned and not writable by others")
		}
	}
	values, err := settings(registration.Content)
	if err != nil {
		return err
	}
	names := strings.Fields(values["SNAPPER_CONFIGS"])
	if !slices.Contains(names, "nimbus") {
		names = append(names, "nimbus")
	}
	line := "SNAPPER_CONFIGS=" + strconv.Quote(strings.Join(names, " "))
	lines := strings.Split(string(registration.Content), "\n")
	found := false
	for i, old := range lines {
		if fields := assignment.FindStringSubmatch(old); fields != nil && fields[1] == "SNAPPER_CONFIGS" {
			lines[i], found = line, true
		}
	}
	if !found {
		lines = append(lines, line)
	}
	config, err := inspect.ObserveFile(src, Config)
	if err != nil {
		return err
	}
	after := inspect.SystemFile{Exists: true, Owner: "root", Group: "root", Mode: "0644", Content: append(slices.Clone(template.Content), pending...)}
	if config.Exists && string(config.Content) != string(after.Content) {
		return errors.New("Snapper adoption configuration changed; preserve it and inspect it explicitly")
	}
	if !config.Exists {
		if err := write(Config, config, after); err != nil {
			return err
		}
	}
	after.Content = []byte(strings.Join(lines, "\n"))
	if registration.Exists {
		after.Owner, after.Group, after.Mode = registration.Owner, registration.Group, registration.Mode
	}
	if !registration.Exists || string(registration.Content) != string(after.Content) {
		if err := write(registry, registration, after); err != nil {
			return fmt.Errorf("register adopted Snapper config; retry sync to finish: %w", err)
		}
	}
	// Use the library directly so a running daemon cannot cache the old config
	// list. Its constructor also applies the native Snapper SELinux contexts.
	if _, err := src.Run("snapper", "--no-dbus", "--config", "nimbus", "list"); err != nil {
		return fmt.Errorf("verify adopted Snapper storage: %w", err)
	}
	config, err = inspect.ObserveFile(src, Config)
	if err != nil {
		return err
	}
	if string(config.Content) != string(template.Content)+pending {
		return errors.New("Snapper adoption configuration changed during verification")
	}
	after = config
	after.Content = slices.Clone(template.Content)
	return write(Config, config, after)
}

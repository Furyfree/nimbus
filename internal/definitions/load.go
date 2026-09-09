package definitions

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// RootFile is the checkout root definition file.
const RootFile = "nimbus.toml"

// DefinitionDirs are the directories inside the definition boundary.
var DefinitionDirs = []string{"machines", "profiles", "components", "system"}

// Checkout is a loaded definition tree.
type Checkout struct {
	// Root is the canonical checkout directory after symlink resolution.
	Root       string
	Root_      Root
	Machines   map[string]*Machine
	Profiles   map[string]*Profile
	Components map[string]*Component
	// Entries are every regular file in the definition boundary, sorted by
	// path. They feed the digest and the system-file source checks.
	Entries []Entry
}

// Definitions returns the parsed nimbus.toml.
func (c *Checkout) Definitions() Root { return c.Root_ }

// Entry returns the boundary entry at a checkout-relative path.
func (c *Checkout) Entry(path string) (Entry, bool) {
	i := sort.Search(len(c.Entries), func(i int) bool { return c.Entries[i].Path >= path })
	if i < len(c.Entries) && c.Entries[i].Path == path {
		return c.Entries[i], true
	}
	return Entry{}, false
}

// Digest returns the canonical definition digest.
func (c *Checkout) Digest() string { return Digest(c.Entries) }

// Load reads the definition tree below path. Structural problems are
// returned as an ErrorList so every one is reported at once.
func Load(path string) (*Checkout, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve checkout %s: %w", path, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("checkout %s is not a directory", root)
	}

	c := &Checkout{
		Root:       root,
		Machines:   map[string]*Machine{},
		Profiles:   map[string]*Profile{},
		Components: map[string]*Component{},
	}
	var errs ErrorList

	content, mode, err := readRegular(root, RootFile)
	if err != nil {
		errs.Add(RootFile, "%v", err)
	} else {
		c.Entries = append(c.Entries, Entry{Path: RootFile, Mode: mode, Content: content})
		if err := decodeStrict(content, &c.Root_); err != nil {
			errs.Add(RootFile, "%v", err)
		}
	}

	for _, dir := range DefinitionDirs {
		entries, walkErrs := walkBoundary(root, dir)
		errs = append(errs, walkErrs...)
		c.Entries = append(c.Entries, entries...)
	}
	slices.SortFunc(c.Entries, func(a, b Entry) int { return cmp.Compare(a.Path, b.Path) })

	for _, e := range c.Entries {
		dir, base := filepath.Split(e.Path)
		dir = strings.TrimSuffix(dir, "/")
		switch dir {
		case "machines":
			m := &Machine{}
			if err := decodeDefinition(e, base, m); err != nil {
				errs.Add(e.Path, "%v", err)
				continue
			}
			c.Machines[m.ID] = m
		case "profiles":
			p := &Profile{}
			if err := decodeDefinition(e, base, p); err != nil {
				errs.Add(e.Path, "%v", err)
				continue
			}
			c.Profiles[p.ID] = p
		case "components":
			comp := &Component{}
			if err := decodeDefinition(e, base, comp); err != nil {
				errs.Add(e.Path, "%v", err)
				continue
			}
			c.Components[comp.ID] = comp
		default:
			if strings.HasPrefix(e.Path, "machines/") || strings.HasPrefix(e.Path, "profiles/") || strings.HasPrefix(e.Path, "components/") {
				errs.Add(e.Path, "unexpected file: definitions live directly in their directory")
			}
		}
	}

	if len(errs) > 0 {
		return c, errs
	}
	return c, nil
}

type identified interface{ id() string }

func (m *Machine) id() string   { return m.ID }
func (p *Profile) id() string   { return p.ID }
func (c *Component) id() string { return c.ID }

func decodeDefinition(e Entry, base string, v identified) error {
	want, ok := strings.CutSuffix(base, ".toml")
	if !ok {
		return fmt.Errorf("unexpected file: definitions are .toml files")
	}
	if err := decodeStrict(e.Content, v); err != nil {
		return err
	}
	if v.id() != want {
		return fmt.Errorf("id %q does not match filename %q", v.id(), want)
	}
	return nil
}

func decodeStrict(data []byte, v any) error {
	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	err := d.Decode(v)
	if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
		return fmt.Errorf("unknown field: %s", strings.TrimSpace(strict.String()))
	}
	if dec, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, col := dec.Position()
		return fmt.Errorf("line %d column %d: %s", row, col, dec.Error())
	}
	return err
}

// entryMode normalizes a file mode to Git's executable state.
func entryMode(m fs.FileMode) uint32 {
	if m&0o100 != 0 {
		return 0o100755
	}
	return 0o100644
}

func readRegular(root, rel string) ([]byte, uint32, error) {
	full := filepath.Join(root, rel)
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, fmt.Errorf("missing")
		}
		return nil, 0, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("must be a regular file")
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, 0, err
	}
	return content, entryMode(info.Mode()), nil
}

func walkBoundary(root, dir string) ([]Entry, ErrorList) {
	var entries []Entry
	var errs ErrorList
	base := filepath.Join(root, dir)
	info, err := os.Lstat(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			errs.Add(dir, "missing directory")
		} else {
			errs.Add(dir, "%v", err)
		}
		return nil, errs
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		errs.Add(dir, "must be a directory, not a symlink or file")
		return nil, errs
	}
	walkErr := filepath.WalkDir(base, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			errs.Add(rel, "symlinks are not allowed inside the definition boundary")
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			errs.Add(rel, "special files are not allowed inside the definition boundary")
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		// The directory entry type can be unknown on some filesystems, so
		// the authoritative mode is checked again before reading.
		if fi.Mode()&fs.ModeSymlink != 0 {
			errs.Add(rel, "symlinks are not allowed inside the definition boundary")
			return nil
		}
		if !fi.Mode().IsRegular() {
			errs.Add(rel, "special files are not allowed inside the definition boundary")
			return nil
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		entries = append(entries, Entry{Path: rel, Mode: entryMode(fi.Mode()), Content: content})
		return nil
	})
	if walkErr != nil {
		errs.Add(dir, "%v", walkErr)
	}
	return entries, errs
}

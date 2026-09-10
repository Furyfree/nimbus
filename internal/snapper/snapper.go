// Package snapper uses native Snapper configuration, snapshots and cleanup.
package snapper

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

const (
	Template = "/etc/snapper/config-templates/nimbus"
	Config   = "/etc/snapper/configs/nimbus"
	marker   = "# Managed by Nimbus: root snapshots.\n"
	pending  = "# Nimbus snapshot storage adoption is incomplete.\n"
	registry = "/etc/sysconfig/snapper"
)

// Plan includes configuration facts so approval detects changes to them.
type Plan struct {
	Reuse    *Storage          `json:"reuse,omitempty"`
	Setup    bool              `json:"setup,omitzero"`
	Settings []string          `json:"settings"`
	Current  map[string]string `json:"current,omitempty"`
	Blocked  string            `json:"blocked,omitempty"`
}

func (p *Plan) Changes() []string {
	var changes []string
	for _, setting := range p.Settings {
		key, value, _ := strings.Cut(setting, "=")
		if current, ok := p.Current[key]; !ok || current != value {
			changes = append(changes, setting)
		}
	}
	return changes
}

// Inspect is read-only, including before Snapper has been installed.
func Inspect(src native.Source, template []byte) (*Plan, error) {
	want, err := settings(template)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(template), marker) || want["SUBVOLUME"] != "/" || want["FSTYPE"] != "btrfs" {
		return nil, errors.New("Snapper template must identify Nimbus root snapshots on Btrfs")
	}
	p := &Plan{}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if key != "SUBVOLUME" && key != "FSTYPE" {
			p.Settings = append(p.Settings, key+"="+want[key])
		}
	}
	file, err := inspect.ObserveFile(src, Config)
	if err != nil {
		return nil, err
	}
	p.Setup = !file.Exists
	if file.Exists {
		dir, err := inspect.ObserveDirectory(src, "/etc/snapper/configs")
		if err != nil {
			return nil, err
		}
		mode, modeErr := strconv.ParseUint(file.Mode, 8, 32)
		dirMode, dirErr := strconv.ParseUint(dir.Mode, 8, 32)
		if modeErr != nil || dirErr != nil || mode&0022 != 0 || dirMode&0022 != 0 || dir.Owner != "root" || file.Owner != "root" || !strings.HasPrefix(string(file.Content), marker) {
			return nil, errors.New("existing Snapper nimbus configuration is not Nimbus-owned; migrate it explicitly")
		}
		p.Current, err = settings(file.Content)
		if err != nil {
			return nil, err
		}
		if p.Current["SUBVOLUME"] != "/" || p.Current["FSTYPE"] != "btrfs" {
			return nil, errors.New("Snapper nimbus configuration no longer covers the Btrfs root; repair it explicitly")
		}
	}
	data, err := src.ReadFile(registry)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	registered, err := settings(data)
	if err != nil {
		return nil, err
	}
	names := strings.Fields(registered["SNAPPER_CONFIGS"])
	// A stopped adoption may have written its config or registration before
	// native verification finished. Only the exact marked file can resume.
	resuming := file.Exists && string(file.Content) == string(template)+pending
	if file.Exists && strings.Contains(string(file.Content), pending) && !resuming {
		return nil, errors.New("unfinished Snapper adoption differs from the template; resolve it explicitly before retrying")
	}
	if resuming {
		p.Setup = true
	}
	if !resuming && slices.Contains(names, "nimbus") == p.Setup {
		return nil, errors.New("Snapper nimbus registration and configuration disagree; repair them with native Snapper tools")
	}
	if p.Setup {
		entries, err := src.ReadDir("/etc/snapper/configs")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		configs := append(slices.Clone(names), entries...)
		slices.Sort(configs)
		for _, name := range slices.Compact(configs) {
			if name == "nimbus" && file.Exists {
				continue
			}
			if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
				return nil, errors.New("unsupported Snapper configuration name")
			}
			data, err := src.ReadFile("/etc/snapper/configs/" + name)
			if err != nil {
				return nil, err
			}
			config, err := settings(data)
			if err != nil {
				return nil, err
			}
			if config["SUBVOLUME"] == "/" {
				return nil, fmt.Errorf("Snapper configuration %q already covers root; migrate it explicitly, preserving its snapshots", name)
			}
		}
		dir, err := inspect.ObserveDirectory(src, "/.snapshots")
		if err != nil {
			return nil, err
		}
		if dir.Exists {
			p.Reuse, err = inspectStorage(src, dir)
			if err != nil {
				return nil, fmt.Errorf("/.snapshots already exists: %w", err)
			}
		} else if file.Exists {
			return nil, errors.New("unfinished Snapper adoption lost its snapshot mount; restore it before retrying")
		}
		out, err := src.Run("findmnt", "--noheadings", "--output", "FSTYPE", "--target", "/")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(out)) != "btrfs" {
			return nil, errors.New("the snapper component requires a Btrfs root filesystem")
		}
	}
	return p, nil
}

// Configure runs after approval. Fresh storage uses native create-config;
// existing empty storage uses the fixed registration helper.
func (p *Plan) Configure(src native.Source) error {
	if p.Setup {
		template, err := src.ReadFile(Template)
		if err != nil {
			return err
		}
		fresh, err := Inspect(src, template)
		if err != nil {
			return err
		}
		if !fresh.Setup || !sameStorage(fresh.Reuse, p.Reuse) || !slices.Equal(fresh.Settings, p.Settings) {
			return errors.New("Snapper setup changed since approval; run sync again")
		}
		if p.Reuse != nil {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			_, err = src.Run("sudo", "--", exe, "internal", "snapper-adopt", "--expected", p.adoptionDigest())
			return err
		}
		if _, err := run(src, "create-config", "--fstype", "btrfs", "--template", "nimbus", "/"); err != nil {
			return err
		}
	} else if changes := p.Changes(); len(changes) > 0 {
		if _, err := run(src, append([]string{"set-config"}, changes...)...); err != nil {
			return err
		}
	}
	return nil
}

func Create(src native.Source, phase, pre string) (string, error) {
	args := []string{"create", "--type", phase, "--print-number", "--cleanup-algorithm", "number", "--description", "Nimbus system changes"}
	if pre != "" {
		args = append(args, "--pre-number", pre)
	}
	out, err := run(src, args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	number, err := strconv.ParseUint(id, 10, 64)
	if err != nil || number == 0 {
		return "", fmt.Errorf("Snapper returned an invalid snapshot number %q", id)
	}
	return id, nil
}

func Cleanup(src native.Source) error {
	_, err := run(src, "cleanup", "number")
	return err
}

func run(src native.Source, args ...string) ([]byte, error) {
	return src.Run("sudo", append([]string{"--", "snapper", "--no-dbus", "--config", "nimbus"}, args...)...)
}

// Snapper sysconfig files contain quoted or bare assignments, not shell code.
var assignment = regexp.MustCompile(`^\s*([0-9A-Z_]+)=(?:"([^"]*)"|'([^']*)'|([^ \t]*))[ \t]*(?:#.*)?$`)

func settings(data []byte) (map[string]string, error) {
	values := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := assignment.FindStringSubmatch(line)
		if parts == nil {
			return nil, fmt.Errorf("invalid Snapper configuration line %q", line)
		}
		if _, exists := values[parts[1]]; exists {
			return nil, fmt.Errorf("duplicate Snapper setting %s", parts[1])
		}
		values[parts[1]] = parts[2] + parts[3] + parts[4]
	}
	return values, nil
}

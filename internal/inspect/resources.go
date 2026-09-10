package inspect

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// SystemFile is a regular, single-link file observed without following symlinks.
type SystemFile struct {
	Exists  bool   `json:"exists"`
	Content []byte `json:"content,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Group   string `json:"group,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// ObserveFile checks every existing path component before reading the target.
// Missing paths are established through a readable parent directory, never by
// treating an arbitrary stat failure as absence.
func ObserveFile(src native.Source, target string) (SystemFile, error) {
	var result SystemFile
	rel, absolute := strings.CutPrefix(target, "/")
	if !absolute || path.Clean(target) != target || target == "/" {
		return result, fmt.Errorf("invalid absolute file target %q", target)
	}
	current := "/"
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		names, err := src.ReadDir(current)
		if err != nil {
			return result, fmt.Errorf("inspect parent %s: %w", current, err)
		}
		if !slices.Contains(names, part) {
			return result, nil
		}
		current = path.Join(current, part)
		out, err := src.Run("stat", "--format=%F|%U|%G|%a|%h", "--", current)
		if err != nil {
			return result, fmt.Errorf("inspect %s: %w", current, err)
		}
		fields := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(fields) != 5 {
			return result, fmt.Errorf("invalid stat output for %s", current)
		}
		if i < len(parts)-1 {
			if fields[0] != "directory" {
				return result, fmt.Errorf("%s is not a directory (symlinks are forbidden)", current)
			}
			continue
		}
		if (fields[0] != "regular file" && fields[0] != "regular empty file") || fields[4] != "1" {
			return result, fmt.Errorf("%s is not a single-link regular file", current)
		}
		mode, err := strconv.ParseUint(fields[3], 8, 32)
		if err != nil {
			return result, fmt.Errorf("invalid mode for %s", current)
		}
		data, err := src.ReadFile(current)
		if err != nil {
			return result, err
		}
		result = SystemFile{Exists: true, Content: data, Owner: fields[1], Group: fields[2], Mode: fmt.Sprintf("%04o", mode)}
	}
	return result, nil
}

type Service struct {
	Unit    string `json:"unit"`
	Load    string `json:"load"`
	Enabled string `json:"enabled"`
	Active  string `json:"active"`
}

func ObserveService(src native.Source, unit string) (Service, error) {
	s := Service{Unit: unit}
	out, err := src.Run("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)
	if err != nil {
		return s, err
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			s.Load = value
		case "UnitFileState":
			s.Enabled = value
		case "ActiveState":
			s.Active = value
		}
	}
	if s.Load == "" || s.Active == "" {
		return s, fmt.Errorf("incomplete systemctl state for %s", unit)
	}
	return s, nil
}

// ObserveMembership includes primary membership so it is never removed.
func ObserveMembership(src native.Source, user, group string) (bool, error) {
	out, err := src.Run("id", "-nG", "--", user)
	if err != nil {
		return false, err
	}
	return slices.Contains(strings.Fields(string(out)), group), nil
}

// Directory describes a real directory reached only through root-controlled
// directory ancestors. It is used by the fixed greeter state integration.
type Directory struct {
	Exists bool   `json:"exists"`
	Owner  string `json:"owner,omitempty"`
	Group  string `json:"group,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

func ObserveDirectory(src native.Source, target string) (Directory, error) {
	var result Directory
	rel, absolute := strings.CutPrefix(target, "/")
	if path.Clean(target) != target || !absolute || target == "/" {
		return result, fmt.Errorf("invalid directory target")
	}
	current := "/"
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		names, err := src.ReadDir(current)
		if err != nil {
			return result, err
		}
		if !slices.Contains(names, part) {
			return result, nil
		}
		current = path.Join(current, part)
		out, err := src.Run("stat", "--format=%F|%U|%G|%a|%h", "--", current)
		if err != nil {
			return result, err
		}
		fields := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(fields) != 5 || fields[0] != "directory" {
			return result, fmt.Errorf("%s is not a real directory", current)
		}
		mode, err := strconv.ParseUint(fields[3], 8, 32)
		if err != nil {
			return result, err
		}
		if i < len(parts)-1 {
			if fields[1] != "root" || mode&0022 != 0 {
				return result, fmt.Errorf("directory ancestor %s is not root-controlled", current)
			}
			continue
		}
		result = Directory{Exists: true, Owner: fields[1], Group: fields[2], Mode: fmt.Sprintf("%04o", mode)}
	}
	return result, nil
}

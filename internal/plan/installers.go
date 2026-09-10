package plan

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
)

// InstallerScript stands for the downloaded installer in a plan step.
// Apply writes the script to its stage directory and fills the path in.
const InstallerScript = "<installer script>"

// userTools plans installers as the user. They write no receipt:
// presence is the record.
func (b *builder) userTools() []Operation {
	var ops []Operation
	if !b.in.Facts.User.Known() {
		// The desired tools are not dropped silently: one blocked
		// operation says why they cannot be planned.
		if len(b.in.Resolved.Installers) > 0 {
			ops = append(ops, Operation{ID: "user:tools", Kind: KindUser, Action: ActionInstall, Risk: RiskLow,
				Summary: "plan the user-scope tools", Blocked: "the user-scope tool state is unknown: " + b.in.Facts.User.Error})
		}
		return ops
	}
	u := b.in.Facts.User.Value
	for _, in := range b.in.Resolved.Installers {
		id := "user:" + in.Component
		binary, err := b.userFile(u.Home, in.Installer.Binary)
		op := Operation{ID: id, Kind: KindUser, Risk: RiskLow, Paths: in.Paths}
		if binary {
			op.Action, op.Summary = ActionKeep, fmt.Sprintf("%s is installed at ~/%s", in.Component, in.Installer.Binary)
		} else {
			op.Action, op.Summary = ActionInstall, fmt.Sprintf("install %s from %s as the user", in.Component, in.Installer.URL)
			op.Steps = []Step{
				{Description: "download " + in.Installer.URL + " to the stage directory and show its sha256"},
				{Description: "run the installer as the user", Argv: []string{"sh", InstallerScript}},
				{Description: "verify ~/" + in.Installer.Binary + " exists"},
			}
		}
		if err != nil {
			op.Blocked = err.Error()
		}
		ops = append(ops, op)
	}
	return ops
}

// userFile reports whether a file relative to the home directory exists,
// read through the source so a test can say what the home holds.
func (b *builder) userFile(home, rel string) (bool, error) {
	path := filepath.Join(home, rel)
	names, err := b.in.Source.ReadDir(filepath.Dir(path))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect ~/%s: %w", rel, err)
	}
	return slices.Contains(names, filepath.Base(path)), nil
}

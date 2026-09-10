package apply

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func (ex *executor) flatpakRemote(op plan.Operation) ([]state.Receipt, []string, error) {
	id := strings.TrimPrefix(op.ID, "flatpak-remote:")
	r := ex.opts.Root.Repositories[id]
	data, err := ex.opts.Fetch(r.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("download remote definition: %w", err)
	}
	encoded := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "GPGKey="); ok {
			encoded = value
		}
	}
	if encoded == "" {
		return nil, nil, errors.New("the remote definition carries no GPGKey")
	}
	keyData, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("remote key: %w", err)
	}
	if _, err := ex.verifiedKey("remote-"+id+".gpg", keyData, r.Key); err != nil {
		return nil, nil, err
	}
	repoFile := filepath.Join(ex.opts.Stage, id+".flatpakrepo")
	if err := os.WriteFile(repoFile, data, 0o600); err != nil {
		return nil, nil, err
	}
	if err := ex.sudo("flatpak", "remote-add", "--if-not-exists", "--system", "--from", id, repoFile); err != nil {
		return nil, nil, err
	}
	f := &inspect.Facts{Flatpak: inspect.SystemFlatpak(ex.opts.Source)}
	if !f.Flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + f.Flatpak.Error)
	}
	i := slices.IndexFunc(f.Flatpak.Value.Remotes, func(remote inspect.FlatpakRemote) bool { return remote.Name == id })
	if i < 0 {
		return nil, nil, fmt.Errorf("verification: remote %s is not present after the operation", id)
	}
	remote := f.Flatpak.Value.Remotes[i]
	if reason := plan.FlatpakKeyDrift(r, remote); reason != "" {
		return nil, nil, fmt.Errorf("verification: remote %s: %s", id, reason)
	}
	url := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "Url="); ok {
			url = strings.TrimSpace(value)
		}
	}
	if url == "" || strings.TrimSuffix(remote.URL, "/") != strings.TrimSuffix(url, "/") {
		return nil, nil, fmt.Errorf("verification: remote %s URL differs from the verified remote definition", id)
	}
	receipt := ex.receipt(op, "flatpak-remote", "absent", "present with key "+definitions.NormalizeFingerprint(r.Key), "remote URL, signature checking and installed key match")
	recordSourceOwnership(&receipt, op, []string{id}, f)
	return []state.Receipt{receipt}, nil, nil
}

func (ex *executor) flatpakApp(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Action == plan.ActionInstall {
		if err := ex.sudo(op.Steps[0].Argv...); err != nil {
			return nil, nil, err
		}
	}
	id := strings.TrimPrefix(op.ID, "flatpak:")
	flatpak := inspect.SystemFlatpak(ex.opts.Source)
	if !flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + flatpak.Error)
	}
	i := slices.IndexFunc(flatpak.Value.Apps, func(app inspect.FlatpakApp) bool { return app.ID == id })
	if i < 0 {
		return nil, nil, fmt.Errorf("verification: %s is not installed after the operation", id)
	}
	app := flatpak.Value.Apps[i]
	installed := "installed " + app.Version + " from " + app.Origin
	previous := "absent"
	if op.Action == plan.ActionAdopt {
		previous = installed
	}
	return []state.Receipt{ex.receipt(op, "flatpak", previous, installed, "flatpak list shows the application")}, nil, nil
}

func (ex *executor) flatpakRemove(op plan.Operation) ([]state.Receipt, []string, error) {
	if err := ex.sudo(op.Steps[0].Argv...); err != nil {
		return nil, nil, err
	}
	id := strings.TrimPrefix(op.ID, "flatpak:")
	flatpak := inspect.SystemFlatpak(ex.opts.Source)
	if !flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + flatpak.Error)
	}
	if slices.ContainsFunc(flatpak.Value.Apps, func(app inspect.FlatpakApp) bool { return app.ID == id }) {
		return nil, nil, fmt.Errorf("verification: %s is still installed", id)
	}
	return nil, []string{op.ID}, nil
}

package apply

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
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

// fingerprint runs gpg over a key file through Source and returns the
// primary fingerprint.
func (ex *executor) fingerprint(path string) (string, error) {
	keys, err := inspect.KeyFingerprints(ex.opts.Source, path)
	if err != nil {
		return "", fmt.Errorf("gpg: %w", err)
	}
	if len(keys) != 1 {
		return "", fmt.Errorf("key file has %d primary keys; exactly the declared key is required", len(keys))
	}
	return keys[0], nil
}

// verifiesKey distinguishes a complete key reconciliation from a repair
// limited to already verified repository options.
func verifiesKey(op plan.Operation) bool {
	return slices.ContainsFunc(op.Steps, func(step plan.Step) bool {
		return len(step.Argv) > 0 && step.Argv[0] == "gpg"
	})
}

// verifiedKey writes key material to the stage and checks its fingerprint.
func (ex *executor) verifiedKey(name string, data []byte, want string) (string, error) {
	path := filepath.Join(ex.opts.Stage, name)
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	got, err := ex.fingerprint(path)
	if err != nil {
		return "", err
	}
	if got != definitions.NormalizeFingerprint(want) {
		_ = os.Remove(path)
		return "", fmt.Errorf("key fingerprint %s does not match the declared %s", got, definitions.NormalizeFingerprint(want))
	}
	return path, nil
}

// dnfConfig writes, adopts, or removes the libdnf5 drop-in and verifies
// the file afterwards by reading it back.
func (ex *executor) dnfConfig(op plan.Operation) ([]state.Receipt, []string, error) {
	want := plan.DNFDropIn(ex.opts.Root, ex.opts.Constraints...)
	verifyContent := func() error {
		data, err := ex.opts.Source.ReadFile(inspect.DNFDropInPath)
		if err != nil {
			return fmt.Errorf("verification: %w", err)
		}
		if string(data) != want {
			return fmt.Errorf("verification: %s does not hold the rendered drop-in", inspect.DNFDropInPath)
		}
		return nil
	}
	switch op.Action {
	case plan.ActionAdopt:
		if err := verifyContent(); err != nil {
			return nil, nil, err
		}
		return []state.Receipt{ex.receipt(op, "dnf-config", "present as declared", "drop-in rendered from nimbus.toml [dnf]", "file content matches the rendered drop-in")}, nil, nil
	case plan.ActionRemove:
		if err := ex.sudo(op.Steps[0].Argv...); err != nil {
			return nil, nil, err
		}
		_, err := ex.opts.Source.ReadFile(inspect.DNFDropInPath)
		if err == nil {
			return nil, nil, fmt.Errorf("verification: %s still exists after removal", inspect.DNFDropInPath)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("verification: %w", err)
		}
		return nil, []string{op.ID}, nil
	}
	previous := "absent"
	if op.Action == plan.ActionRepair {
		previous = "present with other content"
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return nil, nil, err
	}
	staged := filepath.Join(ex.opts.Stage, "dnf-drop-in.conf")
	if err := os.WriteFile(staged, []byte(want), 0o600); err != nil {
		return nil, nil, err
	}
	for _, st := range op.Steps {
		if st.Argv == nil {
			continue
		}
		argv := slices.Clone(st.Argv)
		for i, a := range argv {
			if a == plan.DNFDropInPlaceholder {
				argv[i] = staged
			}
		}
		if err := ex.sudo(argv...); err != nil {
			return nil, nil, err
		}
	}
	if err := verifyContent(); err != nil {
		return nil, nil, err
	}
	return []state.Receipt{ex.receipt(op, "dnf-config", previous, "drop-in rendered from nimbus.toml [dnf]", "file content matches the rendered drop-in")}, nil, nil
}

func (ex *executor) repository(op plan.Operation) ([]state.Receipt, []string, error) {
	id := strings.TrimPrefix(op.ID, "repository:")
	r, ok := ex.opts.Root.Repositories[id]
	if !ok {
		return nil, nil, fmt.Errorf("repository %s is not declared", id)
	}
	var err error
	switch {
	case r.Kind == "copr":
		err = ex.enableCOPR(id, r, op)
	case r.ReleasePackage != "":
		err = ex.enableReleasePackage(id, r, op)
	default:
		err = ex.enableBaseURL(id, r, op)
	}
	if err != nil {
		return nil, nil, err
	}
	for _, st := range op.Steps {
		if st.Description == plan.DisableDuplicateDescription {
			if err := ex.sudo(st.Argv...); err != nil {
				return nil, nil, err
			}
		}
	}
	f := &inspect.Facts{Repositories: inspect.Repositories(ex.opts.Source)}
	if !f.Repositories.Known() {
		return nil, nil, errors.New("verification: repositories are unknown: " + f.Repositories.Error)
	}
	ready, repair, blocked := plan.CheckRepository(ex.opts.Root, id, f.Repositories.Value)
	if !ready {
		reason := cmp.Or(blocked, repair, "not enabled")
		return nil, nil, fmt.Errorf("verification: repository %s is not as declared after the operation: %s", id, reason)
	}
	receipt := ex.receipt(op, "repository", "absent", "enabled with key "+definitions.NormalizeFingerprint(r.Key), "repository enabled, signature checking on, local key fingerprint and priority as declared")
	recordSourceOwnership(&receipt, op, plan.DNFRepoIDs(id, r), f)
	return []state.Receipt{receipt}, nil, nil
}

func (ex *executor) enableBaseURL(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
		// The plan shows exactly which steps the repair needs: a rewrite
		// of the owned file, an override, or both; the duplicate override
		// runs afterwards with the other kinds.
		for _, st := range op.Steps {
			if st.Argv != nil && st.Description != plan.DisableDuplicateDescription {
				if err := ex.sudo(st.Argv...); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var data []byte
	var err error
	if r.KeyFile != "" {
		entry, ok := ex.opts.Checkout.Entry("system/" + r.KeyFile)
		if !ok {
			return fmt.Errorf("key file system/%s is missing from the checkout", r.KeyFile)
		}
		data = entry.Content
	} else {
		data, err = ex.opts.Fetch(r.KeyURL)
		if err != nil {
			return fmt.Errorf("download key: %w", err)
		}
	}
	key, err := ex.verifiedKey("key-"+id+".asc", data, r.Key)
	if err != nil {
		return err
	}
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo(plan.AddRepoStep(id, r, op.Action == plan.ActionRepair).Argv...); err != nil {
		return err
	}
	if op.Action == plan.ActionRepair {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	return nil
}

func (ex *executor) enableReleasePackage(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	data, err := ex.opts.Fetch(r.ReleasePackage)
	if err != nil {
		return fmt.Errorf("download release package: %w", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != r.SHA256 {
		return fmt.Errorf("release package sha256 %s does not match the declared %s", got, r.SHA256)
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return err
	}
	rpm := filepath.Join(ex.opts.Stage, id+"-release.rpm")
	if err := os.WriteFile(rpm, data, 0o600); err != nil {
		return err
	}
	keys, err := ex.opts.Keys(rpm)
	if err != nil {
		return fmt.Errorf("extract keys: %w", err)
	}
	var key string
	var keyErrors []error
	for name, content := range keys {
		if path, err := ex.verifiedKey("key-"+id+"-"+filepath.Base(name), content, r.Key); err == nil {
			key = path
			break
		} else {
			keyErrors = append(keyErrors, fmt.Errorf("key %s: %w", name, err))
		}
	}
	if key == "" {
		return errors.Join(fmt.Errorf("no key in %s has the declared fingerprint %s", r.ReleasePackage, definitions.NormalizeFingerprint(r.Key)), errors.Join(keyErrors...))
	}
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	if op.Action != plan.ActionRepair {
		if _, err := ex.packageTransaction([]string{"dnf5", "install", "-y", rpm}, nil); err != nil {
			return err
		}
	}
	return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
}

func (ex *executor) enableCOPR(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	data, err := ex.opts.Fetch("https://download.copr.fedorainfracloud.org/results/" + r.Project + "/pubkey.gpg")
	if err != nil {
		return fmt.Errorf("download COPR key: %w", err)
	}
	key, err := ex.verifiedKey("key-"+id+".gpg", data, r.Key)
	if err != nil {
		return err
	}
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	if op.Action != plan.ActionRepair {
		if err := ex.sudo("dnf5", "copr", "enable", "-y", r.Project); err != nil {
			return err
		}
	}
	return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
}

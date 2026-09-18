package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// plymouthThemeTrigger selects the Paper Dark theme through the native tool
// and records the previous selection so removal restores what the install
// replaced. The reviewed plan carries the exact command for both directions.
func (ex *executor) plymouthThemeTrigger(op plan.Operation) ([]state.Receipt, []string, error) {
	if len(op.Steps) != 1 || len(op.Steps[0].Argv) != 3 ||
		op.Steps[0].Argv[0] != "plymouth-set-default-theme" || op.Steps[0].Argv[1] != "-R" {
		return nil, nil, fmt.Errorf("Plymouth trigger plan is missing its reviewed command")
	}
	argv := op.Steps[0].Argv
	intended := argv[2]
	before, err := ex.opts.Source.Run("plymouth-set-default-theme")
	if err != nil {
		return nil, nil, fmt.Errorf("read the current Plymouth theme: %w", err)
	}
	previous := strings.TrimSpace(string(before))
	if previous == intended && op.Resource != nil {
		if recorded := strings.TrimSpace(op.Resource.Previous); recorded != "" {
			// The theme was already selected by an earlier run; keep the
			// selection observed before Nimbus took over.
			previous = recorded
		}
	}
	if err := ex.sudo(argv...); err != nil {
		return nil, nil, err
	}
	after, err := ex.opts.Source.Run("plymouth-set-default-theme")
	if err != nil {
		return nil, nil, fmt.Errorf("verify the Plymouth theme: %w", err)
	}
	if observed := strings.TrimSpace(string(after)); observed != intended {
		return nil, nil, fmt.Errorf("Plymouth theme verification failed: expected %s, observed %s", intended, observed)
	}
	rebuilt, err := ex.rebuildFDEUKI()
	if err != nil {
		return nil, nil, err
	}
	if op.Action == plan.ActionRemove {
		// The install receipt carries the previous theme needed to replan a
		// failed removal; a successful removal retires it with the other
		// removal receipts at the end of the run.
		return nil, []string{op.ID}, nil
	}
	verification := "native Plymouth theme selection verified after the command"
	if rebuilt {
		verification += "; signed Nimbus image rebuilt for the running kernel"
	}
	return []state.Receipt{ex.receipt(op, plan.KindTrigger, previous, intended, verification)}, nil, nil
}

// rebuildFDEUKI refreshes the signed image after Plymouth regenerated the
// initramfs, when the approved marker is present with its known content. The
// Plymouth trigger's plan note discloses the rebuild before approval. The
// rebuild is skipped when the image already targets a different kernel, so a
// pending kernel update is left to the kernel-install hook.
func (ex *executor) rebuildFDEUKI() (bool, error) {
	data, err := ex.opts.Source.ReadFile(plan.FDEMarkerPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the FDE marker: %w", err)
	}
	if string(data) != plan.FDEMarkerText {
		return false, nil
	}
	kernelOut, err := ex.opts.Source.Run("uname", "-r")
	if err != nil {
		return false, fmt.Errorf("read the running kernel to rebuild the Nimbus image: %w", err)
	}
	kernel := strings.TrimSpace(string(kernelOut))
	if kernel == "" || strings.ContainsAny(kernel, "/\\ \t\r\n") {
		return false, errors.New("cannot determine a safe running kernel release")
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if err := ex.sudo(exe, "internal", "fde-uki", "add", kernel, "--only-if-current"); err != nil {
		return false, fmt.Errorf("Plymouth regenerated the initramfs but the signed Nimbus image could not be rebuilt; the disk passphrase still unlocks: %w", err)
	}
	return true, nil
}

// FilePayload binds the narrow file helper input to the reviewed plan.
type FilePayload struct {
	PlanDigest string          `json:"plan_digest"`
	Change     plan.FileChange `json:"change"`
}

func (ex *executor) systemResource(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Kind == plan.KindGreeterSync {
		return ex.greeterSync(op)
	}
	if op.Kind == plan.KindFile {
		return ex.systemFile(op)
	}
	if op.Kind == plan.KindTrigger {
		id := strings.TrimPrefix(op.ID, "trigger:")
		argv := definitions.TriggerArgs(id)
		if len(argv) == 0 {
			return nil, nil, fmt.Errorf("unknown trigger %s", op.ID)
		}
		if id == "plymouth-theme" {
			return ex.plymouthThemeTrigger(op)
		}
		if op.ID == "trigger:noctalia-state-directory" {
			if op.Resource == nil {
				return nil, nil, fmt.Errorf("greeter directory preflight is missing")
			}
			before, err := inspect.ObserveDirectory(ex.opts.Source, op.Resource.Name)
			if err != nil {
				return nil, nil, err
			}
			encoded, _ := json.Marshal(before)
			if string(encoded) != op.Resource.Before {
				return nil, nil, fmt.Errorf("greeter state directory changed after approval")
			}
		}
		if err := ex.sudo(argv...); err != nil {
			return nil, nil, err
		}
		if op.ID == "trigger:noctalia-state-directory" {
			have, err := inspect.ObserveDirectory(ex.opts.Source, op.Resource.Name)
			if err != nil {
				return nil, nil, err
			}
			encoded, _ := json.Marshal(have)
			if string(encoded) != op.Resource.After {
				return nil, nil, fmt.Errorf("greeter state directory verification differs from desired state")
			}
		}

		if op.ID == "trigger:systemd-daemon-reload" {
			for _, resource := range ex.p.Operations {
				if resource.Kind == plan.KindService && resource.Resource != nil {
					out, err := ex.opts.Source.Run("systemctl", "show", "--property=NeedDaemonReload", "--value", "--", resource.Resource.Name)
					if err != nil {
						return nil, nil, fmt.Errorf("daemon reload verification failed for %s: %w", resource.Resource.Name, err)
					}
					if strings.TrimSpace(string(out)) != "no" {
						return nil, nil, fmt.Errorf("daemon reload verification failed for %s", resource.Resource.Name)
					}
				}
			}
		}
		return []state.Receipt{ex.receipt(op, plan.KindTrigger, "pending", "completed", "fixed native trigger completed and its declared verification passed")}, nil, nil
	}
	change := op.Resource
	if change == nil {
		return nil, nil, fmt.Errorf("resource payload is missing")
	}
	if err := ex.verifyResource(op, false); err != nil {
		return nil, nil, fmt.Errorf("resource changed after approval: %w", err)
	}
	if op.Kind == plan.KindShell {
		if op.Action == plan.ActionRetire {
			return nil, []string{op.ID}, nil
		}
		var desired inspect.LoginShell
		if err := json.Unmarshal([]byte(change.After), &desired); err != nil {
			return nil, nil, fmt.Errorf("invalid login-shell payload: %w", err)
		}
		if err := inspect.CheckLoginShell(ex.opts.Source, desired.Shell); err != nil {
			return nil, nil, err
		}
	}
	if op.Kind == plan.KindService && op.Action == plan.ActionRetire {
		have, err := inspect.ObserveService(ex.opts.Source, change.Name)
		if err != nil {
			return nil, nil, err
		}
		if have.Load != "not-found" || have.Active != "inactive" {
			return nil, nil, fmt.Errorf("unit reappeared or became active before receipt retirement")
		}
		return nil, []string{op.ID}, nil
	}
	for _, step := range op.Steps {
		if len(step.Argv) > 0 {
			if err := ex.sudo(step.Argv...); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := ex.verifyResource(op, true); err != nil {
		return nil, nil, fmt.Errorf("verification: %w", err)
	}
	if op.Action == plan.ActionRemove {
		return nil, []string{op.ID}, nil
	}
	receipt := ex.receipt(op, op.Kind, change.Previous, change.After, "native state matches the approved resource")
	receipt.Logout = (op.Kind == plan.KindGroup || op.Kind == plan.KindShell) && change.Before != change.After
	receipt.Reboot = op.Kind == plan.KindTarget && change.Before != change.After
	if op.Kind == plan.KindService && change.Name == "greetd.service" && change.Enabled != nil && *change.Enabled && change.Running == nil {
		var before inspect.Service
		if json.Unmarshal([]byte(change.Before), &before) == nil && before.Enabled != "enabled" && before.Active != "active" {
			receipt.Reboot = true
		}
	}
	return []state.Receipt{receipt}, nil, nil
}

func (ex *executor) verifyResource(op plan.Operation, after bool) error {
	c := op.Resource
	want := c.Before
	if after {
		want = c.After
	}
	switch op.Kind {
	case plan.KindShell:
		var expected inspect.LoginShell
		if err := json.Unmarshal([]byte(want), &expected); err != nil {
			return fmt.Errorf("invalid login-shell state: %w", err)
		}
		have, err := inspect.ObserveLoginShell(ex.opts.Source, c.User)
		if err != nil {
			return err
		}
		if have != expected {
			return fmt.Errorf("local account identity or login shell differs from approved state")
		}
	case plan.KindService:
		have, err := inspect.ObserveService(ex.opts.Source, c.Name)
		if err != nil {
			return err
		}
		if !after {
			var before inspect.Service
			if json.Unmarshal([]byte(want), &before) != nil {
				return fmt.Errorf("invalid prior unit state")
			}
			if have != before {
				return fmt.Errorf("unit state differs from the approved observation")
			}
			return nil
		}
		if have.Load != "loaded" {
			return fmt.Errorf("unit is not loaded")
		}
		if have.Enabled == "masked" || have.Enabled == "masked-runtime" {
			return fmt.Errorf("unit enablement is %s", have.Enabled)
		}
		if c.Enabled != nil {
			want := "disabled"
			if *c.Enabled {
				want = "enabled"
			}
			if have.Enabled != want {
				return fmt.Errorf("unit enablement is %s", have.Enabled)
			}
		}
		if c.Running != nil && (have.Active == "active") != *c.Running {
			return fmt.Errorf("unit activity is %s", have.Active)
		}
	case plan.KindGroup:
		have, err := inspect.ObserveMembership(ex.opts.Source, c.User, c.Name)
		if err != nil {
			return err
		}
		if fmt.Sprint(have) != want {
			return fmt.Errorf("membership differs from approved state")
		}
	case plan.KindTarget:
		out, err := ex.opts.Source.Run("systemctl", "get-default")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(out)) != want {
			return fmt.Errorf("default boot target differs from approved state")
		}
	default:
		return fmt.Errorf("unknown resource kind %s", op.Kind)
	}
	return nil
}

func (ex *executor) systemFile(op plan.Operation) (receipts []state.Receipt, remove []string, resultErr error) {
	applied := false
	defer func() {
		if applied && resultErr != nil {
			resultErr = fmt.Errorf("file mutation was applied but not recorded for %s: %w; inspect the live file and restore the reviewed previous state before retrying", op.File.Target, resultErr)
		}
	}()
	c := op.File
	if c == nil {
		return nil, nil, fmt.Errorf("file payload is missing")
	}
	have, err := inspect.ObserveFile(ex.opts.Source, c.Target)
	if err != nil {
		return nil, nil, err
	}
	if !plan.SameFile(have, c.Before) {
		return nil, nil, fmt.Errorf("file changed after approval: %s", c.Target)
	}
	if op.Action != plan.ActionAdopt {
		if err := os.MkdirAll(ex.opts.Stage, 0700); err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(FilePayload{PlanDigest: ex.p.Digest, Change: *c})
		if err != nil {
			return nil, nil, err
		}
		f, err := os.CreateTemp(ex.opts.Stage, "system-file-*.json")
		if err != nil {
			return nil, nil, err
		}
		staged := f.Name()
		defer func() {
			if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErr := fmt.Errorf("remove staged file payload: %w", err)
				if resultErr != nil {
					resultErr = errors.Join(resultErr, cleanupErr)
				} else {
					ex.differences = append(ex.differences, cleanupErr.Error())
				}
			}
		}()
		if _, err = f.Write(payload); err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		if err = f.Close(); err != nil {
			return nil, nil, err
		}
		exe, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		if err := ex.sudo(exe, "internal", "system-file", "--plan", ex.p.Digest, "--payload", filepath.Clean(staged)); err != nil {
			return nil, nil, fmt.Errorf("file helper failed for %s; a mutation may have been applied but not recorded: %w; inspect the live file and restore its reviewed previous state before retrying", c.Target, err)
		}
		applied = true
		if c.After.Exists {
			if err := ex.sudo("restorecon", "--", c.Target); err != nil {
				return nil, nil, err
			}
		}
	}
	have, err = inspect.ObserveFile(ex.opts.Source, c.Target)
	if err != nil {
		return nil, nil, err
	}
	if !plan.SameFile(have, c.After) {
		return nil, nil, fmt.Errorf("verification: file content or metadata differs for %s", c.Target)
	}
	if op.Action == plan.ActionRemove {
		return nil, []string{op.ID}, nil
	}
	data, _ := json.Marshal(c.After)
	receipt := ex.receipt(op, plan.KindFile, c.Previous, string(data), "regular file content, ownership and mode match")
	receipt.Triggers = c.Triggers
	receipt.Reboot = definitions.GreeterAppearanceTarget(c.Target) && (op.Action != plan.ActionAdopt || c.ActivationChanged)
	receipt.ChangedAt = c.ChangedAt
	if op.Action != plan.ActionAdopt || c.ActivationChanged {
		receipt.ChangedAt = receipt.Timestamp
	}
	return []state.Receipt{receipt}, nil, nil
}

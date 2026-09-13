package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/checkout"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/selector"
)

// Resolve before Topgrade replaces the running RPM. The new process opens the
// updated file at this path instead of continuing in the old engine.
var syncExecutable = os.Executable

func runMaintenance(cmd *cobra.Command, opts *options, flags machineFlags, sf syncFlags, upgrade bool) (retErr error) {
	if opts.json && !sf.plan && !sf.yes {
		return usageError{errors.New("mutation with --json requires --yes; use --plan to inspect changes")}
	}
	if os.Getenv(upgradeActive) != "" && !sf.plan {
		return errors.New("recursive sync refused; Topgrade must call nimbus upgrade --system")
	}
	out := cmd.OutOrStdout()
	if opts.json {
		out = cmd.ErrOrStderr()
	}
	if sf.plan {
		engine := "1. Request sudo and refresh the Nimbus RPM check; stop if a newer engine is available."
		if upgrade {
			engine = "1. Request sudo, refresh and upgrade Nimbus if needed; restart into the updated engine."
		}
		if _, err := fmt.Fprintf(out, "Preview only (cached metadata):\n%s\n2. Update repositories, reconcile the system, then apply Chezmoi once.\n\n", engine); err != nil {
			return err
		}
		if err := runSync(cmd, opts, flags, sf); err != nil {
			return err
		}
		if upgrade {
			if _, err := fmt.Fprintln(out, "\n3. Run software updates:"); err != nil {
				return err
			}
			return runTopgrade(cmd, nil, true, flags)
		}
		return nil
	}

	flags, err := maintenanceSelection(flags)
	if err != nil {
		return err
	}
	src := newSource()
	if err := inspect.CheckPlatform(src, []string{"44"}); err != nil {
		return err
	}
	stopSudo, err := sudoKeepalive(src, out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer stopSudo()
	result := syncResult{Executed: []string{}, Differences: []string{}}
	var steps []runStep
	var restarted bool
	phase := "Nimbus update"
	var finalSelection *selected
	defer func() {
		if restarted {
			return
		}
		result.Steps = append(steps, result.Steps...)
		if retErr != nil && result.Error == "" {
			result.Failed, result.Error = phase, retErr.Error()
			result.Steps = append(result.Steps, runStep{Name: phase, Status: "failed", Detail: result.Error})
		}
		if retErr != nil {
			skippedMaintenance(&result, phase, upgrade)
		}
		if finalSelection != nil {
			inspectFinal(src, finalSelection, &result, retErr == nil, false)
		}
		var err error
		if opts.json {
			err = writeJSON(cmd.OutOrStdout(), result, nil)
		} else {
			err = result.render(out, false)
		}
		if err == nil && finalSelection != nil {
			err = rememberNotes(finalSelection.Resolved.Machine, result.Notes)
		}
		if err != nil {
			retErr = errors.Join(retErr, err)
		} else if retErr != nil && !isNativeExit(retErr) {
			retErr = reported{}
		}
	}()
	engine, restarted, err := enginePreflight(cmd, src, flags, sf, upgrade)
	if restarted || err != nil {
		return err
	}
	status := "current"
	if os.Getenv(engineRestart) != "" {
		status = "updated"
	}
	steps = append(steps, runStep{Name: "Nimbus", Status: status, Detail: engine})
	phase = "repository preflight"

	s, err := loadSelected(flags)
	if err != nil {
		return err
	}
	flags = machineFlags{checkout: s.Root, machine: s.Resolved.Machine}
	finalSelection = s
	if err := inspect.CheckPlatform(src, s.Checkout.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "sync", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer func() {
		if lock != nil {
			retErr = errors.Join(retErr, lock.Release())
		}
	}()
	repos, err := maintenanceRepositories(src, s)
	if err != nil {
		return fmt.Errorf("%w\nNo system or user configuration changes were applied", err)
	}
	for _, repo := range repos {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if err := native.Activity(out, "fetch "+repo.Name, func() error { return repo.Prepare(src) }); err != nil {
			return fmt.Errorf("%w\nNo system or user configuration changes were applied", err)
		}
	}

	phase = "repository update"
	for _, repo := range repos {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "-> update %s repository: %s\n", repo.Name, repo.Path); err != nil {
			return err
		}
		if err := repo.Update(src); err != nil {
			return err
		}
		steps = append(steps, runStep{Name: repo.Name + " repository", Status: "succeeded"})
	}
	// Read new definitions only after both repositories have passed preflight.
	s, err = loadSelected(flags)
	if err != nil {
		return fmt.Errorf("repositories updated, but definitions could not be loaded: %w\nUpdate the Nimbus RPM if a newer engine is required; no system or user configuration changes were applied", err)
	}
	finalSelection = s
	freshRepos, err := maintenanceRepositories(src, s)
	if err != nil {
		return err
	}
	if len(freshRepos) != len(repos) {
		return errors.New("the updated manifest changed the dotfiles repository selection; run nimbus init to review the new handoff")
	}
	for i := range repos {
		if freshRepos[i].Path != repos[i].Path || freshRepos[i].Origin != repos[i].Origin {
			return errors.New("repository selection changed during sync; inspect the checkout and rerun nimbus sync")
		}
	}
	phase = "system sync"
	sf.result = &result
	if err := runSyncWith(cmd, opts, flags, sf, lock); err != nil {
		return err
	}
	if len(repos) > 1 {
		phase = "Chezmoi apply"
		if _, err := fmt.Fprintln(out, "System sync completed. Apply the updated Chezmoi configuration and its declared scripts/tools?"); err != nil {
			return err
		}
		if !sf.yes && !approver(cmd.InOrStdin(), out, "") {
			return errors.New("system sync completed; Chezmoi apply was declined. Run nimbus sync again when ready")
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		// Check again after the system phase and its approval prompts.
		for _, repo := range freshRepos {
			if err := repo.Check(src); err != nil {
				return fmt.Errorf("system sync completed; Chezmoi was not applied: %w", err)
			}
		}
		if _, err := maintenanceRepositories(src, s); err != nil {
			return fmt.Errorf("system sync completed; Chezmoi was not applied: %w", err)
		}
		if err := applyMaintenanceDotfiles(src, out, s); err != nil {
			return fmt.Errorf("system sync completed, but Chezmoi apply failed; user configuration may be partly updated. Fix the Chezmoi error and run nimbus sync again: %w", err)
		}
		result.Steps = append(result.Steps, runStep{Name: phase, Status: "succeeded"})
	}
	// Release the system lock before Topgrade's separate system callback.
	if err := lock.Release(); err != nil {
		return err
	}
	lock = nil
	if upgrade {
		phase = "software updates"
		if err := runMaintenanceUpgrade(cmd, flags, &result); err != nil {
			return err
		}
		result.Steps = append(result.Steps, runStep{Name: phase, Status: "succeeded"})
	}
	phase = "final inspection"
	return nil
}

func maintenanceRepositories(src native.Source, s *selected) ([]*checkout.Repository, error) {
	origin, err := selector.CheckoutOrigin(s.Root)
	if err != nil {
		return nil, err
	}
	repo, err := checkout.Inspect(src, "Nimbus", s.Root, origin)
	if err != nil {
		return nil, err
	}
	repos := []*checkout.Repository{repo}
	machine := s.Checkout.Machines[s.Resolved.Machine]
	if machine.Dotfiles == nil {
		return repos, nil
	}
	path, err := src.Run("chezmoi", "source-path")
	if err != nil {
		return nil, errors.New("cannot inspect the Chezmoi repository; run nimbus init to complete user configuration setup")
	}
	root := strings.TrimSpace(string(path))
	if !filepath.IsAbs(root) {
		return nil, errors.New("Chezmoi did not return an absolute repository path; inspect chezmoi source-path")
	}
	dotfiles, err := checkout.Inspect(src, "dotfiles", root, machine.Dotfiles.Repo)
	if err != nil {
		return nil, err
	}
	data, err := src.Run("chezmoi", inspect.ChezmoiDataArgs...)
	if err != nil {
		return nil, fmt.Errorf("read Chezmoi selection: %w", err)
	}
	selection, err := inspect.ParseChezmoiData(data)
	if err != nil {
		return nil, err
	}
	if !selection.ManagedByNimbus || selection.Machine != s.Resolved.Machine {
		return nil, errors.New("Chezmoi is not configured for the selected Nimbus machine; run nimbus init to review its selection")
	}
	return append(repos, dotfiles), nil
}

func applyMaintenanceDotfiles(src native.Source, out io.Writer, s *selected) error {
	data, err := src.Run("chezmoi", inspect.ChezmoiDataArgs...)
	if err != nil {
		return err
	}
	selection, err := inspect.ParseChezmoiData(data)
	if err != nil {
		return err
	}
	if !selection.ManagedByNimbus || selection.Machine != s.Resolved.Machine {
		return errors.New("Chezmoi selection changed; run nimbus init to review it")
	}
	if !slices.Equal(slices.Sorted(slices.Values(selection.Profiles)), slices.Sorted(slices.Values(s.Resolved.Profiles))) {
		if _, err := fmt.Fprintln(out, "-> refresh Chezmoi's selected profiles, preserving the 1Password SSH choice"); err != nil {
			return err
		}
		args := []string{"init", "--prompt", "--promptString", "Machine=" + s.Resolved.Machine, "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=" + strings.Join(s.Resolved.Profiles, "/"), "--promptBool", fmt.Sprintf("Enable 1Password SSH integration=%t", selection.OnePasswordSSH)}
		if err := src.Stream(out, out, "chezmoi", args...); err != nil {
			return err
		}
		data, err := src.Run("chezmoi", inspect.ChezmoiDataArgs...)
		if err != nil {
			return fmt.Errorf("verify refreshed Chezmoi selection: %w", err)
		}
		refreshed, err := inspect.ParseChezmoiData(data)
		if err != nil {
			return err
		}
		if !refreshed.ManagedByNimbus || refreshed.Machine != selection.Machine || refreshed.OnePasswordSSH != selection.OnePasswordSSH || !slices.Equal(slices.Sorted(slices.Values(refreshed.Profiles)), slices.Sorted(slices.Values(s.Resolved.Profiles))) {
			return errors.New("Chezmoi did not retain the requested selection; inspect chezmoi data before applying")
		}
	}
	if _, err := fmt.Fprintln(out, "-> apply user configuration and its declared tools\n   $ chezmoi apply"); err != nil {
		return err
	}
	return src.Stream(out, out, "chezmoi", "apply")
}

func isNativeExit(err error) bool { _, ok := errors.AsType[nativeExit](err); return ok }

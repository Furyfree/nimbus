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
		if _, err := fmt.Fprintf(out, "Preview only (cached metadata):\n%s\n2. Update repositories, reconcile the system, then apply Chezmoi once.\n", engine); err != nil {
			return err
		}
		if detail, err := migrateSelector(true); err != nil {
			return err
		} else if detail != "" {
			if _, err := fmt.Fprintf(out, "Selector: recorded as %s before anything else changes.\n", detail); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := runSync(cmd, opts, flags, sf); err != nil {
			return err
		}
		if upgrade {
			if _, err := fmt.Fprintln(out, "\n3. Run software updates:"); err != nil {
				return err
			}
			if err := runTopgrade(cmd, nil, true, flags); err != nil {
				return err
			}
			_, err := fmt.Fprintln(out, "4. Refresh local agent models if configured; defer when Copilot is closed.")
			return err
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
	command := "sync"
	if upgrade {
		command = "sync --upgrade"
	}
	record, err := beginRunRecord(command, flags.machine)
	if err != nil {
		return fmt.Errorf("start run record: %w", err)
	}
	// This early defer also captures authentication failure before the report exists.
	defer func() {
		if record.Outcome == "running" {
			retErr = finishRunRecord(cmd.ErrOrStderr(), record, &syncResult{}, "authentication", retErr)
		}
	}()
	stopSudo, err := sudoKeepalive(src, out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer stopSudo()
	result := syncResult{Verbose: opts.verbose, Executed: []string{}, Differences: []string{}}
	var steps []runStep
	var restarted bool
	phase := "Nimbus update"
	phaseStarted := time.Now()
	var timings []runPhase
	setPhase := func(next string) {
		timings = append(timings, runPhase{Name: phase, DurationMS: time.Since(phaseStarted).Milliseconds()})
		phase, phaseStarted = next, time.Now()
	}
	var finalSelection *selected
	defer func() {
		if restarted {
			record.Outcome = "restarted"
			retErr = finishRunRecord(cmd.ErrOrStderr(), record, &result, phase, retErr)
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
			inspectFinal(src, finalSelection, &result, retErr == nil)
		}
		timings = append(timings, runPhase{Name: phase, DurationMS: time.Since(phaseStarted).Milliseconds()})
		record.Phases = timings

		if retErr != nil || opts.verbose {
			result.Notices = append(result.Notices, "Run record: "+record.path)
		}
		if recordErr := record.finish(&result, phase, retErr != nil); recordErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("finish run record: %w", recordErr))
			if result.Error != "" {
				result.Error += "; "
			}
			result.Error += "finish run record: " + recordErr.Error()
			result.Steps = append(result.Steps, runStep{Name: "run record", Status: "failed", Detail: recordErr.Error()})
		}
		var err error
		if opts.json {
			err = writeJSON(cmd.OutOrStdout(), result, nil)
		} else {
			err = result.render(out, false)
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
	// The engine that reads schema 2 is running now, after any restart.
	if detail, err := migrateSelector(false); err != nil {
		return fmt.Errorf("%w\nNo system or user configuration changes were applied", err)
	} else if detail != "" {
		if _, err := fmt.Fprintf(out, "-> selector: recorded as %s\n", detail); err != nil {
			return err
		}
		steps = append(steps, runStep{Name: "selector", Status: "updated", Detail: detail})
		result.Notices = append(result.Notices, "Channel support: this machine follows the stable track; run install.sh --channel develop from the checkout to follow develop, or nimbus channel to inspect the track.")
	}
	setPhase("repository preflight")

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

	setPhase("repository update")
	for _, repo := range repos {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		status := "current"
		if repo.Changed() {
			status = "updated"
		}
		if _, err := fmt.Fprintf(out, "-> %s repository: %s\n", repo.Name, status); err != nil {
			return err
		}
		if err := repo.Update(src); err != nil {
			return err
		}
		if repo.Name == "Nimbus" {
			record.Commit = repo.PreparedCommit()
		}
		steps = append(steps, runStep{Name: repo.Name + " repository", Status: status})
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
	setPhase("system sync")
	sf.result = &result
	if err := runSyncWith(cmd, opts, flags, sf, lock); err != nil {
		return err
	}
	if len(repos) > 1 {
		setPhase("Chezmoi apply")
		if _, err := fmt.Fprintln(out, "Apply Chezmoi configuration and tool hooks? Native file-conflict prompts remain enabled."); err != nil {
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
		setPhase("software updates")
		if err := runMaintenanceUpgrade(cmd, flags, &result, sf.yes); err != nil {
			return err
		}
		result.Steps = append(result.Steps, runStep{Name: phase, Status: "succeeded"})
	}
	if upgrade {
		setPhase("agent model refresh")
		if err := refreshAgentProxy(cmd, src, s.Resolved.Machine, out, &result); err != nil {
			return err
		}
	}
	setPhase("final inspection")
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

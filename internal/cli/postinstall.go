package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/state"
)

var postinstallTerminal = func(in io.Reader) bool {
	file, ok := in.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

type postinstallView struct {
	Machine string             `json:"machine"`
	Tasks   []postinstall.Task `json:"tasks"`
}

type postinstallSnapshot struct {
	selected     *selected
	view         postinstallView
	digest       string
	nativeDigest string
}

func postinstallExecutor(opts *options, flags *machineFlags, taskID string) *cobra.Command {
	var yes, preview, markDone, reset bool
	cmd := &cobra.Command{
		Use: taskID, Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.json && (yes || markDone || reset || !preview) {
				return usageError{errors.New("postinstall --json lists tasks only; it cannot select or approve an action")}
			}
			src := newSource()
			before, err := inspectPostinstall(src, *flags, taskID)
			if err != nil {
				return err
			}
			task, err := selectedTask(before.view, taskID)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{task}}, nil)
			}
			if reset {
				if err := resetTask(before.view.Machine, task.ID); err != nil {
					return err
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s acknowledgment reset for %s. Native configuration will still be inspected.\n", task.ID, before.view.Machine)
				return err
			}
			if markDone {
				return markExistingTask(cmd, src, before, task, yes)
			}
			if task.ID == "onepassword" && !preview {
				return runOnePassword(cmd, src, before, task, yes, false)
			}
			if task.ID == "nvidia-mok" && !preview {
				return runMOKVerification(cmd, src, before, task, yes)
			}
			if err := renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{task}}); err != nil {
				return err
			}
			if preview || task.Status == postinstall.NotApplicable {
				return nil
			}
			if task.Status == postinstall.Complete {
				return recordTask(before.view.Machine, task.ID+".complete", "verified")
			}
			if task.Status == postinstall.Blocked {
				return errors.New(task.Detail)
			}
			if task.Action == nil {
				return fmt.Errorf("%s is incomplete: %s", task.ID, task.Detail)
			}
			commands, err := postinstallCommands(task)
			if err != nil {
				return err
			}
			if task.ID == "proton-cachyos" && !taskConfirmed(before.view.Machine, task.ID) {
				if err := confirmManual(cmd, "Start Steam once and close all running games. Ready to continue?"); err != nil {
					return err
				}
				if err := recordTask(before.view.Machine, task.ID+".manual", "confirmed"); err != nil {
					return err
				}
			}
			if !yes {
				if !postinstallTerminal(cmd.InOrStdin()) {
					return usageError{errors.New("selecting a postinstall action requires a terminal or explicit --yes; use --plan to inspect it")}
				}
				if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), before.digest) {
					return errors.New("postinstall action not approved; nothing was run")
				}
			}
			path, err := apply.LockPath()
			if err != nil {
				return err
			}
			lock, err := apply.Acquire(path, apply.LockInfo{Command: "postinstall " + task.ID, Operation: before.digest, PID: os.Getpid(), Started: time.Now().UTC()})
			if err != nil {
				return err
			}
			defer func() { _ = lock.Release() }()
			fresh, err := inspectPostinstall(src, *flags, taskID)
			if err != nil {
				return err
			}
			if fresh.digest != before.digest {
				return errors.New("postinstall ownership, definitions, or observed state changed after approval; inspect and select the task again")
			}
			if err := inspect.CheckPlatform(src, fresh.selected.Checkout.Definitions().Compatibility.Fedora); err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			var runErr error
			if task.Action.Kind == postinstall.RestoreNoctaliaLockscreen {
				runErr = postinstall.RunNoctaliaLockscreen(cmd.Context(), src, cmd.OutOrStdout(), task)
			} else if task.Action.Kind == postinstall.SyncNoctaliaPlugins {
				runErr = postinstall.RunNoctaliaPlugins(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task)
			} else if task.Action.Kind == postinstall.SyncHyprlandPlugins {
				runErr = postinstall.RunHyprlandPlugins(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task)
			} else {
				argv := commands[0]
				runErr = src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), argv[0], argv[1:]...)
			}
			if runErr != nil {
				runErr = fmt.Errorf("postinstall %s action failed: %w", task.ID, runErr)
			}
			after, checkErr := inspectPostinstall(src, *flags, taskID)
			if checkErr != nil {
				return errors.Join(runErr, fmt.Errorf("action finished but task status could not be checked: %w", checkErr))
			}
			current, checkErr := selectedTask(after.view, task.ID)
			if checkErr != nil {
				return errors.Join(runErr, fmt.Errorf("task selection changed during the native action: %w", checkErr))
			}
			var reportErr error
			if runErr == nil && current.Status == postinstall.Complete {
				reportErr = renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: after.view.Machine, Tasks: []postinstall.Task{current}})
			} else {
				_, reportErr = fmt.Fprintf(cmd.OutOrStdout(), "After action: %s: %s\n%s\nVerification: %s\n", current.ID, current.Status, current.Detail, current.Verification)
			}
			if runErr == nil && current.Status != postinstall.Complete {
				checkErr = fmt.Errorf("%s action finished but completion could not be verified: %s", task.ID, current.Detail)
			}
			if runErr == nil && checkErr == nil {
				checkErr = recordTask(before.view.Machine, task.ID+".complete", "verified")
			}

			return errors.Join(runErr, checkErr, reportErr)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the selected native action after its preview")
	cmd.Flags().BoolVarP(&preview, "plan", "p", false, "show the selected task without running its action")
	if slices.Contains([]string{"onepassword", "nvidia-mok", "proton-cachyos"}, taskID) {
		cmd.Flags().BoolVar(&markDone, "mark-done", false, "verify existing setup and record completion without applying configuration")
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "reset this machine's acknowledgment without changing configuration")
	if cmd.Flags().Lookup("mark-done") != nil {
		cmd.MarkFlagsMutuallyExclusive("plan", "mark-done", "reset")
	} else {
		cmd.MarkFlagsMutuallyExclusive("plan", "reset")
	}
	return cmd
}

func inspectPostinstall(src native.Source, flags machineFlags, tasks ...string) (*postinstallSnapshot, error) {
	s, err := loadSelected(flags)
	if err != nil {
		return nil, err
	}
	applied, err := state.Read(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("postinstall ownership state: %w", err)
	}
	f := inspect.InspectTasks(src)
	taskID := ""
	if len(tasks) > 0 {
		taskID = tasks[0]
	}
	view := postinstallView{Machine: s.Resolved.Machine, Tasks: postinstall.Inspect(src, postinstall.Inputs{Task: taskID, Resolved: s.Resolved, Facts: *f, Applied: *applied})}
	if err := enrichPostinstall(src, s, &view); err != nil {
		return nil, err
	}
	data, err := json.Marshal(struct {
		Root, Definitions string
		View              postinstallView
		Facts             *inspect.Facts
		Applied           *state.Applied
	}{s.Root, s.Checkout.Digest(), view, f, applied})
	if err != nil {
		return nil, err
	}
	nativeData, err := json.Marshal(struct {
		Root, Definitions string
		Facts             *inspect.Facts
		Applied           *state.Applied
	}{s.Root, s.Checkout.Digest(), f, applied})
	if err != nil {
		return nil, err
	}
	return &postinstallSnapshot{selected: s, view: view, nativeDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(nativeData)), digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data))}, nil
}

func selectedTask(view postinstallView, id string) (postinstall.Task, error) {
	for _, task := range view.Tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return postinstall.Task{}, usageError{fmt.Errorf("task %q is not selected or applicable to this machine", id)}
}

func postinstallCommands(task postinstall.Task) ([][]string, error) {
	if task.ID == "noctalia-lockscreen" && task.Status == postinstall.Pending && task.Action != nil && task.Action.Kind == postinstall.RestoreNoctaliaLockscreen && task.Action.Lockscreen != nil {
		return postinstall.LockscreenCommands(task.Action.Lockscreen), nil
	}
	if task.Action != nil && task.Action.Kind == postinstall.SyncHyprlandPlugins {
		return postinstall.HyprlandCommands(task)
	}
	if task.Action != nil && task.Action.Kind == postinstall.SyncNoctaliaPlugins {
		return postinstall.NoctaliaCommands(task)
	}
	if task.Action != nil && len(task.Action.Commands) != 0 {
		return nil, errors.New("unexpected native command list")
	}
	argv, err := postinstallArgv(task)
	if err != nil {
		return nil, err
	}
	return [][]string{argv}, nil
}

func postinstallArgv(task postinstall.Task) ([]string, error) {
	if task.Action != nil {
		if task.ID == "account-picture" && task.Status == postinstall.Pending && task.Action.Kind == postinstall.SetAccountPicture && task.Action.Picture != nil {
			expected := postinstall.AccountPictureAction(task.Action.User, *task.Action.Picture)
			if expected != nil && slices.Equal(task.Action.Argv, expected.Argv) {
				return expected.Argv, nil
			}
		}
		if task.ID == "tailscale-operator" && task.Status == postinstall.Pending && task.Action.Kind == postinstall.SetTailscaleOperator {
			expected := postinstall.TailscaleOperatorAction(task.Action.User)
			if expected != nil && slices.Equal(task.Action.Argv, expected.Argv) {
				return expected.Argv, nil
			}
		}
		switch {
		case task.ID == "onepassword" && task.Status == postinstall.Unknown && task.Action.Kind == postinstall.OpenApplication && slices.Equal(task.Action.Argv, []string{"1password"}):
			return []string{"1password"}, nil
		case task.ID == "fingerprint" && task.Status == postinstall.Pending && task.Action.Kind == postinstall.EnrollFingerprint && slices.Equal(task.Action.Argv, []string{"fprintd-enroll"}):
			return []string{"fprintd-enroll"}, nil
		case task.ID == "copilot" && task.Status == postinstall.Pending && task.Action.Kind == postinstall.InstallApplication && slices.Equal(task.Action.Argv, []string{"sudo", "--", "/usr/bin/github-copilot-installer", "install"}):
			return slices.Clone(task.Action.Argv), nil
		case task.ID == "proton-cachyos" && task.Status == postinstall.Pending && task.Action.Kind == postinstall.InstallApplication && slices.Equal(task.Action.Argv, []string{"protonplus", "install", "steam-system", "proton-cachyos", "latest"}):
			return slices.Clone(task.Action.Argv), nil
		}
	}
	return nil, errors.New("task has no supported native action in its current state")
}

func renderPostinstall(out io.Writer, view postinstallView) error {
	var b bytes.Buffer
	if len(view.Tasks) == 1 && view.Tasks[0].Status == postinstall.Complete {
		_, err := fmt.Fprintf(out, "\u2713 %s\n", view.Tasks[0].Detail)
		return err
	}
	fmt.Fprintf(&b, "Setup for %s:\n", view.Machine)
	if len(view.Tasks) == 0 {
		fmt.Fprintln(&b, "No pending tasks were identified from the selected capabilities and available observations.")
	}
	for _, task := range view.Tasks {
		if task.Status == postinstall.Complete {
			fmt.Fprintf(&b, "\n\u2713 %s\n", task.Detail)
			continue
		}
		if task.ID == "nvidia-mok" && task.Status == postinstall.Unknown {
			fmt.Fprintf(&b, "\n%s [%s]: Verify NVIDIA signing-key enrollment\n", task.ID, postinstallStatusLabel(task))
			if task.PreviouslyVerified && task.VerificationNeedsRoot {
				fmt.Fprintln(&b, "Enrollment was verified earlier; a fresh check requires sudo.")
			} else {
				fmt.Fprintln(&b, task.Detail)
			}
			if task.VerificationNeedsRoot {
				fmt.Fprintln(&b, "  Recheck: nimbus postinstall nvidia-mok (requests sudo)")
				fmt.Fprintln(&b, "The task offers read-only certificate and enrollment checks after approval.")
			}
			fmt.Fprintln(&b, "Establish enrollment status before considering enrollment changes.")
			fmt.Fprintln(&b, "No keys are generated or enrolled. Driver loading is not verified.")
			continue
		}
		fmt.Fprintf(&b, "\n%s [%s]: %s\n%s\n", task.ID, postinstallStatusLabel(task), task.Title, task.Detail)
		for _, prerequisite := range task.Prerequisites {
			fmt.Fprintf(&b, "Prerequisite: %s\n", prerequisite)
		}
		for _, instruction := range task.Instructions {
			fmt.Fprintln(&b, instruction)
		}
		if task.Action != nil {
			commands, err := postinstallCommands(task)
			if err != nil {
				return err
			}
			for _, argv := range commands {
				fmt.Fprintf(&b, "Native action: %s\n", strings.Join(argv, " "))
			}
		}
		fmt.Fprintf(&b, "Verification: %s\nRecovery: %s\n", task.Verification, task.Recovery)
	}
	_, err := out.Write(b.Bytes())
	return err
}

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
	selected *selected
	view     postinstallView
	digest   string
}

func newPostinstall(opts *options) *cobra.Command {
	var flags machineFlags
	var yes, preview bool
	cmd := &cobra.Command{
		Use: "postinstall [TASK]", Short: "Inspect manual setup tasks or select one native action",
		Long: "List pending, blocked, and unknown manual tasks without changing the system. Select a task ID to see its instructions or approve its fixed native action. JSON lists tasks without selecting or executing one.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError{errors.New("expected at most one task ID")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.json && (len(args) != 0 || yes) {
				return usageError{errors.New("postinstall --json lists tasks only; it cannot select or approve an action")}
			}
			if yes && len(args) == 0 {
				return usageError{errors.New("postinstall --yes requires one task ID")}
			}
			src := newSource()
			before, err := inspectPostinstall(src, flags)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				pending := before.view
				pending.Tasks = slices.DeleteFunc(slices.Clone(pending.Tasks), func(t postinstall.Task) bool {
					return t.Status == postinstall.Complete || t.Status == postinstall.NotApplicable
				})
				if opts.json {
					return writeJSON(cmd.OutOrStdout(), pending, nil)
				}
				return renderPostinstall(cmd.OutOrStdout(), pending)
			}
			task, err := selectedTask(before.view, args[0])
			if err != nil {
				return err
			}
			if err := renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{task}}); err != nil {
				return err
			}
			if preview || task.Status == postinstall.Complete || task.Status == postinstall.NotApplicable {
				return nil
			}
			if task.Status == postinstall.Blocked {
				return errors.New(task.Detail)
			}
			if task.Action == nil {
				return nil
			}
			commands, err := postinstallCommands(task)
			if err != nil {
				return err
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
			fresh, err := inspectPostinstall(src, flags)
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
			if task.Action.Kind == postinstall.SyncNoctaliaPlugins {
				runErr = postinstall.RunNoctaliaPlugins(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task)
			} else {
				argv := commands[0]
				runErr = src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), argv[0], argv[1:]...)
			}
			if runErr != nil {
				runErr = fmt.Errorf("postinstall %s action failed: %w", task.ID, runErr)
			}
			after, checkErr := inspectPostinstall(src, flags)
			if checkErr != nil {
				return errors.Join(runErr, fmt.Errorf("action finished but task status could not be checked: %w", checkErr))
			}
			current, checkErr := selectedTask(after.view, task.ID)
			if checkErr != nil {
				return errors.Join(runErr, fmt.Errorf("task selection changed during the native action: %w", checkErr))
			}
			_, reportErr := fmt.Fprintf(cmd.OutOrStdout(), "After action: %s: %s\n%s\nVerification: %s\n", current.ID, current.Status, current.Detail, current.Verification)
			if runErr == nil && current.Status != postinstall.Complete && task.Action.Kind == postinstall.EnrollFingerprint {
				checkErr = errors.New("fingerprint enrollment command finished, but completion could not be verified; inspect fprintd before retrying")
			}
			if runErr == nil && current.Status != postinstall.Complete && task.Action.Kind == postinstall.SetTailscaleOperator {
				checkErr = errors.New("Tailscale command finished, but operator permission could not be verified; inspect tailscaled before retrying")
			}
			if runErr == nil && current.Status != postinstall.Complete && task.Action.Kind == postinstall.SyncNoctaliaPlugins {
				checkErr = errors.New("Noctalia plugin installation could not be verified; retry the task")
			}
			return errors.Join(runErr, checkErr, reportErr)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the selected native action after its preview")
	cmd.Flags().BoolVarP(&preview, "plan", "p", false, "show the selected task without running its action")
	return cmd
}

func inspectPostinstall(src native.Source, flags machineFlags) (*postinstallSnapshot, error) {
	s, err := loadSelected(flags)
	if err != nil {
		return nil, err
	}
	applied, err := state.Read(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("postinstall ownership state: %w", err)
	}
	f := inspect.InspectTasks(src)
	view := postinstallView{Machine: s.Resolved.Machine, Tasks: postinstall.Inspect(src, postinstall.Inputs{Resolved: s.Resolved, Facts: *f, Applied: *applied})}
	data, err := json.Marshal(struct {
		Root, Definitions string
		View              postinstallView
		Facts             *inspect.Facts
		Applied           *state.Applied
	}{s.Root, s.Checkout.Digest(), view, f, applied})
	if err != nil {
		return nil, err
	}
	return &postinstallSnapshot{selected: s, view: view, digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data))}, nil
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
		case task.ID == "copilot" && task.Status == postinstall.Unknown && task.Action.Kind == postinstall.InstallApplication && slices.Equal(task.Action.Argv, []string{"sudo", "--", "/usr/bin/github-copilot-installer", "install"}):
			return slices.Clone(task.Action.Argv), nil
		case task.ID == "proton-cachyos" && task.Status == postinstall.Unknown && task.Action.Kind == postinstall.InstallApplication && slices.Equal(task.Action.Argv, []string{"protonplus", "install", "steam-system", "proton-cachyos", "latest"}):
			return slices.Clone(task.Action.Argv), nil
		}
	}
	return nil, errors.New("task has no supported native action in its current state")
}

func renderPostinstall(out io.Writer, view postinstallView) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Manual tasks for %s\n", view.Machine)
	if len(view.Tasks) == 0 {
		fmt.Fprintln(&b, "No pending tasks were identified from the selected capabilities and available observations.")
	}
	for _, task := range view.Tasks {
		fmt.Fprintf(&b, "\n%s [%s]: %s\n%s\n", task.ID, task.Status, task.Title, task.Detail)
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

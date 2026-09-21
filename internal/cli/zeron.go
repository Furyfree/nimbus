package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/spf13/cobra"
)

func zeronSelected(s *selected) bool {
	return slices.ContainsFunc(s.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "zeron" })
}

func zeronOptedIn(machine string) (bool, error) {
	store, err := userstate.Default()
	if err != nil {
		return false, err
	}
	f, err := store.Read("postinstall")
	if err != nil {
		return false, err
	}
	return f.Has(machine, "zeron.daemon", 1, "verified"), nil
}

func zeronUnitPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_CONFIG_HOME must be absolute")
	}
	return filepath.Join(base, "systemd/user/zeron.service"), nil
}

type userUnitState struct{ Load, Active, Enabled, Fragment, DropIns string }

func inspectZeron(src native.Source) (userUnitState, error) {
	return inspectUserUnit(src, "zeron.service")
}

func inspectUserUnit(src native.Source, unit string) (userUnitState, error) {
	out, err := src.Run("systemctl", "--user", "show", unit, "--property=LoadState,ActiveState,UnitFileState,FragmentPath,DropInPaths")
	if err != nil {
		return userUnitState{}, fmt.Errorf("cannot inspect %s; check the user systemd session", unit)
	}
	fields := map[string]string{}
	for line := range strings.Lines(string(out)) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			fields[key] = value
		}
	}
	s := userUnitState{fields["LoadState"], fields["ActiveState"], fields["UnitFileState"], fields["FragmentPath"], fields["DropInPaths"]}
	if s.Load == "" || s.Active == "" {
		return s, fmt.Errorf("incomplete %s inspection", unit)
	}
	return s, nil
}

func (s userUnitState) absent() bool {
	return s.Load == "not-found" && s.Active == "inactive" && s.Fragment == ""
}
func (s userUnitState) running() bool {
	return s.Load == "loaded" && s.Active == "active" && s.Enabled == "enabled"
}

func (s userUnitState) label() string {
	if s.absent() {
		return "Absent"
	}
	switch s.Active {
	case "active":
		return "Running"
	case "inactive":
		return "Stopped"
	case "failed":
		return "Failed"
	default:
		return s.Active
	}
}

func (s userUnitState) detail() string {
	if s.absent() {
		return "unit absent, not running"
	}
	startup := s.Enabled
	if startup == "" {
		startup = "not reported"
	}
	return "load=" + s.Load + ", activity=" + s.Active + ", startup=" + startup
}

// The native CLI owns the unit. Refuse collisions and symlink escapes before invoking it.
func checkZeronOwner(src native.Source, observed userUnitState) error {
	file, err := zeronUnitPath()
	if err != nil {
		return err
	}
	for dir := filepath.Dir(file); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		st, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe Zeron unit ancestor: %s", dir)
		}
	}
	if err := checkZeronDropIns(src, observed.DropIns); err != nil {
		return err
	}
	st, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) && observed.absent() {
		return nil
	}
	if err != nil {
		return err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 || observed.Fragment != file {
		return fmt.Errorf("unrecognized Zeron unit ownership: %s (loaded from %s); refusing replacement or removal", file, observed.Fragment)
	}
	data, err := src.ReadFile(file)
	if err != nil {
		return err
	}
	if !slices.Contains(strings.Split(string(data), "\n"), "ExecStart=%h/.zeron/app/current/zeron headless") {
		return fmt.Errorf("unexpected ExecStart in %s; not the selected Zeron installation", file)
	}
	return nil
}

// Fedora applies this timeout policy to every user service. It does not change
// the native installation's identity; all other overrides still block changes.
func checkZeronDropIns(src native.Source, dropIns string) error {
	for file := range strings.FieldsSeq(dropIns) {
		if file != "/usr/lib/systemd/user/service.d/10-timeout-abort.conf" {
			return fmt.Errorf("unrecognized Zeron override: %s", file)
		}
		info, err := src.Run("stat", "--format=%u:%a:%F", "--", file)
		if err != nil || strings.TrimSpace(string(info)) != "0:644:regular file" {
			return fmt.Errorf("unsafe Fedora timeout override ownership or permissions: %s", file)
		}
		data, err := src.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read Fedora timeout override %s: %w", file, err)
		}
		var settings []string
		for line := range strings.Lines(string(data)) {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") {
				settings = append(settings, line)
			}
		}
		if strings.Join(settings, "\n") != "[Service]\nTimeoutStopFailureMode=abort" {
			return fmt.Errorf("unexpected settings in Fedora timeout override: %s", file)
		}
	}
	return nil
}

func changeZeron(src native.Source, out io.Writer, enable bool) error {
	before, err := inspectZeron(src)
	if err != nil {
		return err
	}
	if err := checkZeronOwner(src, before); err != nil {
		return err
	}
	if !enable && before.absent() {
		return nil
	}
	if _, err := src.LookPath("zeron"); err != nil {
		return errors.New("Zeron is unavailable; run nimbus sync first")
	}
	verb := "uninstall"
	if enable {
		verb = "install"
	}
	if _, err := fmt.Fprintf(out, "-> zeron daemon %s\n", verb); err != nil {
		return err
	}
	if err := src.Stream(out, out, "zeron", "daemon", verb); err != nil {
		return err
	}
	after, err := inspectZeron(src)
	if err != nil {
		return err
	}
	if enable && after.running() {
		return checkZeronOwner(src, after)
	}
	if !enable && after.absent() {
		file, err := zeronUnitPath()
		if err != nil {
			return err
		}
		if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	return errors.New("Zeron daemon change was not verified; retry the task")
}

func zeronTask(src native.Source, machine string) postinstall.Task {
	t := postinstall.Task{ID: "zeron", Owner: "zeron", Title: "Opt into the Zeron background daemon", Status: postinstall.Unknown,
		Instructions: []string{"Use nimbus postinstall zeron to enable the daemon, or --disable to keep it off. The GUI stays installed."},
		Verification: "Native user-service state and explicit opt-in evidence.", Recovery: "Retry the task; native Zeron owns the daemon and application data."}
	opted, err := zeronOptedIn(machine)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	observed, err := inspectZeron(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if !opted && observed.absent() {
		t.Status = postinstall.Complete
	} else if opted && observed.running() {
		t.Status = postinstall.Complete
	} else {
		t.Status = postinstall.Pending
	}
	t.CurrentState = observed.label()
	t.Summary = "Startup " + observed.Enabled
	if observed.absent() {
		t.Summary = "Daemon absent · GUI retained"
	}
	t.Detail = "zeron.service: " + observed.detail() + ". Recorded choice: off. GUI remains installed."
	if opted {
		t.Detail = "zeron.service: " + observed.detail() + ". Recorded choice: daemon on."
	}
	if t.Status == postinstall.Pending {
		t.Detail += " Service differs from the recorded choice."
		t.Summary += "; differs from choice"
	}
	return t
}

func newZeronPostinstall(opts *options, flags *machineFlags) *cobra.Command {
	var preview, yes, disable bool
	cmd := &cobra.Command{Use: "zeron", Short: "Opt into the Zeron background daemon", Args: noArgs,
		Long: "Enable and start the daemon through zeron daemon install, verify it and record opt-in. Later syncs leave it alone. Use --disable to uninstall the daemon and clear opt-in. The GUI, sessions, credentials and linger setting stay. Help and --plan never write state or run native actions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.json && !preview {
				return usageError{errors.New("postinstall --json supports --plan only")}
			}
			s, err := loadSelected(*flags)
			if err != nil {
				return err
			}
			if !zeronSelected(s) {
				return errors.New("Zeron is not selected for this machine")
			}
			if _, err := zeronOptedIn(s.Resolved.Machine); err != nil {
				return err
			}
			src := newSource()
			observed, err := inspectZeron(src)
			if err != nil {
				return err
			}
			if err := checkZeronOwner(src, observed); err != nil {
				return err
			}
			verb := "install"
			if disable {
				verb = "uninstall"
			}
			plan := "zeron daemon " + verb + "; verify service state and record the choice. GUI and linger stay unchanged."
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), plan, nil)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
			if preview {
				return nil
			}
			if !yes && (!postinstallTerminal(cmd.InOrStdin()) || !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "")) {
				return errors.New("Zeron daemon change not approved")
			}
			lockPath, err := apply.LockPath()
			if err != nil {
				return err
			}
			lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "postinstall zeron", PID: os.Getpid(), Started: time.Now().UTC()})
			if err != nil {
				return err
			}
			defer lock.Release()
			fresh, err := loadSelected(*flags)
			if err != nil {
				return err
			}
			if fresh.Checkout.Digest() != s.Checkout.Digest() || fresh.Resolved.Machine != s.Resolved.Machine {
				return errors.New("selection changed; rerun the task")
			}
			if _, err := zeronOptedIn(s.Resolved.Machine); err != nil {
				return err
			}
			if err := changeZeron(src, cmd.OutOrStdout(), !disable); err != nil {
				return err
			}
			if !disable {
				return recordTask(s.Resolved.Machine, "zeron.daemon", "verified")
			}
			store, err := userstate.Default()
			if err != nil {
				return err
			}
			return store.Update("postinstall", s.Resolved.Machine, nil, []string{"zeron.daemon"})
		}}
	cmd.Flags().BoolVarP(&preview, "plan", "p", false, "preview without changing the daemon or state")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the displayed daemon change")
	cmd.Flags().BoolVar(&disable, "disable", false, "uninstall the daemon and clear opt-in")
	return cmd
}

func syncZeron(cmd *cobra.Command, src native.Source, s *selected, out io.Writer, yes bool, result *syncResult) error {
	if !zeronSelected(s) {
		return nil
	}
	opted, err := zeronOptedIn(s.Resolved.Machine)
	if err != nil {
		return err
	}
	if opted {
		return nil
	}
	before, err := inspectZeron(src)
	if err != nil {
		return err
	}
	if before.absent() {
		return nil
	}
	if err := checkZeronOwner(src, before); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Zeron daemon is not opted in: run zeron daemon uninstall. Keep the GUI and linger setting."); err != nil {
		return err
	}
	if !yes && !approver(cmd.InOrStdin(), out, "") {
		return errors.New("Zeron daemon removal not approved")
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "sync zeron", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer lock.Release()
	opted, err = zeronOptedIn(s.Resolved.Machine)
	if err != nil {
		return err
	}
	if opted {
		return errors.New("Zeron opt-in changed while awaiting approval; run sync again")
	}
	if err := changeZeron(src, out, false); err != nil {
		return err
	}
	result.Steps = append(result.Steps, runStep{Name: "Zeron daemon", Status: "succeeded", Detail: "uninstalled; GUI retained"})
	return nil
}

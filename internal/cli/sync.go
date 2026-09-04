package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
	"github.com/Furyfree/nimbus/internal/version"
)

// Test hooks. The real recorder runs the hidden privileged action through
// sudo; the real fetcher uses HTTP.
var (
	newFetcher = func() func(string) ([]byte, error) {
		client := &http.Client{Timeout: 5 * time.Minute}
		return func(url string) ([]byte, error) {
			resp, err := client.Get(url)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("%s: HTTP %s", url, resp.Status)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		}
	}
	newRecorder = func(src facts.Source, stage string) func(string, *state.Stage) error {
		return func(digest string, st *state.Stage) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			data, err := json.Marshal(st)
			if err != nil {
				return err
			}
			path := filepath.Join(stage, "receipts-"+strings.TrimPrefix(digest, "sha256:")[:12]+".json")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}
			defer os.Remove(path)
			_, err = src.Run("sudo", exe, "internal", "record", "--plan", digest, "--stage", path)
			return err
		}
	}
	// sudoKeepalive asks for the sudo password once, before the first
	// privileged command, and renews the credential while apply runs, so a
	// long transaction does not ask again. Tests replace it.
	sudoKeepalive = func(src facts.Source, out, errOut io.Writer) (func(), error) {
		fmt.Fprintln(out, "sudo is needed for the privileged commands; the password is asked once")
		if err := src.Stream(out, errOut, "sudo", "-v"); err != nil {
			return nil, err
		}
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					_, _ = src.Run("sudo", "-n", "-v")
				}
			}
		}()
		return func() { close(done) }, nil
	}
	// approver reads the interactive answer. Tests replace it.
	approver = func(in io.Reader, out io.Writer, _ string) bool {
		fmt.Fprint(out, "Proceed? [Y/n] ")
		reader := bufio.NewReader(in)
		line, _ := reader.ReadString('\n')
		answer := strings.TrimSpace(strings.ToLower(line))
		return answer == "" || answer == "y" || answer == "yes"
	}
)

type syncFlags struct {
	plan, yes, noUpgrade, prune bool
}

func newSync(opts *options) *cobra.Command {
	var flags machineFlags
	var sf syncFlags
	cmd := &cobra.Command{
		Use:   "sync [-p] [-y] [-n] [-r]",
		Short: "Make the system match the definitions and bring it current",
		Long: `Sync is the one command that changes the system. It shows what it will do,
asks once, and runs: prepares the declared sources, installs and removes
packages and Flatpaks as the definitions say, then upgrades the system with
dnf5 upgrade and flatpak update. Native tools print their own progress. At the
end it reports what differed from the plan and records receipts under
/var/lib/nimbus.

  -p, --plan        show the plan and change nothing
  -y, --yes         do not ask
  -n, --no-upgrade  only the definition changes, no system updates
  -r, --prune       also remove the unmanaged packages the plan lists
  -j, --json        machine-readable output; asks nothing`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, opts, flags, sf)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVarP(&sf.plan, "plan", "p", false, "show the plan and change nothing")
	cmd.Flags().BoolVarP(&sf.yes, "yes", "y", false, "do not ask")
	cmd.Flags().BoolVarP(&sf.noUpgrade, "no-upgrade", "n", false, "only the definition changes, no system updates")
	cmd.Flags().BoolVarP(&sf.prune, "prune", "r", false, "also remove the unmanaged packages the plan lists")
	return cmd
}

type syncResult struct {
	Digest      string   `json:"digest"`
	Executed    []string `json:"executed"`
	Differences []string `json:"differences"`
	Upgraded    bool     `json:"upgraded"`
	Failed      string   `json:"failed,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func runSync(cmd *cobra.Command, opts *options, flags machineFlags, sf syncFlags) error {
	return runSyncWith(cmd, opts, flags, sf, nil)
}

// runSyncWith runs sync: refresh, plan, ask once, prepare sources, install,
// upgrade, report. A lock already held by the caller (the selection
// commands hold it across the manifest write) is reused and released here.
func runSyncWith(cmd *cobra.Command, opts *options, flags machineFlags, sf syncFlags, held *apply.Lock) error {
	out := cmd.OutOrStdout()
	// Native tools print their progress to the terminal; in JSON mode that
	// goes to stderr so stdout stays one envelope.
	errOut := cmd.ErrOrStderr()
	execOut := out
	if opts.json {
		execOut = errOut
		sf.yes = true
	}
	defer func() {
		if held != nil {
			held.Release()
		}
	}()
	s, err := loadSelected(flags)
	if err != nil {
		return err
	}
	src := newSource()
	// The run refreshes metadata first so the plan and the upgrade are
	// exact; the cache is the user's and needs no privilege. Plan-only
	// reads the cache as it is and touches nothing.
	if !sf.plan {
		if _, err := src.Run("dnf5", "makecache"); err != nil {
			fmt.Fprintf(errOut, "metadata not refreshed: %v\n", err)
		}
	}
	p, applied, err := planWithState(s, src, sf.prune)
	if err != nil {
		return err
	}
	if sf.plan {
		if opts.json {
			if err := writeJSON(out, p, nil); err != nil {
				return err
			}
		} else {
			out.Write(renderPlan(p, sf.prune, !sf.noUpgrade))
			fmt.Fprintln(out, "\nfrom the local metadata cache; sync refreshes it before it runs")
		}
		if !p.Complete {
			return reported{}
		}
		return nil
	}
	if !p.Complete {
		if !opts.json {
			out.Write(renderPlan(p, sf.prune, !sf.noUpgrade))
		}
		return errors.New("the plan has problems; see above")
	}
	if nothingToRun(p) && sf.noUpgrade {
		if opts.json {
			return writeJSON(out, syncResult{Digest: p.Digest, Executed: []string{}, Differences: []string{}}, nil)
		}
		fmt.Fprintln(out, "nothing to do; the system matches the definitions")
		return nil
	}
	// Everything left waits for something outside this run, such as the
	// Chezmoi handoff: say so and touch nothing, sudo included.
	if runnable(p) == 0 && sf.noUpgrade {
		if opts.json {
			return writeJSON(out, syncResult{Digest: p.Digest, Executed: []string{}, Differences: []string{}}, nil)
		}
		out.Write(renderPlan(p, sf.prune, false))
		fmt.Fprintln(out, "\n"+waitingLine(p))
		return nil
	}
	// One decision, at the start: the plan as it is known now. On a fresh
	// host that names the packages; DNF prints the exact transaction as it
	// starts, and the report at the end names what differed.
	if !opts.json {
		out.Write(renderPlan(p, sf.prune, !sf.noUpgrade))
		if !sf.yes && !approver(cmd.InOrStdin(), out, p.Digest) {
			return errors.New("not applied")
		}
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock := held
	if lock == nil {
		lock, err = apply.Acquire(lockPath, apply.LockInfo{Command: "sync", Operation: p.Digest, PID: os.Getpid(), Started: time.Now().UTC()})
		if err != nil {
			return err
		}
	}
	held = nil
	defer lock.Release()
	// Another Nimbus run may have finished while the question was open;
	// the plan answered must still be the plan that runs.
	if fresh, _, err := planWithState(s, src, sf.prune); err != nil {
		return err
	} else if fresh.Digest != p.Digest {
		return errors.New("the system changed while the question was open and the plan with it; run sync again")
	}
	stage := filepath.Join(filepath.Dir(lockPath), "stage")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	// User-scope steps run without sudo; the credential is primed only when
	// a privileged command, a receipt, or the upgrade will need it.
	if needsSudo(p, sf.noUpgrade, applied.Present) {
		stopSudo, err := sudoKeepalive(src, execOut, errOut)
		if err != nil {
			return err
		}
		defer stopSudo()
	}
	options := func(p *plan.Plan) apply.Options {
		return apply.Options{
			Source: src, Fetch: newFetcher(), Record: newRecorder(src, stage), Keys: apply.ExtractKeysWithRPM2Archive(src),
			Stage: stage, Checkout: s.Checkout, Root: s.Checkout.Definitions(), FirstApply: !applied.Present,
			Engine: version.Engine, Definitions: state.Definitions{Origin: p.Checkout.Origin, Commit: p.Checkout.Commit, Dirty: p.Checkout.Dirty, Digest: p.Definitions},
			Out: execOut, ErrOut: errOut,
		}
	}
	result := syncResult{Digest: p.Digest, Differences: []string{}}
	fail := func(failed, msg string) error {
		result.Failed, result.Error = failed, msg
		if opts.json {
			if err := writeJSON(out, result, nil); err != nil {
				return err
			}
			return reported{}
		}
		fmt.Fprintf(out, "sync stopped at %s: %s\n", failed, msg)
		return reported{}
	}
	// Sources first, so the transactions that follow resolve against them;
	// then whatever the plan holds, in passes until nothing waits.
	for pass := 0; pass < 4; pass++ {
		if pass > 0 {
			if p, applied, err = planWithState(s, src, sf.prune); err != nil {
				return err
			}
			if !p.Complete {
				if !opts.json {
					out.Write(renderPlan(p, sf.prune, false))
				}
				return errors.New("the plan has problems; see above")
			}
			if nothingToRun(p) {
				break
			}
		}
		prep := sourceOperations(p)
		if len(prep) > 0 {
			r := apply.Run(&plan.Plan{Machine: p.Machine, Definitions: p.Definitions, Checkout: p.Checkout, Complete: true, Digest: p.Digest, Operations: prep}, options(p))
			result.Executed = append(result.Executed, r.Executed...)
			if r.Error != "" {
				return fail(r.Failed, r.Error)
			}
			applied.Present = true
			if changedRepositories(r.Executed) {
				if _, err := src.Run("dnf5", "makecache"); err != nil {
					return fmt.Errorf("refresh metadata: %w", err)
				}
			}
			if p, applied, err = planWithState(s, src, sf.prune); err != nil {
				return err
			}
			if !p.Complete {
				if !opts.json {
					out.Write(renderPlan(p, sf.prune, false))
				}
				return errors.New("the plan has problems; see above")
			}
		}
		r := apply.Run(p, options(p))
		result.Executed = append(result.Executed, r.Executed...)
		result.Differences = append(result.Differences, r.Differences...)
		if r.Error != "" {
			return fail(r.Failed, r.Error)
		}
		if len(r.Pending) == 0 || len(r.Executed) == 0 {
			break
		}
	}
	if !sf.noUpgrade {
		fmt.Fprintln(execOut, "-> upgrade the system")
		if err := apply.Upgrade(options(p), s.Checkout.Definitions()); err != nil {
			return fail("upgrade", err.Error())
		}
		result.Upgraded = true
	}
	if opts.json {
		return writeJSON(out, result, nil)
	}
	if len(result.Executed) == 0 {
		fmt.Fprintln(out, "done: nothing applied")
	} else {
		fmt.Fprintf(out, "done: %d operations applied; receipts recorded under %s\n", len(result.Executed), stateRoot)
	}
	if w := waitingLine(p); w != "" {
		fmt.Fprintln(out, w)
	}
	if len(result.Differences) == 0 {
		fmt.Fprintln(out, "differences from the plan: none")
	} else {
		fmt.Fprintln(out, "differences from the plan:")
		for _, d := range result.Differences {
			fmt.Fprintf(out, "  %s\n", d)
		}
	}
	return nil
}

// sourceOperations are the runnable operations that prepare package
// sources: the DNF drop-in, repositories, and Flatpak remotes.
func sourceOperations(p *plan.Plan) []plan.Operation {
	var ops []plan.Operation
	for _, op := range p.Operations {
		switch op.Kind {
		case plan.KindDNFConfig, plan.KindRepository, plan.KindFlatpakRemote:
			if op.After == "" && op.Blocked == "" && op.Action != plan.ActionKeep {
				ops = append(ops, op)
			}
		}
	}
	return ops
}

// changedRepositories reports whether a pass enabled or repaired a DNF
// repository, which is when the next plan needs fresh metadata.
func changedRepositories(executed []string) bool {
	for _, id := range executed {
		if strings.HasPrefix(id, "repository:") {
			return true
		}
	}
	return false
}

// runnable counts the operations this run can execute now: not kept, not
// blocked, and not waiting for an earlier operation or the handoff.
func runnable(p *plan.Plan) int {
	n := 0
	for _, op := range p.Operations {
		if op.Action != plan.ActionKeep && op.Blocked == "" && op.After == "" {
			n++
		}
	}
	return n
}

// needsSudo reports whether the run will invoke sudo: the upgrade, the
// baseline of a first run, or any operation that is not a user-scope step,
// since those run privileged commands or record receipts.
func needsSudo(p *plan.Plan, noUpgrade, statePresent bool) bool {
	if !noUpgrade || !statePresent {
		return true
	}
	for _, op := range p.Operations {
		if op.Action != plan.ActionKeep && op.Kind != plan.KindUser {
			return true
		}
	}
	return false
}

// waitingLine names what the pending operations wait for, or "" when
// nothing waits.
func waitingLine(p *plan.Plan) string {
	n := 0
	var targets []string
	for _, op := range p.Operations {
		if op.After == "" || op.Blocked != "" {
			continue
		}
		n++
		if d := describeAfter(p, op.After); !contains(targets, d) {
			targets = append(targets, d)
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d operations wait for %s; nothing else to run now", n, strings.Join(targets, ", then "))
}

func nothingToRun(p *plan.Plan) bool {
	for _, op := range p.Operations {
		if op.Action != plan.ActionKeep {
			return false
		}
	}
	return true
}

// planWithState builds the plan with the applied state read as the user.
func planWithState(s *selected, src facts.Source, prune bool) (*plan.Plan, *state.Applied, error) {
	applied, err := state.Read(stateRoot)
	if err != nil {
		return nil, nil, err
	}
	f := facts.Inspect(src, s.Root)
	p, err := plan.Build(plan.Inputs{Resolved: s.Resolved, Root: s.Checkout.Definitions(), Definitions: s.Checkout.Digest(), Facts: f, Applied: applied, Source: src, Prune: prune})
	if err != nil {
		return nil, nil, err
	}
	return p, applied, nil
}

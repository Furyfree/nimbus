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
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/snapper"
	"github.com/Furyfree/nimbus/internal/state"
	"github.com/Furyfree/nimbus/internal/version"
)

type syncFlags struct {
	plan, yes, systemUpgrade, prune bool
	approvedDigest                  string
	approvedCheckout                *inspect.Checkout
	definitionsDigest               string
}

func newSync(opts *options) *cobra.Command {
	var flags machineFlags
	var sf syncFlags
	var upgrade, noUpgrade bool
	cmd := &cobra.Command{
		Use: "sync", Short: "Make the system match the definitions",
		Long: "Show and apply system changes from the selected definitions. Use --upgrade to run Topgrade after reconciliation succeeds. --json controls output only; mutation still requires --yes.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if upgrade && noUpgrade {
				return usageError{errors.New("--upgrade and --no-upgrade exclude each other")}
			}
			if upgrade && opts.json {
				return usageError{errors.New("sync --upgrade delegates interactive output to Topgrade and does not support --json")}
			}
			if upgrade && os.Getenv(upgradeActive) != "" {
				return errors.New("recursive upgrade refused; Topgrade must call nimbus upgrade --system")
			}
			if err := runSync(cmd, opts, flags, sf); err != nil {
				return err
			}
			if upgrade {
				return runTopgrade(cmd, nil, sf.plan, flags)
			}
			return nil
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVarP(&sf.plan, "plan", "p", false, "show the plan and change nothing")
	cmd.Flags().BoolVarP(&sf.yes, "yes", "y", false, "approve the displayed changes")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "run Topgrade after a successful sync")
	cmd.Flags().BoolVarP(&noUpgrade, "no-upgrade", "n", false, "compatibility alias; sync already omits general updates")
	_ = cmd.Flags().MarkHidden("no-upgrade")
	cmd.Flags().BoolVarP(&sf.prune, "prune", "r", false, "also remove the unmanaged packages the plan lists")
	return cmd
}

func runSync(cmd *cobra.Command, opts *options, flags machineFlags, sf syncFlags) error {
	return runSyncWith(cmd, opts, flags, sf, nil)
}

// runSyncWith runs sync: refresh, plan, approve, prepare sources, install,
// upgrade, report. A lock already held by the caller (the selection
// commands hold it across the manifest write) remains owned by the caller.
func runSyncWith(cmd *cobra.Command, opts *options, flags machineFlags, sf syncFlags, held *apply.Lock) (retErr error) {
	if opts.json && !sf.plan && !sf.yes {
		return usageError{errors.New("mutation with --json requires --yes; use --plan to inspect changes")}
	}

	out := cmd.OutOrStdout()
	result := syncResult{Executed: []string{}, Differences: []string{}}
	phase := "preflight"
	var currentPlan *plan.Plan
	if !sf.plan {
		defer func() {
			result.finish(phase, retErr, currentPlan)
			var reportErr error
			if opts.json {
				reportErr = writeJSON(out, result, nil)
			} else {
				reportErr = result.render(out, sf.systemUpgrade)
			}
			if reportErr != nil {
				if errors.Is(retErr, reported{}) {
					retErr = errors.New(result.Error)
				}
				retErr = errors.Join(retErr, reportErr)
				return
			}
			if retErr != nil {
				retErr = reported{}
			}
		}()
	}
	// Native tools print their progress to the terminal; in JSON mode that
	// goes to stderr so stdout stays one envelope.
	errOut := cmd.ErrOrStderr()
	execOut := out
	if opts.json {
		execOut = errOut
	}
	s, err := loadSelected(flags)
	if err != nil {
		return err
	}
	src := newSource()
	if opts.installLog != nil && !sf.plan {
		src = installSource{src, opts.installLog, unlogged(cmd.ErrOrStderr())}
	}
	if !sf.plan {
		if err := inspect.CheckPlatform(src, s.Checkout.Definitions().Compatibility.Fedora); err != nil {
			return err
		}
	}
	if sf.definitionsDigest != "" && s.Checkout.Digest() != sf.definitionsDigest {
		return errors.New("definitions changed during initialization; run init again")
	}
	// Execution refreshes native metadata; previews only read the cache.
	if !sf.plan && sf.approvedDigest == "" {
		if _, err := src.Run("dnf5", "makecache"); err != nil {
			if _, writeErr := fmt.Fprintf(errOut, "metadata not refreshed: %v\n", err); writeErr != nil {
				return errors.Join(err, writeErr)
			}
			result.Steps = append(result.Steps, runStep{Name: "metadata refresh", Status: "warning", Detail: err.Error() + "; using cached metadata"})
		}
	}
	p, _, err := planWithState(s, src, sf.prune)
	if err != nil {
		return err
	}
	if sf.systemUpgrade {
		p = systemUpgradePlan(p)
	}
	currentPlan = p
	result.Digest = p.Digest
	if !sf.plan && (p.Checkout.Origin == "" || p.Checkout.Commit == "") {
		return errors.New("checkout origin and commit could not be inspected; sync requires an inspectable Git clone")
	}
	if sf.plan {
		if opts.json {
			if err := writeJSON(out, p, nil); err != nil {
				return err
			}
		} else {
			if _, err := out.Write(renderPlan(p, sf.prune, sf.systemUpgrade)); err != nil {
				return fmt.Errorf("show plan: %w", err)
			}
			if _, err := fmt.Fprintln(out, "\nfrom the local metadata cache; sync refreshes it before it runs"); err != nil {
				return err
			}
		}
		if !p.Complete {
			return reported{}
		}
		return nil
	}
	if sf.approvedCheckout != nil && !sameCheckoutIdentity(p.Checkout, *sf.approvedCheckout) {
		return errors.New("checkout identity changed since approval; run the command again")
	}
	if sf.approvedDigest != "" && p.Digest != sf.approvedDigest {
		return errors.New("the approved plan changed; run the command again")
	}
	if !p.Complete {
		if !opts.json {
			if _, err := out.Write(renderPlan(p, sf.prune, sf.systemUpgrade)); err != nil {
				return fmt.Errorf("show plan: %w", err)
			}
		}
		return errors.New("the plan has problems; see above")
	}
	if nothingToRun(p) && !sf.systemUpgrade {
		if opts.json {
			return nil
		}
		_, err := fmt.Fprintln(out, "nothing to do; the system matches the definitions")
		return err
	}
	// Report unresolved dependencies without starting a partial run.
	if runnable(p) == 0 && !sf.systemUpgrade && !snapshotConfigurationPending(p) {
		if opts.json {
			if !nothingToRun(p) {
				return errors.New(waitingLine(p))
			}
			return nil
		}
		if _, err := out.Write(renderPlan(p, sf.prune, false)); err != nil {
			return fmt.Errorf("show plan: %w", err)
		}
		if _, err := fmt.Fprintln(out, "\n"+waitingLine(p)); err != nil {
			return err
		}
		return errors.New(waitingLine(p))
	}
	// One decision, at the start: the plan as it is known now. On a fresh
	// host that names the packages; DNF prints the exact transaction as it
	// starts, and the report at the end names what differed.
	if !opts.json {
		if _, err := out.Write(renderPlan(p, sf.prune, sf.systemUpgrade)); err != nil {
			return fmt.Errorf("show plan: %w", err)
		}
	}
	if !sf.yes {
		if !approver(cmd.InOrStdin(), out, p.Digest) {
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
	if held == nil {
		defer func() { _ = lock.Release() }()
	}
	// Another Nimbus run may have finished while the question was open;
	// the plan answered must still be the plan that runs.
	freshSelection, err := loadSelected(flags)
	if err != nil {
		return fmt.Errorf("reload approved definitions: %w", err)
	}
	if freshSelection.Root != s.Root || freshSelection.Resolved.Machine != s.Resolved.Machine {
		return errors.New("the selection changed while the question was open; run sync again")
	}
	fresh, applied, err := planWithState(freshSelection, src, sf.prune)
	if err != nil {
		return err
	}
	if sf.systemUpgrade {
		fresh = systemUpgradePlan(fresh)
	}
	if !sameCheckoutIdentity(fresh.Checkout, p.Checkout) {
		return errors.New("checkout identity changed while the question was open; run sync again")
	}
	if fresh.Digest != p.Digest {
		return errors.New("the system changed while the question was open and the plan with it; run sync again")
	}
	if err := inspect.CheckPlatform(src, freshSelection.Checkout.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	p, currentPlan = fresh, fresh
	approvedCheckout := p.Checkout
	s = freshSelection
	stage := filepath.Join(filepath.Dir(lockPath), "stage")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	// User-scope steps run without sudo; the credential is primed only when
	// a privileged command, a receipt, or the upgrade will need it.
	snapshotWork := p.Snapshots != nil && (sf.systemUpgrade || systemChanges(p))
	if needsSudo(p, sf.systemUpgrade, applied.Present) || snapshotWork {
		stopSudo, err := sudoKeepalive(src, execOut, errOut)
		if err != nil {
			return err
		}
		defer stopSudo()
	}
	if snapshotWork && !p.Snapshots.Setup {
		phase = "snapper"
		if _, err := fmt.Fprintln(execOut, "-> create Snapper before snapshot"); err != nil {
			return err
		}
		pre, err := snapper.Create(src, "pre", "")
		if err != nil {
			return fmt.Errorf("create before snapshot; system changes stopped: %w", err)
		}
		result.Steps = append(result.Steps, runStep{Name: "snapper pre", Status: "succeeded", Detail: pre})
		defer func() {
			if retErr != nil && result.Error == "" {
				result.Failed, result.Error = phase, retErr.Error()
			}
			post, postErr := snapper.Create(src, "post", pre)
			cleanupErr := snapper.Cleanup(src)
			for _, step := range []struct {
				name, detail string
				err          error
			}{
				{"snapper post", post, postErr}, {"snapper cleanup", "number retention", cleanupErr},
			} {
				if step.err == nil {
					result.Steps = append(result.Steps, runStep{Name: step.name, Status: "succeeded", Detail: step.detail})
				} else {
					result.Failures = append(result.Failures, apply.Failure{ID: step.name, Error: step.err.Error()})
					if result.Error == "" {
						result.Failed, result.Error = step.name, step.err.Error()
					}
					retErr = errors.Join(retErr, step.err)
				}
			}
		}()
		if err := p.Snapshots.Configure(src); err != nil {
			return err
		}
		if changes := p.Snapshots.Changes(); len(changes) > 0 {
			result.Steps = append(result.Steps, runStep{Name: "snapper settings", Status: "succeeded", Detail: strings.Join(changes, ", ")})
		}
	}
	options := func(p *plan.Plan) apply.Options {
		var upgradePreview *plan.Transaction
		if p.Updates.Unavailable == "" {
			upgradePreview = &plan.Transaction{}
			for _, row := range p.Updates.Available {
				upgradePreview.Packages = append(upgradePreview.Packages, plan.TxPackage{Name: row.Name, Arch: row.Arch, EVR: row.EVR, Repository: row.Repository, Section: "upgrading"})
			}
		}
		return apply.Options{
			Constraints: s.Resolved.Constraints,
			Source:      src, Fetch: newFetcher(), Record: newRecorder(src, stage), Keys: apply.ExtractKeysWithRPM2Archive(src),
			Stage: stage, Checkout: s.Checkout, Root: s.Checkout.Definitions(), FirstApply: !applied.Present,
			Engine: version.Engine, Definitions: state.Definitions{Origin: p.Checkout.Origin, Commit: p.Checkout.Commit, Dirty: p.Checkout.Dirty, Digest: p.Definitions},
			Out: execOut, ErrOut: errOut, UpgradePreview: upgradePreview,
		}
	}
	fail := func(failed, msg string) error {
		result.Failed, result.Error = failed, msg
		return reported{}
	}
	// Sources first, so the transactions that follow resolve against them;
	// then whatever the plan holds, in passes until nothing waits.
	phase = "apply"
	for pass := 0; !sf.systemUpgrade && pass < 4; pass++ {
		if pass > 0 {
			if p, applied, err = replanUnchanged(s, flags, src, sf.prune, approvedCheckout); err != nil {
				return err
			}
			currentPlan = p
			if !p.Complete {
				if _, err := execOut.Write(renderPlan(p, sf.prune, false)); err != nil {
					return fmt.Errorf("show updated plan: %w", err)
				}
				return errors.New("the plan has problems; see above")
			}
			if err := showReplanned(execOut, p, sf.prune, &result); err != nil {
				return err
			}
			if nothingToRun(p) {
				break
			}
		}
		currentPlan = p
		prep := sourceOperations(p)
		if len(prep) > 0 {
			r := apply.Run(&plan.Plan{Machine: p.Machine, Definitions: p.Definitions, Checkout: p.Checkout, Complete: true, Digest: p.Digest, Operations: prep}, options(p))
			result.Reboot = result.Reboot || r.Reboot
			result.Logout = result.Logout || r.Logout
			result.Executed = append(result.Executed, r.Executed...)
			result.Differences = append(result.Differences, r.Differences...)
			result.Failures = append(result.Failures, r.Failures...)
			if r.Error != "" {
				return fail(r.Failed, r.Error)
			}
			applied.Present = true
			if changedRepositories(r.Executed) {
				if _, err := src.Run("dnf5", "makecache"); err != nil {
					return fmt.Errorf("refresh metadata: %w", err)
				}
			}
			if p, applied, err = replanUnchanged(s, flags, src, sf.prune, approvedCheckout); err != nil {
				return err
			}
			currentPlan = p
			if !p.Complete {
				if _, err := execOut.Write(renderPlan(p, sf.prune, false)); err != nil {
					return fmt.Errorf("show updated plan: %w", err)
				}
				return errors.New("the plan has problems; see above")
			}
		}
		if len(prep) > 0 {
			if err := showReplanned(execOut, p, sf.prune, &result); err != nil {
				return err
			}
		}
		currentPlan = p
		executable := *p
		executable.Operations = slices.Clone(p.Operations)
		for i := range executable.Operations {
			if executable.Operations[i].Kind == plan.KindUser && slices.Contains(result.Executed, executable.Operations[i].ID) {
				executable.Operations[i].Action = plan.ActionKeep
			}
		}
		r := apply.Run(&executable, options(p))
		result.Reboot = result.Reboot || r.Reboot
		result.Logout = result.Logout || r.Logout
		result.Executed = append(result.Executed, r.Executed...)
		result.Differences = append(result.Differences, r.Differences...)
		result.Failures = append(result.Failures, r.Failures...)
		if r.Error != "" {
			result.Failed, result.Error = r.Failed, r.Error
			if !onlyUserFailures(r, p) {
				return reported{}
			}
			break
		}
		if p.RepositoryReconciliation != "" {
			if err := reconcileRepositories(s, flags, src, approvedCheckout, options, execOut, &result); err != nil {
				return err
			}
		}
		if len(r.Pending) == 0 || len(r.Executed) == 0 {
			break
		}
	}
	if sf.systemUpgrade {
		phase = "upgrade"
		if _, err := fmt.Fprintln(execOut, "-> upgrade the system"); err != nil {
			return err
		}
		r := apply.Upgrade(options(p), s.Checkout.Definitions())
		result.Executed = append(result.Executed, r.Executed...)
		result.Differences = append(result.Differences, r.Differences...)
		result.Failures = append(result.Failures, r.Failures...)
		if r.Error != "" {
			return fail(r.Failed, r.Error)
		}
		result.Upgraded = true
		if p.RepositoryReconciliation != "" {
			if err := reconcileRepositories(s, flags, src, approvedCheckout, options, execOut, &result); err != nil {
				return err
			}
		}
	}
	if result.Error != "" {
		return reported{}
	}
	for _, op := range p.Operations {
		if op.After != "" && op.Action != plan.ActionKeep && !slices.Contains(result.Executed, op.ID) {
			return fail("dependencies", waitingLine(p))
		}
	}
	if p.Snapshots != nil {
		phase = "snapper"
		if p.Snapshots.Setup {
			if _, err := fmt.Fprintln(execOut, "-> initialize Snapper root configuration (this setup run was not snapshotted)"); err != nil {
				return err
			}
			if err := p.Snapshots.Configure(src); err != nil {
				return err
			}
			result.Steps = append(result.Steps, runStep{Name: "snapper setup", Status: "succeeded"})
		}
		// Re-read native state rather than claiming configuration from command exit alone.
		var template []byte
		for _, file := range s.Resolved.Files {
			if file.Target == snapper.Template {
				template = file.Content
			}
		}
		configured, err := snapper.Inspect(src, template)
		if err != nil {
			return err
		}
		if configured.Setup || len(configured.Changes()) != 0 {
			return errors.New("Snapper configuration did not converge; run sync again")
		}
	}
	return nil
}

func snapshotConfigurationPending(p *plan.Plan) bool {
	return p.Snapshots != nil && (p.Snapshots.Setup || len(p.Snapshots.Changes()) > 0)
}

func systemChanges(p *plan.Plan) bool {
	return snapshotConfigurationPending(p) || slices.ContainsFunc(p.Operations, func(op plan.Operation) bool {
		return op.Kind != plan.KindUser && op.Action != plan.ActionKeep && op.Action != plan.ActionAdopt && op.Action != plan.ActionRetire
	})
}

// replanUnchanged refreshes facts while requiring the approved definitions
// and selection to remain unchanged for the entire run.
func replanUnchanged(s *selected, flags machineFlags, src native.Source, prune bool, approvedCheckout inspect.Checkout) (*plan.Plan, *state.Applied, error) {
	fresh, err := loadSelected(flags)
	if err != nil {
		return nil, nil, err
	}
	if fresh.Root != s.Root || fresh.Resolved.Machine != s.Resolved.Machine || fresh.Checkout.Digest() != s.Checkout.Digest() {
		return nil, nil, errors.New("definitions or selection changed during sync; run sync again")
	}
	p, applied, err := planWithState(fresh, src, prune)
	if err != nil {
		return nil, nil, err
	}
	if !sameCheckoutIdentity(p.Checkout, approvedCheckout) {
		return nil, nil, errors.New("checkout identity changed during sync; run sync again")
	}
	return p, applied, nil
}

func sameCheckoutIdentity(a, b inspect.Checkout) bool {
	return a.Origin != "" && a.Commit != "" && a.Root == b.Root && a.Origin == b.Origin && a.Commit == b.Commit
}

// sourceOperations are the runnable operations that prepare package
// sources and constraints: the DNF drop-in, version locks, repositories,
// and Flatpak remotes.
func sourceOperations(p *plan.Plan) []plan.Operation {
	var ops []plan.Operation
	for _, op := range p.Operations {
		if plan.IsConstraintOperation(op) && op.Blocked == "" && op.Action != plan.ActionKeep {
			ops = append(ops, op)
			continue
		}
		switch op.Kind {
		case plan.KindDNFConfig, plan.KindRepository, plan.KindFlatpakRemote:
			if op.After == "" && op.Blocked == "" && op.Action != plan.ActionKeep && op.Action != plan.ActionRemove && op.Action != plan.ActionRetire {
				ops = append(ops, op)
			}
		}
	}
	return ops
}

// changedRepositories reports whether a pass enabled or repaired a DNF
// repository, which is when the next plan needs fresh metadata.
func changedRepositories(executed []string) bool {
	return slices.ContainsFunc(executed, func(id string) bool { return strings.HasPrefix(id, "repository:") })
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
func needsSudo(p *plan.Plan, systemUpgrade, statePresent bool) bool {
	if systemUpgrade || !statePresent {
		return true
	}
	return slices.ContainsFunc(p.Operations, func(op plan.Operation) bool { return op.Action != plan.ActionKeep && op.Kind != plan.KindUser })
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
		if d := describeAfter(p, op.After); !slices.Contains(targets, d) {
			targets = append(targets, d)
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d operations wait for %s; nothing else to run now", n, strings.Join(targets, ", then "))
}

func nothingToRun(p *plan.Plan) bool {
	return !snapshotConfigurationPending(p) && !slices.ContainsFunc(p.Operations, func(op plan.Operation) bool { return op.Action != plan.ActionKeep })
}

// planWithState builds the plan with the applied state read as the user.
func planWithState(s *selected, src native.Source, prune bool) (*plan.Plan, *state.Applied, error) {
	applied, err := state.Read(stateRoot)
	if err != nil {
		return nil, nil, err
	}
	f := inspect.Inspect(src, s.Root)
	p, err := plan.Build(plan.Inputs{Resolved: s.Resolved, Root: s.Checkout.Definitions(), Definitions: s.Checkout.Digest(), Facts: f, Applied: applied, Source: src, Prune: prune})
	if err != nil {
		return nil, nil, err
	}
	return p, applied, nil
}

func onlyUserFailures(r *apply.Result, p *plan.Plan) bool {
	if len(r.Failures) == 0 {
		return false
	}
	return !slices.ContainsFunc(r.Failures, func(failure apply.Failure) bool {
		return !slices.ContainsFunc(p.Operations, func(op plan.Operation) bool { return op.ID == failure.ID && op.Kind == plan.KindUser })
	})
}

func showReplanned(out io.Writer, p *plan.Plan, prune bool, result *syncResult) error {
	if _, err := fmt.Fprintln(out, "updated plan after completed operations:"); err != nil {
		return fmt.Errorf("show updated plan: %w", err)
	}
	if _, err := out.Write(renderPlan(p, prune, false)); err != nil {
		return fmt.Errorf("show updated plan: %w", err)
	}
	for _, op := range p.Operations {
		for _, note := range op.Notes {
			message := "replanned " + op.ID + ": " + note
			if !slices.Contains(result.Differences, message) {
				result.Differences = append(result.Differences, message)
			}
		}
	}
	return nil
}

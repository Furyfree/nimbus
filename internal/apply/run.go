package apply

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// Options are everything the executor needs beyond the plan. Every side
// effect goes through one of these so tests replace them.
type Options struct {
	Context        context.Context
	Constraints    []definitions.PackageConstraint
	UpgradePreview *plan.Transaction

	Source native.Source
	// Fetch downloads a URL. Apply is the one command allowed to reach the
	// network for keys, release packages, and remote definitions.
	Fetch func(url string) ([]byte, error)
	// Record is the privileged record action: the real one runs
	// `sudo nimbus internal record`; tests write to a temporary root.
	Record func(planDigest string, st *state.Stage) error
	// Keys extracts the key files from a downloaded release RPM. The real
	// one runs rpm2archive through Source and reads the archive.
	Keys func(rpmPath string) (map[string][]byte, error)
	// Stage is a user-owned directory for verified downloads, below the
	// runtime directory.
	Stage string
	// Checkout supplies key files stored in the definition tree.
	Checkout *definitions.Checkout
	// Root is the checkout's definitions, for repository declarations.
	Root definitions.Root
	// FirstApply records the baseline of installed packages with the first
	// receipt, marking everything present now as pre-existing.
	FirstApply bool
	Engine     string
	// ErrOut receives the stderr of native commands; nil means Out.
	ErrOut      io.Writer
	Definitions state.Definitions
	Out         io.Writer
	Now         func() time.Time
}

func (o Options) canceled() error {
	if o.Context != nil {
		return o.Context.Err()
	}
	return nil
}

// Failure identifies one unsuccessful operation.
type Failure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// Result preserves completed work and every failure. Independent user tools
// continue after a failure; a system operation failure stops the run.
type Result struct {
	Reboot   bool      `json:"reboot,omitzero"`
	Logout   bool      `json:"logout,omitzero"`
	Failures []Failure `json:"failures,omitempty"`
	Executed []string  `json:"executed"`
	Pending  []string  `json:"pending"`
	Failed   string    `json:"failed,omitempty"`
	Error    string    `json:"error,omitempty"`
	// Differences lists deviations from the preview, including unexpected
	// package changes and temporary payload cleanup failures.
	Differences []string `json:"differences,omitempty"`
}

// Run executes every runnable operation of a complete plan in order. It
// assumes the caller holds the operation lock and has re-verified the plan
// digest immediately before.
func Run(p *plan.Plan, opts Options) *Result {
	r := &Result{}
	if !p.Complete {
		r.Error = "the plan is incomplete; resolve its blocked operations first"
		return r
	}
	ex := &executor{p: p, opts: opts}
	if ex.opts.Now == nil {
		ex.opts.Now = time.Now
	}
	ex.opts.Out = cmp.Or(ex.opts.Out, io.Discard)
	if err := ex.snapshotPackages(); err != nil {
		r.Error = err.Error()
		return r
	}
	var deferredFileRemovals []string
	for _, op := range p.Operations {
		if err := opts.canceled(); err != nil {
			r.Failed, r.Error = op.ID, err.Error()
			return r
		}
		if op.After != "" {
			r.Pending = append(r.Pending, op.ID)
			continue
		}
		if op.Action == plan.ActionKeep {
			continue
		}
		if _, err := fmt.Fprintf(ex.opts.Out, "-> %s\n", op.Summary); err != nil {
			r.Failed, r.Error = op.ID, "write operation progress: "+err.Error()
			r.Failures = append(r.Failures, Failure{ID: op.ID, Error: r.Error})
			return r
		}
		receipts, remove, err := ex.execute(op)
		r.Differences = append(r.Differences, ex.differences...)
		ex.differences = nil
		if err != nil {
			r.Failures = append(r.Failures, Failure{ID: op.ID, Error: err.Error()})
			if r.Error == "" {
				r.Failed, r.Error = op.ID, err.Error()
			}
			_, _ = fmt.Fprintf(ex.opts.Out, "   failed: %v\n", err)
			if op.Kind == plan.KindUser {
				continue
			}
			return r
		}
		if op.Resource != nil && op.Resource.Before != op.Resource.After {
			r.Reboot = r.Reboot || op.Kind == plan.KindTarget
			r.Logout = r.Logout || op.Kind == plan.KindGroup
		}
		for _, receipt := range receipts {
			r.Reboot = r.Reboot || receipt.Reboot
			r.Logout = r.Logout || receipt.Logout
		}
		if op.Kind == plan.KindFile && op.Action == plan.ActionRemove && op.File != nil && len(op.File.Triggers) > 0 {
			deferredFileRemovals = append(deferredFileRemovals, remove...)
			remove = nil
		}
		st := &state.Stage{Schema: state.Schema, PlanDigest: p.Digest, Receipts: receipts, Remove: remove, Time: ex.opts.Now().UTC()}
		if ex.opts.FirstApply && !ex.baselineDone {
			st.Baseline = &state.Baseline{Schema: state.BaselineSchema, Recorded: st.Time, Packages: ex.baseline()}
			ex.baselineDone = true
		}
		if len(receipts) > 0 || len(remove) > 0 || st.Baseline != nil {
			if err := ex.opts.Record(p.Digest, st); err != nil {
				r.Failed = op.ID
				r.Error = "operation applied and verified, but its receipt was not recorded: " + err.Error()
				_, _ = fmt.Fprintf(ex.opts.Out, "   %s; inspect the live resource and restore its reviewed previous state before retrying.\n", r.Error)
				r.Failures = append(r.Failures, Failure{ID: op.ID, Error: r.Error})
				return r
			}
		}
		r.Executed = append(r.Executed, op.ID)
	}
	if len(deferredFileRemovals) > 0 {
		if err := ex.opts.Record(p.Digest, &state.Stage{Schema: state.Schema, PlanDigest: p.Digest, Remove: deferredFileRemovals, Time: ex.opts.Now().UTC()}); err != nil {
			r.Failed = deferredFileRemovals[0]
			r.Executed = slices.DeleteFunc(r.Executed, func(id string) bool { return slices.Contains(deferredFileRemovals, id) })
			r.Error = "file retirement was applied but not recorded; inspect the live resource and restore its reviewed previous state before retrying: " + err.Error()
			for _, id := range deferredFileRemovals {
				r.Failures = append(r.Failures, Failure{ID: id, Error: r.Error})
			}
			_, _ = fmt.Fprintf(ex.opts.Out, "   %s\n", r.Error)
		}
	}
	return r
}

type executor struct {
	p            *plan.Plan
	opts         Options
	seen         map[string]inspect.Package // installed packages, kept current
	before       []string                   // package names at the start, sorted
	baselineDone bool
	differences  []string // deviations from the last operation's preview
}

// sudo runs one privileged native command with its output on the terminal,
// so DNF's and Flatpak's own progress stays visible.
func (ex *executor) sudo(argv ...string) error {
	if _, err := fmt.Fprintf(ex.opts.Out, "   $ sudo %s\n", strings.Join(argv, " ")); err != nil {
		return fmt.Errorf("write command progress: %w", err)
	}
	errOut := cmp.Or(ex.opts.ErrOut, ex.opts.Out)
	return ex.opts.Source.Stream(ex.opts.Out, errOut, "sudo", argv...)
}

func (ex *executor) receipt(op plan.Operation, provider, previous, intended, verification string) state.Receipt {
	return state.Receipt{Schema: state.ReceiptSchema, Engine: ex.opts.Engine, Definitions: ex.opts.Definitions, Machine: ex.p.Machine,
		Resource: op.ID, Provider: provider, Paths: op.Paths, Previous: previous, Intended: intended, Operation: op.Action,
		PlanDigest: ex.p.Digest, Verified: true, Verification: verification, Timestamp: ex.opts.Now().UTC()}
}

func (ex *executor) execute(op plan.Operation) (receipts []state.Receipt, remove []string, err error) {
	switch {
	case (op.Kind == plan.KindRepository || op.Kind == plan.KindFlatpakRemote) && (op.Action == plan.ActionRemove || op.Action == plan.ActionRetire):
		return ex.sourceRetirement(op)
	case op.Kind == plan.KindFile || op.Kind == plan.KindService || op.Kind == plan.KindGroup || op.Kind == plan.KindTarget || op.Kind == plan.KindTrigger:
		return ex.systemResource(op)
	case op.Kind == plan.KindUser:
		return nil, nil, ex.userTool(op)
	case op.Kind == plan.KindDNFConfig:
		return ex.dnfConfig(op)
	case op.Kind == plan.KindRepository:
		return ex.repository(op)
	case op.Kind == plan.KindFlatpakRemote:
		return ex.flatpakRemote(op)
	case op.Kind == plan.KindFlatpak && (op.Action == plan.ActionInstall || op.Action == plan.ActionAdopt):
		return ex.flatpakApp(op)
	case op.Kind == plan.KindFlatpak && op.Action == plan.ActionRemove:
		return ex.flatpakRemove(op)
	case (op.Kind == plan.KindPackage || op.Kind == plan.KindFlatpak) && op.Action == plan.ActionRetire:
		if name := op.AbsentPackage; name != "" {
			var present bool
			if op.Kind == plan.KindPackage {
				packages := inspect.Packages(ex.opts.Source)
				if !packages.Known() {
					return nil, nil, fmt.Errorf("verify absence of %s: installed packages are unknown: %s", name, packages.Error)
				}
				_, present = inspect.FindPackage(packages.Value, name)
			} else {
				flatpak := inspect.SystemFlatpak(ex.opts.Source)
				if !flatpak.Known() {
					return nil, nil, fmt.Errorf("verify absence of %s: Flatpak state is unknown: %s", name, flatpak.Error)
				}
				present = slices.ContainsFunc(flatpak.Value.Apps, func(app inspect.FlatpakApp) bool { return app.ID == name })
			}
			if present {
				return nil, nil, fmt.Errorf("%s is installed again; replan before retiring its receipt", name)
			}
		}
		return nil, []string{op.ID}, nil
	case op.Kind == plan.KindPackage && op.Action == plan.ActionAdopt:
		name := op.ID[strings.LastIndexByte(op.ID, ':')+1:]
		name = cmp.Or(op.Resolved[name], name)
		inst, ok := inspect.FindPackage(slices.Collect(maps.Values(ex.seen)), name)
		if !ok {
			return nil, nil, fmt.Errorf("%s is no longer installed", name)
		}
		r := ex.receipt(op, "dnf", "installed "+inst.EVR()+" ("+inst.FromRepo+")", "installed", "dnf5 repoquery --installed lists it")
		r.Package = inst.ID()
		return []state.Receipt{r}, nil, nil
	case op.ID == "packages:install":
		return ex.installTransaction(op)
	case op.Kind == plan.KindPackage && (op.Action == plan.ActionRemove || op.Action == plan.ActionPrune):
		return ex.removeTransaction(op)
	}
	return nil, nil, fmt.Errorf("operation %s (%s %s) has no executor", op.ID, op.Kind, op.Action)
}

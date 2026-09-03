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
	// approver reads the interactive answer. Tests replace it.
	approver = func(in io.Reader, out io.Writer, digest string) bool {
		fmt.Fprintf(out, "apply plan %s? [y/N] ", digest)
		reader := bufio.NewReader(in)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(strings.ToLower(line)) == "y"
	}
)

func newApply(opts *options) *cobra.Command {
	var flags machineFlags
	var prune bool
	var approve string
	cmd := &cobra.Command{
		Use:   "apply [--prune] [--approve DIGEST]",
		Short: "Apply the reviewed plan for the selected machine",
		Long: `Apply shows the complete plan, requires explicit approval, takes the
operation lock, re-inspects the system, refuses a plan whose digest changed,
runs the exact native commands the plan showed, verifies each result, and
records a receipt for every verified operation. A failed operation stops the
run; earlier receipts stay. With --prune the unmanaged packages plan --prune
listed are removed as well. --approve DIGEST approves that exact plan without
a prompt; the digest comes from nimbus plan.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.json && approve == "" {
				return usageError{errors.New("apply --json needs --approve DIGEST; JSON output cannot answer the approval prompt")}
			}
			return runApply(cmd, opts, flags, prune, approve)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVar(&prune, "prune", false, "also remove the unmanaged packages plan --prune lists")
	cmd.Flags().StringVar(&approve, "approve", "", "approve exactly this plan digest without a prompt")
	return cmd
}

type applyResult struct {
	Digest   string   `json:"digest"`
	Executed []string `json:"executed"`
	Pending  []string `json:"pending"`
	Failed   string   `json:"failed,omitempty"`
	Error    string   `json:"error,omitempty"`
	Rounds   int      `json:"rounds"`
}

func runApply(cmd *cobra.Command, opts *options, flags machineFlags, prune bool, approve string) error {
	return runApplyWith(cmd, opts, flags, prune, approve, nil)
}

// runApplyWith runs apply; a lock already held by the caller (the selection
// commands hold it across the manifest write) is reused and released here.
func runApplyWith(cmd *cobra.Command, opts *options, flags machineFlags, prune bool, approve string, held *apply.Lock) error {
	out := cmd.OutOrStdout()
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
	result := applyResult{}
	for round := 1; ; round++ {
		result.Rounds = round
		p, applied, err := planWithState(s, src, prune)
		if err != nil {
			return err
		}
		if !p.Complete {
			if !opts.json {
				out.Write(renderPlan(p, prune))
			}
			return errors.New("the plan is incomplete; resolve the blocked operations shown above")
		}
		if nothingToRun(p) {
			if round == 1 {
				fmt.Fprintln(out, "nothing to apply; the system matches the definitions")
			}
			break
		}
		if !opts.json {
			out.Write(renderPlan(p, prune))
		}
		switch {
		case approve != "" && round == 1 && approve == p.Digest:
			fmt.Fprintf(out, "approved by --approve %s\n", p.Digest)
		case approve != "" && round == 1:
			return fmt.Errorf("--approve %s does not match the current plan %s; review the plan and approve its digest", approve, p.Digest)
		case opts.json:
			return errors.New("a further round needs interactive approval; run nimbus apply again")
		default:
			if !approver(cmd.InOrStdin(), out, p.Digest) {
				return errors.New("not approved")
			}
		}

		lockPath, err := apply.LockPath()
		if err != nil {
			return err
		}
		lock := held
		if lock == nil {
			lock, err = apply.Acquire(lockPath, apply.LockInfo{Command: "apply", Operation: p.Digest, PID: os.Getpid(), Started: time.Now().UTC()})
			if err != nil {
				return err
			}
		}
		held = nil
		// Re-resolve and re-inspect under the lock; the approved digest must
		// still describe the system.
		fresh, _, err := planWithState(s, src, prune)
		if err != nil {
			lock.Release()
			return err
		}
		if fresh.Digest != p.Digest {
			lock.Release()
			return fmt.Errorf("the plan changed before execution (%s is now %s); review and approve again", p.Digest, fresh.Digest)
		}
		stage := filepath.Join(filepath.Dir(lockPath), "stage")
		if err := os.MkdirAll(stage, 0o700); err != nil {
			lock.Release()
			return err
		}
		r := apply.Run(fresh, apply.Options{
			Source: src, Fetch: newFetcher(), Record: newRecorder(src, stage), Keys: apply.ExtractKeysWithRPM2Archive(src),
			Stage: stage, Checkout: s.Checkout, Root: s.Checkout.Definitions(), FirstApply: !applied.Present,
			Engine: version.Engine, Definitions: state.Definitions{Origin: fresh.Checkout.Origin, Commit: fresh.Checkout.Commit, Dirty: fresh.Checkout.Dirty, Digest: fresh.Definitions},
			Out: out,
		})
		lock.Release()
		result.Digest = fresh.Digest
		result.Executed = append(result.Executed, r.Executed...)
		result.Pending = r.Pending
		if r.Error != "" {
			result.Failed, result.Error = r.Failed, r.Error
			break
		}
		if len(r.Pending) == 0 {
			break
		}
		// Pending operations wait for repositories this round enabled; the
		// new metadata is needed before their transaction can be reviewed.
		fmt.Fprintf(out, "%d operations waited for this round; refreshing metadata and planning again\n", len(r.Pending))
		if _, err := src.Run("dnf5", "makecache"); err != nil {
			return fmt.Errorf("refresh metadata for the next round: %w", err)
		}
		approve = ""
	}
	if opts.json {
		if err := writeJSON(out, result, nil); err != nil {
			return err
		}
		if result.Error != "" {
			return reported{}
		}
		return nil
	}
	if result.Error != "" {
		fmt.Fprintf(out, "apply stopped at %s: %s\n", result.Failed, result.Error)
		return reported{}
	}
	if len(result.Executed) > 0 {
		fmt.Fprintf(out, "applied %d operations in %d round(s); receipts recorded under %s\n", len(result.Executed), result.Rounds, stateRoot)
	}
	return nil
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

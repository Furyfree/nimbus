package apply

import (
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type retiringSource struct {
	*scripted
	refs string
}

func (s *retiringSource) Run(name string, args ...string) ([]byte, error) {
	if facts.Key(name, args...) == "flatpak list --system --all --app --runtime --columns=ref,origin,options" {
		return []byte(s.refs), nil
	}
	return s.scripted.Run(name, args...)
}

func (s *retiringSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	if facts.Key(name, args...) == "sudo flatpak remote-delete --system old" {
		s.log = append(s.log, facts.Key(name, args...))
		if msg := s.fail["remote-delete"]; msg != "" {
			return errors.New(msg)
		}
		if !s.privilegedNoop {
			s.remotes = nil
		}
		return nil
	}
	return s.scripted.Stream(out, errOut, name, args...)
}

func retirementExecutor(kind string) (*executor, *retiringSource, plan.Operation) {
	src := &retiringSource{scripted: newScripted()}
	native := "nimbus-old"
	argv := []string{"dnf5", "config-manager", "setopt", "nimbus-old.enabled=0"}
	if kind == plan.KindFlatpakRemote {
		native = "old"
		src.remotes = []string{native}
		argv = []string{"flatpak", "remote-delete", "--system", native}
	} else {
		src.Dirs[facts.RepoDir] = []string{"nimbus-old.repo"}
		src.Files[filepath.Join(facts.RepoDir, "nimbus-old.repo")] = []byte("[nimbus-old]\nbaseurl=https://example.invalid/repo\nenabled=1\ngpgcheck=1\n")
	}
	f := facts.Inspect(src, "")
	op := plan.Operation{ID: kind + ":old", Kind: kind, Action: plan.ActionRemove,
		Source: &state.SourceOwnership{Applied: plan.SourceSnapshot(kind, []string{native}, f)},
		Steps:  []plan.Step{{Argv: argv, Privileged: true}}}
	ex := &executor{opts: Options{Source: src, Out: io.Discard}, p: &plan.Plan{Machine: "vm"}}
	return ex, src, op
}

func TestSourceRetirementVerifiesNativeDisableOrRemoval(t *testing.T) {
	for _, kind := range []string{plan.KindRepository, plan.KindFlatpakRemote} {
		t.Run(kind, func(t *testing.T) {
			ex, src, op := retirementExecutor(kind)
			receipts, removed, err := ex.execute(op)
			if err != nil || len(receipts) != 0 || len(removed) != 1 || removed[0] != op.ID {
				t.Fatalf("retirement: %v %v %v", receipts, removed, err)
			}
			if kind == plan.KindRepository {
				if _, ok := src.Files[filepath.Join(facts.RepoDir, "nimbus-old.repo")]; !ok {
					t.Fatal("retirement deleted a repository file")
				}
			}
		})
	}
}

func TestSourceRetirementKeepsReceiptOnNativeFailureOrFalseSuccess(t *testing.T) {
	for _, kind := range []string{plan.KindRepository, plan.KindFlatpakRemote} {
		for _, which := range []string{"failure", "noop", "changed identity", "new dependency"} {
			t.Run(kind+"/"+which, func(t *testing.T) {
				ex, src, op := retirementExecutor(kind)
				switch which {
				case "failure":
					src.fail["sudo dnf5"] = "failed"
					src.fail["remote-delete"] = "failed"
				case "noop":
					src.privilegedNoop = true
				case "changed identity":
					op.Source.Applied[0].URL = "https://different.invalid/repo"
				case "new dependency":
					if kind == plan.KindRepository {
						// Native read failure must be equally conservative.
						src.fail["dnf5"] = "installed inventory unavailable"
					} else {
						src.refs = "org.example.Platform/x86_64/stable\told\truntime\n"
					}
				}
				receipts, removed, err := ex.execute(op)
				if err == nil || len(receipts) != 0 || len(removed) != 0 {
					t.Fatalf("unverified retirement succeeded: %v %v %v", receipts, removed, err)
				}
				if which == "changed identity" || which == "new dependency" {
					if i := slices.IndexFunc(src.log, func(call string) bool { return strings.HasPrefix(call, "sudo ") }); i >= 0 {
						t.Fatalf("changed source was mutated: %s", src.log[i])
					}
				}
			})
		}
	}
}

func TestRepairReceiptPreservesOriginalSourceOwnership(t *testing.T) {
	ex, _, op := retirementExecutor(plan.KindRepository)
	op.Source.Original = []state.NativeSource{{ID: "nimbus-old", Enabled: false}}
	var receipt state.Receipt
	recordSourceOwnership(&receipt, op, plan.SourceIDs(op.Source), facts.Inspect(ex.opts.Source, ""))
	if receipt.Source == nil || len(receipt.Source.Original) != 1 || receipt.Source.Original[0].Enabled || !receipt.Source.Applied[0].Enabled {
		t.Fatal("repair forgot original disabled state")
	}
}

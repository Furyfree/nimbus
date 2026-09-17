package apply

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type shellSource struct {
	*nativetest.FakeSource
	current   inspect.LoginShell
	noEffect  bool
	fail      bool
	mutations int
}

func (s *shellSource) Run(name string, args ...string) ([]byte, error) {
	if nativetest.Key(name, args...) == "getent --service=files passwd owner" {
		return fmt.Appendf(nil, "owner:x:%d:1000::/home/owner:%s\n", s.current.UID, s.current.Shell), nil
	}
	return s.FakeSource.Run(name, args...)
}

func (s *shellSource) Stream(_, _ io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	if key != "sudo usermod --shell /bin/zsh -- owner" {
		return fmt.Errorf("unexpected mutation %s", key)
	}
	s.mutations++
	if s.fail {
		return errors.New("usermod failed")
	}
	if !s.noEffect {
		s.current.Shell = "/bin/zsh"
	}
	return nil
}

func TestLoginShellApplyVerifyRepairAndRetire(t *testing.T) {
	src := &shellSource{current: inspect.LoginShell{UID: 1000, Shell: "/bin/bash"}, FakeSource: &nativetest.FakeSource{
		Commands: map[string][]byte{
			"test -x /bin/zsh": nil,
			nativetest.Key("dnf5", inspect.PackageQueryArgs...): nil,
		}, Files: map[string][]byte{"/etc/shells": []byte("/bin/bash\n/bin/zsh\n")},
	}}
	applied := &state.Applied{Receipts: map[string]state.Receipt{}}
	resolved := &definitions.Resolved{Machine: "vm", Shell: "zsh"}
	build := func() *plan.Plan {
		t.Helper()
		p, err := plan.Build(plan.Inputs{Resolved: resolved, Facts: &inspect.Facts{User: inspect.Section[inspect.User]{Value: inspect.User{Name: "owner"}}}, Source: src, Applied: applied})
		if err != nil || !p.Complete || len(p.Operations) != 1 {
			t.Fatalf("login-shell plan: %+v, %v", p, err)
		}
		return p
	}
	apply := func(p *plan.Plan) *Result {
		t.Helper()
		return Run(p, Options{Source: src, Out: io.Discard, Record: func(_ string, stage *state.Stage) error {
			for _, receipt := range stage.Receipts {
				applied.Receipts[receipt.Resource] = receipt
			}
			return nil
		}})
	}
	p := build()
	if src.mutations != 0 {
		t.Fatal("planning changed the shell")
	}
	result := apply(p)
	if result.Error != "" || !result.Logout || src.current.Shell != "/bin/zsh" || src.mutations != 1 {
		t.Fatalf("apply: %+v current=%+v mutations=%d", result, src.current, src.mutations)
	}
	receipt := applied.Receipts["login-shell:owner"]
	if !receipt.Verified || !receipt.Logout || !strings.Contains(receipt.Previous, "/bin/bash") {
		t.Fatalf("missing verified original state: %+v", receipt)
	}
	if next := build(); next.Operations[0].Action != plan.ActionKeep {
		t.Fatalf("did not converge: %+v", next)
	}
	src.current.Shell = "/bin/bash"
	if result := apply(build()); result.Error != "" || src.mutations != 2 {
		t.Fatalf("drift repair: %+v", result)
	}
	resolved.Shell = ""
	op := build().Operations[0]
	receipts, retired, err := resourceExecutor(src).systemResource(op)
	if err != nil || len(receipts) != 0 || len(retired) != 1 || retired[0] != "login-shell:owner" || src.mutations != 2 || src.current.Shell != "/bin/zsh" {
		t.Fatalf("retirement changed the account: receipts=%v retired=%v err=%v", receipts, retired, err)
	}
}

func TestLoginShellFailedOrStaleChangeHasNoReceipt(t *testing.T) {
	for _, scenario := range []string{"command failure", "no effect", "changed shell", "changed UID", "removed executable", "removed allowlist"} {
		t.Run(scenario, func(t *testing.T) {
			before := inspect.LoginShell{UID: 1000, Shell: "/bin/bash"}
			after := inspect.LoginShell{UID: 1000, Shell: "/bin/zsh"}
			src := &shellSource{current: before, FakeSource: &nativetest.FakeSource{
				Commands: map[string][]byte{"test -x /bin/zsh": nil}, Files: map[string][]byte{"/etc/shells": []byte("/bin/zsh\n")},
			}}
			op := plan.Operation{ID: "login-shell:owner", Kind: plan.KindShell, Action: plan.ActionRepair,
				Resource: &plan.ResourceChange{User: "owner", Before: encodeTest(before), After: encodeTest(after), Previous: encodeTest(before)},
				Steps:    []plan.Step{{Argv: []string{"usermod", "--shell", "/bin/zsh", "--", "owner"}, Privileged: true}}}
			switch scenario {
			case "command failure":
				src.fail = true
			case "no effect":
				src.noEffect = true
			case "changed shell":
				src.current.Shell = "/bin/sh"
			case "changed UID":
				src.current.UID = 1001
			case "removed executable":
				delete(src.Commands, "test -x /bin/zsh")
			case "removed allowlist":
				delete(src.Files, "/etc/shells")
			}
			receipts, retired, err := resourceExecutor(src).systemResource(op)
			if err == nil || len(receipts) != 0 || len(retired) != 0 {
				t.Fatalf("failed change was recorded: receipts=%v retired=%v err=%v", receipts, retired, err)
			}
			if scenario != "command failure" && scenario != "no effect" && src.mutations != 0 {
				t.Fatalf("stale state caused mutation: %s", scenario)
			}
		})
	}
}

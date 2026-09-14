package apply

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type greeterTestSource struct {
	*nativetest.FakeSource
	enabled, fail, ineffective, unreadable bool
	mutations, privilegedReads             int
}

func (s *greeterTestSource) Run(name string, args ...string) ([]byte, error) {
	key := nativetest.Key(name, args...)
	if key == inspect.GreeterBinary+" passwordless-sync status owner" {
		return nil, errors.New("failed to open the Polkit rules directory: Permission denied")
	}
	if key == "sudo -- "+inspect.GreeterBinary+" passwordless-sync status owner" {
		s.privilegedReads++
		if s.unreadable {
			return nil, errors.New("unrecognized native rule")
		}
		prefix := "not enabled"
		if s.enabled {
			prefix = "enabled"
		}
		return fmt.Appendf(nil, "owner: %s by the noctalia-greeter managed rule\nOther administrator-authored Polkit rules are not included in this status.\n", prefix), nil
	}
	return s.FakeSource.Run(name, args...)
}

func (s *greeterTestSource) Stream(_, _ io.Writer, name string, args ...string) error {
	if nativetest.Key(name, args...) != "sudo -- "+inspect.GreeterBinary+" passwordless-sync enable owner" {
		return fmt.Errorf("unexpected mutation %s", nativetest.Key(name, args...))
	}
	s.mutations++
	if s.fail {
		return errors.New("native enable failed")
	}
	if !s.ineffective {
		s.enabled = true
	}
	return nil
}

func greeterApplySource() *greeterTestSource {
	return &greeterTestSource{FakeSource: &nativetest.FakeSource{Commands: map[string][]byte{
		"getent --service=files passwd owner":                []byte("owner:x:1000:1000::/home/owner:/bin/bash\n"),
		"rpm -q --queryformat %{VERSION} noctalia":           []byte("5.1.0"),
		inspect.GreeterHelper + " --supports secure-sync-v1": []byte("secure-sync-v1\n"),
		nativetest.Key("dnf5", inspect.PackageQueryArgs...):  nil,
	}, Files: map[string][]byte{inspect.GreeterPolicy: []byte(`<policyconfig><action id="org.noctalia.greeter.sync-appearance"><annotate key="org.freedesktop.policykit.exec.path">/usr/bin/noctalia-greeter-apply-appearance</annotate><annotate key="org.freedesktop.policykit.exec.argv1">--sync</annotate></action></policyconfig>`)}}}
}

func greeterApplyPlan(t *testing.T, src *greeterTestSource, applied *state.Applied) *plan.Plan {
	t.Helper()
	p, err := plan.Build(plan.Inputs{Resolved: &definitions.Resolved{Machine: "vm", GreeterPasswordlessSync: "hyprland-session"}, Facts: &inspect.Facts{User: inspect.Section[inspect.User]{Value: inspect.User{Name: "owner"}}}, Source: src, Applied: applied})
	if err != nil || !p.Complete || len(p.Operations) != 1 {
		t.Fatalf("plan: %+v error=%v", p, err)
	}
	return p
}

func TestGreeterAuthorizationApplyRepeatAndRepair(t *testing.T) {
	src := greeterApplySource()
	applied := &state.Applied{Receipts: map[string]state.Receipt{}}
	p := greeterApplyPlan(t, src, applied)
	if src.privilegedReads != 0 || src.mutations != 0 {
		t.Fatal("preview requested privilege")
	}
	options := Options{Source: src, Out: io.Discard, Record: func(_ string, st *state.Stage) error {
		for _, r := range st.Receipts {
			applied.Receipts[r.Resource] = r
		}
		return nil
	}}
	if r := Run(p, options); r.Error != "" || src.mutations != 1 || !src.enabled {
		t.Fatalf("first apply: %+v", r)
	}
	if r := applied.Receipts["greeter-sync:owner"]; !r.Verified || r.Previous != encodeTest(inspect.GreeterSync{UID: 1000, Known: true}) {
		t.Fatalf("receipt: %+v", r)
	}
	if r := Run(greeterApplyPlan(t, src, applied), options); r.Error != "" || src.mutations != 1 {
		t.Fatalf("repeat rewrote rule: %+v", r)
	}
	src.enabled = false
	if r := Run(greeterApplyPlan(t, src, applied), options); r.Error != "" || src.mutations != 2 {
		t.Fatalf("receipt hid authorization drift: %+v", r)
	}
	op := p.Operations[0]
	op.Action = plan.ActionRetire
	before := src.privilegedReads
	r, removed, err := resourceExecutor(src).systemResource(op)
	if err != nil || len(r) != 0 || len(removed) != 1 || src.mutations != 2 || src.privilegedReads != before || !src.enabled {
		t.Fatalf("retire changed authorization: %v %v %v", r, removed, err)
	}
}

func TestGreeterFailuresNeverRecordCompletion(t *testing.T) {
	for _, scenario := range []string{"enable failure", "ineffective enable", "unreadable policy", "changed UID", "downgraded helper"} {
		t.Run(scenario, func(t *testing.T) {
			src := greeterApplySource()
			p := greeterApplyPlan(t, src, nil)
			switch scenario {
			case "enable failure":
				src.fail = true
			case "ineffective enable":
				src.ineffective = true
			case "unreadable policy":
				src.unreadable = true
			case "changed UID":
				src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1001:1001::/home/owner:/bin/bash\n")
			case "downgraded helper":
				src.Commands[inspect.GreeterHelper+" --supports secure-sync-v1"] = []byte("unsupported")
			}
			recorded := false
			r := Run(p, Options{Source: src, Out: io.Discard, Record: func(string, *state.Stage) error { recorded = true; return nil }})
			if r.Error == "" || recorded {
				t.Fatalf("failure recorded: %+v", r)
			}
			if scenario != "enable failure" && scenario != "ineffective enable" && src.mutations != 0 {
				t.Fatal("failed prerequisite mutated authorization")
			}
			// Retry after restoring the native prerequisite must succeed.
			src.fail, src.ineffective, src.unreadable = false, false, false
			src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/bash\n")
			src.Commands[inspect.GreeterHelper+" --supports secure-sync-v1"] = []byte("secure-sync-v1\n")
			if retry := Run(greeterApplyPlan(t, src, nil), Options{Source: src, Out: io.Discard, Record: func(string, *state.Stage) error { return nil }}); retry.Error != "" {
				t.Fatalf("retry: %+v", retry)
			}
		})
	}
}

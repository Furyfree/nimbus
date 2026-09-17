package cli

import (
	"errors"
	"reflect"

	"github.com/Furyfree/nimbus/internal/plan"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

// approvedGreeterSource follows an announced administrator inspection during
// execution (or init plan approval). It permits one fixed read-only query for the selected account;
// preview, status and unrelated commands keep the ordinary source.
type approvedGreeterSource struct {
	native.Source
	user string
}

func (s approvedGreeterSource) Run(name string, args ...string) ([]byte, error) {
	if name == inspect.GreeterBinary && len(args) == 3 && args[0] == "passwordless-sync" && args[1] == "status" && args[2] == s.user {
		out, err := s.Source.Run("sudo", "--", inspect.GreeterBinary, "passwordless-sync", "status", s.user)
		if err != nil {
			err = errors.Join(inspect.ErrGreeterAdminStatus, err)
		}
		return out, err
	}
	return s.Source.Run(name, args...)
}

// A provisional init approval covers resolving the announced greeter check,
// not unrelated drift appearing while administrator inspection is in progress.
func sameNonGreeterOperations(before, after *plan.Plan) bool {
	withoutGreeter := func(p *plan.Plan) []plan.Operation {
		var result []plan.Operation
		for _, op := range p.Operations {
			if op.Kind != plan.KindGreeterSync {
				result = append(result, op)
			}
		}
		return result
	}
	return reflect.DeepEqual(withoutGreeter(before), withoutGreeter(after)) && reflect.DeepEqual(before.Prune, after.Prune) && reflect.DeepEqual(before.Snapshots, after.Snapshots)
}

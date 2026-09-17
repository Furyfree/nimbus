package apply

import (
	"encoding/json"
	"fmt"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func (ex *executor) greeterSync(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Action != plan.ActionRepair && op.Action != plan.ActionAdopt && op.Action != plan.ActionRetire {
		return nil, nil, fmt.Errorf("unsupported greeter authorization action %s", op.Action)
	}
	c := op.Resource
	var desired inspect.GreeterSync
	if c == nil || json.Unmarshal([]byte(c.After), &desired) != nil || desired.UID == 0 || !desired.Known || !desired.Enabled || op.ID != plan.KindGreeterSync+":"+c.User {
		return nil, nil, fmt.Errorf("invalid greeter authorization payload")
	}
	account, err := inspect.ObserveLoginShell(ex.opts.Source, c.User)
	if err != nil || account.UID != desired.UID {
		return nil, nil, fmt.Errorf("greeter authorization account identity changed after approval")
	}
	if op.Action == plan.ActionRetire {
		return nil, []string{op.ID}, nil
	}
	if err := inspect.CheckGreeterSync(ex.opts.Source); err != nil {
		return nil, nil, err
	}
	before, err := inspect.ObserveGreeterSync(ex.opts.Source, c.User, true)
	if err != nil {
		return nil, nil, err
	}
	if before.UID != desired.UID {
		return nil, nil, fmt.Errorf("greeter authorization account changed during inspection")
	}
	if !before.Enabled {
		if err := ex.sudo("--", inspect.GreeterBinary, "passwordless-sync", "enable", c.User); err != nil {
			return nil, nil, fmt.Errorf("enable greeter appearance sync: %w; retry after inspecting native status", err)
		}
	}
	after, err := inspect.ObserveGreeterSync(ex.opts.Source, c.User, true)
	if err != nil || after != desired {
		return nil, nil, fmt.Errorf("greeter authorization verification failed; no completion recorded; inspect native status and retry: %v", err)
	}
	previous := c.Previous
	var recorded inspect.GreeterSync
	if json.Unmarshal([]byte(previous), &recorded) != nil || !recorded.Known {
		data, _ := json.Marshal(before)
		previous = string(data)
	}
	return []state.Receipt{ex.receipt(op, plan.KindGreeterSync, previous, c.After, "native managed rule authorizes secure greeter appearance sync")}, nil, nil
}

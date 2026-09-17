package plan

import (
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestGreeterFreshInstallWaitsForPackages(t *testing.T) {
	b, src := resourceBuilder()
	b.in.Resolved.GreeterPasswordlessSync = "hyprland-session"
	src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/bash\n")
	op := b.greeterSync("packages:install")[0]
	if op.Blocked != "" || op.After != "packages:install" || op.Kind != KindGreeterSync || len(op.Steps) != 2 {
		t.Fatalf("fresh installation: %+v", op)
	}
	if op := b.greeterSync("")[0]; op.Blocked == "" {
		t.Fatal("missing native support did not block")
	}
	delete(src.Commands, "getent --service=files passwd owner")
	if op := b.greeterSync("packages:install")[0]; op.Blocked == "" {
		t.Fatal("missing local identity did not block")
	}
}

func TestGreeterReceiptCannotGrantAccessOrHideUnknownNativeState(t *testing.T) {
	b, src := resourceBuilder()
	b.in.Resolved.GreeterPasswordlessSync = "hyprland-session"
	src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/bash\n")
	id := "greeter-sync:owner"
	r := state.Receipt{Resource: id, Provider: KindGreeterSync, Machine: "vm", Verified: true, Intended: encodeResource(inspect.GreeterSync{UID: 1000, Known: true, Enabled: true})}
	b.in.Applied.Receipts[id] = r
	// Missing compatibility evidence remains blocked even with a receipt.
	if op := b.greeterSync("")[0]; op.Blocked == "" || op.Action == ActionKeep {
		t.Fatalf("receipt substituted for observation: %+v", op)
	}
	r.Intended = encodeResource(inspect.GreeterSync{UID: 1001, Known: true, Enabled: true})
	b.in.Applied.Receipts[id] = r
	if op := b.greeterSync("packages:install")[0]; op.Blocked == "" {
		t.Fatal("reused user name accepted a foreign UID")
	}
	r.Intended = encodeResource(inspect.GreeterSync{UID: 1000, Known: true, Enabled: true})
	b.in.Applied.Receipts[id] = r
	b.in.Resolved.GreeterPasswordlessSync = ""
	op := b.greeterSync("")[0]
	if op.Blocked != "" || op.Action != ActionRetire || len(op.Steps) != 0 {
		t.Fatalf("retirement must preserve native authorization: %+v", op)
	}
	r.Machine = "foreign"
	b.in.Applied.Receipts[id] = r
	if op := b.greeterSync("")[0]; op.Blocked == "" {
		t.Fatal("foreign receipt was retired")
	}
}

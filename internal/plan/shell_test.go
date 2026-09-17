package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestLoginShellPlan(t *testing.T) {
	b, src := resourceBuilder()
	b.in.Resolved.Shell = "zsh"
	src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/bash\n")
	src.Files["/etc/shells"] = []byte("/bin/bash\n/bin/zsh\n/usr/bin/zsh\n")
	src.Commands["test -x /bin/zsh"] = nil
	src.Commands["test -x /usr/bin/zsh"] = nil
	op := b.loginShell("")[0]
	if op.Blocked != "" || op.Action != ActionRepair || len(op.Steps) != 1 || !slices.Equal(op.Steps[0].Argv, []string{"usermod", "--shell", "/bin/zsh", "--", "owner"}) || !op.Steps[0].Privileged || !strings.Contains(op.Summary, "/bin/bash -> /bin/zsh") {
		t.Fatalf("missing reviewed shell change: %+v", op)
	}
	b.in.Applied.Receipts[op.ID] = state.Receipt{Resource: op.ID, Provider: KindShell, Machine: "vm", Verified: true, Previous: op.Resource.Before, Intended: op.Resource.After}
	if repair := b.loginShell("")[0]; repair.Action != ActionRepair || repair.Blocked != "" {
		t.Fatalf("drift was not repairable: %+v", repair)
	}
	src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/zsh\n")
	if keep := b.loginShell("")[0]; keep.Action != ActionKeep || len(keep.Steps) != 0 {
		t.Fatalf("matching shell did not converge: %+v", keep)
	}
	src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/usr/bin/zsh\n")
	if alias := b.loginShell("")[0]; alias.Action != ActionAdopt || len(alias.Steps) != 0 {
		t.Fatalf("merged-/usr spelling caused a mutation: %+v", alias)
	}
	b.in.Resolved.Shell = ""
	retire := b.loginShell("")[0]
	if retire.Action != ActionRetire || retire.Blocked != "" || len(retire.Steps) != 0 || retire.Resource.Before != retire.Resource.After {
		t.Fatalf("removing selection must leave the current login shell: %+v", retire)
	}
	b.in.Applied.Receipts = nil
	if ops := b.loginShell(""); len(ops) != 0 {
		t.Fatalf("omitted field must not inspect or manage a shell: %+v", ops)
	}
}

func TestLoginShellBlocksUnknownAccountsAndForeignReceipts(t *testing.T) {
	for _, scenario := range []string{"missing account", "changed UID", "foreign machine", "invalid receipt", "shell not installed"} {
		t.Run(scenario, func(t *testing.T) {
			b, src := resourceBuilder()
			b.in.Resolved.Shell = "zsh"
			src.Commands["getent --service=files passwd owner"] = []byte("owner:x:1000:1000::/home/owner:/bin/bash\n")
			src.Files["/etc/shells"] = []byte("/bin/bash\n/bin/zsh\n")
			src.Commands["test -x /bin/zsh"] = nil
			r := state.Receipt{Resource: "login-shell:owner", Provider: KindShell, Machine: "vm", Verified: true, Intended: encodeResource(inspect.LoginShell{UID: 1000, Shell: "/bin/zsh"})}
			switch scenario {
			case "missing account":
				delete(src.Commands, "getent --service=files passwd owner")
			case "changed UID":
				r.Intended = encodeResource(inspect.LoginShell{UID: 1001, Shell: "/bin/zsh"})
			case "foreign machine":
				r.Machine = "another"
			case "invalid receipt":
				r.Verified = false
			case "shell not installed":
				delete(src.Commands, nativetest.Key("test", "-x", "/bin/zsh"))
			}
			b.in.Applied.Receipts[r.Resource] = r
			if op := b.loginShell("")[0]; op.Blocked == "" {
				t.Fatalf("unsafe change was allowed: %+v", op)
			}
			if scenario == "shell not installed" {
				if op := b.loginShell("packages:install")[0]; op.Blocked != "" || op.After != "packages:install" {
					t.Fatalf("must wait for package installation then replan: %+v", op)
				}
			}
		})
	}
}

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type reconcileSource struct {
	*facts.FakeSource
	calls        []string
	leaveEnabled bool
	fail         bool
}

func (s *reconcileSource) Stream(_, _ io.Writer, name string, args ...string) error {
	call := facts.Key(name, args...)
	s.calls = append(s.calls, call)
	if name != "sudo" || len(args) != 4 || strings.Join(args[:3], " ") != "dnf5 config-manager setopt" {
		return fmt.Errorf("unexpected mutation: %s", call)
	}
	if s.fail {
		return errors.New("override failed")
	}
	if !s.leaveEnabled {
		id := strings.TrimSuffix(args[3], ".enabled=0")
		path := filepath.Join(facts.RepoOverride, "99-config_manager.repo")
		s.Dirs[facts.RepoOverride] = []string{"99-config_manager.repo"}
		s.Files[path] = append(s.Files[path], []byte("["+id+"]\nenabled=0\n")...)
	}
	return nil
}

func TestReconcileVendorRepositoriesAfterPackageTransaction(t *testing.T) {
	for _, mode := range []string{"success", "native failure", "verification failure", "key drift", "checkout drift"} {
		t.Run(mode, func(t *testing.T) {
			root := applyEnv(t)
			fake := fixtureSource(t, root)
			readyRepositories(t, fake, root)
			fake.Commands["dnf5 makecache"] = nil
			src := &reconcileSource{FakeSource: fake}
			flags := machineFlags{checkout: root, machine: "laptop"}
			s, err := loadSelected(flags)
			if err != nil {
				t.Fatal(err)
			}
			approved, _, err := planWithState(s, src, false)
			if err != nil {
				t.Fatal(err)
			}
			vendorFiles := map[string]string{}
			for id, host := range map[string]string{"chatgpt": "openai-chatgpt", "onepassword": "1password"} {
				name := host + ".repo"
				path := filepath.Join(facts.RepoDir, name)
				data := fmt.Sprintf("[%s]\nbaseurl=%s\nenabled=1\ngpgcheck=1\n", host, s.Checkout.Definitions().Repositories[id].BaseURL)
				vendorFiles[path] = data
				fake.Files[path] = []byte(data)
				fake.Dirs[facts.RepoDir] = append(fake.Dirs[facts.RepoDir], name)
			}
			switch mode {
			case "native failure":
				src.fail = true
			case "verification failure":
				src.leaveEnabled = true
			case "key drift":
				fake.Commands[facts.Key("gpg", facts.KeyInspectArgs(plan.KeyPath("chatgpt"))...)] = []byte("pub:::::::::\nfpr:::::::::AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA:\n")
			case "checkout drift":
				fake.Commands[facts.Key("git", facts.GitArgs(root, "rev-parse", "HEAD")...)] = []byte("different-head\n")
			}
			var out bytes.Buffer
			options := func(p *plan.Plan) apply.Options {
				return apply.Options{Source: src, Root: s.Checkout.Definitions(), Out: &out, Record: func(digest string, st *state.Stage) error {
					return state.Record(stateRoot, digest, st)
				}}
			}
			result := &syncResult{}
			err = reconcileRepositories(s, flags, src, approved.Checkout, options, &out, result)
			if mode != "success" {
				if err == nil {
					t.Fatal("reconciliation accepted a failed or unsafe state")
				}
				if (mode == "key drift" || mode == "checkout drift") && len(src.calls) != 0 {
					t.Fatalf("mutated after unapproved drift: %v", src.calls)
				}
				applied, readErr := state.Read(stateRoot)
				if readErr != nil || len(applied.Receipts) != 0 {
					t.Fatalf("failed reconciliation recorded success: %+v %v", applied, readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(src.calls) != 2 || len(result.Executed) != 2 || len(result.Differences) != 2 {
				t.Fatalf("unexpected reconciliation: calls=%v result=%+v", src.calls, result)
			}
			for path, before := range vendorFiles {
				if string(fake.Files[path]) != before {
					t.Fatalf("vendor file changed: %s", path)
				}
			}
			applied, err := state.Read(stateRoot)
			if err != nil || len(applied.Receipts) != 2 {
				t.Fatalf("missing verified receipts: %+v %v", applied, err)
			}
			if !strings.Contains(out.String(), "dnf5 config-manager setopt openai-chatgpt.enabled=0") || !strings.Contains(out.String(), "dnf5 config-manager setopt 1password.enabled=0") {
				t.Fatalf("exact overrides missing from output: %s", out.String())
			}
			if err := reconcileRepositories(s, flags, src, approved.Checkout, options, &out, result); err != nil || len(src.calls) != 2 {
				t.Fatalf("second pass did not converge: %v calls=%v", err, src.calls)
			}
		})
	}
}

func TestDuplicateReconciliationRejectsOtherRepositoryChanges(t *testing.T) {
	for _, op := range []plan.Operation{
		{Action: plan.ActionEnable},
		{Action: plan.ActionRepair, Blocked: "foreign file"},
		{Action: plan.ActionRepair, Steps: []plan.Step{{Description: "write repository", Argv: []string{"install", "new.repo", "owned.repo"}, Privileged: true}}},
		{Action: plan.ActionRepair, Steps: []plan.Step{{Description: plan.DisableDuplicateDescription, Argv: []string{"dnf5", "config-manager", "setopt", "nimbus-example.gpgcheck=0"}, Privileged: true}}},
	} {
		op.Kind = plan.KindRepository
		if _, err := duplicateRepositoryRepairs(&plan.Plan{Operations: []plan.Operation{op}}); err == nil {
			t.Fatalf("accepted unapproved operation: %+v", op)
		}
	}
}

type transactionReconcileSource struct {
	*reconcileSource
}

func (s *transactionReconcileSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	call := facts.Key(name, args...)
	if call != "sudo dnf5 -y install demo" && call != "sudo dnf5 -y upgrade" {
		return s.reconcileSource.Stream(out, errOut, name, args...)
	}
	s.calls = append(s.calls, call)
	host := "vendor-install"
	if call == "sudo dnf5 -y upgrade" {
		if !slices.Contains(s.calls, "sudo dnf5 config-manager setopt vendor-install.enabled=0") {
			return errors.New("upgrade started while install-created duplicate remained enabled")
		}
		host = "vendor-upgrade"
	}
	file := host + ".repo"
	s.Dirs[facts.RepoDir] = append(s.Dirs[facts.RepoDir], file)
	s.Files[filepath.Join(facts.RepoDir, file)] = []byte("[" + host + "]\nbaseurl=https://example.invalid/repo\nenabled=1\ngpgcheck=1\n")
	if host == "vendor-install" {
		key := facts.Key("dnf5", facts.PackageQueryArgs...)
		s.Commands[key] = append(s.Commands[key], []byte("demo|0|1|1|x86_64|nimbus-vendor|User\n")...)
	}
	return nil
}

func TestSyncReconcilesInstallAndUpgradeCreatedRepositories(t *testing.T) {
	root, base := installerFixture(t)
	rootFile := filepath.Join(root, "nimbus.toml")
	data, err := os.ReadFile(rootFile)
	if err != nil {
		t.Fatal(err)
	}
	declaration := "\n[repositories.vendor]\nkind=\"dnf\"\nbaseurl=\"https://example.invalid/repo\"\nkey_url=\"https://example.invalid/key.asc\"\nkey=\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\"\npriority=101\n"
	if err := os.WriteFile(rootFile, append(data, declaration...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid=\"common\"\npackages=[\"vendor:demo\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	readyRepositories(t, base.FakeSource, root)
	base.Commands["dnf5 --assumeno --cacheonly install demo"] = []byte("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n demo x86_64 1-1 nimbus-vendor 1 KiB\n\nTransaction Summary:\n")
	src := &transactionReconcileSource{reconcileSource: &reconcileSource{FakeSource: base.FakeSource}}
	withSource(t, src)
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK {
		t.Fatalf("sync failed: %d %s%s calls=%v", code, out, errOut, src.calls)
	}
	want := []string{
		"sudo dnf5 -y install demo",
		"sudo dnf5 config-manager setopt vendor-install.enabled=0",
		"sudo dnf5 -y upgrade",
		"sudo dnf5 config-manager setopt vendor-upgrade.enabled=0",
	}
	if strings.Join(src.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("transaction/reconciliation order: %v", src.calls)
	}
	applied, err := state.Read(stateRoot)
	if err != nil || applied.Receipts["repository:vendor"].Resource == "" {
		t.Fatalf("missing verified repository receipt: %+v %v", applied, err)
	}
	code, out, errOut = run(t, "sync", "--plan", "--checkout", root, "--machine", "vm", "--json")
	if code != ExitOK || strings.Contains(out, `"kind": "repository"`) {
		t.Fatalf("follow-up plan did not converge: %d %s%s", code, out, errOut)
	}
}

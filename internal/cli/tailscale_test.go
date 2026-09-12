package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/state"
)

type tailscaleSource struct {
	*postinstallSource
	applyPreference bool
}

func (s *tailscaleSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	err := s.postinstallSource.Stream(out, errOut, name, args...)
	if err == nil && s.applyPreference && nativetest.Key(name, args...) == "sudo -- /usr/bin/tailscale set --operator=test" {
		s.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":false,"OperatorUser":"test","Config":{"private":"do-not-render"}}`)
	}
	return err
}

func tailscaleFixture(t *testing.T) (string, *tailscaleSource) {
	t.Helper()
	root, base := postinstallFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['tailscale']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	base.Commands[key] = append(base.Commands[key], []byte("tailscale|0|1.98.8|1|x86_64|fedora|User\n")...)
	base.Paths["/usr/bin/tailscale"] = "/usr/bin/tailscale"
	base.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":false,"Config":{"private":"do-not-render"}}`)
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:tailscale", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}
	src := &tailscaleSource{postinstallSource: base, applyPreference: true}
	withSource(t, src)
	return root, src
}

func TestTailscaleOperatorPreviewApprovalVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"list", "json", "preview", "cancel", "set", "no effect", "failure", "changed operator", "changed user"} {
		t.Run(mode, func(t *testing.T) {
			root, src := tailscaleFixture(t)
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"tailscale-operator", "--yes"}
			switch mode {
			case "list", "json":
				args = nil
			case "preview":
				args = append(args, "--plan")
			case "no effect":
				src.applyPreference = false
			case "failure":
				src.streamErr = io.ErrUnexpectedEOF
			case "cancel", "changed operator", "changed user":
				args = []string{"tailscale-operator"}
				savedTerminal, savedApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "changed operator" {
						src.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":false,"OperatorUser":"another"}`)
					} else if mode == "changed user" {
						src.Commands["id -un"] = []byte("another\n")
					}
					return mode != "cancel"
				}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err = cmd.Execute()
			wantError := slices.Contains([]string{"cancel", "no effect", "failure", "changed operator", "changed user"}, mode)
			if (err != nil) != wantError {
				t.Fatalf("unexpected result: %v\n%s", err, out)
			}
			wantStream := slices.Contains([]string{"set", "no effect", "failure"}, mode)
			if wantStream {
				if !slices.Equal(src.streams, []string{"sudo -- /usr/bin/tailscale set --operator=test"}) {
					t.Fatalf("wrong native action: %v", src.streams)
				}
			} else if len(src.streams) != 0 {
				t.Fatalf("unapproved action: %v", src.streams)
			}
			if strings.HasPrefix(mode, "changed ") && !strings.Contains(err.Error(), "changed after approval") {
				t.Fatal(err)
			}
			if mode == "no effect" && !strings.Contains(err.Error(), "could not be verified") {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "do-not-render") || strings.Contains(out.String(), "WantRunning") {
				t.Fatal("private preferences were rendered")
			}
			if mode == "set" || mode == "no effect" || mode == "failure" {
				// A failed action can be retried; a successful one converges without
				// another command. An explicit native revocation makes it pending again.
				src.streamErr, src.applyPreference = nil, true
				cmd, out = postinstallCommand(root, false, "tailscale-operator", "--yes")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "test is already the local Tailscale operator") {
					t.Fatalf("retry: %v %s", err, out)
				}
				count := len(src.streams)
				if (mode == "set" && count != 1) || (mode != "set" && count != 2) {
					t.Fatalf("incorrect retry count: %v", src.streams)
				}
				src.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":false}`)
				cmd, out = postinstallCommand(root, false, "tailscale-operator", "--plan")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "tailscale-operator [pending]") || len(src.streams) != count {
					t.Fatalf("revocation status: %v %s", err, out)
				}
			}
			after, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("postinstall created a preference ownership receipt")
			}
		})
	}
}

func TestTailscaleOperatorRejectsForgedActions(t *testing.T) {
	for _, mode := range []string{"other binary", "extra option", "other user", "root", "other kind", "blocked"} {
		task := postinstall.Task{ID: "tailscale-operator", Status: postinstall.Pending, Action: postinstall.TailscaleOperatorAction("test")}
		switch mode {
		case "other binary":
			task.Action.Argv[2] = "/tmp/tailscale"
		case "extra option":
			task.Action.Argv = append(task.Action.Argv, "--ssh")
		case "other user":
			task.Action.Argv[4] = "--operator=other"
		case "root":
			task.Action.User = "root"
			task.Action.Argv[4] = "--operator=root"
		case "other kind":
			task.Action.Kind = postinstall.OpenApplication
		case "blocked":
			task.Status = postinstall.Blocked
		}
		if _, err := postinstallArgv(task); err == nil {
			t.Fatalf("accepted forged %s action", mode)
		}
	}
}

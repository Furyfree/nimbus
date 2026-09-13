package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
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

type accountPictureSource struct {
	*postinstallSource
	applyPicture bool
}

const accountPropertyPrefix = "/usr/bin/busctl --system --auto-start=no --allow-interactive-authorization=no --json=short get-property org.freedesktop.Accounts /org/freedesktop/Accounts/User1000 org.freedesktop.Accounts.User "

func accountSourcePath() string {
	return filepath.Join(os.Getenv("HOME"), ".config/noctalia/assets/profile-picture.jpg")
}
func accountActionKey() string {
	return "/usr/bin/busctl --system --auto-start=no --allow-interactive-authorization=yes call org.freedesktop.Accounts /org/freedesktop/Accounts/User1000 org.freedesktop.Accounts.User SetIconFile s " + accountSourcePath()
}
func (s *accountPictureSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	err := s.postinstallSource.Stream(out, errOut, name, args...)
	if err == nil && s.applyPicture && nativetest.Key(name, args...) == accountActionKey() {
		s.Commands[accountPropertyPrefix+"IconFile"] = []byte(`{"type":"s","data":"/var/lib/AccountsService/icons/test"}`)
		s.Files["/var/lib/AccountsService/icons/test"] = slices.Clone(s.Files[accountSourcePath()])
	}
	return err
}

func accountPictureFixture(t *testing.T) (string, *accountPictureSource) {
	t.Helper()
	root, base := postinstallFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['accountsservice']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	base.Commands[key] = append(base.Commands[key], []byte("accountsservice|0|23.13.9|1|x86_64|fedora|User\n")...)
	base.Paths["/usr/bin/busctl"] = "/usr/bin/busctl"
	base.Commands["id -u"] = []byte("1000\n")
	base.Commands[accountPropertyPrefix+"UserName"] = []byte(`{"type":"s","data":"test"}`)
	var img bytes.Buffer
	if err := jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	base.Files[accountSourcePath()] = img.Bytes()
	base.Commands[accountPropertyPrefix+"IconFile"] = []byte(`{"type":"s","data":""}`)
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:accountsservice", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}
	src := &accountPictureSource{postinstallSource: base, applyPicture: true}
	withSource(t, src)
	return root, src
}

func TestAccountPicturePreviewApprovalVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"list", "json", "preview", "cancel", "set", "no effect", "failure", "changed icon", "changed user", "changed source"} {
		t.Run(mode, func(t *testing.T) {
			root, src := accountPictureFixture(t)
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"account-picture", "--yes"}
			switch mode {
			case "list", "json":
				args = nil
			case "preview":
				args = append(args, "--plan")
			case "no effect":
				src.applyPicture = false
			case "failure":
				src.streamErr = io.ErrUnexpectedEOF
			case "cancel", "changed icon", "changed user", "changed source":
				args = []string{"account-picture"}
				savedTerminal, savedApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "changed icon" {
						src.Commands[accountPropertyPrefix+"IconFile"] = []byte(`{"type":"s","data":"/var/lib/AccountsService/icons/test"}`)
					} else if mode == "changed source" {
						src.Files[accountSourcePath()] = append(slices.Clone(src.Files[accountSourcePath()]), []byte("changed")...)
					} else if mode == "changed user" {
						src.Commands["id -un"] = []byte("another\n")
					}
					return mode != "cancel"
				}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err = cmd.Execute()
			wantError := slices.Contains([]string{"cancel", "no effect", "failure", "changed icon", "changed user", "changed source"}, mode)
			if (err != nil) != wantError {
				t.Fatalf("unexpected result: %v\n%s", err, out)
			}
			wantStream := slices.Contains([]string{"set", "no effect", "failure"}, mode)
			if wantStream {
				if !slices.Equal(src.streams, []string{accountActionKey()}) {
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
			if bytes.Contains(out.Bytes(), src.Files[accountSourcePath()]) {
				t.Fatal("image bytes were rendered")
			}
			if mode == "set" || mode == "no effect" || mode == "failure" {
				// A failed action can be retried; a successful one converges without
				// another command. An explicit native revocation makes it pending again.
				src.streamErr, src.applyPicture = nil, true
				cmd, out = postinstallCommand(root, false, "account-picture", "--yes")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "The account picture matches the Chezmoi source") {
					t.Fatalf("retry: %v %s", err, out)
				}
				count := len(src.streams)
				if (mode == "set" && count != 1) || (mode != "set" && count != 2) {
					t.Fatalf("incorrect retry count: %v", src.streams)
				}
				src.Commands[accountPropertyPrefix+"IconFile"] = []byte(`{"type":"s","data":""}`)
				cmd, out = postinstallCommand(root, false, "account-picture", "--plan")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "account-picture [pending]") || len(src.streams) != count {
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

func TestAccountPictureRejectsForgedActions(t *testing.T) {
	for _, mode := range []string{"other binary", "extra option", "other account", "root", "other kind", "blocked", "source path", "missing metadata"} {
		task := postinstall.Task{ID: "account-picture", Status: postinstall.Pending, Action: postinstall.AccountPictureAction("test", postinstall.AccountPicture{UID: "1000", Home: "/home/test", SourceSHA256: strings.Repeat("a", 64)})}
		switch mode {
		case "other binary":
			task.Action.Argv[0] = "/tmp/busctl"
		case "extra option":
			task.Action.Argv = append(task.Action.Argv, "--help")
		case "other account":
			task.Action.Argv[6] = "/org/freedesktop/Accounts/User0"
		case "root":
			task.Action.Picture.UID = "0"
		case "other kind":
			task.Action.Kind = postinstall.OpenApplication
		case "blocked":
			task.Status = postinstall.Blocked
		case "source path":
			task.Action.Argv[len(task.Action.Argv)-1] = "/etc/shadow"
		case "missing metadata":
			task.Action.Picture = nil
		}
		if _, err := postinstallArgv(task); err == nil {
			t.Fatalf("accepted forged %s action", mode)
		}
	}
}

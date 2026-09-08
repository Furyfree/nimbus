package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/state"
)

func acceptanceFixture(t *testing.T) (string, string, string) {
	t.Helper()
	applyEnv(t)
	checkout := t.TempDir()
	oldRoot, oldUID := filesSystemRoot, filesReceiptUID
	filesSystemRoot = t.TempDir()
	filesReceiptUID = uint32(os.Getuid())
	t.Cleanup(func() { filesSystemRoot = oldRoot; filesReceiptUID = oldUID })
	owner, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	group, err := user.LookupGroupId(owner.Gid)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{
		"nimbus.toml":                      "schema = 1\n[compatibility]\nfedora = [\"44\"]\nmin_engine = \"0.1.1\"\n",
		"machines/test.toml":               "schema = 1\nid = \"test\"\nprofiles = [\"common\"]\ncomponents = [\"test\"]\n",
		"profiles/common.toml":             "schema = 1\nid = \"common\"\n",
		"components/test.toml":             fmt.Sprintf("schema = 1\nid = \"test\"\n[[files]]\nsource = \"etc/nimbus-test.conf\"\nowner = %q\ngroup = %q\nmode = \"0644\"\n", owner.Username, group.Name),
		"system/root/etc/nimbus-test.conf": "old\n",
		".git/config":                      "[remote \"origin\"]\nurl = https://example.invalid/nimbus.git\n",
		".git/HEAD":                        "0123456789012345678901234567890123456789abcd\n",
	}
	for path, data := range entries {
		p := filepath.Join(checkout, path)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(checkout, "profiles"), 0755)
	source := filepath.Join(checkout, "system/root/etc/nimbus-test.conf")
	os.Chmod(source, 0755)
	live := filepath.Join(filesSystemRoot, "etc/nimbus-test.conf")
	os.MkdirAll(filepath.Dir(live), 0755)
	os.WriteFile(live, []byte("new\n"), 0644)
	receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: "file:/etc/nimbus-test.conf", Provider: "system-file", Verified: true, Machine: "test", PlanDigest: "sha256:test", Operation: "install"}
	data, _ := json.Marshal(receipt)
	os.MkdirAll(filepath.Join(stateRoot, state.ReceiptsDir), 0755)
	if err := os.WriteFile(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName(receipt.Resource)), data, 0644); err != nil {
		t.Fatal(err)
	}
	return checkout, source, live
}

func TestFilesAcceptCapturesOnlySourceAndPreservesMode(t *testing.T) {
	checkout, source, live := acceptanceFixture(t)
	before, _ := os.Stat(live)
	code, out, errOut := run(t, "files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--yes")
	if code != ExitOK {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	data, _ := os.ReadFile(source)
	info, _ := os.Stat(source)
	after, _ := os.Stat(live)
	if string(data) != "new\n" || info.Mode().Perm() != 0755 || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("capture changed metadata/target or lost content")
	}
	if !strings.Contains(out, "-old\n+new\n") || !strings.Contains(out, "must not contain secrets") {
		t.Fatal(out)
	}
}

func TestFilesAcceptPreviewCancellationAndChangedInput(t *testing.T) {
	for _, scenario := range []string{"preview", "cancel", "target", "source", "receipt", "head"} {
		t.Run(scenario, func(t *testing.T) {
			checkout, source, live := acceptanceFixture(t)
			saved := approver
			t.Cleanup(func() { approver = saved })
			approver = func(io.Reader, io.Writer, string) bool {
				switch scenario {
				case "cancel":
					return false
				case "target":
					os.WriteFile(live, []byte("changed\n"), 0644)
				case "source":
					os.WriteFile(source, []byte("changed source\n"), 0755)
				case "receipt":
					os.Remove(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("file:/etc/nimbus-test.conf")))
				case "head":
					os.WriteFile(filepath.Join(checkout, ".git/HEAD"), []byte("other\n"), 0644)
				}
				return true
			}
			args := []string{"files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test"}
			if scenario == "preview" {
				args = append(args, "--plan")
			}
			code, out, errOut := run(t, args...)
			if scenario == "preview" && code != ExitOK || scenario != "preview" && code != ExitFailure {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
			data, _ := os.ReadFile(source)
			if string(data) == "new\n" {
				t.Fatal("captured despite cancellation/input change")
			}
		})
	}
}

func TestFilesAcceptRefusesUnsafeTargetsAndOwnership(t *testing.T) {
	for _, scenario := range []string{"outside", "traversal", "unselected", "symlink", "ancestor", "hardlink", "directory", "unverified", "foreign", "metadata", "binary"} {
		t.Run(scenario, func(t *testing.T) {
			checkout, _, live := acceptanceFixture(t)
			target := "/etc/nimbus-test.conf"
			switch scenario {
			case "outside":
				target = "/usr/test"
			case "traversal":
				target = "/etc/../etc/nimbus-test.conf"
			case "unselected":
				target = "/etc/other"
			case "symlink":
				os.Remove(live)
				os.Symlink("/etc/passwd", live)
			case "ancestor":
				os.Rename(filepath.Dir(live), filepath.Join(filesSystemRoot, "real-etc"))
				os.Symlink("real-etc", filepath.Dir(live))
			case "hardlink":
				os.Link(live, live+".link")
			case "directory":
				os.Remove(live)
				os.Mkdir(live, 0755)
			case "unverified", "foreign":
				path := filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("file:"+target))
				data, _ := os.ReadFile(path)
				var r state.Receipt
				json.Unmarshal(data, &r)
				if scenario == "unverified" {
					r.Verified = false
				} else {
					r.Machine = "other"
				}
				data, _ = json.Marshal(r)
				os.WriteFile(path, data, 0644)
			case "metadata":
				os.Chmod(live, 0600)
			case "binary":
				os.WriteFile(live, []byte{0}, 0644)
			}
			if code, out, errOut := run(t, "files", "accept", target, "--checkout", checkout, "--machine", "test", "--yes"); code != ExitFailure {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
		})
	}
}

func TestFilesAcceptJSONRequiresExplicitMode(t *testing.T) {
	checkout, source, _ := acceptanceFixture(t)
	args := []string{"files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--json"}
	if code, _, _ := run(t, args...); code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if code, out, errOut := run(t, append(args, "--plan")...); code != ExitOK || !strings.Contains(out, `"changed": false`) {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	data, _ := os.ReadFile(source)
	if string(data) != "old\n" {
		t.Fatal("JSON preview mutated source")
	}
}

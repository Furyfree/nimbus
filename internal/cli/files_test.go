package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/state"
)

func TestFilesAcceptStopsWhenPreviewCannotBeWritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		yes   bool
		after int
	}{
		{name: "interactive preview"},
		{name: "automatic preview", yes: true},
		{name: "interactive prompt", after: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout, source, _ := acceptanceFixture(t)
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			writeErr := errors.New("preview output unavailable")
			cmd := newFiles(&options{})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			args := []string{"accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test"}
			if tc.yes {
				args = append(args, "--yes")
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader("yes\n"))
			cmd.SetOut(&previewErrorWriter{after: tc.after, err: writeErr})
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil || tc.after == 0 && !errors.Is(err, writeErr) {
				t.Fatalf("preview failure not reported: %v", err)
			}
			after, err := os.ReadFile(source)
			if err != nil || string(after) != string(before) {
				t.Fatalf("failed preview changed source: %q, %v", after, err)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "nimbus", "operation.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed preview reached mutation lock: %v", err)
			}
		})
	}
}

func TestFilesAcceptReportsClosingOutputFailure(t *testing.T) {
	for _, mode := range []string{"capture", "preview", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			checkout, source, live := acceptanceFixture(t)
			if mode == "unchanged" {
				if err := os.WriteFile(live, []byte("old\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--yes"}
			if mode == "preview" {
				args = append(args, "--plan")
			}
			writeErr := errors.New("result output unavailable")
			cmd := newFiles(&options{})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs(args)
			cmd.SetOut(&previewErrorWriter{after: 1, err: writeErr})
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); !errors.Is(err, writeErr) {
				t.Fatalf("closing output failure not reported: %v", err)
			}
			want := "old\n"
			if mode == "capture" {
				want = "new\n"
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != want {
				t.Fatalf("source = %q, %v; want %q", data, err, want)
			}
		})
	}
}

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
	source := filepath.Join(checkout, "system/root/etc/nimbus-test.conf")
	if err := os.Chmod(source, 0755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(filesSystemRoot, "etc/nimbus-test.conf")
	if err := os.MkdirAll(filepath.Dir(live), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: "file:/etc/nimbus-test.conf", Provider: "system-file", Verified: true, Machine: "test", PlanDigest: "sha256:test", Operation: "install"}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, state.ReceiptsDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, state.SchemaFile), fmt.Appendln(nil, state.Schema), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName(receipt.Resource)), data, 0644); err != nil {
		t.Fatal(err)
	}
	return checkout, source, live
}

func TestFilesAcceptCapturesOnlySourceAndPreservesMode(t *testing.T) {
	checkout, source, live := acceptanceFixture(t)
	before, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--yes")
	if code != ExitOK {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" || info.Mode().Perm() != 0755 || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("capture changed metadata/target or lost content")
	}
	if !strings.Contains(out, "-old\n+new\n") || !strings.Contains(out, "must not contain secrets") {
		t.Fatal(out)
	}
}

func TestFilesAcceptReadsMatchingLegacyReceipts(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			checkout, source, _ := acceptanceFixture(t)
			const resource = "file:/etc/nimbus-test.conf"
			current := filepath.Join(stateRoot, state.ReceiptsDir, state.FileName(resource))
			legacy := filepath.Join(stateRoot, state.ReceiptsDir, state.LegacyFileName(resource))
			data, err := os.ReadFile(current)
			if err != nil {
				t.Fatal(err)
			}
			if foreign {
				var receipt state.Receipt
				if err := json.Unmarshal(data, &receipt); err != nil {
					t.Fatal(err)
				}
				receipt.Resource = "file:/etc/another.conf"
				data, err = json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(legacy, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(current); err != nil {
				t.Fatal(err)
			}
			code, out, errOut := run(t, "files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--yes")
			wantCode, wantSource := ExitOK, "new\n"
			if foreign {
				wantCode, wantSource = ExitFailure, "old\n"
			}
			if code != wantCode {
				t.Fatalf("exit %d, want %d: %s%s", code, wantCode, out, errOut)
			}
			got, err := os.ReadFile(source)
			if err != nil || string(got) != wantSource {
				t.Fatalf("source = %q, %v; want %q", got, err, wantSource)
			}
			retained, err := os.ReadFile(legacy)
			if err != nil || string(retained) != string(data) {
				t.Fatalf("file acceptance changed the legacy receipt: %q, %v", retained, err)
			}
		})
	}
}

func TestFilesAcceptRefusesAmbiguousOrUnsupportedState(t *testing.T) {
	for _, scenario := range []string{"duplicate receipt", "future schema", "missing schema"} {
		t.Run(scenario, func(t *testing.T) {
			checkout, source, _ := acceptanceFixture(t)
			switch scenario {
			case "duplicate receipt":
				const resource = "file:/etc/nimbus-test.conf"
				data, err := os.ReadFile(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName(resource)))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(stateRoot, state.ReceiptsDir, state.LegacyFileName(resource)), data, 0644); err != nil {
					t.Fatal(err)
				}
			case "future schema":
				if err := os.WriteFile(filepath.Join(stateRoot, state.SchemaFile), []byte("999\n"), 0644); err != nil {
					t.Fatal(err)
				}
			case "missing schema":
				if err := os.Remove(filepath.Join(stateRoot, state.SchemaFile)); err != nil {
					t.Fatal(err)
				}
			}
			if code, out, errOut := run(t, "files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test", "--yes"); code != ExitFailure {
				t.Fatalf("accepted ambiguous ownership state: %d %s%s", code, out, errOut)
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != "old\n" {
				t.Fatalf("rejected ownership state changed the source: %q, %v", data, err)
			}
		})
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
					if err := os.WriteFile(live, []byte("changed\n"), 0644); err != nil {
						t.Fatal(err)
					}
				case "source":
					if err := os.WriteFile(source, []byte("changed source\n"), 0755); err != nil {
						t.Fatal(err)
					}
				case "receipt":
					if err := os.Remove(filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("file:/etc/nimbus-test.conf"))); err != nil {
						t.Fatal(err)
					}
				case "head":
					if err := os.WriteFile(filepath.Join(checkout, ".git/HEAD"), []byte("other\n"), 0644); err != nil {
						t.Fatal(err)
					}
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
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) == "new\n" {
				t.Fatal("captured despite cancellation/input change")
			}
		})
	}
}

func TestFilesAcceptRejectsUnencodableOwnershipReceipts(t *testing.T) {
	for _, tc := range []struct {
		name, want            string
		malformedBeforeReview bool
		changePlan            bool
	}{
		{"unchanged malformed receipt", "encode reviewed ownership receipt", true, false},
		{"changed malformed receipt", "encode reviewed ownership receipt", true, true},
		{"receipt malformed after review", "encode current ownership receipt", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout, source, _ := acceptanceFixture(t)
			path := filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("file:/etc/nimbus-test.conf"))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var receipt map[string]json.RawMessage
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			write := func() {
				t.Helper()
				// This offset decodes as time.Time but cannot be encoded again.
				receipt["timestamp"] = json.RawMessage(`"2026-09-09T12:00:00+24:00"`)
				data, err := json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.malformedBeforeReview {
				write()
			}
			saved := approver
			t.Cleanup(func() { approver = saved })
			approver = func(io.Reader, io.Writer, string) bool {
				if tc.changePlan {
					receipt["plan_digest"] = json.RawMessage(`"sha256:changed"`)
				}
				write()
				return true
			}
			code, out, errOut := run(t, "files", "accept", "/etc/nimbus-test.conf", "--checkout", checkout, "--machine", "test")
			if code != ExitFailure || !strings.Contains(errOut, tc.want) {
				t.Errorf("receipt encoding failure not reported: %d %s%s", code, out, errOut)
			}
			data, err = os.ReadFile(source)
			if err != nil || string(data) != "old\n" {
				t.Fatalf("receipt encoding failure changed source: %q, %v", data, err)
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
				if err := os.Remove(live); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/etc/passwd", live); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				if err := os.Rename(filepath.Dir(live), filepath.Join(filesSystemRoot, "real-etc")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real-etc", filepath.Dir(live)); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(live, live+".link"); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(live); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(live, 0755); err != nil {
					t.Fatal(err)
				}
			case "unverified", "foreign":
				path := filepath.Join(stateRoot, state.ReceiptsDir, state.FileName("file:"+target))
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var r state.Receipt
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				if scenario == "unverified" {
					r.Verified = false
				} else {
					r.Machine = "other"
				}
				data, err = json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			case "metadata":
				if err := os.Chmod(live, 0600); err != nil {
					t.Fatal(err)
				}
			case "binary":
				if err := os.WriteFile(live, []byte{0}, 0644); err != nil {
					t.Fatal(err)
				}
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
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatal("JSON preview mutated source")
	}
}

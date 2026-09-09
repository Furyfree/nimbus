package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func receipt(resource, digest string) Receipt {
	return Receipt{Schema: Schema, Engine: "0.0.0-dev", Machine: "desktop", Resource: resource, Provider: "dnf",
		Operation: "install", PlanDigest: digest, Verified: true, Verification: "rpm -q", Timestamp: time.Unix(0, 0).UTC(),
		Definitions: Definitions{Origin: "github.com/furyfree-org/nimbus", Commit: "abc", Digest: "sha256:def"}}
}

func TestReadEmptyStateIsNotAnError(t *testing.T) {
	a, err := Read(filepath.Join(t.TempDir(), "absent"))
	if err != nil || a.Present || a.Baseline != nil || len(a.Receipts) != 0 {
		t.Fatalf("empty state = %+v %v", a, err)
	}
}

func TestRecordWritesAtomicallyAndReadsBack(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:plan"
	st := &Stage{Schema: Schema, PlanDigest: digest, Time: time.Unix(1, 0).UTC(),
		Baseline: &Baseline{Recorded: time.Unix(1, 0).UTC(), Packages: []string{"zsh", "bash"}},
		Receipts: []Receipt{receipt("package:dnf:ripgrep", digest), receipt("repository:terra", digest)}}
	if err := Record(root, digest, st); err != nil {
		t.Fatal(err)
	}
	a, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Present || len(a.Receipts) != 2 || a.Receipts["package:dnf:ripgrep"].Operation != "install" {
		t.Fatalf("read back = %+v", a)
	}
	if !a.InBaseline("bash") || a.InBaseline("ripgrep") || a.Baseline.Packages[0] != "bash" {
		t.Fatalf("baseline = %+v", a.Baseline)
	}
	info, _ := os.Stat(filepath.Join(root, ReceiptsDir, FileName("package:dnf:ripgrep")))
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("receipt mode %o", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(root)
	if slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".nimbus-") }) {
		t.Fatal("temporary file left behind")
	}

	// A second record never replaces the baseline and removes a receipt.
	second := &Stage{Schema: Schema, PlanDigest: "sha256:two", Time: time.Unix(2, 0).UTC(),
		Baseline: &Baseline{Packages: []string{"other"}}, Remove: []string{"package:dnf:ripgrep"}}
	if err := Record(root, "sha256:two", second); err != nil {
		t.Fatal(err)
	}
	a, _ = Read(root)
	if a.InBaseline("other") || !a.InBaseline("bash") || len(a.Receipts) != 1 {
		t.Fatalf("after second record = %+v", a)
	}
	journal, _ := os.ReadFile(filepath.Join(root, JournalFile))
	if strings.Count(string(journal), "\n") != 3 || !strings.Contains(string(journal), `"action":"removed"`) {
		t.Fatalf("journal:\n%s", journal)
	}
}

func TestAtomicWriteCleansUpAfterRenameFailure(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "receipt.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(target, "keep")
	if err := os.WriteFile(retained, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("replacement"), 0o644); err == nil {
		t.Fatal("replaced a directory with a file")
	}
	data, err := os.ReadFile(retained)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("destination changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".nimbus-") }) {
		t.Fatal("temporary file left behind after failed rename")
	}
}

func TestRecordRejectsBaselineInspectionFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(BaselineFile, filepath.Join(root, BaselineFile)); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:plan"
	r := receipt("package:dnf:ripgrep", digest)
	stage := &Stage{Schema: Schema, PlanDigest: digest, Baseline: &Baseline{Packages: []string{"bash"}}, Receipts: []Receipt{r}}
	if err := Record(root, digest, stage); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("baseline inspection error = %v, want symlink loop", err)
	}
	if _, err := os.Stat(filepath.Join(root, ReceiptsDir, FileName(r.Resource))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("receipt recorded after baseline inspection failed: %v", err)
	}
}

func TestReceiptChangeTimeSurvivesLegacyReadAndRecord(t *testing.T) {
	for _, operation := range []string{"install", "repair", "adopt"} {
		for _, tc := range []struct {
			name    string
			field   string
			changed time.Time
		}{
			{"omitted", "", time.Time{}},
			{"legacy-zero", `,"changed_at":"0001-01-01T00:00:00Z"`, time.Time{}},
			{"nonzero", `,"changed_at":"1970-01-01T00:00:05Z"`, time.Unix(5, 0).UTC()},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				stage := &Stage{Schema: Schema, PlanDigest: "approved", Time: time.Unix(30, 0).UTC()}
				if err := Record(root, stage.PlanDigest, stage); err != nil {
					t.Fatal(err)
				}
				resource := "file:/etc/nimbus.conf"
				path := filepath.Join(root, ReceiptsDir, FileName(resource))
				legacy := fmt.Appendf(nil, `{"schema":2,"resource":%q,"provider":"system-file","machine":"vm","operation":%q,"plan_digest":"approved","verified":true,"timestamp":"1970-01-01T00:00:20Z"%s}`, resource, operation, tc.field)
				if err := os.WriteFile(path, legacy, 0o644); err != nil {
					t.Fatal(err)
				}
				want := tc.changed
				if want.IsZero() && operation != "adopt" {
					want = time.Unix(20, 0).UTC()
				}
				for range 2 {
					applied, err := Read(root)
					if err != nil {
						t.Fatal(err)
					}
					r := applied.Receipts[resource]
					if !r.ChangedAt.Equal(tc.changed) || !r.ChangeTime().Equal(want) {
						t.Fatalf("changed_at=%s, change time=%s; want %s, %s", r.ChangedAt, r.ChangeTime(), tc.changed, want)
					}
					stage.Receipts = []Receipt{r}
					if err := Record(root, stage.PlanDigest, stage); err != nil {
						t.Fatal(err)
					}
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if present := strings.Contains(string(data), `"changed_at"`); present == tc.changed.IsZero() {
					t.Fatalf("recorded changed_at presence=%t for %s: %s", present, tc.changed, data)
				}
			})
		}
	}
}

func TestRecordRefusesUnboundOrUnverifiedData(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		st   *Stage
		plan string
		want string
	}{
		{"wrong digest", &Stage{Schema: Schema, PlanDigest: "sha256:a"}, "sha256:b", "bound to plan"},
		{"empty digest", &Stage{Schema: Schema, PlanDigest: ""}, "", "bound to plan"},
		{"receipt from another plan", &Stage{Schema: Schema, PlanDigest: "sha256:a", Receipts: []Receipt{receipt("x", "sha256:other")}}, "sha256:a", "bound to plan"},
		{"unverified receipt", func() *Stage {
			r := receipt("x", "sha256:a")
			r.Verified = false
			return &Stage{Schema: Schema, PlanDigest: "sha256:a", Receipts: []Receipt{r}}
		}(), "sha256:a", "never gets a receipt"},
		{"wrong schema", &Stage{Schema: 3, PlanDigest: "sha256:a"}, "sha256:a", "schema 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Record(root, tc.plan, tc.st)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, SchemaFile)); err == nil {
		t.Fatal("a refused stage must write nothing")
	}
}

func TestReadRejectsUnsupportedSchema(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, SchemaFile), []byte("3\n"), 0o644)
	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("unsupported schema accepted: %v", err)
	}
}

func TestFileNameIsSafe(t *testing.T) {
	if FileName("package:dnf:xorg-x11-drv-nvidia-libs.i686") != "package_dnf_xorg-x11-drv-nvidia-libs.i686.json" || strings.ContainsAny(FileName("a/b:c d"), "/: ") {
		t.Fatal(FileName("a/b:c d"))
	}
}

func TestNativeIdentityStateIsVersionedAndLegacyRemainsReadable(t *testing.T) {
	root := t.TempDir()
	for _, schema := range []int{1, ReceiptSchema} {
		receipt := Receipt{Schema: schema, Resource: "package:dnf:demo", Provider: "dnf", Package: "demo.i686", Operation: "install", PlanDigest: "digest", Verified: true}
		if schema == 1 {
			receipt.Package = ""
		}
		stage := &Stage{Schema: Schema, PlanDigest: "digest", Receipts: []Receipt{receipt}}
		if err := Record(root, "digest", stage); err != nil {
			t.Fatal(err)
		}
		got, err := Read(root)
		if err != nil {
			t.Fatal(err)
		}
		if r := got.Receipts[receipt.Resource]; r.Schema != schema || r.Package != receipt.Package {
			t.Fatalf("receipt lost identity: %+v", r)
		}
	}
}

func TestLegacyStateUpgradePreservesReceiptsAndRejectsOldReader(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ReceiptsDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SchemaFile), []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	legacy := `{"schema":1,"resource":"package:dnf:foot","provider":"dnf","operation":"install","plan_digest":"old","verified":true}`
	file := filepath.Join(root, ReceiptsDir, FileName("package:dnf:foot"))
	if err := os.WriteFile(file, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := Read(root)
	if err != nil || !before.Receipts["package:dnf:foot"].Verified {
		t.Fatalf("legacy read: %+v %v", before, err)
	}
	if err := Record(root, "approved", &Stage{Schema: Schema, PlanDigest: "approved"}); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(root, SchemaFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(marker)) != "2" {
		t.Fatalf("upgraded marker = %q", marker)
	}
	// This is the v0.1.1 reader's exact schema acceptance condition. The
	// marker must fail it before that engine can plan package removals.
	legacyReaderAccepts := strings.TrimSpace(string(marker)) == "1"
	if legacyReaderAccepts {
		t.Fatal("package-only engine would accept system-resource state")
	}
	after, err := Read(root)
	if err != nil || after.Receipts["package:dnf:foot"].Resource != "package:dnf:foot" {
		t.Fatalf("upgraded read: %+v %v", after, err)
	}
	unchanged, err := os.ReadFile(file)
	if err != nil || string(unchanged) != legacy {
		t.Fatalf("legacy receipt changed during schema upgrade: %q %v", unchanged, err)
	}
}

func TestRecordNeverDowngradesFutureState(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, SchemaFile)
	if err := os.WriteFile(marker, []byte("3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, "approved", &Stage{Schema: Schema, PlanDigest: "approved"}); err == nil {
		t.Fatal("future state was overwritten")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "3\n" {
		t.Fatalf("future marker changed: %q %v", data, err)
	}
}

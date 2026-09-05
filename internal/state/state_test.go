package state

import (
	"os"
	"path/filepath"
	"strings"
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
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".nimbus-") {
			t.Fatal("temporary file left behind")
		}
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
		{"wrong schema", &Stage{Schema: 2, PlanDigest: "sha256:a"}, "sha256:a", "schema 2"},
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
	os.WriteFile(filepath.Join(root, SchemaFile), []byte("2\n"), 0o644)
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

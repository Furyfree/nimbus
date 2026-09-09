package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture returns the recorded output of one named dnf5 run.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "dnf5-previews.txt"))
	if err != nil {
		t.Fatal(err)
	}
	marker := "##### " + name + "\n"
	_, rest, ok := strings.Cut(string(data), marker)
	if !ok {
		t.Fatalf("fixture %s not recorded", name)
	}
	_, rest, ok = strings.Cut(rest, "\n") // drop the "$ dnf5 ..." line
	if !ok {
		t.Fatalf("fixture %s has no command line", name)
	}
	out, _, ok := strings.Cut(rest, "##### exit")
	if !ok {
		t.Fatalf("fixture %s has no exit marker", name)
	}
	return []byte(out)
}

func TestParsePreviewInstall(t *testing.T) {
	tx, err := ParsePreview(fixture(t, "install-one"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Packages) != 1 || tx.Packages[0] != (TxPackage{Name: "ripgrep", Arch: "x86_64", EVR: "0:15.2.0-1.fc44", Repository: "updates", Section: "installing"}) {
		t.Fatalf("packages = %+v", tx.Packages)
	}
	tx, err = ParsePreview(fixture(t, "install-deps"))
	if err != nil {
		t.Fatal(err)
	}
	deps := len(tx.Rows("installing dependencies")) + len(tx.Rows("installing weak dependencies"))
	if len(tx.Rows("installing")) != 1 || deps != 90 || len(tx.Packages) != 91 {
		t.Fatalf("git preview: %d direct, %d deps, %d total", len(tx.Rows("installing")), deps, len(tx.Packages))
	}
	tx, err = ParsePreview(fixture(t, "install-allowerasing"))
	if err != nil {
		t.Fatal(err)
	}
	if rows := tx.Rows("installing"); len(rows) != 1 || rows[0].Repository != "rpmfusion-free-updates" {
		t.Fatalf("ffmpeg preview = %+v", rows)
	}
}

func TestParsePreviewOutcomes(t *testing.T) {
	tx, err := ParsePreview(fixture(t, "nothing-to-do"))
	if err != nil || !tx.NothingToDo || len(tx.Packages) != 0 {
		t.Fatalf("nothing to do: %+v %v", tx, err)
	}
	_, err = ParsePreview(fixture(t, "no-match"))
	if _, ok := errors.AsType[*ResolveError](err); !ok || !strings.Contains(err.Error(), "No match for argument: this-package-does-not-exist") {
		t.Fatalf("no match: %v", err)
	}
	_, err = ParsePreview(fixture(t, "remove-with-dependents"))
	if resolveErr, ok := errors.AsType[*ResolveError](err); !ok || len(resolveErr.Problems) < 2 || !strings.Contains(resolveErr.Problems[0], "protected packages") {
		t.Fatalf("protected: %v", err)
	}
	if _, err := ParsePreview([]byte("Package Arch Version Repository Size\nSurprising:\n foo x86_64 0:1-1 fedora 1 KiB\n")); err == nil || !strings.Contains(err.Error(), "unknown preview section") {
		t.Fatalf("unknown section accepted: %v", err)
	}
	if _, err := ParsePreview([]byte("garbage\n")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestParseCheckUpgrade(t *testing.T) {
	ups, err := ParseCheckUpgrade(fixture(t, "check-upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 3 || ups[0] != (Upgrade{Name: "ca-certificates", Arch: "noarch", EVR: "2026.2.90_v9.0.317-1.fc44", Repository: "updates"}) || ups[2].Name != "openssl-libs" {
		t.Fatalf("upgrades = %+v", ups)
	}
	if ups, err := ParseCheckUpgrade([]byte("Updating and loading repositories:\nRepositories loaded.\n")); err != nil || len(ups) != 0 {
		t.Fatalf("empty = %+v %v", ups, err)
	}
}

func TestPreviewReadsTheReplacedVersionBelowAnUpgrade(t *testing.T) {
	out := []byte(`Updating and loading repositories:
Repositories loaded.
Package                                  Arch   Version         Repository      Size
Upgrading:
 openssl-libs                            x86_64 1:3.5.8-1.fc44  updates      9.2 MiB
   replacing openssl-libs                x86_64 1:3.5.7-2.fc44  updates      9.2 MiB
Installing:
 openssl                                 x86_64 1:3.5.8-1.fc44  updates      1.8 MiB

Transaction Summary:
 Installing:        1 package
 Upgrading:         1 package
 Replacing:         1 package

Operation aborted by the user.
`)
	tx, err := ParsePreview(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Packages) != 3 || tx.Packages[0].Section != "upgrading" || tx.Packages[1].Section != SectionReplaced || tx.Packages[1].Name != "openssl-libs" || tx.Packages[1].EVR != "1:3.5.7-2.fc44" || tx.Packages[2].Section != "installing" {
		t.Fatalf("rows = %+v", tx.Packages)
	}
}

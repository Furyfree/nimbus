package cli

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestNoUpdatesSkipsTransactionAndSnapshots(t *testing.T) {
	root, src := snapshotFixture(t)
	src.Commands["dnf5 --cacheonly --assumeno upgrade"] = []byte("Repositories loaded.\nNothing to do.\n")
	code, out, errOut := run(t, "upgrade", "--system", "--checkout", root, "--machine", "vm", "--yes")
	if code != 0 || len(src.mutations) != 0 || !strings.Contains(out, "no transaction or snapshots") {
		t.Fatalf("%d %s%s %v", code, out, errOut, src.mutations)
	}
}

func TestFailedUpdateCheckNeverStartsSnapshotsOrTransactions(t *testing.T) {
	root, src := snapshotFixture(t)
	src.Commands["dnf5 --cacheonly --assumeno upgrade"] = nil
	src.Failures["dnf5 --cacheonly --assumeno upgrade"] = "metadata unreadable"
	code, out, errOut := run(t, "upgrade", "--system", "--checkout", root, "--machine", "vm", "--yes")
	if code == 0 || len(src.mutations) != 0 || !strings.Contains(out+errOut, "DNF update check") {
		t.Fatalf("%d %s%s %v", code, out, errOut, src.mutations)
	}
}

func TestUpdateCheckIncludesFlatpakAndRejectsUnknown(t *testing.T) {
	for _, mode := range []string{"current", "runtime", "dnf-change", "dnf-failure", "flatpak-failure", "remote-failure"} {
		t.Run(mode, func(t *testing.T) {
			_, src := installerFixture(t)
			dnf := "dnf5 --cacheonly --assumeno upgrade"
			src.Commands[dnf] = []byte("Repositories loaded.\nNothing to do.\n")
			src.Commands["flatpak remotes --system --columns=name"] = []byte("flathub\n")
			refs := nativetest.Key("flatpak", "remote-ls", "--system", "--updates", "--all", "--columns=ref", "--", "flathub")
			src.Commands[refs] = nil
			switch mode {
			case "runtime":
				src.Commands[refs] = []byte("runtime/org.freedesktop.Platform/x86_64/25.08\n")
			case "dnf-change":
				src.Commands[dnf] = []byte("Package Arch Version Repository Size\nInstalling dependencies:\n new-lib x86_64 1-1 fedora 1 KiB\n\nTransaction Summary:\n")
			case "dnf-failure":
				src.Failures[dnf] = "repository unavailable"
			case "flatpak-failure":
				src.Failures[refs] = "offline"
			case "remote-failure":
				src.Failures["flatpak remotes --system --columns=name"] = "cannot inspect installation"
			}
			pending, err := systemUpdatesPending(src, definitions.Root{Repositories: map[string]definitions.Repository{"flathub": {Kind: "flatpak"}}})
			wantErr := strings.HasSuffix(mode, "failure")
			if (err != nil) != wantErr || pending != (mode == "runtime" || mode == "dnf-change") {
				t.Fatal(pending, err)
			}
			if len(src.calls) > 0 {
				t.Fatal("check started a transaction", src.calls)
			}
		})
	}
}

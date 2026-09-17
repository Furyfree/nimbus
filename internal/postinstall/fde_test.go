package postinstall

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

const setupModeVar = "/sys/firmware/efi/efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"

func fdeFixture(t *testing.T) (Inputs, *nativetest.FakeSource) {
	t.Helper()
	in, src := fixture("systemd-ukify", "sbsigntools", "efibootmgr")
	if src.Dirs == nil {
		src.Dirs = map[string][]string{}
	}
	in.Resolved.Components = []definitions.ResolvedComponent{{ID: "fde"}}
	in.Facts.SecureBoot.Value = inspect.SecureBootDisabled
	src.Files["/proc/mounts"] = []byte("/dev/mapper/luks-1234[/root] / btrfs rw,relatime 0 0\n/dev/vda1 /boot/efi vfat rw 0 0\n")
	src.Dirs["/sys/class/block"] = []string{"dm-0", "vda"}
	src.Files["/sys/class/block/dm-0/dm/name"] = []byte("luks-1234\n")
	src.Files["/sys/class/block/dm-0/dm/uuid"] = []byte("CRYPT-LUKS2-1234-luks-1234\n")
	src.Files["/sys/class/tpm/tpm0/tpm_version_major"] = []byte("2\n")
	src.Dirs["/sys/class/tpm"] = []string{"tpm0"}
	src.Dirs["/sys/firmware/efi/efivars"] = []string{"SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"}
	src.Files[setupModeVar] = []byte{6, 0, 0, 0, 0}
	src.Files[FDEHookPath] = []byte("#!/bin/sh\n")
	for _, tool := range []string{"systemd-cryptenroll", "ukify", "sbsign", "kernel-install", "dracut", "efibootmgr"} {
		src.Paths[tool] = "/usr/bin/" + tool
	}
	src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nTimeout: 0 seconds\nBootOrder: 0008,0000\nBoot0008* Fedora\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n")
	src.Commands[nativetest.Key("stat", "--format=%a", "--", FDEHookPath)] = []byte("755\n")
	return in, src
}

func TestFDEInspectionStates(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		in, src := fdeFixture(t)
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Pending || got.Action == nil || got.Action.Kind != SetupFDE || !strings.Contains(got.Detail, "Approved setup writes the marker") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("unselected component", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Resolved.Components = nil
		for _, task := range Inspect(src, in) {
			if task.ID == "fde" {
				t.Fatalf("unselected capability exposed a task: %+v", task)
			}
		}
	})
	t.Run("plain root", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files["/proc/mounts"] = []byte("/dev/vda3 / btrfs rw 0 0\n")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("non-LUKS mapper", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files["/sys/class/block/dm-0/dm/uuid"] = []byte("CRYPT-PLAIN-1234\n")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("dm device source", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files["/proc/mounts"] = []byte("/dev/dm-0 / btrfs rw 0 0\n")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != Pending {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("no TPM", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(src.Dirs, "/sys/class/tpm")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("TPM 1.2", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files["/sys/class/tpm/tpm0/tpm_version_major"] = []byte("1\n")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("TPM version unreadable", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(src.Files, "/sys/class/tpm/tpm0/tpm_version_major")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != Unknown {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("setup mode", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Facts.SecureBoot.Value = inspect.SecureBootEnabled
		src.Files[setupModeVar] = []byte{6, 0, 0, 0, 1}
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("setup mode unreadable", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Facts.SecureBoot.Value = inspect.SecureBootEnabled
		delete(src.Dirs, "/sys/firmware/efi/efivars")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != Unknown {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("secure boot enabled blocks", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Facts.SecureBoot.Value = inspect.SecureBootEnabled
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || got.Action != nil || !strings.Contains(got.Detail, "next milestone") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("secure boot unavailable", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Facts.SecureBoot.Value = inspect.SecureBootUnavailable
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != NotApplicable {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("secure boot unknown", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Facts.SecureBoot.Value = ""
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != Unknown {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing package receipt", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(in.Applied.Receipts, "package:dnf:systemd-ukify")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "systemd-ukify") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing package selection", func(t *testing.T) {
		in, src := fdeFixture(t)
		in.Resolved.Packages = nil
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "systemd-ukify") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing tools", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(src.Paths, "ukify")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "ukify") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("unreadable mounts", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(src.Files, "/proc/mounts")
		if got := findTask(t, Inspect(src, in), "fde"); got.Status != Unknown {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing hook payload", func(t *testing.T) {
		in, src := fdeFixture(t)
		delete(src.Files, FDEHookPath)
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "does not ship the kernel-install hook") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("non-executable hook blocks", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Commands[nativetest.Key("stat", "--format=%a", "--", FDEHookPath)] = []byte("644\n")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "not executable") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("active setup needs root", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009,0008\nBoot0009* Nimbus UKI\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Unknown || !got.VerificationNeedsRoot || got.Action == nil || got.Action.Kind != SetupFDE {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("wrong entry offers repair", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008,0009\nBoot0008* Fedora\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\nBoot0009* Nimbus UKI\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\\\EFI\\\\Linux\\\\nimbus.efi\n")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Pending || got.Action == nil || got.Action.Kind != SetupFDE || !strings.Contains(got.Detail, "missing or different") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("foreign marker blocks", func(t *testing.T) {
		in, src := fdeFixture(t)
		src.Files[FDEUKIMarker] = []byte("someone else's file")
		got := findTask(t, Inspect(src, in), "fde")
		if got.Status != Blocked || !strings.Contains(got.Detail, "unfamiliar content") {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestRootSource(t *testing.T) {
	if got := rootSource("/dev/vda2 /boot ext4 rw 0 0\n/dev/mapper/luks-1[/root] / btrfs rw 0 0\n"); got != "/dev/mapper/luks-1[/root]" {
		t.Fatalf("got %q", got)
	}
	if got := rootSource("/dev/mapper/luks-1 / btrfs rw 0 0\n/dev/mapper/luks-2[/root] / btrfs rw 0 0\n"); got != "/dev/mapper/luks-2[/root]" {
		t.Fatalf("overmount did not win: %q", got)
	}
	if got := rootSource(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

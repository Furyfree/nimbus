package postinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestFDEBootEntries(t *testing.T) {
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	output := "BootCurrent: 0008\nBootOrder: 0008,0009,000A\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n" +
		"Boot000A  Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/EFI/Linux/old.efi\n"
	entries := fdeBootEntries(output)
	if len(entries) != 2 {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].ID != "0009" || entries[0].Loader != `EFI\Linux\nimbus.efi` {
		t.Fatalf("got %+v", entries[0])
	}
	if entries[1].ID != "000A" || entries[1].Loader != `EFI\Linux\old.efi` {
		t.Fatalf("forward slashes were not normalized: %+v", entries[1])
	}
	if !fdeEntryCorrect(entries) {
		t.Fatal("the current entry was not recognized")
	}
	if fdeEntryCorrect(entries[1:]) {
		t.Fatal("a stale entry was treated as correct")
	}
	if fdeLoaderPath("HD(1,GPT,g,0x800,0x200000)") != "" {
		t.Fatal("a device path without a loader was not rejected")
	}
}

func TestFDEESPDevice(t *testing.T) {
	cases := map[string][2]string{
		"/dev/vda1 /boot/efi vfat rw 0 0\n":                                  {"/dev/vda", "1"},
		"/dev/nvme0n1p2 /boot/efi vfat rw 0 0\n":                             {"/dev/nvme0n1", "2"},
		"/dev/mmcblk0p1 /boot/efi vfat rw 0 0\n":                             {"/dev/mmcblk0", "1"},
		"/dev/vda1 /boot/efi vfat rw 0 0\n/dev/vdb1 /boot/efi vfat rw 0 0\n": {"/dev/vdb", "1"},
	}
	for mounts, want := range cases {
		src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{"/proc/mounts": []byte(mounts)}, Dirs: map[string][]string{}, Paths: map[string]string{}}
		disk, part, err := fdeESPDevice(src)
		if err != nil || disk != want[0] || part != want[1] {
			t.Fatalf("%q: got %s %s, %v", mounts, disk, part, err)
		}
	}
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{"/proc/mounts": []byte("/dev/mapper/esp /boot/efi vfat rw 0 0\n")}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	if _, _, err := fdeESPDevice(src); err == nil {
		t.Fatal("a non-partition ESP was accepted")
	}
}

// fdeSetupSource replays the privileged setup and simulates its effects.
type fdeSetupSource struct {
	*nativetest.FakeSource
	t       *testing.T
	streams []string
	staged  string
	digest  string
	built   string
	entry   bool
	stale   bool
}

func (s *fdeSetupSource) Run(name string, args ...string) ([]byte, error) {
	if name == "efibootmgr" {
		const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
		fedora := "Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n"
		switch {
		case s.entry:
			return []byte("BootCurrent: 0009\nBootOrder: 0009,0008\n" + fedora + "Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n"), nil
		case s.stale:
			return []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" + fedora + "Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\\\EFI\\\\Linux\\\\nimbus.efi\n"), nil
		default:
			return []byte("BootCurrent: 0008\nBootOrder: 0008\n" + fedora), nil
		}
	}
	return s.FakeSource.Run(name, args...)
}

func (s *fdeSetupSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	if name != "sudo" {
		s.t.Fatalf("unexpected native mutation: %s", name)
	}
	if len(args) == 1 && args[0] == "--validate" {
		return nil
	}
	if len(args) < 2 || args[0] != "--" {
		s.t.Fatalf("unexpected sudo invocation: %v", args)
	}
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "internal system-file"):
		s.staged = args[len(args)-1]
		data, err := os.ReadFile(s.staged)
		if err != nil {
			s.t.Fatal(err)
		}
		info, err := os.Stat(s.staged)
		if err != nil || info.Mode().Perm() != 0600 {
			s.t.Fatal("unsafe staging")
		}
		var payload apply.FilePayload
		if err := json.Unmarshal(data, &payload); err != nil {
			s.t.Fatal(err)
		}
		for i, arg := range args {
			if arg == "--plan" {
				s.digest = args[i+1]
			}
		}
		if payload.PlanDigest != s.digest || s.digest == "" {
			s.t.Fatal("payload is not bound to the approved digest")
		}
		if payload.Change.Target != FDEUKIMarker || !bytes.Equal(payload.Change.After.Content, []byte(fdeMarkerText)) || payload.Change.After.Mode != "0644" || payload.Change.After.Owner != "root" || payload.Change.After.Group != "root" {
			s.t.Fatal("wrong file payload")
		}
		s.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	case strings.Contains(joined, "internal fde-uki"):
		s.built = args[len(args)-1]
	case args[1] == "efibootmgr" && strings.Contains(joined, " -c "):
		s.entry = true
	case args[1] == "efibootmgr" && strings.Contains(joined, " -B"):
	default:
		s.t.Fatalf("unexpected native mutation: %s", joined)
	}
	return nil
}

func fdeSetupFixture(t *testing.T) *fdeSetupSource {
	t.Helper()
	const version = "6.19.10-300.fc44.x86_64"
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	base.Files["/proc/mounts"] = []byte("/dev/mapper/luks-1234[/root] / btrfs rw 0 0\n/dev/vda1 /boot/efi vfat rw 0 0\n")
	base.Dirs["/"] = []string{"etc"}
	base.Dirs["/etc"] = []string{"kernel"}
	base.Dirs["/sys/firmware/efi/efivars"] = []string{}
	base.Commands[nativetest.Key("uname", "-r")] = []byte(version + "\n")
	base.Commands[nativetest.Key("stat", "--format=%s", "--", fdeKernelDir+"/"+version+"/vmlinuz")] = []byte("18497536\n")
	base.Commands[nativetest.Key("stat", "--format=%s", "--", "/boot/initramfs-"+version+".img")] = []byte("47000000\n")
	base.Commands[nativetest.Key("sudo", "-n", "--", "df", "--output=avail", "-B1", "/boot/efi")] = []byte("Avail\n995000000\n")
	base.Commands[nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc")] = []byte("directory|root|root|755|6\n")
	src := &fdeSetupSource{FakeSource: base, t: t}
	src.Files[fdeCmdlineFile] = []byte("root=UUID=test ro\n")
	return src
}

func fdeSetupTask() Task {
	return Task{ID: "fde", Owner: "component:fde", Title: "Set up TPM automatic disk unlock", Status: Pending, Action: &Action{Kind: SetupFDE}}
}

func TestRunFDESetup(t *testing.T) {
	t.Run("fresh setup", func(t *testing.T) {
		src := fdeSetupFixture(t)
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err != nil {
			t.Fatal(err)
		}
		if src.staged == "" || src.built != "6.19.10-300.fc44.x86_64" || !src.entry {
			t.Fatalf("staged=%q built=%q entry=%v", src.staged, src.built, src.entry)
		}
		if !strings.Contains(out.String(), "Reboot when ready") {
			t.Fatalf("missing reboot guidance: %s", out.String())
		}
		if _, err := os.Stat(filepath.Dir(src.staged)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("staged payload remains")
		}
	})
	t.Run("repair keeps the marker", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		src.stale = true
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err != nil {
			t.Fatal(err)
		}
		if src.staged != "" {
			t.Fatal("an existing marker was rewritten")
		}
		if src.built == "" || !src.entry {
			t.Fatalf("repair did not rebuild: %q", src.streams)
		}
	})
	t.Run("foreign marker blocks", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Files[FDEUKIMarker] = []byte("foreign")
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err == nil || !strings.Contains(err.Error(), "unfamiliar content") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("insufficient space blocks before changes", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Commands[nativetest.Key("sudo", "-n", "--", "df", "--output=avail", "-B1", "/boot/efi")] = []byte("Avail\n1000\n")
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err == nil || !strings.Contains(err.Error(), "bytes free") {
			t.Fatalf("got %v", err)
		}
		for _, stream := range src.streams {
			if strings.Contains(stream, "system-file") || strings.Contains(stream, "fde-uki") {
				t.Fatalf("mutated before the space check: %q", src.streams)
			}
		}
	})
}

// fdeInspectSource replays the privileged ukify inspection.
type fdeInspectSource struct {
	*nativetest.FakeSource
	out []byte
	err error
}

func (s fdeInspectSource) Run(name string, args ...string) ([]byte, error) {
	if name == "ukify" {
		return s.out, s.err
	}
	return s.FakeSource.Run(name, args...)
}

func fdeVerifyFixture(t *testing.T) fdeInspectSource {
	t.Helper()
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	base.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	base.Files[fdeCmdlineFile] = []byte("root=UUID=test ro\n")
	base.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009,0008\nBoot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
	return fdeInspectSource{FakeSource: base}
}

const fdeInspectOutput = `.sbat:
  size: 400 bytes
  sha256: aa
.linux:
  size: 18497536 bytes
  sha256: bb
.osrel:
  size: 629 bytes
  sha256: cc
.cmdline:
  size: 137 bytes
  sha256: dd
  text:
    %s
.uname:
  size: 100 bytes
  sha256: ee
.initrd:
  size: 47000000 bytes
  sha256: ff
`

func TestVerifyFDE(t *testing.T) {
	t.Run("verified", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", "root=UUID=test ro", 1))
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Complete || got.VerificationNeedsRoot {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("command line mismatch", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", "root=UUID=other ro", 1))
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !strings.Contains(got.Detail, "does not embed") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing section", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(".linux:\n  size: 1 bytes\n  sha256: aa\n")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !strings.Contains(got.Detail, "lacks the .initrd section") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("administrator access unavailable", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.err = errors.New("sudo: a password is required")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !got.VerificationNeedsRoot {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("inactive setup", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		delete(src.Files, FDEUKIMarker)
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || !strings.Contains(got.Detail, "not active") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("wrong entry", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008\nBoot0008* Fedora\tHD(1,GPT,g,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || !strings.Contains(got.Detail, "rerun the approved setup") {
			t.Fatalf("got %+v", got)
		}
	})
}

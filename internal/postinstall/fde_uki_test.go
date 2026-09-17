package postinstall

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func fdeBuildSource() *nativetest.FakeSource {
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	src.Files[fdeCmdlineFile] = []byte("root=UUID=test ro\n")
	src.Dirs["/sys/firmware/efi/efivars"] = []string{}
	return src
}

func fdeStagedTarget(t *testing.T, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(target + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(fdeMinimumSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFDECmdline(t *testing.T) {
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Files[fdeCmdlineFile] = []byte("# managed\nroot=UUID=x ro\n\nrootflags=subvol=root\n")
	if got, err := fdeCmdline(src); err != nil || got != "root=UUID=x ro rootflags=subvol=root" {
		t.Fatalf("got %q, %v", got, err)
	}
	src.Files[fdeCmdlineFile] = []byte("# only comments\n")
	src.Files["/proc/cmdline"] = []byte("BOOT_IMAGE=/vmlinuz-6.1 rd.luks.uuid=luks-1 initrd=/initrd.img ro\n")
	if got, err := fdeCmdline(src); err != nil || got != "rd.luks.uuid=luks-1 ro" {
		t.Fatalf("fallback got %q, %v", got, err)
	}
	delete(src.Files, "/proc/cmdline")
	if _, err := fdeCmdline(src); err == nil {
		t.Fatal("expected an error without any command line")
	}
}

func TestBuildFDEUKI(t *testing.T) {
	const version = "6.19.10-300.fc44.x86_64"
	base := []string{
		"build",
		"--linux=" + fdeKernelDir + "/" + version + "/vmlinuz",
		"--initrd=/boot/initramfs-" + version + ".img",
		"--cmdline=root=UUID=test ro",
	}
	t.Run("unsigned when signature enforcement is off", func(t *testing.T) {
		src := fdeBuildSource()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target)
		src.Commands[nativetest.Key("ukify", append(base, "--output="+target+".new")...)] = []byte{}
		if err := buildFDEUKI(src, version, io.Discard, target); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("image was not moved into place: %v", err)
		}
		if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
			t.Fatal("staged image remains")
		}
	})
	t.Run("signed with the akmods pair", func(t *testing.T) {
		src := fdeBuildSource()
		src.Files[mokPrivateKey] = []byte("PRIVATE")
		src.Files[MOKCertificate] = []byte("DER")
		src.Dirs["/sys/firmware/efi/efivars"] = []string{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"}
		src.Files["/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 1}
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target)
		argv := append(base, "--output="+target+".new", "--secureboot-private-key="+mokPrivateKey, "--secureboot-certificate="+MOKCertificate)
		src.Commands[nativetest.Key("ukify", argv...)] = []byte{}
		if err := buildFDEUKI(src, version, io.Discard, target); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("enforcement without a key blocks", func(t *testing.T) {
		src := fdeBuildSource()
		src.Dirs["/sys/firmware/efi/efivars"] = []string{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"}
		src.Files["/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 1}
		err := buildFDEUKI(src, version, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"))
		if err == nil || !strings.Contains(err.Error(), "enroll a MOK signing key") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("gate and version are enforced", func(t *testing.T) {
		src := fdeBuildSource()
		if err := buildFDEUKI(src, "6.1/../x", io.Discard, filepath.Join(t.TempDir(), "nimbus.efi")); err == nil {
			t.Fatal("expected an invalid version error")
		}
		src = fdeBuildSource()
		delete(src.Files, FDEUKIMarker)
		if err := buildFDEUKI(src, version, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi")); err == nil || !strings.Contains(err.Error(), "marker is absent") {
			t.Fatalf("got %v", err)
		}
	})
}

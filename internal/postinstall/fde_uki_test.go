package postinstall

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

const (
	fdeTestVersion = "6.19.10-300.fc44.x86_64"
	fdeTestBase    = "root=UUID=test ro rd.luks.uuid=luks-1"
	fdeTestCmdline = fdeTestBase + " rd.luks.options=1=discard,x-initrd.attach,tpm2-device=auto rd.shell=0 rd.emergency=reboot"
	fdeStagedText  = ".pcrsig:\n  size: 1 bytes\n.pcrpkey:\n  size: 1 bytes\n"
)

// fdeUCS2Hex renders the byte order efibootmgr prints for unterminated load
// option data: raw UTF-16LE bytes, low byte first.
func fdeUCS2Hex(value string) string {
	var out strings.Builder
	for _, r := range value {
		fmt.Fprintf(&out, "%02x%02x", byte(r&0xff), byte(r>>8))
	}
	return out.String()
}

// fdeTestKeys installs a synthetic key set in an isolated directory and
// restores the production path afterwards.
func fdeTestKeys(t *testing.T) fdeKeyPaths {
	t.Helper()
	previous := fdeKeyDir
	fdeKeyDir = t.TempDir()
	t.Cleanup(func() { fdeKeyDir = previous })
	keys := fdeKeys()
	for path, mode := range map[string]os.FileMode{
		keys.mokKey:     0o400,
		keys.mokCert:    0o644,
		keys.pcrPrivate: 0o400,
		keys.pcrPublic:  0o644,
		keys.mokDER:     0o644,
	} {
		if err := os.WriteFile(path, []byte("test key material"), mode); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func fdeBuildSource(t *testing.T) *nativetest.FakeSource {
	t.Helper()
	fdeTestKeys(t)
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	src.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	src.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
	src.Dirs["/sys/firmware/efi/efivars"] = []string{}
	return src
}

func fdeEnableSecureBoot(src *nativetest.FakeSource, enabled bool) {
	value := byte(0)
	if enabled {
		value = 1
	}
	const name = "SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
	src.Dirs["/sys/firmware/efi/efivars"] = []string{name}
	src.Files["/sys/firmware/efi/efivars/"+name] = []byte{6, 0, 0, 0, value}
}

func fdeBuildArgv(target string, secure bool) []string {
	keys := fdeKeys()
	argv := []string{
		"build",
		"--linux=" + fdeKernelDir + "/" + fdeTestVersion + "/vmlinuz",
		"--initrd=/boot/initramfs-" + fdeTestVersion + ".img",
		"--cmdline=" + fdeTestCmdline,
		"--os-release=@/etc/os-release",
	}
	if secure {
		argv = append(argv,
			"--secureboot-private-key="+keys.mokKey,
			"--secureboot-certificate="+keys.mokCert)
	}
	return append(argv,
		"--pcr-private-key="+keys.pcrPrivate,
		"--pcr-public-key="+keys.pcrPublic,
		"--phases=enter-initrd",
		"--pcr-banks=sha256",
		"--output="+target+".new")
}

func fdeStagedTarget(t *testing.T, target string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(target + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
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

func TestFDEUnlockOption(t *testing.T) {
	fixture := func() *nativetest.FakeSource {
		src := &nativetest.FakeSource{Commands: map[string][]byte{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
		src.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
		return src
	}
	src := fixture()
	option, err := fdeUnlockOption(src, fdeTestBase)
	if err != nil || option != "rd.luks.options=1=discard,x-initrd.attach,tpm2-device=auto" {
		t.Fatalf("got %q, %v", option, err)
	}
	src = fixture()
	src.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none none\n")
	if option, err = fdeUnlockOption(src, fdeTestBase); err != nil || option != "rd.luks.options=1=tpm2-device=auto" {
		t.Fatalf("none options got %q, %v", option, err)
	}
	if _, err := fdeUnlockOption(fixture(), "root=UUID=test ro"); err == nil {
		t.Fatal("a command line without a LUKS root was accepted")
	}
	src = fixture()
	src.Files[fdeCrypttab] = []byte("# empty\n")
	if _, err := fdeUnlockOption(src, fdeTestBase); err == nil {
		t.Fatal("a missing crypttab entry was accepted")
	}
}

func TestBuildFDEUKICurrent(t *testing.T) {
	existing := func(t *testing.T, target string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("skips an image built for another kernel", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		existing(t, target)
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target)] =
			[]byte(strings.Replace(fdeInspectOutput, "6.19.10-300.fc44.x86_64", "6.20.1-300.fc44.x86_64", 1))
		var out bytes.Buffer
		if err := buildFDEUKI(src, fdeTestVersion, &out, target, true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "already targets kernel 6.20.1-300.fc44.x86_64") {
			t.Fatalf("got %q", out.String())
		}
	})
	t.Run("an uninspectable image is not replaced", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		existing(t, target)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, true); err == nil ||
			!strings.Contains(err.Error(), "could not be inspected") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a stat failure is not treated as a missing image", func(t *testing.T) {
		src := fdeBuildSource(t)
		file := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(file, "nimbus.efi")
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, true); err == nil ||
			!strings.Contains(err.Error(), "could not be examined") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("an image without a kernel release is not replaced", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		existing(t, target)
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target)] = []byte(".uname:\n  size: 1 bytes\n")
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, true); err == nil ||
			!strings.Contains(err.Error(), "does not identify its kernel") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("an image for the same kernel is rebuilt", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		existing(t, target)
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target)] =
			[]byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(fdeStagedText)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, true); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(target); err != nil || string(data) == "existing" {
			t.Fatalf("image was not replaced: %q %v", data, err)
		}
	})
	t.Run("a missing image is built", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(fdeStagedText)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, true); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("image was not moved into place: %v", err)
		}
	})
}

func TestFDEUKIRebuildArgv(t *testing.T) {
	previous := fdeExecutable
	fdeExecutable = func() (string, error) { return "/usr/bin/nimbus", nil }
	t.Cleanup(func() { fdeExecutable = previous })
	src := fdeBuildSource(t)
	argv, err := FDEUKIRebuildArgv(src, fdeTestVersion)
	want := []string{"sudo", "--", "/usr/bin/nimbus", "internal", "fde-uki", "add", fdeTestVersion, "--only-if-current"}
	if err != nil || !slices.Equal(argv, want) {
		t.Fatalf("argv=%v err=%v", argv, err)
	}
	delete(src.Files, FDEUKIMarker)
	if argv, err := FDEUKIRebuildArgv(src, fdeTestVersion); err != nil || argv != nil {
		t.Fatalf("argv=%v err=%v", argv, err)
	}
	src.Files[FDEUKIMarker] = []byte("foreign")
	if _, err := FDEUKIRebuildArgv(src, fdeTestVersion); err == nil {
		t.Fatal("a foreign marker was accepted")
	}
}

func TestBuildFDEUKI(t *testing.T) {
	t.Run("installs atomically", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(fdeStagedText)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("image was not moved into place: %v", err)
		}
		if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
			t.Fatal("staged image remains")
		}
	})
	t.Run("a signed build verifies with the MOK certificate", func(t *testing.T) {
		src := fdeBuildSource(t)
		fdeEnableSecureBoot(src, true)
		keys := fdeKeys()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, true)...)] = []byte{}
		src.Commands[nativetest.Key("sbverify", "--cert", keys.mokCert, target+".new")] = []byte{}
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(fdeStagedText)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("signed image was not moved into place: %v", err)
		}
	})
	t.Run("a failed signature check keeps the previous image", func(t *testing.T) {
		src := fdeBuildSource(t)
		fdeEnableSecureBoot(src, true)
		keys := fdeKeys()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, true)...)] = []byte{}
		src.Failures[nativetest.Key("sbverify", "--cert", keys.mokCert, target+".new")] = "signature verification failed"
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false); err == nil || !strings.Contains(err.Error(), "did not verify") {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("an unverified image was moved into place")
		}
	})
	t.Run("the key material is required and never generated by the hook", func(t *testing.T) {
		src := fdeBuildSource(t)
		fdeKeyDir = filepath.Join(t.TempDir(), "missing")
		err := buildFDEUKI(src, fdeTestVersion, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"), false)
		if err == nil || !strings.Contains(err.Error(), "key material") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a failed build leaves the previous image", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, 1)
		if err := os.Rename(target+".new", target); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("current image"), 0o644); err != nil {
			t.Fatal(err)
		}
		src.Failures[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = "ukify failed"
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false); err == nil {
			t.Fatal("expected a ukify failure")
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "current image" {
			t.Fatalf("previous image changed: %q, %v", data, err)
		}
	})
	t.Run("an undersized image is rejected", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, 10)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
		err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false)
		if err == nil || !strings.Contains(err.Error(), "implausibly small") {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("an undersized image was moved into place")
		}
	})
	t.Run("an image without the PCR policy is rejected", func(t *testing.T) {
		src := fdeBuildSource(t)
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
		src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(".linux:\n  size: 1 bytes\n")
		err := buildFDEUKI(src, fdeTestVersion, io.Discard, target, false)
		if err == nil || !strings.Contains(err.Error(), ".pcrsig") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("gate and version are enforced", func(t *testing.T) {
		src := fdeBuildSource(t)
		if err := buildFDEUKI(src, "6.1/../x", io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"), false); err == nil {
			t.Fatal("expected an invalid version error")
		}
		src = fdeBuildSource(t)
		delete(src.Files, FDEUKIMarker)
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"), false); err == nil || !strings.Contains(err.Error(), "marker is absent") {
			t.Fatalf("got %v", err)
		}
		src = fdeBuildSource(t)
		src.Files[FDEUKIMarker] = []byte("foreign")
		if err := buildFDEUKI(src, fdeTestVersion, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"), false); err == nil || !strings.Contains(err.Error(), "unfamiliar content") {
			t.Fatalf("got %v", err)
		}
	})
}

// fdeGenkeySource simulates ukify genkey writing a real self-signed
// certificate so the DER conversion can be exercised.
type fdeGenkeySource struct {
	*nativetest.FakeSource
	t *testing.T
}

func (s *fdeGenkeySource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.t.Helper()
	if name != FDEUKITool || len(args) == 0 || args[0] != "genkey" {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Nimbus test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keys := fdeKeys()
	if err := os.WriteFile(keys.mokCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(keys.pcrPublic, []byte("public"), 0o600); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keys.mokKey, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(keys.pcrPrivate, []byte("private"), 0o600)
}

func TestEnsureFDEKeys(t *testing.T) {
	previous := fdeKeyDir
	fdeKeyDir = filepath.Join(t.TempDir(), "fde")
	t.Cleanup(func() { fdeKeyDir = previous })
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src := &fdeGenkeySource{FakeSource: base, t: t}
	keys, err := ensureFDEKeys(src, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		keys.mokKey:     0o400,
		keys.mokCert:    0o644,
		keys.pcrPrivate: 0o400,
		keys.pcrPublic:  0o644,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode=%v err=%v", path, info.Mode().Perm(), err)
		}
	}
	der, err := os.ReadFile(keys.mokDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x509.ParseCertificate(der); err != nil {
		t.Fatalf("the DER copy is not a certificate: %v", err)
	}
	if again, err := ensureFDEKeys(src, io.Discard); err != nil || again.mokKey != keys.mokKey {
		t.Fatalf("existing keys were not reused: %v", err)
	}
	// Partial material must block instead of silently completing the set.
	if err := os.Remove(keys.pcrPublic); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFDEKeys(src, io.Discard); err == nil || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("partial keys were accepted: %v", err)
	}
	// A missing public DER copy is derived data and is always regenerated.
	if err := os.WriteFile(keys.pcrPublic, []byte("public"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keys.mokDER); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFDEKeys(src, io.Discard); err != nil {
		t.Fatalf("the DER copy was not regenerated: %v", err)
	}
	if _, err := x509.ParseCertificate(mustRead(t, keys.mokDER)); err != nil {
		t.Fatalf("the regenerated DER copy is not a certificate: %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fdeStreamRecorder records the native commands streamed by a test.
type fdeStreamRecorder struct {
	*nativetest.FakeSource
	streams []string
}

func (s *fdeStreamRecorder) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	return s.FakeSource.Stream(nil, nil, name, args...)
}

func TestRemoveFDEUKI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		removed string
		rebuild bool
	}{
		{"the embedded kernel is rebuilt for the running one", fdeTestVersion, true},
		{"another kernel is kept", "7.2.5-200.fc44.x86_64", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fdeBuildSource(t)
			target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
			fdeStagedTarget(t, target, fdeMinimumSize+1)
			src.Commands[nativetest.Key(FDEUKITool, "inspect", target)] = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
			src.Commands[nativetest.Key("uname", "-r")] = []byte(fdeTestVersion + "\n")
			if tc.rebuild {
				src.Commands[nativetest.Key(FDEUKITool, fdeBuildArgv(target, false)...)] = []byte{}
				src.Commands[nativetest.Key(FDEUKITool, "inspect", target+".new")] = []byte(fdeStagedText)
			}
			recorder := &fdeStreamRecorder{FakeSource: src}
			if err := removeFDEUKI(recorder, tc.removed, io.Discard, target); err != nil {
				t.Fatal(err)
			}
			built := slices.ContainsFunc(recorder.streams, func(stream string) bool {
				return strings.Contains(stream, "build")
			})
			if built != tc.rebuild {
				t.Fatalf("rebuild recorded=%v, want %v", built, tc.rebuild)
			}
		})
	}
}

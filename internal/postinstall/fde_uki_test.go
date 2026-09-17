package postinstall

import (
	"io"
	"os"
	"path/filepath"
	"slices"
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

func TestBuildFDEUKI(t *testing.T) {
	const version = "6.19.10-300.fc44.x86_64"
	argvFor := func(target string) []string {
		return []string{
			"build",
			"--linux=" + fdeKernelDir + "/" + version + "/vmlinuz",
			"--initrd=/boot/initramfs-" + version + ".img",
			"--cmdline=root=UUID=test ro",
			"--output=" + target + ".new",
		}
	}
	t.Run("installs atomically", func(t *testing.T) {
		src := fdeBuildSource()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, argvFor(target)...)] = []byte{}
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
	t.Run("enforcement off without signing", func(t *testing.T) {
		src := fdeBuildSource()
		src.Dirs["/sys/firmware/efi/efivars"] = []string{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"}
		src.Files["/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 0}
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, fdeMinimumSize+1)
		src.Commands[nativetest.Key(FDEUKITool, argvFor(target)...)] = []byte{}
		if err := buildFDEUKI(src, version, io.Discard, target); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("enforcement without a shim chain blocks", func(t *testing.T) {
		src := fdeBuildSource()
		src.Dirs["/sys/firmware/efi/efivars"] = []string{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"}
		src.Files["/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 1}
		err := buildFDEUKI(src, version, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi"))
		if err == nil || !strings.Contains(err.Error(), "shim chain") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a failed build leaves the previous image", func(t *testing.T) {
		src := fdeBuildSource()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, 1)
		if err := os.Rename(target+".new", target); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("current image"), 0o644); err != nil {
			t.Fatal(err)
		}
		src.Failures[nativetest.Key(FDEUKITool, argvFor(target)...)] = "ukify failed"
		if err := buildFDEUKI(src, version, io.Discard, target); err == nil {
			t.Fatal("expected a ukify failure")
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "current image" {
			t.Fatalf("previous image changed: %q, %v", data, err)
		}
	})
	t.Run("an undersized image is rejected", func(t *testing.T) {
		src := fdeBuildSource()
		target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
		fdeStagedTarget(t, target, 10)
		src.Commands[nativetest.Key(FDEUKITool, argvFor(target)...)] = []byte{}
		err := buildFDEUKI(src, version, io.Discard, target)
		if err == nil || !strings.Contains(err.Error(), "implausibly small") {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("an undersized image was moved into place")
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
		src = fdeBuildSource()
		src.Files[FDEUKIMarker] = []byte("foreign")
		if err := buildFDEUKI(src, version, io.Discard, filepath.Join(t.TempDir(), "nimbus.efi")); err == nil || !strings.Contains(err.Error(), "unfamiliar content") {
			t.Fatalf("got %v", err)
		}
	})
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
		{"the embedded kernel is rebuilt for the running one", "6.19.10-300.fc44.x86_64", true},
		{"another kernel is kept", "7.2.5-200.fc44.x86_64", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fdeBuildSource()
			target := filepath.Join(t.TempDir(), "EFI", "Linux", "nimbus.efi")
			fdeStagedTarget(t, target, fdeMinimumSize+1)
			src.Commands[nativetest.Key(FDEUKITool, "inspect", target)] = []byte(fdeInspectOutput)
			src.Commands[nativetest.Key("uname", "-r")] = []byte("6.19.10-300.fc44.x86_64\n")
			if tc.rebuild {
				src.Commands[nativetest.Key(FDEUKITool, "build",
					"--linux="+fdeKernelDir+"/6.19.10-300.fc44.x86_64/vmlinuz",
					"--initrd=/boot/initramfs-6.19.10-300.fc44.x86_64.img",
					"--cmdline=root=UUID=test ro",
					"--output="+target+".new")] = []byte{}
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

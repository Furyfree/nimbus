package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

// The verification wrapper must send only the fixed image inspection through
// sudo; everything else passes through untouched.
func TestInternalFDEUKIAcceptsReconcileFlag(t *testing.T) {
	for _, args := range [][]string{
		{"add", "6.19.10-300.fc44.x86_64", "--only-if-current"},
		{"add", "6.19.10-300.fc44.x86_64"},
		{"remove", "6.19.10-300.fc44.x86_64"},
	} {
		cmd := newInternalFDEUKI()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "must run as root") {
			t.Fatalf("args %v: parse failed before the root check: %v", args, err)
		}
	}
}

func TestFDEVerificationSourceAllowlist(t *testing.T) {
	base := &nativetest.FakeSource{
		Commands: map[string][]byte{
			nativetest.Key("sudo", "-n", "--", postinstall.FDEUKITool, "inspect", postinstall.FDEUKIPath): []byte("image"),
		},
		Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{},
	}
	src := fdeVerificationSource{base}
	if got, err := src.Run(postinstall.FDEUKITool, "inspect", postinstall.FDEUKIPath); err != nil || string(got) != "image" {
		t.Fatalf("allowlisted inspection did not use sudo: %q, %v", got, err)
	}
	if _, err := src.Run("ukify", "inspect", "/boot/efi/EFI/Linux/other.efi"); err == nil {
		t.Fatal("a non-allowlisted ukify call reached the base source")
	}
}

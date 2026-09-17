package cli

import (
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

// The verification wrapper must send only the fixed image inspection through
// sudo; everything else passes through untouched.
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

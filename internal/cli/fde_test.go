package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

// The internal command parses flags and argument counts before its root gate;
// RunE is stubbed so the test never touches live state.
func TestInternalFDEUKIArgumentContract(t *testing.T) {
	const version = "6.19.10-300.fc44.x86_64"
	reached := errors.New("reached RunE")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"reconcile flag", []string{"add", version, "--only-if-current"}, "reached"},
		{"plain add", []string{"add", version}, "reached"},
		{"remove", []string{"remove", version}, "reached"},
		{"record", []string{"record", "2", "1"}, "reached"},
		{"genkey", []string{"genkey"}, "reached"},
		{"state", []string{"state"}, "reached"},
		{"hook-shaped add", []string{"add", version, "/boot/efi/linux", "/boot/vmlinuz"}, "accepts between"},
		{"unknown flag", []string{"add", version, "--bogus"}, "unknown flag"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := newInternalFDEUKI()
			cmd.RunE = func(*cobra.Command, []string) error { return reached }
			cmd.SetArgs(test.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if test.want == "reached" {
				if !errors.Is(err, reached) {
					t.Fatalf("args %v: parse did not reach RunE: %v", test.args, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("args %v: got %v, want %q", test.args, err, test.want)
			}
		})
	}
}

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

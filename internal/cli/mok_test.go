package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

func TestMOKVerificationClassifiesMissingCertificate(t *testing.T) {
	cat := nativetest.Key("sudo", "-n", "--", "/usr/bin/cat", "--", postinstall.MOKCertificate)
	for _, tc := range []struct {
		name    string
		failure string
		want    bool
	}{
		{"certificate missing", cat + ": /usr/bin/cat: " + postinstall.MOKCertificate + ": No such file or directory: exit status 1", true},
		{"cat missing", "sudo: /usr/bin/cat: No such file or directory", false},
		{"sudo password required", "sudo: a password is required", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &nativetest.FakeSource{Failures: map[string]string{cat: tc.failure}}
			_, err := (mokVerificationSource{src}).ReadFile(postinstall.MOKCertificate)
			if err == nil {
				t.Fatal("missing failure returned no error")
			}
			if got := errors.Is(err, os.ErrNotExist); got != tc.want {
				t.Fatalf("os.ErrNotExist = %t, want %t: %v", got, tc.want, err)
			}
			if !strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("native diagnostic lost: %v", err)
			}
		})
	}
}

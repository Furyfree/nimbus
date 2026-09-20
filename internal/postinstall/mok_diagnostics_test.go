package postinstall

import (
	"os"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

type certificateErrorSource struct {
	native.Source
	err error
}

func (s certificateErrorSource) ReadFile(string) ([]byte, error) { return nil, s.err }

func TestMOKCertificateDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status Status
		root   bool
		text   string
	}{
		{"missing", os.ErrNotExist, Blocked, false, "kmodgenca"},
		{"permission", os.ErrPermission, Unknown, true, "Permission denied"},
		{"other", os.ErrInvalid, Unknown, false, "could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := VerifyMOK(certificateErrorSource{err: &os.PathError{Op: "open", Path: MOKCertificate, Err: tc.err}}, Task{ID: "nvidia-mok", Status: Unknown})
			if task.Status != tc.status || task.VerificationNeedsRoot != tc.root || !strings.Contains(task.Detail, tc.text) {
				t.Fatal(task)
			}
		})
	}
	missing := VerifyMOK(certificateErrorSource{err: &os.PathError{Op: "open", Path: MOKCertificate, Err: os.ErrNotExist}}, Task{ID: "nvidia-mok", Status: Unknown})
	for _, line := range []string{"\n$ " + MOKKeyPairHint, "\n$ nimbus postinstall nvidia-mok"} {
		if !strings.Contains(missing.Detail, line) {
			t.Fatalf("the missing-certificate hint must list %q on its own line: %q", line, missing.Detail)
		}
	}
	src := &nativetest.FakeSource{Files: map[string][]byte{MOKCertificate: nil}}
	task := VerifyMOK(src, Task{ID: "nvidia-mok", Status: Unknown})
	if task.Status != Blocked || !strings.Contains(task.Detail, "empty") {
		t.Fatal(task)
	}
	src.Files[MOKCertificate] = []byte("public certificate")
	src.Commands = map[string][]byte{"mokutil --ignore-keyring --test-key " + MOKCertificate: []byte("unexpected result")}
	task = VerifyMOK(src, Task{ID: "nvidia-mok", Status: Complete, PreviouslyVerified: true})
	if task.Status != Unknown || task.PreviouslyVerified {
		t.Fatal("previous completion survived an inconclusive native check", task)
	}
}

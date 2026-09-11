package postinstall

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

// Different serial and subject-key IDs catch using the wrong certificate field
// for modinfo's PKCS#7 sig_key, which reports the certificate serial number.
func mokTestCertificate(t *testing.T) []byte {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(0x1234), SubjectKeyId: []byte{0xab, 0xcd}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

type mokSetupSource struct {
	*nativetest.FakeSource
	calls, streams                  []string
	files, enrollment, fail         string
	signed, buildUnsigned, noImport bool
	exitOne                         error
	cancel                          context.CancelFunc
	signature                       string
}

func (s *mokSetupSource) Run(name string, args ...string) ([]byte, error) {
	command := nativetest.Key(name, args...)
	s.calls = append(s.calls, command)
	if command == s.fail {
		return nil, errors.New("native failure")
	}
	if strings.HasPrefix(command, "sudo -n -- find ") {
		return []byte(s.files), nil
	}
	if command == "sudo -n -- mokutil --test-key "+mokCertificate {
		if s.enrollment == " is not enrolled" {
			return []byte(mokCertificate + s.enrollment), s.exitOne
		}
		return []byte(mokCertificate + s.enrollment), nil
	}
	if strings.HasPrefix(command, "modinfo -k test-kernel -F ") {
		if !s.signed {
			return nil, nil
		}
		if args[3] == "sig_key" {
			if s.signature != "" {
				return []byte(s.signature), nil
			}
			return []byte("12:34"), nil
		}
		return []byte("test-signer"), nil
	}
	return s.FakeSource.Run(name, args...)
}

func (s *mokSetupSource) Stream(_, _ io.Writer, name string, args ...string) error {
	command := nativetest.Key(name, args...)
	s.streams = append(s.streams, command)
	if command == s.fail {
		return errors.New("native failure")
	}
	switch command {
	case "sudo --validate", "sudo -- dracut --force --kver test-kernel", "nvidia-smi":
	case "sudo -- kmodgenca -a":
		s.files = mokCertificate + "\n" + mokPrivateKey
	case "sudo -- akmods --force --rebuild --akmod nvidia --kernels test-kernel":
		s.signed = !s.buildUnsigned
		if s.cancel != nil {
			s.cancel()
		}
	case "sudo -- mokutil --import " + mokCertificate:
		if !s.noImport {
			s.enrollment = " is already in the enrollment request"
		}
	default:
		return errors.New("unexpected native action: " + command)
	}
	return nil
}

func TestNVIDIAMOKSetup(t *testing.T) {
	exitOne := exec.Command("sh", "-c", "exit 1").Run()
	if exited, ok := errors.AsType[*exec.ExitError](exitOne); !ok || exited.ExitCode() != 1 {
		t.Fatal("could not prepare native exit-code fixture")
	}
	const build = "sudo -- akmods --force --rebuild --akmod nvidia --kernels test-kernel"
	const dracut = "sudo -- dracut --force --kver test-kernel"
	const enroll = "sudo -- mokutil --import " + mokCertificate
	for _, test := range []struct {
		name    string
		change  func(*mokSetupSource)
		wantErr string
		want    []string
	}{
		{"existing keys", nil, "", []string{"sudo --validate", build, dracut, enroll}},
		{"new keys", func(s *mokSetupSource) { s.files = "" }, "", []string{"sudo --validate", "sudo -- kmodgenca -a", build, dracut, enroll}},
		{"certificate only", func(s *mokSetupSource) { s.files = mokCertificate }, "incomplete", []string{"sudo --validate"}},
		{"private key only", func(s *mokSetupSource) { s.files = mokPrivateKey }, "incomplete", []string{"sudo --validate"}},
		{"native authentication failure", func(s *mokSetupSource) { s.fail = "sudo --validate" }, "native failure", []string{"sudo --validate"}},
		{"key inspection failure", func(s *mokSetupSource) {
			s.fail = "sudo -n -- find /etc/pki/akmods -maxdepth 2 ( -path " + mokCertificate + " -o -path " + mokPrivateKey + " ) -print"
		}, "inspect akmods", []string{"sudo --validate"}},
		{"cancel after build", nil, "context canceled", []string{"sudo --validate", build}},
		{"public certificate unreadable", func(s *mokSetupSource) { s.fail = "sudo -n -- cat " + mokCertificate }, "native failure", []string{"sudo --validate"}},
		{"invalid certificate", func(s *mokSetupSource) { s.Commands["sudo -n -- cat "+mokCertificate] = []byte("invalid") }, "invalid", []string{"sudo --validate"}},
		{"key generation failure", func(s *mokSetupSource) { s.files = ""; s.fail = "sudo -- kmodgenca -a" }, "create akmods", []string{"sudo --validate", "sudo -- kmodgenca -a"}},
		{"build failure", func(s *mokSetupSource) { s.fail = build }, "rebuild NVIDIA", []string{"sudo --validate", build}},
		{"wrong signing certificate", func(s *mokSetupSource) { s.signed = true; s.signature = "AB:CD" }, "do not all report signatures", []string{"sudo --validate", build}},
		{"build still unsigned", func(s *mokSetupSource) { s.buildUnsigned = true }, "do not all report signatures", []string{"sudo --validate", build}},
		{"boot image failure", func(s *mokSetupSource) { s.fail = dracut }, "refresh boot image", []string{"sudo --validate", build, dracut}},
		{"import failure", func(s *mokSetupSource) { s.fail = enroll }, "request MOK", []string{"sudo --validate", build, dracut, enroll}},
		{"import unverified", func(s *mokSetupSource) { s.noImport = true }, "could not be confirmed", []string{"sudo --validate", build, dracut, enroll}},
		{"pending request", func(s *mokSetupSource) { s.enrollment = " is already in the enrollment request" }, "", []string{"sudo --validate", build, dracut}},
		{"blocked key", func(s *mokSetupSource) { s.enrollment = " is blocked in dbx" }, "cannot establish", []string{"sudo --validate"}},
		{"unexpected mokutil output", func(s *mokSetupSource) { s.enrollment = "garbage" }, "cannot establish", []string{"sudo --validate"}},
		{"enrolled unsigned modules", func(s *mokSetupSource) { s.enrollment = " is already enrolled" }, "", []string{"sudo --validate", build, dracut}},
		{"trusted signed retry", func(s *mokSetupSource) { s.enrollment = " is already enrolled"; s.signed = true }, "", []string{"sudo --validate", dracut}},
		{"retry boot image", func(s *mokSetupSource) { s.signed = true }, "", []string{"sudo --validate", dracut, enroll}},
		{"disabled Secure Boot", func(s *mokSetupSource) { s.Commands["mokutil --sb-state"] = []byte("SecureBoot disabled") }, "requires confirmed enabled Secure Boot", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			src := &mokSetupSource{FakeSource: &nativetest.FakeSource{Commands: map[string][]byte{
				"mokutil --sb-state": []byte("SecureBoot enabled"), "uname -r": []byte("test-kernel"),
				"sudo -n -- cat " + mokCertificate: mokTestCertificate(t),
			}}, files: mokCertificate + "\n" + mokPrivateKey, enrollment: " is not enrolled", exitOne: exitOne}
			if test.change != nil {
				test.change(src)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.name == "cancel after build" {
				src.cancel = cancel
			}
			var out bytes.Buffer
			err := RunNVIDIAMOK(ctx, src, &out, &out)
			if (err == nil) != (test.wantErr == "") || (err != nil && !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("got %v, want %q; output=%s", err, test.wantErr, &out)
			}
			if !slices.Equal(src.streams, test.want) {
				t.Fatalf("native actions=%v, want=%v", src.streams, test.want)
			}
			if err == nil && src.enrollment == " is already in the enrollment request" && !strings.Contains(out.String(), "pending, not complete") {
				t.Fatalf("pending enrollment reported incorrectly: %s", &out)
			}
			if strings.Contains(strings.Join(src.calls, "\n"), "cat "+mokPrivateKey) {
				t.Fatal("read private key")
			}
		})
	}
}

func TestNVIDIASignatureIdentifiers(t *testing.T) {
	for _, key := range []string{"12:34", "00:12:34", "ab:cd", "", "12:35"} {
		src := &nativetest.FakeSource{Commands: map[string][]byte{}}
		for _, module := range []string{"nvidia", "nvidia_modeset", "nvidia_drm", "nvidia_uvm"} {
			src.Commands["modinfo -k test-kernel -F signer "+module] = []byte("test-signer")
			src.Commands["modinfo -k test-kernel -F sig_key "+module] = []byte("12:34")
		}
		// A bad companion module must fail even when nvidia itself is signed.
		src.Commands["modinfo -k test-kernel -F sig_key nvidia_uvm"] = []byte(key)
		if got, want := nvidiaSignaturesMatch(src, "test-kernel", "1234"), key == "12:34" || key == "00:12:34"; got != want {
			t.Fatalf("sig_key=%q: got %t, want %t", key, got, want)
		}
	}
}

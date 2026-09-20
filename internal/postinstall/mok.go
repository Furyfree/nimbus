package postinstall

import (
	"errors"
	"os"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

const (
	MOKCertificate = "/etc/pki/akmods/certs/public_key.der"
	// MOKKeyPairHint is the native command that creates the akmods signing
	// key pair when it is missing.
	MOKKeyPairHint = "sudo kmodgenca -a"
)

func mok(src native.Source, in Inputs) Task {
	t := Task{
		ID: "nvidia-mok", Owner: "component:nvidia", Title: "Sign NVIDIA modules and enroll their key",
		Status: Unknown, Detail: "Secure Boot state could not be established.",
		Prerequisites: []string{"NVIDIA and akmods packages have been applied.", "Secure Boot is enabled; existing signing keys are preserved."},
		Instructions: []string{
			"After approval, authenticate sudo and inspect the akmods key pair with elevated access.",
			"The akmods package generates the signing key pair through akmods-keygen before it builds modules; Nimbus preserves an existing or incomplete pair and never replaces it.",
			"When that signing key pair does not exist yet, generate it with: " + MOKKeyPairHint + ".",
			"If modules do not match the certificate: sudo -- akmods --force --rebuild --akmod nvidia --kernels <running-kernel>.",
			"Check every NVIDIA module's signer and certificate serial, then refresh the boot image: sudo -- dracut --force --kver <running-kernel>.",
			"If needed: sudo -- mokutil --import /etc/pki/akmods/certs/public_key.der. Enter the temporary password in mokutil's native prompt.",
			"Until enrollment, the NVIDIA modules are untrusted under Secure Boot and the driver does not load; do not skip the blue Enroll MOK screen at this reboot.",
			"Existing trust and pending requests are preserved. Reboot yourself when ready; Enroll MOK -> Continue -> Yes -> password -> Reboot (US/QWERTY keyboard).",
		},
		Verification: "After reboot, run nvidia-smi and mokutil --sb-state. Certificate enrollment alone does not prove the driver loads.",
		Recovery:     "Cancel enrollment before confirming it, or boot a working kernel and inspect the akmods Secure Boot instructions before changing keys.",
		Reboot:       true,
		BeforeReboot: true,
	}
	if !in.Facts.SecureBoot.Known() {
		return t
	}
	switch in.Facts.SecureBoot.Value {
	case inspect.SecureBootDisabled, inspect.SecureBootUnavailable:
		t.Status, t.Detail, t.Reboot, t.BeforeReboot = NotApplicable, "MOK enrollment is not required in the observed boot mode. This does not certify Secure Boot policy or driver readiness.", false, false
		return t
	case inspect.SecureBootEnabled:
	default:
		return t
	}
	for _, name := range []string{"akmod-nvidia", "akmods", "mokutil"} {
		found := false
		for _, pkg := range in.Resolved.Packages {
			if pkg.Name != name {
				continue
			}
			found = true
			if status, detail := packageReady(in, pkg); status != Complete {
				t.Status, t.Detail = status, detail
				return t
			}
		}
		if !found {
			t.Status, t.Detail = Blocked, "The selected NVIDIA component is missing the "+name+" package prerequisite."
			return t
		}
	}
	for _, tool := range []string{"sudo", "akmods", "dracut", "mokutil", "modinfo", "nvidia-smi"} {
		if _, err := src.LookPath(tool); err != nil {
			t.Status, t.Detail = Blocked, tool+" is unavailable; repair the NVIDIA packages before setup."
			return t
		}
	}
	// Only an unenrolled machine offers the mutating setup; Complete records
	// verification, Blocked explains itself and Unknown keeps the read-only
	// root check so approval never skips the existing state.
	verified := VerifyMOK(src, t)
	if verified.Status == Pending {
		verified.Action = &Action{Kind: SetupNVIDIA}
	}
	return verified
}

// VerifyMOK verifies only the fixed public akmods certificate after the caller
// has established task applicability and package prerequisites.
func VerifyMOK(src native.Source, t Task) Task {
	t.PreviouslyVerified = false
	t.VerificationNeedsRoot = false
	t.Status = Unknown
	data, err := src.ReadFile(MOKCertificate)
	switch {
	case errors.Is(err, os.ErrPermission):
		t.Status, t.Detail, t.VerificationNeedsRoot = Unknown, "Permission denied reading the akmods public certificate; approved setup inspects it with sudo before changing anything.", true
		return t
	case errors.Is(err, os.ErrNotExist):
		t.Status, t.Detail = Blocked, "The akmods public certificate is missing at "+MOKCertificate+".\nGenerate the signing key pair, then run this task again:\n$ "+MOKKeyPairHint+"\n$ nimbus postinstall nvidia-mok"
		return t
	case err != nil:
		t.Status, t.Detail = Unknown, "The akmods public certificate could not be read: "+err.Error()+".\nIf akmods has not generated the key pair yet, generate it, then run this task again:\n$ "+MOKKeyPairHint+"\n$ nimbus postinstall nvidia-mok"
		return t
	case len(data) == 0:
		t.Status, t.Detail = Blocked, "The akmods public certificate is empty; inspect akmods key generation before enrollment."
		return t
	}
	// mokutil 0.7.2 returns 1 when this certificate is already enrolled or
	// pending, and 0 when not enrolled. Ignore the separate kernel-keyring
	// shortcut: it does not prove MOK/firmware enrollment and reports on stderr.
	out, err := src.Run("mokutil", "--ignore-keyring", "--test-key", MOKCertificate)
	code := 0
	if err != nil {
		code = -1
		if exit, ok := errors.AsType[interface {
			error
			ExitCode() int
		}](err); ok {
			code = exit.ExitCode()
		}
	}
	switch strings.TrimSpace(string(out)) {
	case MOKCertificate + " is already enrolled", MOKCertificate + " is already in db":
		if code == 1 {
			t.Status, t.Detail, t.Reboot = Complete, "The akmods public certificate is enrolled or trusted by the firmware database.", false
			return t
		}
	case MOKCertificate + " is not enrolled":
		if code == 0 {
			t.Status, t.Detail = Pending, "The akmods public certificate is not enrolled."
			return t
		}
	case MOKCertificate + " is already in the enrollment request":
		if code == 1 {
			t.Status, t.Detail, t.BeforeReboot = Pending, "Enrollment has been requested but still needs confirmation in the MOK manager after reboot.", false
			return t
		}
	case MOKCertificate + " is blocked in dbx", MOKCertificate + " is blocked in MokListX":
		if code == 1 {
			t.Status, t.Detail = Blocked, "The akmods public certificate is blocked by a firmware or MOK denylist; inspect native trust policy before changing keys."
			return t
		}
	}
	t.Detail = "mokutil did not establish certificate enrollment; inspect its native status before changing keys."
	return t
}

package postinstall

import (
	"errors"
	"os"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

const MOKCertificate = "/etc/pki/akmods/certs/public_key.der"

func mok(src native.Source, in Inputs) Task {
	t := Task{
		ID: "nvidia-mok", Owner: "component:nvidia", Title: "Enroll the NVIDIA module signing key",
		Status: Unknown, Detail: "Secure Boot state could not be established.",
		Prerequisites: []string{"NVIDIA and akmods packages have been applied.", "Secure Boot is enabled and the akmods public certificate exists."},
		Instructions:  []string{"Follow /usr/share/doc/akmods/README.secureboot to inspect the generated certificate and request enrollment with mokutil.", "Complete enrollment in the firmware MOK manager at reboot; never supply the password through Nimbus."},
		Verification:  "mokutil --ignore-keyring --test-key /etc/pki/akmods/certs/public_key.der must confirm enrollment. This does not by itself prove the NVIDIA driver loads.",
		Recovery:      "Cancel enrollment before confirming it, or boot a working kernel and inspect the akmods Secure Boot instructions before changing keys.",
		Reboot:        true,
	}
	if !in.Facts.SecureBoot.Known() {
		return t
	}
	switch in.Facts.SecureBoot.Value {
	case inspect.SecureBootDisabled, inspect.SecureBootUnavailable:
		t.Status, t.Detail, t.Reboot = NotApplicable, "MOK enrollment is not required in the observed boot mode. This does not certify Secure Boot policy or driver readiness.", false
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
	if _, err := src.LookPath("mokutil"); err != nil {
		t.Status, t.Detail = Blocked, "mokutil is unavailable; run nimbus sync."
		return t
	}
	return VerifyMOK(src, t)
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
		t.Status, t.Detail, t.VerificationNeedsRoot = Unknown, "Permission denied reading the akmods public certificate; run nimbus postinstall nvidia-mok for an approved read-only administrator check.", true
		return t
	case errors.Is(err, os.ErrNotExist):
		t.Status, t.Detail = Blocked, "The akmods public certificate is missing at "+MOKCertificate+"; inspect akmods key generation before enrollment."
		return t
	case err != nil:
		t.Status, t.Detail = Unknown, "The akmods public certificate could not be read: "+err.Error()
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
			t.Status, t.Detail = Pending, "Enrollment has been requested but still needs confirmation in the MOK manager after reboot."
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

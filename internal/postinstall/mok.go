package postinstall

import (
	"crypto/x509"
	"encoding/hex"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

const mokCertificate = "/etc/pki/akmods/certs/public_key.der"

func mok(src native.Source, in Inputs) Task {
	t := Task{
		ID: "nvidia-mok", Owner: "component:nvidia", Title: "Sign NVIDIA modules and enroll their key",
		Status: Unknown, Detail: "Secure Boot state could not be established.",
		Prerequisites: []string{"NVIDIA and akmods packages have been applied.", "Secure Boot is enabled; existing signing keys are preserved."},
		Instructions: []string{
			"After approval, authenticate sudo and inspect the akmods key pair with elevated access.",
			"Only if both key files are missing: sudo -- kmodgenca -a. Never replace an existing or incomplete pair.",
			"If modules do not match the certificate: sudo -- akmods --force --rebuild --akmod nvidia --kernels <running-kernel>.",
			"Check every NVIDIA module's signer and certificate serial, then refresh the boot image: sudo -- dracut --force --kver <running-kernel>.",
			"If needed: sudo -- mokutil --import /etc/pki/akmods/certs/public_key.der. Enter the temporary password in mokutil's native prompt.",
			"Existing trust and pending requests are preserved. Reboot yourself when ready; Enroll MOK -> Continue -> Yes -> password -> Reboot (US/QWERTY keyboard).",
		},
		Verification: "After reboot, run nvidia-smi and mokutil --sb-state. Certificate enrollment alone does not prove the driver loads.",
		Recovery:     "Cancel enrollment before confirming it, or boot a working kernel and inspect the akmods Secure Boot instructions before changing keys.",
		Reboot:       true,
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
	for _, tool := range []string{"sudo", "kmodgenca", "akmods", "dracut", "mokutil", "modinfo", "nvidia-smi"} {
		if _, err := src.LookPath(tool); err != nil {
			t.Status, t.Detail = Blocked, tool+" is unavailable; repair the NVIDIA packages before setup."
			return t
		}
	}
	t.Action = &Action{Kind: SetupNVIDIA}

	data, readErr := src.ReadFile(mokCertificate)
	if readErr != nil || len(data) == 0 {
		t.Detail = "The public certificate cannot be inspected as this user. Approved setup checks it with sudo; this does not prove it is missing."
		return t
	}
	out, err := src.Run("mokutil", "--test-key", mokCertificate)
	switch strings.TrimSpace(string(out)) {
	case mokCertificate + " is already enrolled", mokCertificate + " is already in db":
		if err == nil {
			t.Status, t.Detail = Pending, "The akmods certificate is trusted; NVIDIA driver readiness still needs checking."
			cert, certErr := x509.ParseCertificate(data)
			kernel, kernelErr := src.Run("uname", "-r")
			if certErr == nil && kernelErr == nil && nvidiaSignaturesMatch(src, strings.TrimSpace(string(kernel)), hex.EncodeToString(cert.SerialNumber.Bytes())) {
				if _, err := src.Run("nvidia-smi", "--query-gpu=name", "--format=csv,noheader"); err == nil {
					t.Status, t.Detail, t.Reboot = Complete, "The akmods certificate is trusted and NVIDIA responds with Secure Boot enabled.", false
				}
			}
			return t
		}
	case mokCertificate + " is not enrolled":
		if err != nil {
			t.Status, t.Detail = Pending, "The akmods public certificate is not enrolled."
			return t
		}
	case mokCertificate + " is already in the enrollment request":
		if err == nil {
			t.Status, t.Detail = Pending, "Enrollment has been requested but still needs confirmation in the MOK manager after reboot."
			return t
		}
	}
	t.Detail = "mokutil did not establish certificate enrollment; inspect its native status before changing keys."
	return t
}

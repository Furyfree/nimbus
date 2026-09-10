package postinstall

import (
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

const mokCertificate = "/etc/pki/akmods/certs/public_key.der"

func mok(src native.Source, in Inputs) Task {
	t := Task{
		ID: "nvidia-mok", Owner: "component:nvidia", Title: "Enroll the NVIDIA module signing key",
		Status: Unknown, Detail: "Secure Boot state could not be established.",
		Prerequisites: []string{"NVIDIA and akmods packages have been applied.", "Secure Boot is enabled and the akmods public certificate exists."},
		Instructions:  []string{"Follow /usr/share/doc/akmods/README.secureboot to inspect the generated certificate and request enrollment with mokutil.", "Complete enrollment in the firmware MOK manager at reboot; never supply the password through Nimbus."},
		Verification:  "mokutil --test-key /etc/pki/akmods/certs/public_key.der must confirm enrollment. This does not by itself prove the NVIDIA driver loads.",
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
	if data, err := src.ReadFile(mokCertificate); err != nil || len(data) == 0 {
		t.Detail = "The akmods public certificate is absent or unreadable; inspect the akmods setup before enrollment."
		return t
	}
	out, err := src.Run("mokutil", "--test-key", mokCertificate)
	switch strings.TrimSpace(string(out)) {
	case mokCertificate + " is already enrolled", mokCertificate + " is already in db":
		if err == nil {
			t.Status, t.Detail, t.Reboot = Complete, "The akmods public certificate is enrolled or trusted by the firmware database.", false
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

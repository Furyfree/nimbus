package postinstall

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

const fprintService = "net.reactivated.Fprint"

func fingerprint(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "fingerprint", Owner: "package:" + pkg.Canonical, Title: "Enroll a fingerprint",
		Status: Unknown, Detail: "Fingerprint device and enrollment state could not be established without starting fprintd.",
		Prerequisites: []string{"The selected fprintd package is applied and a supported reader is connected."},
		Instructions:  []string{"Run fprintd-enroll as your normal user when ready to enroll a finger.", "Verify a recorded finger with fprintd-verify before relying on fingerprint authentication."},
		Verification:  "The running fprintd service reports an enrolled finger for the invoking user. Enrollment alone does not establish PAM or greeter integration.",
		Recovery:      "Keep password login available. Manage unwanted enrollments through fprintd; Nimbus does not read or store biometric templates.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	if _, err := src.LookPath("busctl"); err != nil {
		return t
	}
	devices, err := fprintCall(src, "/net/reactivated/Fprint/Manager", fprintService+".Manager", "GetDevices", "ao")
	if err != nil {
		return t
	}
	if len(devices) == 0 {
		t.Status, t.Detail = NotApplicable, "The running fprintd service reports no supported fingerprint reader."
		return t
	}
	unknown := false
	for _, device := range devices {
		if !strings.HasPrefix(device, "/net/reactivated/Fprint/Device/") || strings.ContainsAny(device, " \t\r\n") {
			unknown = true
			continue
		}
		// An empty username means the D-Bus caller, avoiding authorization
		// requests for another user's enrollment records.
		fingers, err := fprintCall(src, device, fprintService+".Device", "ListEnrolledFingers", "as", "s", "")
		if err != nil {
			unknown = true
			continue
		}
		if len(fingers) > 0 {
			valid := true
			for _, finger := range fingers {
				side, name, ok := strings.Cut(finger, "-")
				valid = valid && ok && (side == "left" || side == "right") && (name == "thumb" || name == "index-finger" || name == "middle-finger" || name == "ring-finger" || name == "little-finger")
			}
			if valid {
				t.Status, t.Detail = Complete, "The running fprintd service reports an enrolled finger for this user."
				return t
			}
			unknown = true
		}
	}
	if unknown {
		t.Detail = "A fingerprint reader is present, but enrollment could not be determined without interactive authorization."
		return t
	}
	t.Status, t.Detail = Pending, "A supported fingerprint reader is present and reports no enrolled fingers for this user."
	if _, err := src.LookPath("fprintd-enroll"); err == nil {
		t.Action = &Action{Kind: EnrollFingerprint, Argv: []string{"fprintd-enroll"}}
	}
	return t
}

func fprintCall(src native.Source, path, iface, method, signature string, args ...string) ([]string, error) {
	argv := []string{"--system", "--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call", fprintService, path, iface, method}
	argv = append(argv, args...)
	out, err := src.Run("busctl", argv...)
	if err != nil {
		// busctl drops the D-Bus error name. fprintd's NoEnrolledPrints
		// becomes this exact message; other failures must remain unknown.
		noPrints := "busctl " + strings.Join(argv, " ") + ": Call failed: No fingerprints enrolled: exit status 1"
		if method == "ListEnrolledFingers" && len(out) == 0 && err.Error() == noPrints {
			return []string{}, nil
		}
		return nil, err
	}
	var response struct {
		Type string     `json:"type"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, err
	}
	if response.Type != signature || len(response.Data) != 1 || response.Data[0] == nil {
		return nil, fmt.Errorf("unexpected fprintd response")
	}
	return response.Data[0], nil
}

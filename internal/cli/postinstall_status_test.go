package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/postinstall"
)

func TestPostinstallChecklistKeepsDetailsSeparate(t *testing.T) {
	view := postinstallView{Machine: "vm", Tasks: []postinstall.Task{
		{ID: "onepassword", Status: postinstall.Complete, Detail: "GUI prerequisites confirmed; local configuration checked. Current vault unlock and remote access are not inspected by status.", Removal: "Change integration through Chezmoi."},
		{ID: "nvidia-mok", Status: postinstall.Unknown, PreviouslyVerified: true, VerificationNeedsRoot: true, Detail: "Enrollment verified earlier; current check requires sudo."},
		{ID: "noctalia-plugins", Status: postinstall.Blocked, Detail: "Required plugin files are missing."},
	}}
	var out bytes.Buffer
	if err := renderPostinstallStatus(&out, view); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Configured", "Local files checked; sign-in unchecked", "Unable to check", "Current check needs authorization", "Required plugin files are missing."} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "Previously verified") || strings.Contains(text, "Verified") {
		t.Fatalf("historical success used as current status: %s", text)
	}
	if strings.Contains(text, "Removal:") || strings.Contains(text, "Recheck:") || strings.Contains(text, view.Tasks[0].Detail) {
		t.Fatal("verbose detail leaked into compact status", text)
	}
	for line := range strings.Lines(text) {
		if len([]rune(strings.TrimSuffix(line, "\n"))) > 80 {
			t.Fatalf("compact row exceeds 80 columns: %q", line)
		}
	}
	out.Reset()
	if err := renderPostinstallStatus(&out, view, true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{view.Tasks[0].Detail, "Removal: " + view.Tasks[0].Removal, "Enrollment verified earlier", "Recheck: nimbus postinstall nvidia-mok (requests sudo)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("verbose status lost %q: %s", want, out.String())
		}
	}
	out.Reset()
	if err := writeJSON(&out, view, nil); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), view.Tasks[0].Detail) || !strings.Contains(out.String(), `"status": "unknown"`) {
		t.Fatalf("native details changed: %s", out.String())
	}
}

func TestCompactServiceStatusKeepsLiveFacts(t *testing.T) {
	view := postinstallView{Machine: "laptop", Tasks: []postinstall.Task{
		{ID: "fingerprint", Status: postinstall.Pending},
		{ID: "tailscale-operator", Status: postinstall.Complete, CurrentState: "Stopped", Summary: "Signed in; operator configured"},
		{ID: "voxtype", Status: postinstall.Complete, CurrentState: "Running", Summary: "Startup enabled · model: small"},
		{ID: "zeron", Status: postinstall.Complete, CurrentState: "Absent", Summary: "Daemon absent · GUI retained"},
	}}
	var out bytes.Buffer
	if err := renderPostinstallStatus(&out, view); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  fingerprint          Pending        No fingers enrolled\n",
		"  tailscale-operator   Stopped        Signed in; operator configured\n",
		"  voxtype              Running        Startup enabled · model: small\n",
		"  zeron                Off            Daemon absent · GUI retained\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing aligned row %q in %s", want, out.String())
		}
	}
}

func TestPostinstallHelpExplainsVerificationLimits(t *testing.T) {
	for _, task := range []struct{ id, limit string }{
		{"onepassword", "It does not inspect current vault unlock"},
		{"nvidia-mok", "Enrollment alone does not prove the NVIDIA driver loads"},
	} {
		code, out, errOut := run(t, "postinstall", task.id, "--help")
		if code != 0 || !strings.Contains(out, task.limit) {
			t.Fatal(code, out, errOut)
		}
	}
}

func TestTaskRemovalGuidanceAndIdleFingerprintReport(t *testing.T) {
	for _, spec := range taskDescriptions {
		if postinstallRemoval(spec.id) == "" {
			t.Errorf("missing removal guidance: %s", spec.id)
		}
		code, out, errOut := run(t, "postinstall", spec.id, "--help")
		if code != 0 || !strings.Contains(out, "Removal:") || !strings.Contains(out, "does not disable or uninstall") {
			t.Fatal(code, out, errOut)
		}
	}
	var out bytes.Buffer
	result := syncResult{Tasks: []postinstall.Task{{ID: "fingerprint", Status: postinstall.Unknown, ActivationRequired: true, Detail: "fprintd is idle"}}}
	if err := renderFinalDetails(&out, &result, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Verification problems") || !strings.Contains(out.String(), "Remaining setup") {
		t.Fatal(out.String())
	}
	result.Tasks[0].ActivationRequired = false
	out.Reset()
	if err := renderFinalDetails(&out, &result, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Verification problems") {
		t.Fatal("real inspection failure was hidden", out.String())
	}
}

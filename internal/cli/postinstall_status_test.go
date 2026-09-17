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
		{ID: "onepassword", Status: postinstall.Complete, Detail: "GUI prerequisites confirmed; local configuration checked. Current vault unlock and remote access are not inspected by status."},
		{ID: "nvidia-mok", Status: postinstall.Unknown, PreviouslyVerified: true, VerificationNeedsRoot: true, Detail: "Enrollment verified earlier; current check requires sudo."},
		{ID: "noctalia-plugins", Status: postinstall.Blocked, Detail: "Required plugin files are missing."},
	}}
	var out bytes.Buffer
	if err := renderPostinstallStatus(&out, view); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"GUI and local integration checked.", "Previously verified", "Recheck requires sudo.\n    Recheck: nimbus postinstall nvidia-mok (requests sudo)\n", "Required plugin files are missing."} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for line := range strings.Lines(text) {
		if len(strings.TrimSuffix(line, "\n")) > 80 {
			t.Fatalf("checklist line exceeds 80 columns: %q", line)
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

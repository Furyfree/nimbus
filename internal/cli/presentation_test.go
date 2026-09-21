package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/doctor"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

func TestDoctorKeepsOneResourceNameAndIndentedDetails(t *testing.T) {
	var b bytes.Buffer
	writeDoctorCheck(&b, doctor.Check{ID: "file:/etc/sample", Status: doctor.Pass, Observation: "/etc/sample: matches\nowner root:root"})
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if strings.Count(b.String(), "/etc/sample") != 1 || len(lines) != 2 || strings.TrimSpace(lines[1]) != "owner root:root" || !strings.HasPrefix(lines[1], " ") {
		t.Fatal(b.String())
	}
}

func TestMOKPreviewShowsSetupOnlyWithoutHistoricalEvidence(t *testing.T) {
	task := postinstall.Task{ID: "nvidia-mok", Title: "Enroll key", Status: postinstall.Unknown, PreviouslyVerified: true, VerificationNeedsRoot: true, Instructions: []string{"Complete enrollment in the MOK manager."}}
	var b bytes.Buffer
	if err := renderPostinstall(&b, postinstallView{Machine: "vm", Tasks: []postinstall.Task{task}}); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	if !strings.Contains(text, "[Unable to check]") || !strings.Contains(text, "verified earlier") || !strings.Contains(text, "read-only") || strings.Contains(text, "MOK manager") {
		t.Fatal(text)
	}
	task.PreviouslyVerified = false
	b.Reset()
	if err := renderPostinstall(&b, postinstallView{Machine: "vm", Tasks: []postinstall.Task{task}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "[Unable to check]") || !strings.Contains(b.String(), "MOK manager") {
		t.Fatal(b.String())
	}
	task.Status = postinstall.Pending
	b.Reset()
	if err := renderPostinstall(&b, postinstallView{Machine: "vm", Tasks: []postinstall.Task{task}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "[Pending]") || !strings.Contains(b.String(), "MOK manager") {
		t.Fatal(b.String())
	}
}

func TestUpgradePreviewFollowsExecutionOrder(t *testing.T) {
	root, src := installerFixture(t)
	code, out, errOut := run(t, "sync", "--upgrade", "--plan", "--checkout", root, "--machine", "vm")
	first, plan, last := strings.Index(out, "1. Request sudo"), strings.Index(out, "plan for"), strings.Index(out, "Run Topgrade")
	if code != 0 || first < 0 || plan <= first || last <= plan || !strings.Contains(out, "apply Chezmoi once.") || !strings.Contains(out, "zeron daemon uninstall") || len(src.calls) != 0 {
		t.Fatal(code, out, errOut, src.calls)
	}
}

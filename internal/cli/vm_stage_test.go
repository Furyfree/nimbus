package cli

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestVMCandidateStaging(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-B", filepath.Join(repoRoot(t), "tools/vm/test_stage.py"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("candidate staging regressions: %v\n%s", err, output)
	}
}

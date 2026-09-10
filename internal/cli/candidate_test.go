package cli

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/version"
)

func TestCandidateDefinitionsRequireCompatibleEngine(t *testing.T) {
	saved := version.Engine
	t.Cleanup(func() { version.Engine = saved })
	for _, v := range []string{"0.1.1", "0.2.0", "0.2.3", "0.3.0", "0.0.0-dev.candidate"} {
		version.Engine = v
		code, out, errOut := run(t, "validate", "--checkout", repoRoot(t))
		if strings.HasPrefix(v, "0.1.") || strings.HasPrefix(v, "0.2.") {
			if code != ExitFailure || !strings.Contains(out+errOut, "min_engine 0.3.0 is newer") {
				t.Fatalf("old engine: %d %s %s", code, out, errOut)
			}
		} else if code != ExitOK {
			t.Fatalf("candidate %s: %d %s %s", v, code, out, errOut)
		}
	}
}

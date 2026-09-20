package cli

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/version"
)

func TestCandidateDefinitionsRequireCompatibleEngine(t *testing.T) {
	saved := version.Engine
	t.Cleanup(func() { version.Engine = saved })
	// Develop builds (<base>~dev.<stamp>) and local candidates are not
	// releases, so the release floor does not bind them.
	current := map[string]bool{"0.6.1": true, "0.6.2": true, "0.6.1~dev.20260920193406": true, "0.0.0-dev.candidate": true}
	for _, v := range []string{"0.1.1", "0.2.0", "0.2.3", "0.3.0", "0.3.1", "0.3.2", "0.4.0", "0.4.1", "0.4.2", "0.4.3", "0.4.6", "0.5.0", "0.5.2", "0.5.3", "0.5.10", "0.6.0", "0.6.1", "0.6.2", "0.6.1~dev.20260920193406", "0.0.0-dev.candidate"} {
		version.Engine = v
		code, out, errOut := run(t, "validate", "--checkout", repoRoot(t))
		if !current[v] {
			if code != ExitFailure || !strings.Contains(out+errOut, "min_engine 0.6.1 is newer") {
				t.Fatalf("old engine: %d %s %s", code, out, errOut)
			}
		} else if code != ExitOK {
			t.Fatalf("candidate %s: %d %s %s", v, code, out, errOut)
		}
	}
}

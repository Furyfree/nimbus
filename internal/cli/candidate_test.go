package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/version"
)

func TestCandidateDefinitionsRequireCompatibleEngine(t *testing.T) {
	saved := version.Engine
	t.Cleanup(func() { version.Engine = saved })
	root, _ := installerFixture(t)
	path := filepath.Join(root, "nimbus.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `min_engine = "0.0.0"`, `min_engine = "2.3.0"`, 1)), 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version    string
		compatible bool
	}{
		{"2.2.9", false}, {"2.3.0", true}, {"2.4.0", true},
		{"2.2.0~dev.20260920193406", true}, {"0.0.0-dev.candidate", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			version.Engine = tc.version
			code, out, errOut := run(t, "validate", "--checkout", root)
			if tc.compatible {
				if code != ExitOK {
					t.Fatalf("compatible engine: %d %s%s", code, out, errOut)
				}
			} else if code != ExitFailure || !strings.Contains(out+errOut, "min_engine 2.3.0 is newer") {
				t.Fatalf("incompatible engine: %d %s%s", code, out, errOut)
			}
		})
	}
}

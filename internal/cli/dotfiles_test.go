package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/selector"
)

func TestDotfilesDiffDoesNotNeedASelectorOrOperationLock(t *testing.T) {
	root, src := installerFixture(t)
	withSource(t, streamOutputSource{src.FakeSource})
	t.Setenv("XDG_RUNTIME_DIR", "")
	src.Commands["chezmoi diff"] = []byte("a local configuration diff\n")
	code, out, errOut := run(t, "dotfiles", "diff")
	if code != ExitOK || !strings.Contains(out, "a local configuration diff") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "operation.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("diff created a lock")
	}
}

func TestDotfilesUpdateDelegatesNativeLifecycleAndKeepsJSONSeparate(t *testing.T) {
	root, src := installerFixture(t)
	path, _ := selector.DefaultPath()
	if err := selector.Write(path, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: "vm", Origin: "github.com/Furyfree/nimbus"}); err != nil {
		t.Fatal(err)
	}
	src.Commands["chezmoi update"] = []byte("native pull and apply output\n")
	code, out, errOut := run(t, "dotfiles", "update", "--json")
	var env struct{ Data []runStep }
	if code != ExitOK || json.Unmarshal([]byte(out), &env) != nil || len(env.Data) != 1 || env.Data[0].Status != "succeeded" {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "native pull and apply output") || len(src.calls) != 1 || src.calls[0] != "chezmoi update" {
		t.Fatalf("native delegation: %v %s", src.calls, errOut)
	}
}

func TestDotfilesApplyReportsToolScriptFailure(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			root, src := installerFixture(t)
			path, _ := selector.DefaultPath()
			if err := selector.Write(path, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: "vm", Origin: "github.com/Furyfree/nimbus"}); err != nil {
				t.Fatal(err)
			}
			src.Failures["chezmoi apply"] = "Mise install script: exit status 23"
			args := []string{"dotfiles", "apply"}
			if jsonOutput {
				args = append(args, "--json")
			}
			code, out, errOut := run(t, args...)
			if code != ExitFailure || !strings.Contains(out, "failed") || !strings.Contains(out, "exit status 23") {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			if len(src.calls) != 1 || src.calls[0] != "chezmoi apply" {
				t.Fatalf("expected a single native apply: %v", src.calls)
			}
		})
	}
}

type streamOutputSource struct{ *facts.FakeSource }

func (s streamOutputSource) Stream(out, _ io.Writer, name string, args ...string) error {
	data, err := s.Run(name, args...)
	if _, writeErr := out.Write(data); writeErr != nil {
		return writeErr
	}
	return err
}

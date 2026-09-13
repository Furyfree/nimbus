package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/output"
	"github.com/spf13/cobra"
)

func TestCommandHelpUsesSharedColors(t *testing.T) {
	saved := terminalColors
	t.Cleanup(func() { terminalColors = saved })
	root, _ := newRoot()
	var visit func(*cobra.Command, []string)
	visit = func(command *cobra.Command, args []string) {
		if command.Hidden {
			return
		}
		invocation := append(append([]string{}, args...), "--help")
		terminalColors = func(io.Writer) bool { return false }
		plainCode, plain, plainErr := run(t, invocation...)
		terminalColors = func(io.Writer) bool { return true }
		code, colored, errOut := run(t, invocation...)
		strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
		if code != plainCode || strip.ReplaceAllString(colored, "") != plain || strip.ReplaceAllString(errOut, "") != plainErr || !strings.Contains(colored, "\x1b[") {
			t.Fatalf("help changed for %v: %d %q %q", args, code, colored, errOut)
		}
		for _, child := range command.Commands() {
			visit(child, append(append([]string{}, args...), child.Name()))
		}
	}
	visit(root, nil)
}

func TestJSONAndRedirectedOutputRemainPlain(t *testing.T) {
	saved := terminalColors
	t.Cleanup(func() { terminalColors = saved })
	terminalColors = func(io.Writer) bool { return true }
	code, text, errOut := run(t, "version", "--json")
	if code != 0 || !json.Valid([]byte(text)) || strings.Contains(text+errOut, "\x1b[") {
		t.Fatal(code, text, errOut)
	}
	code, _, errOut = run(t, "--unknown-color-test")
	if code != ExitUsage || !strings.Contains(errOut, "\x1b[91m") {
		t.Fatal(code, errOut)
	}
	terminalColors = output.ColorEnabled
	code, text, errOut = run(t, "version")
	if code != 0 || strings.Contains(text+errOut, "\x1b[") {
		t.Fatal(code, text, errOut)
	}
}

func TestNimbusColorsStayOutOfInstallLog(t *testing.T) {
	log := testInstallLog(t)
	var terminal bytes.Buffer
	w := installWriter{output.ColorWriter(&terminal, func() bool { return true }), log}
	if _, err := fmt.Fprintln(w, "succeeded sample operation"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(log.dir, "engine.log"))
	if err != nil || strings.Contains(string(data), "\x1b[") || !strings.Contains(string(data), "succeeded sample operation") || !strings.Contains(terminal.String(), "\x1b[92m") {
		t.Fatal("console/log separation failed", err, terminal.String(), string(data))
	}
}

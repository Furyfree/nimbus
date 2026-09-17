package agentproxy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestInspectNeverInvokesNativeCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	src := &nativetest.FakeSource{Files: map[string][]byte{}, Paths: map[string]string{"mise": "mise", "herdr": "herdr"}}
	p, err := StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if got := Inspect(src, "desktop"); got.Status != "not-configured" {
		t.Fatal(got)
	}
	src.Files[p] = []byte(`{"version":1,"machine":"desktop","providerId":"owned","configured":true}`)
	if got := Inspect(src, "laptop"); got.Status != "pending" {
		t.Fatal(got)
	}
	if got := Inspect(src, "desktop"); got.Status != "pending" {
		t.Fatal(got)
	}
	src.Files[filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agent-proxy/config.yaml")] = []byte("fixture")
	version := filepath.Join(os.Getenv("XDG_DATA_HOME"), "agent-proxy/current/VERSION")
	src.Files[version] = []byte("1.3.0-nimbus.2-source\n")
	if got := Inspect(src, "desktop"); got.Status != "configured" {
		t.Fatal(got)
	}
	delete(src.Files, version)
	if got := Inspect(src, "desktop"); got.Status != "pending" {
		t.Fatalf("receipt hid missing installation: %+v", got)
	}
	src.Files[p] = []byte(`not json`)
	if got := Inspect(src, "desktop"); got.Status != "blocked" {
		t.Fatal(got)
	}
}

func TestClosedCopilotDefersWithoutDiscoveringOrModifyingModels(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("adapter refuses root")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	bin := t.TempDir()
	node, _ := exec.LookPath("node")
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nprintf '%s\\n' '"+node+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	p, _ := StatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"version":1,"machine":"test","providerId":"owned","configured":true,"entries":[]}`)
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	result, err := Run(t.Context(), "refresh", "test", &out)
	if err != nil || result.Status != "deferred" {
		t.Fatalf("%+v %v %s", result, err, out.String())
	}
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("deferred refresh changed inventory")
	}
	if strings.Contains(out.String(), "catalog") {
		t.Fatal("discovery started without Copilot")
	}
}

func TestCatalogAndRecoveryAdapter(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node not installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	command := exec.CommandContext(t.Context(), node, "--test", "resources/catalog.test.mjs", "resources/storage.test.mjs")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
}

func TestStatePathRejectsRelativeXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative")
	if _, err := StatePath(); err == nil {
		t.Fatal("relative state accepted")
	}
}

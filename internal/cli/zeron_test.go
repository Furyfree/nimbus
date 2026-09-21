package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zeronShow = "systemctl --user show zeron.service --property=LoadState,ActiveState,UnitFileState,FragmentPath,DropInPaths"

func zeronFixture(t *testing.T) (string, *postinstallSource) {
	t.Helper()
	root, src := postinstallFixture(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "components/zeron.toml"), []byte("schema=1\nid='zeron'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "machines/vm.toml")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "components = [", "components = [\"zeron\",", 1))
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
	src.Paths["zeron"] = "/fixture/zeron"
	setZeronFixture(t, src, true)
	src.onStream = func(key string) {
		if key == "zeron daemon uninstall" {
			setZeronFixture(t, src, false)
		}
		if key == "zeron daemon install" {
			setZeronFixture(t, src, true)
		}
	}
	return root, src
}

func setZeronFixture(t *testing.T, src *postinstallSource, running bool) {
	t.Helper()
	file, err := zeronUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if running {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		data := []byte("ExecStart=%h/.zeron/app/current/zeron headless\n")
		if err := os.WriteFile(file, data, 0644); err != nil {
			t.Fatal(err)
		}
		src.Files[file] = data
		src.Commands[zeronShow] = fmt.Appendf(nil, "LoadState=loaded\nActiveState=active\nUnitFileState=enabled\nFragmentPath=%s\nDropInPaths=\n", file)
	} else {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		delete(src.Files, file)
		src.Commands[zeronShow] = []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\nFragmentPath=\nDropInPaths=\n")
	}
}

func TestZeronOptInAndDisableSurviveSync(t *testing.T) {
	root, src := zeronFixture(t)
	cmd, out := postinstallCommand(root, false, "zeron", "--yes")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if opted, err := zeronOptedIn("vm"); err != nil || !opted {
		t.Fatal(opted, err)
	}
	s, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	src.streams = nil
	if err := syncZeron(cmd, src, s, io.Discard, true, &syncResult{}); err != nil {
		t.Fatal(err)
	}
	if len(src.streams) != 0 {
		t.Fatal("sync removed opted-in daemon", src.streams)
	}
	cmd, out = postinstallCommand(root, false, "zeron", "--disable", "--yes")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if opted, err := zeronOptedIn("vm"); err != nil || opted {
		t.Fatal(opted, err)
	}
	setZeronFixture(t, src, true) // An upstream update re-enabled it.
	src.streams = nil
	var result syncResult
	if err := syncZeron(cmd, src, s, io.Discard, true, &result); err != nil {
		t.Fatal(err)
	}
	if len(src.streams) != 1 || src.streams[0] != "zeron daemon uninstall" || len(result.Steps) != 1 {
		t.Fatal(src.streams, result)
	}
	src.streams = nil
	if err := syncZeron(cmd, src, s, io.Discard, true, &result); err != nil {
		t.Fatal(err)
	}
	if len(src.streams) != 0 {
		t.Fatal("unchanged daemon mutated")
	}
}

func TestZeronPreviewFailureAndForeignOwnership(t *testing.T) {
	for _, mode := range []string{"preview", "declined", "failed stop", "foreign unit", "corrupt evidence"} {
		t.Run(mode, func(t *testing.T) {
			root, src := zeronFixture(t)
			args := []string{"zeron", "--disable", "--yes"}
			switch mode {
			case "preview":
				args = []string{"zeron", "--disable", "--plan"}
			case "declined":
				args = []string{"zeron", "--disable"}
			case "failed stop":
				src.onStream = nil
			case "foreign unit":
				file, _ := zeronUnitPath()
				src.Files[file] = []byte("ExecStart=/other/daemon\n")
			case "corrupt evidence":
				if err := recordTask("vm", "zeron.daemon", "verified"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus/postinstall.json"), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd, out := postinstallCommand(root, false, args...)
			err := cmd.Execute()
			if mode == "preview" {
				if err != nil {
					t.Fatal(err, out.String())
				}
			} else if err == nil {
				t.Fatal("unsafe operation succeeded", out.String())
			}
			if mode != "failed stop" && len(src.streams) != 0 {
				t.Fatal(src.streams)
			}
		})
	}
}

func TestAgentProxyUninstallPreviewAndMode(t *testing.T) {
	root, _ := postinstallFixture(t)
	cmd, out := postinstallCommand(root, false, "agent-proxy", "--uninstall", "--plan")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Mise Herdr") {
		t.Fatal(out.String())
	}
	cmd, _ = postinstallCommand(root, false, "agent-proxy", "--uninstall", "--reset")
	if err := cmd.Execute(); err == nil {
		t.Fatal("conflicting modes accepted")
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func hyprlandPostinstallFixture(t *testing.T) (string, *postinstallSource) {
	t.Helper()
	root, src := postinstallFixture(t)
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "fixture")
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['hyprland-copr:hyprland-devel']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	src.Commands[key] = append(src.Commands[key], []byte("hyprland-devel|0|0.56.2|3|x86_64|fedora|User\n")...)
	for _, tool := range []string{"Hyprland", "hyprpm", "hyprctl"} {
		src.Paths[tool] = "/usr/bin/" + tool
	}
	src.Commands["Hyprland --version-json"] = []byte(`{"version":"0.56.2","abiHash":"test"}`)
	src.Commands["hyprctl -j version"] = src.Commands["Hyprland --version-json"]
	src.Commands["hyprctl -j plugin list"] = []byte("[]")
	src.Commands["hyprctl -j getoption plugin.scrolloverview.scale"] = []byte(`{"set":false}`)
	src.Files[filepath.Join(os.Getenv("HOME"), ".config/hypr/plugins.toml")] = []byte("schema=1\nenabled=['scrolloverview']")
	cache := "/var/cache/hyprpm/test"
	src.Dirs[cache] = []string{"state.toml", "headersRoot", "hyprland-scroll-overview"}
	src.Files[cache+"/state.toml"] = []byte("[state]\nhash='test'")
	src.Files[cache+"/headersRoot/share/pkgconfig/hyprland.pc"] = []byte("Name: Hyprland")
	src.Files[cache+"/hyprland-scroll-overview/state.toml"] = []byte(`[repository]
name='hyprland-scroll-overview'
author='yayuuu'
url='https://github.com/yayuuu/hyprland-scroll-overview.git'
[scrolloverview]
enabled=true
failed=false
filename='scrolloverview.so'
`)
	src.Files[cache+"/hyprland-scroll-overview/scrolloverview.so"] = []byte("\x7fELFbinary")
	src.onStream = func(command string) {
		if command == "hyprpm reload" {
			src.Commands["hyprctl -j plugin list"] = []byte(`[{"name":"scrolloverview","author":"Vaxry, yayuuu","version":"test"}]`)
		}
		if command == "hyprctl reload config-only" {
			src.Commands["hyprctl -j getoption plugin.scrolloverview.scale"] = []byte(`{"set":true}`)
		}
	}
	r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:hyprland-copr:hyprland-devel", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
	if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
		t.Fatal(err)
	}
	return root, src
}

func TestHyprlandPostinstallApprovalAndCompletion(t *testing.T) {
	for _, mode := range []string{"list", "json", "preview", "cancel", "nonterminal", "load", "no effect", "failure", "changed selection", "changed binary", "changed session"} {
		t.Run(mode, func(t *testing.T) {
			root, src := hyprlandPostinstallFixture(t)
			before, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"hyprland-plugins", "--yes"}
			switch mode {
			case "list", "json":
				args = nil
			case "preview":
				args = append(args, "--plan")
			case "no effect":
				src.onStream = nil
			case "failure":
				src.onStream = nil
				src.streamErr = io.ErrUnexpectedEOF
			case "nonterminal":
				args = []string{"hyprland-plugins"}
			case "cancel", "changed selection", "changed binary", "changed session":
				args = []string{"hyprland-plugins"}
				savedTerminal, savedApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					switch mode {
					case "changed selection":
						src.Files[filepath.Join(os.Getenv("HOME"), ".config/hypr/plugins.toml")] = []byte("schema=1\nenabled=[]")
					case "changed binary":
						src.Files["/var/cache/hyprpm/test/hyprland-scroll-overview/scrolloverview.so"] = []byte("\x7fELFchanged")
					case "changed session":
						t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "changed")
					}
					return mode != "cancel"
				}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err = cmd.Execute()
			success := mode == "list" || mode == "json" || mode == "preview" || mode == "load"
			if (err == nil) != success {
				t.Fatalf("%v: %s", err, out)
			}
			if strings.HasPrefix(mode, "changed") && (err == nil || !strings.Contains(err.Error(), "changed after approval")) {
				t.Fatalf("stale approval: %v", err)
			}
			if mode == "load" {
				if len(src.streams) != 2 || !strings.Contains(out.String(), "Selected Hyprland plugin ScrollOverview is built, enabled and loaded.") {
					t.Fatalf("%v: %s", src.streams, out)
				}
				cmd, out = postinstallCommand(root, false, "hyprland-plugins", "--yes")
				if err := cmd.Execute(); err != nil || len(src.streams) != 2 || strings.Contains(out.String(), "Native action:") {
					t.Fatalf("completed setup mutated: %v %s", err, out)
				}
			} else if mode == "no effect" || mode == "failure" {
				if len(src.streams) != 1 || !strings.Contains(out.String(), "After action: hyprland-plugins: pending") {
					t.Fatalf("unverified success: %v %s", src.streams, out)
				}
			} else if len(src.streams) != 0 {
				t.Fatalf("read-only path mutated: %v", src.streams)
			}
			after, err := state.Read(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("postinstall wrote a completion receipt")
			}
		})
	}
}

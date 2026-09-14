package cli

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestLockscreenCommandReadOnlyAndApproval(t *testing.T) {
	for _, mode := range []string{"help", "plan", "json", "status", "decline", "nonterminal", "changed after approval"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['noctalia']\n"), 0644); err != nil {
				t.Fatal(err)
			}
			key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
			src.Commands[key] = append(src.Commands[key], []byte("noctalia|0|5.0.1|1|x86_64|fedora|User\n")...)
			r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:noctalia", Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
			if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "noctalia/config.toml")
			settings := filepath.Join(os.Getenv("XDG_STATE_HOME"), "noctalia/settings.toml")
			src.Files[config] = []byte(`[lockscreen_widgets]
enabled=true
schema_version=2
widget_order=['login','clock','avatar']
[lockscreen_widgets.widget.login]
type='login_box'
[lockscreen_widgets.widget.clock]
type='clock'
[lockscreen_widgets.widget.avatar]
type='sticker'
`)
			src.Files[settings] = []byte("[lockscreen_widgets]\nenabled=false\n[private]\nvalue='do-not-render'\n")
			src.Commands["chezmoi --skip-secrets verify "+config] = nil
			src.Commands["noctalia config validate "+config] = nil
			src.Commands["noctalia msg status"] = []byte(`{"locked":false,"panelOpen":false}`)
			src.Commands["pgrep -u "+strconv.Itoa(os.Getuid())+" -x noctalia"] = []byte("1234\n")
			src.Paths["noctalia"] = "/usr/bin/noctalia"
			src.Commands["readlink /proc/1234/exe"] = []byte("/usr/bin/noctalia\n")
			src.Files["/proc/1234/cmdline"] = []byte("noctalia\x00")
			src.Files["/proc/1234/cgroup"] = []byte("0::/user.slice/wayland-wm@hyprland.desktop.service\n")
			src.Files["/proc/1234/stat"] = []byte("1234 (noctalia) S " + strings.Repeat("0 ", 18) + "12345\n")
			args := []string{"noctalia-lockscreen", "--plan"}
			switch mode {
			case "help":
				args = []string{"noctalia-lockscreen", "--help"}
			case "status":
				args = []string{"status"}
			case "decline", "nonterminal", "changed after approval":
				args = []string{"noctalia-lockscreen"}
			}
			if mode == "decline" {
				old := postinstallTerminal
				postinstallTerminal = func(_ io.Reader) bool { return true }
				t.Cleanup(func() { postinstallTerminal = old })
				oldApprove := approver
				approver = func(io.Reader, io.Writer, string) bool { return false }
				t.Cleanup(func() { approver = oldApprove })
			}
			if mode == "changed after approval" {
				old := postinstallTerminal
				postinstallTerminal = func(io.Reader) bool { return true }
				t.Cleanup(func() { postinstallTerminal = old })
				oldApprove := approver
				approver = func(io.Reader, io.Writer, string) bool {
					src.Files[settings] = append(src.Files[settings], []byte("# new edit\n")...)
					return true
				}
				t.Cleanup(func() { approver = oldApprove })
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err := cmd.Execute()
			if (err != nil) != (mode == "decline" || mode == "nonterminal" || mode == "changed after approval") {
				t.Fatalf("%v %s", err, out)
			}
			if len(src.streams) != 0 {
				t.Fatal("ran a mutation")
			}
			if mode == "help" && len(src.reads) != 0 {
				t.Fatal("help inspected host")
			}
			if strings.Contains(out.String(), "do-not-render") {
				t.Fatal("private config displayed")
			}
			for _, path := range []string{settings, filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus/postinstall.json")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("created local state %s", path)
				}
			}
		})
	}
}

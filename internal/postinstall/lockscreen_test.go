package postinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/pelletier/go-toml/v2"
)

const lockscreenConfig = `[lockscreen_widgets]
enabled = true
schema_version = 2
widget_order = ['login','clock','avatar']
[lockscreen_widgets.widget.login]
type = 'login_box'
[lockscreen_widgets.widget.clock]
type = 'clock'
[lockscreen_widgets.widget.avatar]
type = 'sticker'
`

// Synthetic layout with the laptop's observed reference coordinates.
const placedLockscreenConfig = `[lockscreen_widgets]
enabled = true
schema_version = 2
widget_order = ['login','date','clock','avatar']
[lockscreen_widgets.widget.login]
type = 'login_box'
cx = 960.0
cy = 745.0
placement_width = 1920.0
placement_height = 1080.0
box_width = 320.0
box_height = 72.0
[lockscreen_widgets.widget.login.settings]
show_login_button = false
[lockscreen_widgets.widget.date]
type = 'clock'
cx = 960.0
cy = 240.0
placement_width = 1920.0
placement_height = 1080.0
[lockscreen_widgets.widget.clock]
type = 'clock'
cx = 960.0
cy = 330.0
placement_width = 1920.0
placement_height = 1080.0
[lockscreen_widgets.widget.avatar]
type = 'sticker'
cx = 960.0
cy = 654.0
placement_width = 1920.0
placement_height = 1080.0
`

func laptopLockscreen(t *testing.T) []byte {
	t.Helper()
	doc, err := lockscreenDocument([]byte(placedLockscreenConfig))
	if err != nil {
		t.Fatal(err)
	}
	widgets := doc["lockscreen_widgets"].(map[string]any)["widget"].(map[string]any)
	for id, cy := range map[string]float64{"login": 827.77783203125, "date": 266.66668701171875, "clock": 366.66668701171875, "avatar": 726.6666870117188} {
		widget := widgets[id].(map[string]any)
		widget["cy"], widget["placement_height"] = cy, float64(1200)
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const lockscreenSettings = `# Keep my wallpaper
[wallpaper]
path = '/pictures/example.jpg'
# Keep the unrelated secret private
[calendar.account.work]
password = 'do-not-render'
[lockscreen_widgets]
enabled = false
[lockscreen_widgets.widget.old.settings]
layout = 'regular'
[plugins]
enabled = ['local/example']
`

func TestRemoveLockscreenOverrides(t *testing.T) {
	for _, data := range []string{lockscreenSettings,
		`lockscreen_widgets = {enabled = false}
[other]
text = '''
[lockscreen_widgets]
not a real table
'''
`,
		`lockscreen_widgets.enabled = false
["lockscreen_widgets"."widget"."odd]key"]
type = 'clock'
[other]
x = 5
`,
		`[[lockscreen_widgets.widget]]
type = 'old'
[[other]]
x = 1
`,
	} {
		got, changed, err := removeLockscreenOverrides([]byte(data))
		if err != nil || !changed {
			t.Fatalf("remove: %v %v", changed, err)
		}
		before, _ := lockscreenDocument([]byte(data))
		after, _ := lockscreenDocument(got)
		delete(before, "lockscreen_widgets")
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		if !bytes.Equal(a, b) {
			t.Fatal("unrelated values changed")
		}
		again, changed, err := removeLockscreenOverrides(got)
		if err != nil || changed || !bytes.Equal(got, again) {
			t.Fatal("not idempotent")
		}
	}
	result, _, _ := removeLockscreenOverrides([]byte(lockscreenSettings))
	for _, fragment := range []string{"# Keep my wallpaper", "password = 'do-not-render'", "[plugins]\nenabled = ['local/example']"} {
		if !bytes.Contains(result, []byte(fragment)) {
			t.Fatal("unrelated formatting lost")
		}
	}
	if _, _, err := removeLockscreenOverrides([]byte("secret = 'do-not-render")); err == nil || strings.Contains(err.Error(), "do-not-render") {
		t.Fatal("invalid TOML or privacy failure")
	}
}

func lockscreenFixture(t *testing.T) (Inputs, *nativetest.FakeSource, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	config, settings, err := lockscreenPaths()
	if err != nil {
		t.Fatal(err)
	}
	in, src := fixture("noctalia")
	src.Files[config] = []byte(lockscreenConfig)
	src.Files[settings] = []byte(lockscreenSettings)
	src.Commands["chezmoi --skip-secrets verify "+config] = nil
	src.Commands["noctalia config validate "+config] = nil
	src.Commands["noctalia config export merged"] = []byte(lockscreenConfig)
	src.Commands["noctalia msg status"] = []byte(`{"locked":false,"panelOpen":false}`)
	src.Commands["pgrep -u "+strconv.Itoa(os.Getuid())+" -x noctalia"] = []byte("1234\n")
	src.Paths["noctalia"] = "/usr/bin/noctalia"
	src.Commands["readlink /proc/1234/exe"] = []byte("/usr/bin/noctalia\n")
	src.Files["/proc/1234/cmdline"] = []byte("noctalia\x00")
	src.Files["/proc/1234/cgroup"] = []byte("0::/user.slice/wayland-wm@hyprland.desktop.service\n")
	src.Files["/proc/1234/stat"] = []byte("1234 (noctalia) S " + strings.Repeat("0 ", 18) + "12345\n")
	return in, src, config, settings
}

func TestLockscreenInspection(t *testing.T) {
	for _, mode := range []string{"pending", "complete", "equivalent saved", "scaled effective", "different saved", "different effective", "missing settings", "missing config", "malformed", "locked", "panel", "offline", "unmanaged", "invalid native", "other config", "multiple processes", "missing receipt"} {
		t.Run(mode, func(t *testing.T) {
			in, src, config, settings := lockscreenFixture(t)
			want := Blocked
			switch mode {
			case "pending":
				want = Pending
			case "complete":
				src.Files[settings] = []byte("[wallpaper]\npath='foo'\n")
				want = Complete
			case "equivalent saved", "scaled effective", "different saved", "different effective":
				src.Files[config] = []byte(placedLockscreenConfig)
				src.Files[settings] = laptopLockscreen(t)
				src.Commands["noctalia config export merged"] = laptopLockscreen(t)
				want = Complete
				if mode == "scaled effective" {
					src.Files[settings] = []byte("[wallpaper]\npath='foo'\n")
				} else if mode == "different saved" {
					src.Files[settings] = bytes.ReplaceAll(src.Files[settings], []byte("show_login_button = false"), []byte("show_login_button = true"))
					want = Pending
				} else if mode == "different effective" {
					src.Commands["noctalia config export merged"] = bytes.ReplaceAll(laptopLockscreen(t), []byte("show_login_button = false"), []byte("show_login_button = true"))
					want = Unknown
				}
			case "missing settings":
				delete(src.Files, settings)
				want = Complete
			case "missing config":
				delete(src.Files, config)
			case "malformed":
				src.Files[settings] = []byte("secret = 'do-not-render")
				want = Unknown
			case "locked":
				src.Commands["noctalia msg status"] = []byte(`{"locked":true,"panelOpen":false}`)
			case "panel":
				src.Commands["noctalia msg status"] = []byte(`{"locked":false,"panelOpen":true}`)
			case "offline":
				src.Failures["noctalia msg status"] = "secret"
			case "unmanaged":
				src.Failures["chezmoi --skip-secrets verify "+config] = "secret"
			case "invalid native":
				src.Failures["noctalia config validate "+config] = "secret"
			case "other config":
				src.Files[settings] = nil
				src.Commands["noctalia config export merged"] = []byte("[lockscreen_widgets]\nenabled=false\n")
				want = Unknown
			case "multiple processes":
				src.Commands["pgrep -u "+strconv.Itoa(os.Getuid())+" -x noctalia"] = []byte("1234\n5678\n")
			case "missing receipt":
				clear(in.Applied.Receipts)
			}
			task := findTask(t, Inspect(src, in), "noctalia-lockscreen")
			if task.Status != want || (task.Action != nil) != (want == Pending) {
				t.Fatalf("%+v", task)
			}
			data, _ := json.Marshal(task)
			if bytes.Contains(data, []byte("do-not-render")) {
				t.Fatal("secret retained")
			}
			if _, err := os.Stat(filepath.Dir(settings)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("inspection created state")
			}
		})
	}
}

type lockscreenSource struct {
	*nativetest.FakeSource
	onStart   func()
	failStart bool
	starts    int
	stops     int
	service   bool
	failStop  bool
}

func (s *lockscreenSource) Stream(_, _ io.Writer, name string, args ...string) error {
	if s.service && name == "systemctl" && strings.Join(args, " ") == "--user stop app-noctalia.service" {
		s.stops++
		if s.failStop {
			return errors.New("failed stop")
		}
		return nil
	}
	if !(s.service && name == "uwsm" && strings.Join(args, " ") == "app -s s -t service -u app-noctalia.service -p TimeoutStopSec=10s -p SendSIGKILL=no -- /usr/bin/noctalia --daemon") && (name != "/usr/bin/noctalia" || strings.Join(args, " ") != "--daemon") {
		return fmt.Errorf("unexpected mutation %s", name)
	}
	s.starts++
	if s.failStart {
		return errors.New("do-not-render")
	}
	if s.onStart != nil {
		s.onStart()
	}
	return nil
}

func TestLockscreenRepair(t *testing.T) {
	for _, mode := range []string{"success", "scaled restart", "reserialized restart", "unrelated restart write", "wrong saved", "scaled wrong effective", "cancelled", "changed before", "changed during shutdown", "symlink", "hardlink", "symlink ancestor", "restart failure", "concurrent restart write", "wrong effective"} {
		t.Run(mode, func(t *testing.T) {
			in, fake, config, settings := lockscreenFixture(t)
			if slices.Contains([]string{"scaled restart", "unrelated restart write", "wrong saved", "scaled wrong effective"}, mode) {
				fake.Files[config] = []byte(placedLockscreenConfig)
				fake.Commands["noctalia config export merged"] = laptopLockscreen(t)
			}
			for path, data := range map[string][]byte{config: fake.Files[config], settings: fake.Files[settings]} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			task := noctaliaLockscreen(fake, in, in.Resolved.Packages[0])
			if task.Status != Pending {
				t.Fatalf("fixture: %+v", task)
			}
			src := &lockscreenSource{FakeSource: fake}
			stopCount := 0
			old := stopLockscreenShell
			t.Cleanup(func() { stopLockscreenShell = old })
			stopLockscreenShell = func(context.Context, int, string, string) error {
				stopCount++
				if mode == "changed during shutdown" {
					return os.WriteFile(settings, []byte(lockscreenSettings+"# saved on exit\n"), 0600)
				}
				return nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "scaled restart", "unrelated restart write", "wrong saved", "scaled wrong effective", "reserialized restart":
				src.onStart = func() {
					data, err := os.ReadFile(settings)
					if err != nil {
						t.Fatal(err)
					}
					doc, err := lockscreenDocument(data)
					if err != nil {
						t.Fatal(err)
					}
					if mode != "reserialized restart" {
						layout, err := lockscreenDocument(laptopLockscreen(t))
						if err != nil {
							t.Fatal(err)
						}
						doc["lockscreen_widgets"] = layout["lockscreen_widgets"]
					}
					if mode == "unrelated restart write" {
						doc["wallpaper"] = map[string]any{"path": "new preference"}
					}
					data, err = toml.Marshal(doc)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "wrong saved" {
						data = bytes.ReplaceAll(data, []byte("show_login_button = false"), []byte("show_login_button = true"))
					}
					if err := os.WriteFile(settings, data, 0600); err != nil {
						t.Fatal(err)
					}
					if mode == "scaled wrong effective" {
						src.Commands["noctalia config export merged"] = []byte(lockscreenConfig)
					}
				}
			case "cancelled":
				cancel()
			case "changed before":
				if err := os.WriteFile(settings, []byte(lockscreenSettings+"# edited\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := settings + ".original"
				_ = os.Rename(settings, target)
				_ = os.Symlink(target, settings)
			case "hardlink":
				_ = os.Link(settings, settings+".link")
			case "symlink ancestor":
				parent := filepath.Dir(settings)
				_ = os.Rename(parent, parent+".original")
				_ = os.Symlink(parent+".original", parent)
			case "restart failure":
				src.failStart = true
			case "concurrent restart write":
				src.onStart = func() { _ = os.WriteFile(settings, []byte(lockscreenSettings), 0600) }
			case "wrong effective":
				src.Commands["noctalia config export merged"] = []byte("[lockscreen_widgets]\nenabled=false\n")
			}
			var out bytes.Buffer
			err := RunNoctaliaLockscreen(ctx, src, &out, task)
			if (err == nil) != (slices.Contains([]string{"success", "scaled restart", "reserialized restart"}, mode)) {
				t.Fatalf("%s: %v %s", mode, err, &out)
			}
			if strings.Contains(out.String(), "do-not-render") || (err != nil && strings.Contains(err.Error(), "do-not-render")) {
				t.Fatal("secret leaked")
			}
			backups, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "*.backup"))
			if slices.Contains([]string{"success", "scaled restart", "reserialized restart", "unrelated restart write", "wrong saved", "scaled wrong effective", "restart failure", "concurrent restart write", "wrong effective"}, mode) {
				if stopCount != 1 || src.starts != 1 || len(backups) != 1 {
					t.Fatalf("lifecycle: %d %d %v", stopCount, src.starts, backups)
				}
				data, _ := os.ReadFile(backups[0])
				info, _ := os.Stat(backups[0])
				if string(data) != lockscreenSettings || info.Mode().Perm() != 0600 {
					t.Fatal("backup not exact/private")
				}
			} else if mode == "changed during shutdown" {
				if src.starts != 1 || len(backups) != 0 {
					t.Fatal("shutdown change not preserved/restarted")
				}
			} else if stopCount != 0 || src.starts != 0 || len(backups) != 0 {
				t.Fatal("unsafe work before prerequisite validation")
			}
			if mode == "success" {
				data, _ := os.ReadFile(settings)
				want, _, _ := removeLockscreenOverrides([]byte(lockscreenSettings))
				if !bytes.Equal(data, want) {
					t.Fatal("incorrect replacement")
				}
			}
		})
	}
}

func managedNoctaliaService(src *nativetest.FakeSource) {
	group := "/user.slice/user-1000.slice/user@1000.service/session.slice/session-graphical.slice/app-noctalia.service"
	src.Files["/proc/1234/cgroup"] = []byte("0::" + group + "\n")
	src.Commands["systemctl --user show app-noctalia.service --property=ControlGroup,ActiveState,Restart,SendSIGKILL,TimeoutStopUSec,KillSignal,KillMode,ExecStop,Transient,TimeoutStopFailureMode"] = []byte(
		"ControlGroup=" + group + "\nActiveState=active\nRestart=no\nSendSIGKILL=no\nTimeoutStopUSec=10s\nKillSignal=15\nKillMode=control-group\nTransient=yes\nTimeoutStopFailureMode=terminate\n")
	src.Paths["uwsm"] = "/usr/bin/uwsm"
	src.Commands["uwsm check is-active compositor-only"] = nil
	src.Commands["systemctl --user show app-noctalia.service --property=ActiveState --value"] = []byte("inactive\n")
}

func TestLockscreenManagedService(t *testing.T) {
	for _, mode := range []string{"success", "supervisor changed", "stop failed", "child remains", "restart failed"} {
		t.Run(mode, func(t *testing.T) {
			in, fake, config, settings := lockscreenFixture(t)
			managedNoctaliaService(fake)
			for path, data := range map[string][]byte{config: fake.Files[config], settings: fake.Files[settings]} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			task := noctaliaLockscreen(fake, in, in.Resolved.Packages[0])
			if task.Status != Pending || task.Action.Lockscreen.Service != noctaliaService {
				t.Fatalf("%+v", task)
			}
			if commands := LockscreenCommands(task.Action.Lockscreen); len(commands) != 2 || commands[0][0] != "systemctl" || commands[1][0] != "uwsm" {
				t.Fatalf("%v", commands)
			}
			src := &lockscreenSource{FakeSource: fake, service: true}
			switch mode {
			case "supervisor changed":
				fake.Files["/proc/1234/cgroup"] = []byte("0::/other.service\n")
			case "stop failed":
				src.failStop = true
			case "child remains":
				fake.Commands["systemctl --user show app-noctalia.service --property=ActiveState --value"] = []byte("deactivating\n")
			case "restart failed":
				src.failStart = true
			}
			err := RunNoctaliaLockscreen(t.Context(), src, io.Discard, task)
			if (err == nil) != (mode == "success") {
				t.Fatalf("%v", err)
			}
			if mode == "supervisor changed" && (src.starts != 0 || src.stops != 0) {
				t.Fatal("mutated changed supervisor")
			}
			if mode == "success" || mode == "restart failed" {
				if src.stops != 1 || src.starts != 1 {
					t.Fatalf("stop/start %d/%d", src.stops, src.starts)
				}
			} else {
				got, _ := os.ReadFile(settings)
				if string(got) != lockscreenSettings {
					t.Fatal("settings changed before successful stop")
				}
			}
		})
	}
}

func TestLockscreenRejectsUncontrolledService(t *testing.T) {
	for _, mode := range []string{"foreign", "unreadable", "restart policy", "kill policy", "abort policy", "timeout", "group mismatch", "inactive", "uwsm missing", "kill signal", "stop hook", "nested supervisor"} {
		t.Run(mode, func(t *testing.T) {
			in, src, _, _ := lockscreenFixture(t)
			managedNoctaliaService(src)
			key := "systemctl --user show app-noctalia.service --property=ControlGroup,ActiveState,Restart,SendSIGKILL,TimeoutStopUSec,KillSignal,KillMode,ExecStop,Transient,TimeoutStopFailureMode"
			switch mode {
			case "kill signal":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("KillSignal=15"), []byte("KillSignal=9"))
			case "stop hook":
				src.Commands[key] = append(src.Commands[key], []byte("ExecStop=/custom/stop\n")...)
			case "nested supervisor":
				src.Files["/proc/1234/cgroup"] = []byte("0::/custom-noctalia.service/subgroup\n")
			case "foreign":
				src.Files["/proc/1234/cgroup"] = []byte("0::/custom-noctalia.service\n")
			case "unreadable":
				delete(src.Files, "/proc/1234/cgroup")
			case "restart policy":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("Restart=no"), []byte("Restart=always"))
			case "kill policy":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("SendSIGKILL=no"), []byte("SendSIGKILL=yes"))
			case "abort policy":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("TimeoutStopFailureMode=terminate"), []byte("TimeoutStopFailureMode=abort"))
			case "timeout":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("10s"), []byte("infinity"))
			case "group mismatch":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("ControlGroup=/user.slice"), []byte("ControlGroup=/other.slice"))
			case "inactive":
				src.Commands[key] = bytes.ReplaceAll(src.Commands[key], []byte("ActiveState=active"), []byte("ActiveState=inactive"))
			case "uwsm missing":
				delete(src.Paths, "uwsm")
			}
			task := noctaliaLockscreen(src, in, in.Resolved.Packages[0])
			if task.Status != Blocked || task.Action != nil {
				t.Fatalf("%+v", task)
			}
		})
	}
}

func TestEquivalentLockscreen(t *testing.T) {
	for _, mode := range []string{"laptop", "width", "integers", "moved", "resized box", "appearance", "order", "missing widget", "extra widget", "extra field", "missing coordinate", "zero extent", "negative extent", "nan", "infinity", "string coordinate"} {
		t.Run(mode, func(t *testing.T) {
			desired, err := managedLockscreen([]byte(placedLockscreenConfig))
			if err != nil {
				t.Fatal(err)
			}
			actual, err := managedLockscreen(laptopLockscreen(t))
			if err != nil {
				t.Fatal(err)
			}
			widgets := actual["widget"].(map[string]any)
			widget := widgets["login"].(map[string]any)
			want := false
			switch mode {
			case "laptop":
				want = true
			case "width":
				for _, w := range widgets {
					w := w.(map[string]any)
					w["cx"], w["placement_width"] = float64(640), float64(1280)
				}
				want = true
			case "integers":
				widget["cx"], widget["placement_width"] = int64(960), int64(1920)
				want = true
			case "moved":
				widget["cy"] = float64(830)
			case "resized box":
				widget["box_width"] = float64(321)
			case "appearance":
				widget["settings"].(map[string]any)["show_login_button"] = true
			case "order":
				actual["widget_order"] = []any{"clock", "login", "date", "avatar"}
			case "missing widget":
				delete(widgets, "clock")
			case "extra widget":
				widgets["extra"] = map[string]any{"type": "clock"}
			case "extra field":
				widget["extra"] = true
			case "missing coordinate":
				delete(widget, "cy")
			case "zero extent":
				widget["placement_height"] = float64(0)
			case "negative extent":
				widget["placement_height"] = float64(-1200)
			case "nan":
				widget["cy"] = math.NaN()
			case "infinity":
				widget["placement_height"] = math.Inf(1)
			case "string coordinate":
				widget["cx"] = "960"
			}
			if equivalentLockscreen(actual, desired) != want || equivalentLockscreen(desired, actual) != want {
				t.Fatal("incorrect layout equivalence", mode)
			}
			unchanged, _ := managedLockscreen([]byte(placedLockscreenConfig))
			if !reflect.DeepEqual(desired, unchanged) {
				t.Fatal("comparison mutated managed layout")
			}
		})
	}
}

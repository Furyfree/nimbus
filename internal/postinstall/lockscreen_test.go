package postinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
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
	src.Files["/proc/1234/stat"] = []byte("1234 (noctalia) S " + strings.Repeat("0 ", 18) + "12345\n")
	return in, src, config, settings
}

func TestLockscreenInspection(t *testing.T) {
	for _, mode := range []string{"pending", "complete", "missing settings", "missing config", "malformed", "locked", "panel", "offline", "unmanaged", "invalid native", "other config", "multiple processes", "missing receipt"} {
		t.Run(mode, func(t *testing.T) {
			in, src, config, settings := lockscreenFixture(t)
			want := Blocked
			switch mode {
			case "pending":
				want = Pending
			case "complete":
				src.Files[settings] = []byte("[wallpaper]\npath='foo'\n")
				want = Complete
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
}

func (s *lockscreenSource) Stream(_, _ io.Writer, name string, args ...string) error {
	if name != "/usr/bin/noctalia" || strings.Join(args, " ") != "--daemon" {
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
	for _, mode := range []string{"success", "cancelled", "changed before", "changed during shutdown", "symlink", "hardlink", "symlink ancestor", "restart failure", "concurrent restart write", "wrong effective"} {
		t.Run(mode, func(t *testing.T) {
			in, fake, config, settings := lockscreenFixture(t)
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
			if (err == nil) != (mode == "success") {
				t.Fatalf("%s: %v %s", mode, err, &out)
			}
			if strings.Contains(out.String(), "do-not-render") || (err != nil && strings.Contains(err.Error(), "do-not-render")) {
				t.Fatal("secret leaked")
			}
			backups, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "*.backup"))
			if mode == "success" || mode == "restart failure" || mode == "concurrent restart write" || mode == "wrong effective" {
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

package postinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

const pluginConfig = `[plugins]
enabled = ["noctalia/timer"]
[[plugins.source]]
name = "official"
kind = "git"
location = "https://github.com/noctalia-dev/official-plugins"
enabled = true
[calendar.account.private]
password = "do-not-render"
`

func noctaliaFixture(t *testing.T) (Inputs, *nativetest.FakeSource, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	in, src := fixture("noctalia")
	src.Commands["noctalia config export full"] = []byte(pluginConfig)
	src.Commands["noctalia msg plugins list"] = []byte("noctalia/timer [official] 1.2.1 enabled\n")
	return in, src, filepath.Join(state, "noctalia", "plugins", "materialized", "official", "timer")
}

func installTimer(src *nativetest.FakeSource, root string) {
	src.Files[filepath.Join(root, "plugin.toml")] = []byte("id = 'noctalia/timer'\nversion = '1.2.1'\n[[widget]]\nid = 'bar'\nentry = 'bar.luau'\n")
	src.Files[filepath.Join(root, "bar.luau")] = []byte("return {}")
}

func TestNoctaliaReadinessAndPrivacy(t *testing.T) {
	for _, mode := range []string{"missing", "empty catalog", "complete", "missing script", "wrong manifest", "malformed config", "malformed listing", "incompatible", "selection drift", "no session", "no plugins", "no receipt", "invalid id", "unsafe script"} {
		t.Run(mode, func(t *testing.T) {
			in, src, root := noctaliaFixture(t)
			want := Unknown
			switch mode {
			case "missing":
				want = Pending
			case "empty catalog":
				src.Commands["noctalia msg plugins list"] = []byte("(no plugins)\n")
				want = Unknown
			case "complete":
				installTimer(src, root)
				want = Complete
			case "missing script":
				installTimer(src, root)
				delete(src.Files, filepath.Join(root, "bar.luau"))
				want = Pending
			case "wrong manifest":
				installTimer(src, root)
				src.Files[filepath.Join(root, "plugin.toml")] = []byte("id = 'other/timer'")
				want = Pending
			case "malformed config":
				src.Commands["noctalia config export full"] = []byte("password = 'do-not-render")
			case "malformed listing":
				src.Commands["noctalia msg plugins list"] = []byte("unexpected")
			case "incompatible":
				src.Commands["noctalia msg plugins list"] = []byte("noctalia/timer [official] 1.2.1 enabled incompatible")
			case "selection drift":
				src.Commands["noctalia msg plugins list"] = []byte("noctalia/timer [official] 1.2.1 disabled")
			case "no session":
				src.Failures["noctalia msg plugins list"] = "do-not-render"
			case "no plugins":
				src.Commands["noctalia config export full"] = []byte("[plugins]\nenabled = []")
				want = Complete
			case "no receipt":
				clear(in.Applied.Receipts)
				want = Blocked
			case "invalid id":
				src.Commands["noctalia config export full"] = []byte("[plugins]\nenabled = ['../secret']")
			case "unsafe script":
				src.Files[filepath.Join(root, "plugin.toml")] = []byte("id = 'noctalia/timer'\n[[widget]]\nentry = '../private'")
				want = Pending
			}
			guard := &readGuard{FakeSource: src}
			task := findTask(t, Inspect(guard, in), "noctalia-plugins")
			if task.Status != want || (task.Action != nil) != (want == Pending) {
				t.Fatalf("unexpected task: %+v", task)
			}
			encoded, _ := json.Marshal(task)
			if strings.Contains(string(encoded), "do-not-render") {
				t.Fatal("leaked config or native error")
			}
			for _, cmd := range guard.commands {
				if cmd != "noctalia config export full" && cmd != "noctalia msg plugins list" {
					t.Fatalf("inspection ran %s", cmd)
				}
			}
			for _, path := range guard.files {
				if !strings.HasPrefix(path, root+"/") {
					t.Fatalf("unexpected read %s", path)
				}
			}
		})
	}
}

type exportingNoctalia struct {
	*nativetest.FakeSource
	root    string
	streams []string
	export  bool
	fail    string
}

func (s *exportingNoctalia) Stream(_, _ io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	s.streams = append(s.streams, key)
	if key == s.fail {
		return errors.New("native failure")
	}
	if s.export && key == "noctalia msg plugins update official" {
		installTimer(s.FakeSource, s.root)
	}
	return nil
}

func TestNoctaliaExportVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"success", "native failure", "timeout", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			in, src, root := noctaliaFixture(t)
			task := findTask(t, Inspect(src, in), "noctalia-plugins")
			live := &exportingNoctalia{FakeSource: src, root: root, export: mode == "success"}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			switch mode {
			case "native failure":
				live.fail = "noctalia msg plugins update official"
			case "cancelled":
				cancel()
			}
			err := RunNoctaliaPlugins(ctx, live, io.Discard, io.Discard, task)
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected error %v", err)
			}
			if mode == "cancelled" && len(live.streams) != 0 {
				t.Fatal("ran a cancelled action")
			}
			if mode == "success" {
				if !slices.Equal(live.streams, []string{"noctalia msg plugins update official"}) {
					t.Fatal(live.streams)
				}
				if task := findTask(t, Inspect(src, in), "noctalia-plugins"); task.Status != Complete || task.Action != nil {
					t.Fatalf("did not converge: %+v", task)
				}
			}
			if mode == "timeout" || mode == "native failure" {
				live.export, live.fail = true, ""
				if err := RunNoctaliaPlugins(context.Background(), live, io.Discard, io.Discard, task); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNoctaliaCommandsRejectUnexpectedMutation(t *testing.T) {
	in, src, _ := noctaliaFixture(t)
	for _, argv := range [][]string{{"sh", "-c", "unexpected"}, {"noctalia", "msg", "plugins", "disable", "noctalia/timer"}, {"noctalia", "msg", "plugins", "update", "../secret"}} {
		task := findTask(t, Inspect(src, in), "noctalia-plugins")
		task.Action.Commands[0] = argv
		if _, err := NoctaliaCommands(task); err == nil {
			t.Fatalf("accepted %v", argv)
		}
	}
}

func TestNoctaliaUpdatesEachAffectedSourceOnce(t *testing.T) {
	in, src, root := noctaliaFixture(t)
	src.Commands["noctalia config export full"] = []byte(strings.Replace(pluginConfig,
		`enabled = ["noctalia/timer"]`, `enabled = ["noctalia/timer", "noctalia/notes", "aristides/udiskie"]`, 1) +
		"\n[[plugins.source]]\nname='community'\nkind='git'\nenabled=true\n")
	src.Commands["noctalia msg plugins list"] = []byte("noctalia/timer [official] 1.2.1 enabled\nnoctalia/notes [official] 1.0 enabled\naristides/udiskie [community] 0.1 enabled\n")
	task := findTask(t, Inspect(src, in), "noctalia-plugins")
	commands, err := NoctaliaCommands(task)
	if err != nil || len(commands) != 2 || commands[0][4] != "community" || commands[1][4] != "official" {
		t.Fatalf("wrong source plan: %v %v", commands, err)
	}
	// A partial native result must not turn the remaining missing exports into
	// completion or request updates for a source whose exports are now ready.
	installTimer(src, root)
	notes := filepath.Join(filepath.Dir(root), "notes")
	src.Files[filepath.Join(notes, "plugin.toml")] = []byte("id='noctalia/notes'\n[[widget]]\nentry='main.luau'")
	src.Files[filepath.Join(notes, "main.luau")] = []byte("return {}")
	task = findTask(t, Inspect(src, in), "noctalia-plugins")
	commands, err = NoctaliaCommands(task)
	if err != nil || len(commands) != 1 || commands[0][4] != "community" {
		t.Fatalf("partial result lost: %+v %v", task, err)
	}
}

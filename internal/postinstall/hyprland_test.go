package postinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

const hyprCache = "/var/cache/hyprpm/tester"
const hyprRepoState = hyprCache + "/hyprland-scroll-overview/state.toml"
const hyprBinary = hyprCache + "/hyprland-scroll-overview/scrolloverview.so"
const hyprHeaders = hyprCache + "/headersRoot/share/pkgconfig/hyprland.pc"
const hyprVersion = `{"version":"0.56.2","abiHash":"test-abi"}`
const overviewState = `[repository]
name = "hyprland-scroll-overview"
author = "yayuuu"
url = "https://github.com/yayuuu/hyprland-scroll-overview.git"
hash = "abc"
[scrolloverview]
enabled = false
failed = false
filename = "scrolloverview.so"
`

func hyprlandFixture(t *testing.T) (Inputs, *nativetest.FakeSource) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test-session")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	in, src := fixture("hyprland-devel")
	for _, tool := range []string{"Hyprland", "hyprctl", "hyprpm"} {
		src.Paths[tool] = "/usr/bin/" + tool
	}
	src.Files[filepath.Join(home, ".config/hypr/plugins.toml")] = []byte("schema = 1\nenabled = ['scrolloverview']\n")
	src.Commands["Hyprland --version-json"] = []byte(hyprVersion)
	src.Commands["hyprctl -j version"] = []byte(hyprVersion)
	src.Commands["hyprctl -j plugin list"] = []byte("[]")
	src.Commands["hyprctl -j getoption plugin.scrolloverview.scale"] = []byte(`{"set":false}`)
	src.Dirs = map[string][]string{}
	return in, src
}

func hyprlandCache(src *nativetest.FakeSource) {
	src.Dirs[hyprCache] = []string{"headersRoot", "state.toml", overviewRepo}
	src.Files[hyprCache+"/state.toml"] = []byte("[state]\nhash = 'test-abi'")
	src.Files[hyprHeaders] = []byte("Name: Hyprland")
	src.Files[hyprRepoState] = []byte(overviewState)
	src.Files[hyprBinary] = []byte("\x7fELFbinary")
}
func hyprlandEnabled(src *nativetest.FakeSource) {
	src.Files[hyprRepoState] = []byte(strings.ReplaceAll(string(src.Files[hyprRepoState]), "enabled = false", "enabled = true"))
}
func hyprlandLoaded(src *nativetest.FakeSource) {
	src.Commands["hyprctl -j plugin list"] = []byte(`[{"name":"scrolloverview","author":"Vaxry, yayuuu","version":"abc"}]`)
}

func TestHyprlandReadOnlyReadiness(t *testing.T) {
	for _, mode := range []string{"fresh", "complete", "disabled", "unloaded", "failed build", "missing binary", "missing headers", "outdated headers", "upgraded session", "SSH", "missing selection", "empty selection", "bad selection", "unknown plugin", "foreign repo", "bad state", "bad listing", "no receipt", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			in, src := hyprlandFixture(t)
			if mode != "fresh" {
				hyprlandCache(src)
				hyprlandEnabled(src)
				hyprlandLoaded(src)
				src.Commands["hyprctl -j getoption plugin.scrolloverview.scale"] = []byte(`{"set":true}`)
			}
			selection := filepath.Join(os.Getenv("HOME"), ".config/hypr/plugins.toml")
			want := Pending
			switch mode {
			case "complete":
				want = Complete
			case "disabled":
				src.Files[hyprRepoState] = []byte(overviewState)
			case "unloaded":
				src.Commands["hyprctl -j plugin list"] = []byte("[]")
			case "failed build":
				src.Files[hyprRepoState] = []byte(strings.ReplaceAll(string(src.Files[hyprRepoState]), "failed = false", "failed = true"))
			case "missing binary":
				delete(src.Files, hyprBinary)
			case "missing headers":
				delete(src.Files, hyprHeaders)
			case "outdated headers":
				src.Files[hyprCache+"/state.toml"] = []byte("[state]\nhash='old'")
			case "upgraded session":
				src.Commands["hyprctl -j version"] = []byte(`{"version":"0.56.1","abiHash":"old"}`)
				want = Blocked
			case "SSH":
				t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
				want = Unknown
			case "missing selection":
				delete(src.Files, selection)
				want = Unknown
			case "empty selection":
				src.Files[selection] = []byte("schema=1\nenabled=[]")
				want = NotApplicable
			case "bad selection":
				src.Files[selection] = []byte("schema=1\nenabled=['scrolloverview']\ncommand='do-not-render'")
				want = Unknown
			case "unknown plugin":
				src.Files[selection] = []byte("schema=1\nenabled=['unknown']")
				want = Unknown
			case "foreign repo":
				src.Files[hyprRepoState] = []byte(strings.ReplaceAll(overviewState, "yayuuu", "someone"))
				want = Unknown
			case "bad state":
				src.Files[hyprRepoState] = []byte("do-not-render")
				want = Unknown
			case "bad listing":
				src.Commands["hyprctl -j plugin list"] = []byte("null")
				want = Unknown
			case "no receipt":
				clear(in.Applied.Receipts)
				want = Blocked
			case "unreadable":
				delete(src.Files, hyprRepoState)
				src.Dirs[hyprRepoState] = nil
				want = Unknown
			}
			guard := &readGuard{FakeSource: src}
			task := findTask(t, Inspect(guard, in), "hyprland-plugins")
			if task.Status != want || (task.Action != nil) != (want == Pending) {
				t.Fatalf("got %+v; want %s", task, want)
			}
			for _, command := range guard.commands {
				if !slices.Contains([]string{"Hyprland --version-json", "hyprctl -j version", "hyprctl -j plugin list", "hyprctl -j getoption plugin.scrolloverview.scale"}, command) {
					t.Fatalf("inspection invoked %s", command)
				}
			}
			data, _ := json.Marshal(task)
			if strings.Contains(string(data), "do-not-render") {
				t.Fatal("leaked input")
			}
			if task.Action != nil {
				commands, err := HyprlandCommands(task)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "fresh":
					if len(commands) != 5 || strings.Join(commands[1], " ") != "hyprpm add "+overviewURL {
						t.Fatal(commands)
					}
				case "failed build", "missing binary":
					if !slices.Equal(commands[0], []string{"hyprpm", "update", "--force"}) {
						t.Fatal(commands)
					}
				case "disabled":
					if commands[0][1] != "enable" {
						t.Fatal(commands)
					}
				case "unloaded":
					if len(commands) != 2 || commands[0][1] != "reload" {
						t.Fatal(commands)
					}
				}
			}
		})
	}
}

type installingHyprland struct {
	*nativetest.FakeSource
	streams        []string
	fail, noEffect string
}

func (s *installingHyprland) Stream(_, _ io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	s.streams = append(s.streams, key)
	if key == s.fail {
		return errors.New("native failure")
	}
	if key == s.noEffect {
		return nil
	}
	switch args[0] {
	case "update":
		s.Files[hyprCache+"/state.toml"] = []byte("[state]\nhash='test-abi'")
		s.Files[hyprHeaders] = []byte("Name: Hyprland")
		if _, ok := s.Files[hyprRepoState]; ok {
			s.Files[hyprRepoState] = []byte(strings.ReplaceAll(string(s.Files[hyprRepoState]), "failed = true", "failed = false"))
			s.Files[hyprBinary] = []byte("\x7fELFbinary")
		}
	case "add":
		hyprlandCache(s.FakeSource)
	case "enable":
		hyprlandEnabled(s.FakeSource)
		hyprlandLoaded(s.FakeSource)
	case "reload":
		hyprlandLoaded(s.FakeSource)
		if name == "hyprctl" {
			s.Commands["hyprctl -j getoption plugin.scrolloverview.scale"] = []byte(`{"set":true}`)
		}
	}
	return nil
}

func TestHyprlandInstallationVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"install", "failed build retry", "update failure", "add failure", "add no effect", "enable failure", "reload failure", "config reload failure", "cancel", "changed cache", "changed selection", "changed session"} {
		t.Run(mode, func(t *testing.T) {
			in, src := hyprlandFixture(t)
			if mode == "failed build retry" {
				hyprlandCache(src)
				src.Files[hyprRepoState] = []byte(strings.ReplaceAll(overviewState, "failed = false", "failed = true"))
			}
			task := findTask(t, Inspect(src, in), "hyprland-plugins")
			live := &installingHyprland{FakeSource: src}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "update failure":
				live.fail = "hyprpm update"
			case "add failure":
				live.fail = "hyprpm add " + overviewURL
			case "add no effect":
				live.noEffect = "hyprpm add " + overviewURL
			case "enable failure":
				live.fail = "hyprpm enable yayuuu/scrolloverview"
			case "reload failure":
				live.fail = "hyprpm reload"
			case "config reload failure":
				live.fail = "hyprctl reload config-only"
			case "cancel":
				cancel()
			case "changed cache":
				hyprlandCache(src)
			case "changed selection":
				src.Files[filepath.Join(os.Getenv("HOME"), ".config/hypr/plugins.toml")] = []byte("schema=1\nenabled=[]")
			case "changed session":
				t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "other")
			}
			err := RunHyprlandPlugins(ctx, live, io.Discard, io.Discard, task)
			if (err == nil) != (mode == "install" || mode == "failed build retry") {
				t.Fatalf("unexpected result %v (%v)", err, live.streams)
			}
			if strings.HasPrefix(mode, "changed") || mode == "cancel" {
				if len(live.streams) != 0 {
					t.Fatal(live.streams)
				}
				return
			}
			if mode == "add no effect" && len(live.streams) != 2 {
				t.Fatalf("continued after unverifiable build: %v", live.streams)
			}
			if err != nil {
				live.fail, live.noEffect = "", ""
				retry := findTask(t, Inspect(src, in), "hyprland-plugins")
				if err := RunHyprlandPlugins(context.Background(), live, io.Discard, io.Discard, retry); err != nil {
					t.Fatal(err)
				}
			}
			if err == nil {
				complete := findTask(t, Inspect(src, in), "hyprland-plugins")
				if complete.Status != Complete || complete.Action != nil {
					t.Fatal(complete)
				}
			}
		})
	}
}

func TestHyprlandRejectsChangedCommands(t *testing.T) {
	for _, mode := range []string{"wrong URL", "extra command", "reorder", "sudo", "blocked", "no metadata", "extra argv"} {
		t.Run(mode, func(t *testing.T) {
			in, src := hyprlandFixture(t)
			task := findTask(t, Inspect(src, in), "hyprland-plugins")
			switch mode {
			case "wrong URL":
				task.Action.Commands[1][2] = "https://example.com/other"
			case "extra command":
				task.Action.Commands = append(task.Action.Commands, []string{"hyprpm", "purge-cache"})
			case "reorder":
				task.Action.Commands[0], task.Action.Commands[1] = task.Action.Commands[1], task.Action.Commands[0]
			case "sudo":
				task.Action.Commands[0][0] = "sudo"
			case "blocked":
				task.Status = Blocked
			case "no metadata":
				task.Action.Hyprland = nil
			case "extra argv":
				task.Action.Argv = []string{"sh"}
			}
			if _, err := HyprlandCommands(task); err == nil {
				t.Fatal("accepted changed workflow")
			}
		})
	}
}

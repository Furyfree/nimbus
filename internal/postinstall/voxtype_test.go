package postinstall

import (
	"context"
	"io"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func voxtypeFixture(t *testing.T) (Inputs, *nativetest.FakeSource) {
	t.Helper()
	in, src := fixture("voxtype")
	if src.ExitCodes == nil {
		src.ExitCodes = map[string]int{}
	}
	src.Paths["voxtype"] = "/usr/bin/voxtype"
	src.Paths["systemctl"] = "/usr/bin/systemctl"
	setVoxtypeCatalog(t, src, false)
	// Fedora presets leave the user unit disabled, which systemctl reports as
	// a non-zero exit with the state on stdout.
	setUnit(t, src, "is-enabled", "disabled", 1)
	setUnit(t, src, "is-active", "inactive", 3)
	return in, src
}

func setVoxtypeCatalog(t *testing.T, src *nativetest.FakeSource, installed bool) {
	t.Helper()
	state := "false"
	if installed {
		state = "true"
	}
	src.Commands[nativetest.Key("voxtype", "info", "models", "--json", "--engine", "whisper")] = []byte(
		`{"engines":{"whisper":{"models":[{"name":"small","installed":` + state + `},{"name":"medium","installed":false}]}},"verified":false}`)
}

func setUnit(t *testing.T, src *nativetest.FakeSource, verb, state string, code int) {
	t.Helper()
	key := nativetest.Key("systemctl", "--user", verb, "voxtype.service")
	src.Commands[key] = []byte(state + "\n")
	if code != 0 {
		src.ExitCodes[key] = code
	}
}

func TestVoxtypeSetupStates(t *testing.T) {
	download := []string{"voxtype", "setup", "--download", "--model", "small"}
	enable := []string{"systemctl", "--user", "enable", "--now", "voxtype.service"}
	for _, test := range []struct {
		name      string
		installed bool
		enabled   string
		active    string
		want      Status
		commands  [][]string
	}{
		{name: "no model", want: Pending, commands: [][]string{download, enable}},
		{name: "model disabled", installed: true, want: Pending, commands: [][]string{enable}},
		{name: "model enabled", installed: true, enabled: "enabled", active: "active", want: Complete},
		{name: "model enabled but stopped", installed: true, enabled: "enabled", active: "inactive", want: Complete},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := voxtypeFixture(t)
			if test.installed {
				setVoxtypeCatalog(t, src, true)
			}
			if test.enabled != "" {
				setUnit(t, src, "is-enabled", test.enabled, 0)
			}
			if test.active != "" {
				setUnit(t, src, "is-active", test.active, 0)
			}
			got := findTask(t, Inspect(src, in), "voxtype")
			if got.Status != test.want {
				t.Fatalf("got %s (%s), want %s", got.Status, got.Detail, test.want)
			}
			if test.commands == nil {
				if got.Action != nil {
					t.Fatalf("complete task offered an action: %+v", got.Action)
				}
				return
			}
			if got.Action == nil || got.Action.Kind != SetupVoxtype {
				t.Fatalf("pending task has no Voxtype action: %+v", got)
			}
			if !equalCommands(got.Action.Commands, test.commands) {
				t.Fatalf("got %v, want %v", got.Action.Commands, test.commands)
			}
			if _, err := VoxtypeCommands(got); err != nil {
				t.Fatalf("offered action was rejected: %v", err)
			}
		})
	}
}

func TestVoxtypeSetupBlocksAndUnknowns(t *testing.T) {
	t.Run("package missing", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		in.Facts.Packages.Value = nil
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Blocked || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("binary missing", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		delete(src.Paths, "voxtype")
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Blocked || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("catalog unreadable", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		delete(src.Commands, nativetest.Key("voxtype", "info", "models", "--json", "--engine", "whisper"))
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("model unlisted", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		src.Commands[nativetest.Key("voxtype", "info", "models", "--json", "--engine", "whisper")] = []byte(`{"engines":{"whisper":{"models":[{"name":"medium","installed":false}]}}}`)
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("no user session", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		setVoxtypeCatalog(t, src, true)
		delete(src.Commands, nativetest.Key("systemctl", "--user", "is-enabled", "voxtype.service"))
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("unrecognized unit state", func(t *testing.T) {
		in, src := voxtypeFixture(t)
		setVoxtypeCatalog(t, src, true)
		setUnit(t, src, "is-enabled", "banana", 0)
		got := findTask(t, Inspect(src, in), "voxtype")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestVoxtypeCommandsRejectForgedActions(t *testing.T) {
	valid := Task{ID: "voxtype", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeDownloadCommand(), voxtypeEnableCommand()}}}
	if _, err := VoxtypeCommands(valid); err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}
	enableOnly := Task{ID: "voxtype", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand()}}}
	if _, err := VoxtypeCommands(enableOnly); err != nil {
		t.Fatalf("enable-only workflow rejected: %v", err)
	}
	for _, task := range []Task{
		{ID: "voxtype", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{{"sh", "-c", "voxtype setup"}, voxtypeEnableCommand()}}},
		{ID: "voxtype", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand(), voxtypeDownloadCommand()}}},
		{ID: "voxtype", Status: Complete, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand()}}},
		{ID: "other", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand()}}},
	} {
		if _, err := VoxtypeCommands(task); err == nil {
			t.Fatalf("forged action accepted: %+v", task)
		}
	}
}

func TestRunVoxtypeSetupStreamsApprovedWorkflow(t *testing.T) {
	in, src := voxtypeFixture(t)
	fresh := findTask(t, Inspect(src, in), "voxtype")
	if fresh.Action == nil {
		t.Fatal("fixture offered no action")
	}
	src.Commands[nativetest.Key("voxtype", "setup", "--download", "--model", "small")] = nil
	src.Commands[nativetest.Key("systemctl", "--user", "enable", "--now", "voxtype.service")] = nil
	task := Task{ID: "voxtype", Status: Pending, Action: &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeDownloadCommand(), voxtypeEnableCommand()}}}
	if err := RunVoxtypeSetup(context.Background(), src, io.Discard, io.Discard, task); err != nil {
		t.Fatalf("recorded workflow failed: %v", err)
	}
	// Native commands alone never complete the task; re-inspection decides.
	after := findTask(t, Inspect(src, in), "voxtype")
	if after.Status != Pending {
		t.Fatalf("unrecorded native commands completed the task: %+v", after)
	}
}

package postinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

// The Chezmoi-managed desktop config selects the small multilingual model, and
// the Hyprland session hook starts the unit once a model exists. The task keeps
// that selection: it downloads the same model and makes the service persistent.
const voxtypeModel = "small"

func voxtypeDownloadCommand() []string {
	return []string{"voxtype", "setup", "--download", "--model", voxtypeModel}
}

func voxtypeEnableCommand() []string {
	return []string{"systemctl", "--user", "enable", "--now", "voxtype.service"}
}

// VoxtypeCommands validates the ordered native workflow for the current state.
// A downloaded model only needs enablement; a missing model needs both steps.
func VoxtypeCommands(task Task) ([][]string, error) {
	if task.ID != "voxtype" || task.Status != Pending || task.Action == nil || task.Action.Kind != SetupVoxtype || len(task.Action.Argv) != 0 {
		return nil, errors.New("task has no supported Voxtype action")
	}
	expected := [][]string{voxtypeDownloadCommand(), voxtypeEnableCommand()}
	if equalCommands(task.Action.Commands, [][]string{voxtypeEnableCommand()}) {
		return task.Action.Commands, nil
	}
	if equalCommands(task.Action.Commands, expected) {
		return task.Action.Commands, nil
	}
	return nil, errors.New("unsupported Voxtype setup command")
}

func equalCommands(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// RunVoxtypeSetup downloads the selected model and makes the user unit
// persistent. The caller keeps approval, the operation lock and the digest
// recheck; completion is verified by re-inspection, never by these commands.
func RunVoxtypeSetup(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	commands, err := VoxtypeCommands(task)
	if err != nil {
		return err
	}
	for _, argv := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := src.Stream(out, errOut, argv[0], argv[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func voxtypeSetup(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "voxtype", Owner: "package:" + pkg.Canonical, Title: "Set up local dictation", Status: Unknown,
		Prerequisites: []string{
			"The selected Voxtype package is applied and recorded by Nimbus.",
			"The Chezmoi desktop configuration selects the same small model.",
		},
		Instructions: []string{
			"Whisper model downloads need network access and are several hundred megabytes; the native tool verifies the download.",
			"Enabling voxtype.service starts dictation at every graphical login. It is a user unit; no system service or root access is involved.",
		},
		Verification: "The task re-reads the native model catalog and the user unit state. Package presence alone is not completion.",
		Recovery:     "Disable dictation with systemctl --user disable --now voxtype.service. Downloaded models stay user data under the XDG data directory; Nimbus never removes them.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	if _, err := src.LookPath("voxtype"); err != nil {
		t.Status, t.Detail = Blocked, "The Voxtype executable is unavailable; repair the selected package with nimbus sync."
		return t
	}
	installed, err := voxtypeModelInstalled(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	enabled, active, err := voxtypeUnitState(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	switch {
	case installed && enabled:
		t.Status = Complete
		t.Detail = "The " + voxtypeModel + " model is installed and voxtype.service is enabled."
		if active {
			t.Detail += " Dictation is running."
		} else {
			t.Detail += " It starts at the next graphical login."
		}
	case !installed:
		t.Status = Pending
		t.Detail = "The " + voxtypeModel + " Whisper model is not installed."
		t.Action = &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeDownloadCommand(), voxtypeEnableCommand()}}
	default:
		t.Status = Pending
		t.Detail = "The " + voxtypeModel + " model is installed, but voxtype.service is not enabled."
		t.Action = &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand()}}
	}
	return t
}

// voxtypeModelInstalled reads the native model catalog. Its per-model check is
// the tool's own integrity check; Nimbus never reads model files or the home.
func voxtypeModelInstalled(src native.Source) (bool, error) {
	out, err := src.Run("voxtype", "info", "models", "--json", "--engine", "whisper")
	if err != nil {
		return false, errors.New("The Voxtype model catalog could not be read; run voxtype info models, then retry")
	}
	var catalog struct {
		Engines map[string]struct {
			Models []struct {
				Name      string `json:"name"`
				Installed bool   `json:"installed"`
			} `json:"models"`
		} `json:"engines"`
	}
	if json.Unmarshal(out, &catalog) != nil {
		return false, errors.New("Voxtype returned an unrecognized model catalog; inspect voxtype info models")
	}
	whisper, ok := catalog.Engines["whisper"]
	if !ok {
		return false, errors.New("Voxtype no longer reports the whisper engine; inspect voxtype info engines")
	}
	for _, model := range whisper.Models {
		if model.Name == voxtypeModel {
			return model.Installed, nil
		}
	}
	return false, errors.New("The Voxtype catalog no longer lists the " + voxtypeModel + " model; review the managed config and package version")
}

// voxtypeUnitState reads the persistent and current unit state. Native user
// units are disabled by default; is-enabled reports that as a non-zero exit
// with the state on stdout.
func voxtypeUnitState(src native.Source) (enabled, active bool, err error) {
	out, runErr := src.Run("systemctl", "--user", "is-enabled", "voxtype.service")
	state := strings.TrimSpace(string(out))
	switch state {
	case "enabled":
		enabled = true
	case "disabled", "static", "masked", "linked", "indirect", "alias", "generated", "transient":
	default:
		if runErr != nil {
			return false, false, errors.New("The voxtype.service unit state could not be read; check that a user systemd session is available, then retry")
		}
		return false, false, errors.New("The voxtype.service unit returned an unrecognized state; inspect systemctl --user status voxtype.service")
	}
	out, runErr = src.Run("systemctl", "--user", "is-active", "voxtype.service")
	state = strings.TrimSpace(string(out))
	switch state {
	case "active":
		active = true
	case "inactive", "failed", "activating", "deactivating":
	default:
		if runErr != nil {
			return false, false, errors.New("The voxtype.service activity could not be read; check that a user systemd session is available, then retry")
		}
		return false, false, errors.New("The voxtype.service unit returned an unrecognized activity state; inspect systemctl --user status voxtype.service")
	}
	return enabled, active, nil
}

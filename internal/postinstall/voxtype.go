package postinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

// The managed Chezmoi config selects the model per machine; the task downloads
// exactly that model. The Hyprland session hook starts the unit once a model
// exists.
//
// voxtypeModelName is the accepted model identifier shape; it keeps the config
// value from becoming an unexpected native argument.
var voxtypeModelName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

func voxtypeDownloadCommand(model string) []string {
	return []string{"voxtype", "setup", "--download", "--model", model}
}

func voxtypeEnableCommand() []string {
	return []string{"systemctl", "--user", "enable", "--now", "voxtype.service"}
}

// VoxtypeCommands validates the ordered native workflow for the current state.
// A downloaded model only needs enablement; a missing model needs both steps.
// The model comes from the managed config, so its name shape is re-validated
// instead of being compared to a fixed value.
func VoxtypeCommands(task Task) ([][]string, error) {
	if task.ID != "voxtype" || task.Status != Pending || task.Action == nil || task.Action.Kind != SetupVoxtype || len(task.Action.Argv) != 0 {
		return nil, errors.New("task has no supported Voxtype action")
	}
	enable := voxtypeEnableCommand()
	if equalCommands(task.Action.Commands, [][]string{enable}) {
		return task.Action.Commands, nil
	}
	if commands := task.Action.Commands; len(commands) == 2 && len(commands[0]) == 5 &&
		slices.Equal(commands[0][:4], []string{"voxtype", "setup", "--download", "--model"}) &&
		voxtypeModelName.MatchString(commands[0][4]) &&
		equalCommands(commands[1:], [][]string{enable}) {
		return commands, nil
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
			"The Chezmoi configuration is applied and selects the Whisper model in ~/.config/voxtype/config.toml.",
		},
		Instructions: []string{
			"Whisper model downloads need network access and can exceed a gigabyte (large-v3-turbo is about 1.6 GB); the native tool verifies the download.",
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
	model, err := voxtypeConfiguredModel(src)
	if err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	installed, err := voxtypeModelInstalled(src, model)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	enabled, active, err := voxtypeUnitState(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	t.CurrentState = active
	if active == "active" {
		t.CurrentState = "Running"
	} else if active == "inactive" {
		t.CurrentState = "Stopped"
	} else if active == "failed" {
		t.CurrentState = "Failed"
	}
	switch {
	case installed && enabled:
		t.Status = Complete
		t.Detail = "The " + model + " model is installed and voxtype.service is enabled."
		if active == "active" {
			t.Detail += " Dictation is running."
		}
	case !installed:
		t.Status = Pending
		t.Detail = "The " + model + " Whisper model is not installed."
		t.Action = &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeDownloadCommand(model), voxtypeEnableCommand()}}
	default:
		t.Status = Pending
		t.Detail = "The " + model + " model is installed, but voxtype.service is not enabled."
		t.Action = &Action{Kind: SetupVoxtype, Commands: [][]string{voxtypeEnableCommand()}}
	}
	t.Detail += " Current service activity: " + active + "."
	if !enabled {
		t.Detail += " Startup is not enabled."
	}
	if active == "failed" {
		t.Status = Pending
	}
	t.Summary = "Startup disabled"
	if enabled {
		t.Summary = "Startup enabled"
	}
	if installed {
		t.Summary += " · model: " + model
	} else {
		t.Summary += " · model missing: " + model
	}
	return t
}

// voxtypeConfiguredModel reads the model the managed Chezmoi config selects.
// The config owns the choice; Nimbus never guesses one.
func voxtypeConfiguredModel(src native.Source) (string, error) {
	path, err := voxtypeConfigPath()
	if err != nil {
		return "", err
	}
	data, err := src.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("the managed Voxtype config is missing at " + path + "; apply the Chezmoi configuration first")
		}
		return "", errors.New("the managed Voxtype config could not be read: " + err.Error())
	}
	var config struct {
		Whisper struct {
			Model string `toml:"model"`
		} `toml:"whisper"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return "", errors.New("the managed Voxtype config is not valid TOML: " + err.Error())
	}
	model := config.Whisper.Model
	if model == "" {
		return "", errors.New("the managed Voxtype config does not select a [whisper] model; review " + path)
	}
	if !voxtypeModelName.MatchString(model) {
		return "", errors.New("the managed Voxtype config selects an unsupported model name " + model)
	}
	return model, nil
}

// voxtypeConfigPath resolves the managed config through the session's absolute
// XDG configuration directory, falling back to the user's home directory. A
// relative XDG value is ignored, as the XDG spec requires.
func voxtypeConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "voxtype", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("the user home directory is unknown; inspect the session environment")
	}
	return filepath.Join(home, ".config", "voxtype", "config.toml"), nil
}

// voxtypeModelInstalled reads the native model catalog. Its per-model check is
// the tool's own integrity check; Nimbus never reads model files.
func voxtypeModelInstalled(src native.Source, model string) (bool, error) {
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
	for _, listed := range whisper.Models {
		if listed.Name == model {
			return listed.Installed, nil
		}
	}
	return false, errors.New("The Voxtype catalog no longer lists the " + model + " model; review the managed config and package version")
}

// voxtypeUnitState reads the persistent and current unit state. Native user
// units are disabled by default; is-enabled reports that as a non-zero exit
// with the state on stdout.
func voxtypeUnitState(src native.Source) (enabled bool, active string, err error) {
	out, runErr := src.Run("systemctl", "--user", "is-enabled", "voxtype.service")
	state := strings.TrimSpace(string(out))
	switch state {
	case "enabled":
		enabled = true
	case "disabled", "static", "masked", "linked", "indirect", "alias", "generated", "transient":
	default:
		if runErr != nil {
			return false, "", errors.New("The voxtype.service unit state could not be read; check that a user systemd session is available, then retry")
		}
		return false, "", errors.New("The voxtype.service unit returned an unrecognized state; inspect systemctl --user status voxtype.service")
	}
	out, runErr = src.Run("systemctl", "--user", "is-active", "voxtype.service")
	state = strings.TrimSpace(string(out))
	switch state {
	case "active", "inactive", "failed", "activating", "deactivating":
		active = state
	default:
		if runErr != nil {
			return false, "", errors.New("The voxtype.service activity could not be read; check that a user systemd session is available, then retry")
		}
		return false, "", errors.New("The voxtype.service unit returned an unrecognized activity state; inspect systemctl --user status voxtype.service")
	}
	return enabled, active, nil
}

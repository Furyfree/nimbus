package postinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

const overviewRepo = "hyprland-scroll-overview"
const overviewURL = "https://github.com/yayuuu/" + overviewRepo + ".git"

// HyprlandSetup binds the approved repair to its user, session and observations.
// Plugin selection is user configuration; this engine supports ScrollOverview.
type HyprlandSetup struct {
	Home     string `json:"home"`
	User     string `json:"user"`
	Session  string `json:"session"`
	Observed string `json:"observed"`
	Update   bool   `json:"update"`
	Rebuild  bool   `json:"rebuild"`
	Add      bool   `json:"add"`
	Enable   bool   `json:"enable"`
}

func (h HyprlandSetup) commands() [][]string {
	var result [][]string
	if h.Update {
		argv := []string{"hyprpm", "update"}
		if h.Rebuild {
			argv = append(argv, "--force")
		}
		result = append(result, argv)
	}
	if h.Add {
		result = append(result, []string{"hyprpm", "add", overviewURL})
	}
	if h.Enable {
		result = append(result, []string{"hyprpm", "enable", "yayuuu/scrolloverview"})
	}
	reload := []string{"hyprpm", "reload"}
	if h.Update && !h.Add {
		reload = append(reload, "--force")
	}
	return append(result, reload, []string{"hyprctl", "reload", "config-only"})
}

func hyprlandPlugins(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "hyprland-plugins", Owner: "package:" + pkg.Canonical,
		Title: "Install and load selected Hyprland plugins", Status: Unknown,
		Instructions: []string{
			"Apply Chezmoi first, then run inside your Hyprland session. HyprPM downloads and builds third-party plugin code and may ask for administrator authentication.",
			"Updates can rebuild all registered HyprPM repositories and reload their enabled plugins. The final config reload applies your Lua settings. Nimbus does not change plugin selection or create a completion receipt.",
		},
		Verification: "Read Chezmoi's plugin selection, HyprPM build/enabled state and binary, matching installed/running Hyprland ABI, and the live plugin list and configured overview options. This does not test overview appearance.",
		Recovery:     "Retry after fixing connectivity or build errors. HyprPM keeps partial results. Use its disable/remove commands for removal; deselection in Chezmoi does not remove native plugins.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	home, err := os.UserHomeDir()
	user := in.Facts.User.Value.Name
	if err != nil || !filepath.IsAbs(home) || !in.Facts.User.Known() || !operatorName.MatchString(user) || user == "root" {
		t.Detail = "The invoking non-root user could not be determined."
		return t
	}
	enabled, err := hyprlandSelection(src, home)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if !enabled {
		t.Status, t.Detail = NotApplicable, "No Hyprland plugins are selected; existing native plugins are preserved."
		return t
	}
	for _, tool := range []string{"Hyprland", "hyprctl", "hyprpm"} {
		if _, err := src.LookPath(tool); err != nil {
			t.Status, t.Detail = Blocked, "Hyprland build tools are unavailable; run nimbus sync first."
			return t
		}
	}
	session := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if session == "" || !filepath.IsAbs(os.Getenv("XDG_RUNTIME_DIR")) {
		t.Detail = "Run this task from a terminal in the active Hyprland desktop session."
		return t
	}
	observed, err := inspectHyprlandPlugins(src, home, user)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if observed.InstalledABI != observed.RunningABI {
		t.Status, t.Detail, t.Logout = Blocked, "Installed Hyprland differs from the running session; log out and back in, then retry.", true
		return t
	}
	if observed.ready() {
		t.Status, t.Detail = Complete, "Selected Hyprland plugin ScrollOverview is built, enabled and loaded."
		return t
	}
	setup := &HyprlandSetup{Home: home, User: user, Session: session, Observed: observed.Digest,
		Update:  !observed.Headers || (observed.Repository && !observed.Built),
		Rebuild: observed.Repository && !observed.Built,
		Add:     !observed.Repository, Enable: !observed.Enabled,
	}
	t.Status, t.Detail = Pending, "ScrollOverview needs native installation, build repair, enablement or loading."
	t.Action = &Action{Kind: SyncHyprlandPlugins, Hyprland: setup, Commands: setup.commands()}
	return t
}

func hyprlandSelection(src native.Source, home string) (bool, error) {
	data, err := src.ReadFile(filepath.Join(home, ".config/hypr/plugins.toml"))
	if err != nil {
		return false, errors.New("Hyprland plugin selection is unavailable; apply Chezmoi's ~/.config/hypr/plugins.toml first.")
	}
	var config struct {
		Schema  int      `toml:"schema"`
		Enabled []string `toml:"enabled"`
	}
	if toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&config) != nil || config.Schema != 1 || config.Enabled == nil || len(config.Enabled) > 1 || (len(config.Enabled) == 1 && config.Enabled[0] != "scrolloverview") {
		return false, errors.New("Unsupported Hyprland plugin selection; this engine accepts schema 1 with enabled = [] or [\"scrolloverview\"].")
	}
	return len(config.Enabled) == 1, nil
}

type hyprlandObservation struct {
	InstalledABI, RunningABI, Digest                        string
	Headers, Repository, Built, Enabled, Loaded, Configured bool
}

func (o hyprlandObservation) ready() bool {
	return o.Headers && o.Repository && o.Built && o.Enabled && o.Loaded && o.Configured && o.InstalledABI == o.RunningABI
}

// HyprPM 0.56's list command initializes privileged state. Inspect files and IPC
// directly instead. Missing files are repairable; unreadable/malformed state is
// unknown, never permission to overwrite another repository.
func inspectHyprlandPlugins(src native.Source, home, user string) (hyprlandObservation, error) {
	var o hyprlandObservation
	digest := sha256.New()
	record := func(label string, data []byte) {
		fmt.Fprintf(digest, "%s\x00%d\x00", label, len(data))
		_, _ = digest.Write(data)
	}
	read := func(path string) ([]byte, error) {
		data, err := src.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			record(path, nil)
			return nil, nil
		}
		if err != nil {
			return nil, errors.New("HyprPM files are unreadable; inspect native cache permissions before retrying.")
		}
		record(path, data)
		return data, nil
	}
	for _, query := range []struct {
		argv []string
		abi  *string
	}{
		{[]string{"Hyprland", "--version-json"}, &o.InstalledABI},
		{[]string{"hyprctl", "-j", "version"}, &o.RunningABI},
	} {
		data, err := src.Run(query.argv[0], query.argv[1:]...)
		var v struct {
			Version string `json:"version"`
			ABI     string `json:"abiHash"`
		}
		if err != nil || json.Unmarshal(data, &v) != nil || v.ABI == "" || !strings.HasPrefix(v.Version, "0.56.") {
			return o, errors.New("Cannot inspect supported Hyprland 0.56 ABI state; run inside the Hyprland session and check its version.")
		}
		*query.abi = v.ABI
		record(query.argv[0], data)
	}
	selection, err := read(filepath.Join(home, ".config/hypr/plugins.toml"))
	if err != nil {
		return o, err
	}
	record("selection", selection)
	root := filepath.Join("/var/cache/hyprpm", user)
	entries, err := src.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return o, errors.New("HyprPM cache cannot be inspected.")
	}
	slices.Sort(entries)
	record("cache entries", []byte(strings.Join(entries, "\x00")))
	data, err := read(filepath.Join(root, "state.toml"))
	if err != nil {
		return o, err
	}
	if data != nil {
		var state struct {
			State *struct {
				Hash *string `toml:"hash"`
			} `toml:"state"`
		}
		if toml.Unmarshal(data, &state) != nil || state.State == nil || state.State.Hash == nil {
			return o, errors.New("Unrecognized HyprPM global state; inspect it with HyprPM before retrying.")
		}
		o.Headers = *state.State.Hash == o.InstalledABI
	}
	// HyprPM validates this header tree before building/loading. Check its marker
	// too, so a removed header cache cannot leave a falsely completed task.
	data, err = read(filepath.Join(root, "headersRoot/share/pkgconfig/hyprland.pc"))
	if err != nil {
		return o, err
	}
	o.Headers = o.Headers && len(data) > 0
	for _, entry := range entries {
		if entry == "state.toml" || entry == "headersRoot" {
			continue
		}
		if !filepath.IsLocal(entry) || strings.Contains(entry, "/") {
			return o, errors.New("Unrecognized HyprPM cache entry.")
		}
		data, err = read(filepath.Join(root, entry, "state.toml"))
		if err != nil {
			return o, err
		}
		if data == nil {
			return o, errors.New("Incomplete HyprPM repository state; inspect the cache before retrying.")
		}
		var repo struct {
			Repository *struct{ Name, Author, URL, Hash, Rev string } `toml:"repository"`
			Overview   *struct {
				Enabled, Failed *bool
				Filename        string
			} `toml:"scrolloverview"`
		}
		if toml.Unmarshal(data, &repo) != nil || repo.Repository == nil {
			return o, errors.New("Unrecognized HyprPM repository state.")
		}
		if entry != overviewRepo && repo.Overview == nil {
			continue
		}
		if entry != overviewRepo || repo.Repository.Name != overviewRepo || repo.Repository.Author != "yayuuu" || strings.TrimSuffix(repo.Repository.URL, ".git") != strings.TrimSuffix(overviewURL, ".git") {
			return o, errors.New("ScrollOverview's cache name belongs to another repository; resolve it with HyprPM before retrying.")
		}
		if repo.Overview == nil || repo.Overview.Enabled == nil || repo.Overview.Failed == nil || repo.Overview.Filename != "scrolloverview.so" {
			return o, errors.New("Unrecognized ScrollOverview build state.")
		}
		o.Repository, o.Enabled = true, *repo.Overview.Enabled
		data, err = read(filepath.Join(root, entry, "scrolloverview.so"))
		if err != nil {
			return o, err
		}
		o.Built = !*repo.Overview.Failed && bytes.HasPrefix(data, []byte("\x7fELF"))
	}
	data, err = src.Run("hyprctl", "-j", "plugin", "list")
	var loaded []struct{ Name, Author, Version string }
	if err != nil || json.Unmarshal(data, &loaded) != nil || loaded == nil {
		return o, errors.New("Hyprland's live plugin list is unavailable or unrecognized.")
	}
	record("loaded", data)
	for _, p := range loaded {
		if p.Name == "" {
			return o, errors.New("Unrecognized Hyprland plugin listing.")
		}
		if p.Name == "scrolloverview" {
			if p.Author != "Vaxry, yayuuu" || p.Version == "" {
				return o, errors.New("A different ScrollOverview plugin is already loaded; inspect it before retrying.")
			}
			o.Loaded = true
		}
	}
	if o.Loaded {
		data, err := src.Run("hyprctl", "-j", "getoption", "plugin.scrolloverview.scale")
		var option struct {
			Set *bool `json:"set"`
		}
		if err != nil || json.Unmarshal(data, &option) != nil || option.Set == nil {
			return o, errors.New("ScrollOverview's live configuration cannot be inspected.")
		}
		o.Configured = *option.Set
		record("overview configuration", data)
	}
	o.Digest = fmt.Sprintf("%x", digest.Sum(nil))
	return o, nil
}

// HyprlandCommands accepts only the fixed, ordered native repair workflow.
func HyprlandCommands(task Task) ([][]string, error) {
	if task.ID != "hyprland-plugins" || task.Status != Pending || task.Action == nil || task.Action.Kind != SyncHyprlandPlugins || task.Action.Hyprland == nil || len(task.Action.Argv) != 0 {
		return nil, errors.New("task has no supported Hyprland action")
	}
	h := task.Action.Hyprland
	if !filepath.IsAbs(h.Home) || !operatorName.MatchString(h.User) || h.User == "root" || h.Session == "" || len(h.Observed) != 64 || (h.Rebuild && !h.Update) || (h.Add && !h.Enable) || !reflect.DeepEqual(task.Action.Commands, h.commands()) {
		return nil, errors.New("unsupported Hyprland plugin workflow")
	}
	return h.commands(), nil
}

// RunHyprlandPlugins verifies each native stage before proceeding. In particular,
// hyprpm add can exit successfully while recording a failed plugin build.
func RunHyprlandPlugins(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	commands, err := HyprlandCommands(task)
	if err != nil {
		return err
	}
	h := task.Action.Hyprland
	if h.Session != os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") {
		return errors.New("Hyprland session changed; inspect the task again")
	}
	enabled, err := hyprlandSelection(src, h.Home)
	if err != nil || !enabled {
		return errors.New("Hyprland plugin selection changed; inspect the task again")
	}
	before, err := inspectHyprlandPlugins(src, h.Home, h.User)
	if err != nil {
		return err
	}
	if before.Digest != h.Observed || before.InstalledABI != before.RunningABI {
		return errors.New("Hyprland state changed; inspect the task again")
	}
	for _, argv := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := src.Stream(out, errOut, argv[0], argv[1:]...); err != nil {
			return err
		}
		o, err := inspectHyprlandPlugins(src, h.Home, h.User)
		if err != nil {
			return err
		}
		if o.InstalledABI != before.InstalledABI || o.RunningABI != before.RunningABI {
			return errors.New("Hyprland changed during setup; log in again before retrying")
		}
		ok := o.Headers
		switch argv[1] {
		case "update":
			ok = ok && (!before.Repository || o.Built)
		case "add":
			ok = ok && o.Repository && o.Built
		case "enable":
			ok = ok && o.Built && o.Enabled && o.Loaded
		case "reload":
			if argv[0] == "hyprctl" {
				ok = o.ready()
			} else {
				ok = ok && o.Built && o.Enabled && o.Loaded
			}
		}
		if !ok {
			return fmt.Errorf("%s finished but its native result could not be verified; retry after inspecting HyprPM's build output", strings.Join(argv, " "))
		}
	}
	return nil
}

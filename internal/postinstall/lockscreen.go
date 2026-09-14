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
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

// LockscreenRepair contains approval evidence, never exported configuration.
type LockscreenRepair struct {
	Config         string `json:"config"`
	Settings       string `json:"settings"`
	ConfigDigest   string `json:"config_digest"`
	SettingsDigest string `json:"settings_digest"`
	PID            int    `json:"pid"`
	Started        string `json:"started"`
	Binary         string `json:"binary"`
	Service        string `json:"service,omitempty"`
}

func lockscreenPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local/state")
	}
	if !filepath.IsAbs(config) || !filepath.IsAbs(state) {
		return "", "", errors.New("Noctalia XDG paths must be absolute")
	}
	return filepath.Join(config, "noctalia/config.toml"), filepath.Join(state, "noctalia/settings.toml"), nil
}

func lockscreenDigest(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }

func lockscreenDocument(data []byte) (map[string]any, error) {
	var doc map[string]any
	if len(data) > 4*1024*1024 || toml.Unmarshal(data, &doc) != nil {
		return nil, errors.New("Noctalia configuration is invalid or too large; contents are not displayed")
	}
	return doc, nil
}

func managedLockscreen(data []byte) (map[string]any, error) {
	doc, err := lockscreenDocument(data)
	if err != nil {
		return nil, err
	}
	section, ok := doc["lockscreen_widgets"].(map[string]any)
	if !ok || section["enabled"] != true || section["schema_version"] != int64(2) {
		return nil, errors.New("Apply Chezmoi's enabled schema-2 lockscreen layout first")
	}
	order, ok := section["widget_order"].([]any)
	if !ok || len(order) == 0 {
		return nil, errors.New("Managed lockscreen widget order is missing")
	}
	widgets, ok := section["widget"].(map[string]any)
	if !ok {
		return nil, errors.New("Managed lockscreen widgets are missing")
	}
	types := map[string]bool{}
	for _, value := range order {
		id, ok := value.(string)
		if !ok {
			return nil, errors.New("Invalid managed lockscreen widget ID")
		}
		widget, ok := widgets[id].(map[string]any)
		if !ok {
			return nil, errors.New("Managed lockscreen order references a missing widget")
		}
		kind, _ := widget["type"].(string)
		types[kind] = true
	}
	if !types["login_box"] || !types["clock"] || !types["sticker"] {
		return nil, errors.New("Managed lockscreen must contain a login box, clock and avatar")
	}
	return section, nil
}

// Remove expressions by their parsed TOML ranges, not line-based regexes.
// Strings, quoted keys and unrelated comments/formatting remain untouched.
func removeLockscreenOverrides(data []byte) ([]byte, bool, error) {
	doc, err := lockscreenDocument(data)
	if err != nil {
		return nil, false, err
	}
	if _, ok := doc["lockscreen_widgets"]; !ok {
		return bytes.Clone(data), false, nil
	}
	var parser unstable.Parser
	parser.Reset(data)
	var out []byte
	cursor := 0
	inSection := false
	inRoot := true
	for parser.NextExpression() {
		n := parser.Expression()
		remove := false
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			key := n.Key()
			key.Next()
			inSection = string(key.Node().Data) == "lockscreen_widgets"
			inRoot = false
			remove = inSection
		case unstable.KeyValue:
			key := n.Key()
			key.Next()
			remove = inSection || (inRoot && string(key.Node().Data) == "lockscreen_widgets")
		}
		if remove {
			start, end := int(n.Raw.Offset), int(n.Raw.Offset+n.Raw.Length)
			if n.Kind == unstable.Table || n.Kind == unstable.ArrayTable {
				keys := n.Key()
				keys.Next()
				start = bytes.LastIndexByte(data[:keys.Node().Raw.Offset], '[')
				if n.Kind == unstable.ArrayTable {
					start--
				}
				last := keys.Node().Raw
				for keys.Next() {
					last = keys.Node().Raw
				}
				end = int(last.Offset + last.Length)
				for end < len(data) && data[end] != ']' {
					end++
				}
				end++
				if n.Kind == unstable.ArrayTable {
					end++
				}
			}
			if start < cursor || end > len(data) {
				return nil, false, errors.New("Unsupported TOML expression range")
			}
			out = append(out, data[cursor:start]...)
			cursor = end
		}
	}
	if parser.Error() != nil {
		return nil, false, errors.New("Could not safely parse Noctalia overrides")
	}
	out = append(out, data[cursor:]...)
	after, err := lockscreenDocument(out)
	if err != nil {
		return nil, false, err
	}
	delete(doc, "lockscreen_widgets")
	if !reflect.DeepEqual(doc, after) {
		return nil, false, errors.New("Override removal would change unrelated settings")
	}
	return out, true, nil
}

func noctaliaUnlocked(src native.Source) error {
	data, err := src.Run("noctalia", "msg", "status")
	var status struct {
		Locked    *bool `json:"locked"`
		PanelOpen *bool `json:"panelOpen"`
	}
	if err != nil || json.Unmarshal(data, &status) != nil || status.Locked == nil || status.PanelOpen == nil {
		return errors.New("Run this task inside the active Noctalia desktop session")
	}
	if *status.Locked {
		return errors.New("Unlock the screen before repairing its layout")
	}
	if *status.PanelOpen {
		return errors.New("Close Noctalia panels and the lockscreen editor before retrying")
	}
	return nil
}

func noctaliaProcess(src native.Source) (int, string, string, error) {
	data, err := src.Run("pgrep", "-u", strconv.Itoa(os.Getuid()), "-x", "noctalia")
	fields := strings.Fields(string(data))
	if err != nil || len(fields) != 1 {
		return 0, "", "", errors.New("Expected exactly one Noctalia process owned by this user")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 1 {
		return 0, "", "", errors.New("Invalid Noctalia process identity")
	}
	binary, err := src.LookPath("noctalia")
	if err != nil || !filepath.IsAbs(binary) {
		return 0, "", "", errors.New("Noctalia executable is unavailable")
	}
	data, err = src.Run("readlink", fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || strings.TrimSpace(string(data)) != binary {
		return 0, "", "", errors.New("Running Noctalia differs from the installed executable; log in again first")
	}
	data, err = src.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return 0, "", "", errors.New("Cannot inspect Noctalia launch arguments")
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if len(args) < 1 || filepath.Base(args[0]) != "noctalia" || len(args) > 2 || (len(args) == 2 && args[1] != "--daemon" && args[1] != "-d") {
		return 0, "", "", errors.New("Noctalia has custom launch arguments; automatic restart is not supported")
	}
	data, err = src.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, "", "", errors.New("Cannot inspect Noctalia process start time")
	}
	_, tail, ok := strings.Cut(string(data), ") ")
	values := strings.Fields(tail)
	if !ok || len(values) < 20 {
		return 0, "", "", errors.New("Invalid Noctalia process start time")
	}
	return pid, values[19], binary, nil
}

func noctaliaLockscreen(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{ID: "noctalia-lockscreen", Owner: "package:" + pkg.Canonical, Title: "Restore the managed lockscreen layout", Status: Unknown,
		Prerequisites: []string{"Apply Chezmoi and close Noctalia panels and its lockscreen editor. Keep the screen unlocked during repair."},
		Instructions:  []string{"After approval, briefly stop only the invoking user's Noctalia shell, back up settings privately, and remove only lockscreen_widgets overrides.", "Restart Noctalia and verify the effective layout. The bar and shell services briefly disappear; Hyprland and applications stay running. The screen is not locked automatically."},
		Verification:  "Verify managed configuration, absence of widget overrides, restarted shell readiness and effective widget settings. Visual placement still needs a manual lockscreen test.",
		Recovery:      "The task prints its private backup path before changing settings. On failure it attempts to restart Noctalia; if that fails run noctalia --daemon from the desktop terminal. Preserve the backup and review it locally before any restoration; restoring it wholesale would undo later preferences."}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	config, settings, err := lockscreenPaths()
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	managed, err := src.ReadFile(config)
	if err != nil {
		t.Status, t.Detail = Blocked, "Managed Noctalia config is missing or unreadable; apply Chezmoi first."
		return t
	}
	desired, err := managedLockscreen(managed)
	if err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	if _, err = src.Run("chezmoi", "--skip-secrets", "verify", config); err != nil {
		t.Status, t.Detail = Blocked, "Noctalia config does not match Chezmoi, or verification failed; review chezmoi diff first."
		return t
	}
	if _, err = src.Run("noctalia", "config", "validate", config); err != nil {
		t.Status, t.Detail = Blocked, "Native Noctalia configuration validation failed; review it locally."
		return t
	}
	data, err := src.ReadFile(settings)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		t.Detail = "Noctalia GUI settings are unreadable."
		return t
	}
	_, hasOverrides, err := removeLockscreenOverrides(data)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if !hasOverrides {
		if err := verifyLockscreenLayout(src, desired); err != nil {
			t.Detail = err.Error()
			return t
		}
		t.Status, t.Detail = Complete, "Managed lockscreen layout matches the effective configuration; visual appearance is not inspected."
		return t
	}
	if err := noctaliaUnlocked(src); err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	pid, started, binary, err := noctaliaProcess(src)
	if err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	service, err := noctaliaSessionService(src, pid)
	if err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	t.Status, t.Detail = Pending, "Saved GUI lockscreen-widget overrides take precedence over the managed layout."
	t.Instructions = append(t.Instructions, fmt.Sprintf("Stop Noctalia PID %d with SIGTERM; back up %s under the private Nimbus state directory before replacement.", pid, settings))
	if service != "" {
		t.Instructions[len(t.Instructions)-1] = fmt.Sprintf("Stop user service %s gracefully without force-killing; back up %s before replacement. Restart it through UWSM.", service, settings)
		t.Recovery = "The private backup path is printed before replacement. On failure Nimbus attempts to restart the same UWSM service. Retry the task or log in again if restart fails; preserve the backup. Restoring it wholesale would undo later preferences."
	}
	t.Action = &Action{Kind: RestoreNoctaliaLockscreen, Lockscreen: &LockscreenRepair{
		Config: config, Settings: settings,
		ConfigDigest: lockscreenDigest(managed), SettingsDigest: lockscreenDigest(data),
		PID: pid, Started: started, Binary: binary, Service: service,
	}}
	return t
}

func verifyLockscreenLayout(src native.Source, desired map[string]any) error {
	data, err := src.Run("noctalia", "config", "export", "merged")
	if err != nil {
		return errors.New("Noctalia's effective configuration could not be read")
	}
	doc, err := lockscreenDocument(data)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(doc["lockscreen_widgets"], desired) {
		return errors.New("Effective lockscreen differs from the managed layout; inspect other Noctalia configuration sources")
	}
	return nil
}

var stopLockscreenShell = native.StopNoctalia

// RunNoctaliaLockscreen requires approval and the caller's operation lock.
func RunNoctaliaLockscreen(ctx context.Context, src native.Source, out io.Writer, task Task) (result error) {
	if task.ID != "noctalia-lockscreen" || task.Status != Pending || task.Action == nil || task.Action.Kind != RestoreNoctaliaLockscreen || task.Action.Lockscreen == nil {
		return errors.New("No approved lockscreen repair")
	}
	p := task.Action.Lockscreen
	config, settings, err := lockscreenPaths()
	if err != nil || p.Config != config || p.Settings != settings {
		return errors.New("Lockscreen paths changed; inspect again")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = noctaliaUnlocked(src); err != nil {
		return err
	}
	pid, started, binary, err := noctaliaProcess(src)
	if err != nil || pid != p.PID || started != p.Started || binary != p.Binary {
		return errors.New("Noctalia process changed after approval; inspect again")
	}
	service, err := noctaliaSessionService(src, pid)
	if err != nil || service != p.Service {
		return errors.New("Noctalia service changed after approval; inspect again")
	}
	managed, err := safeLockscreenRead(config)
	if err != nil {
		return err
	}
	original, err := safeLockscreenRead(settings)
	if err != nil {
		return err
	}
	if lockscreenDigest(managed) != p.ConfigDigest || lockscreenDigest(original) != p.SettingsDigest {
		return errors.New("Noctalia configuration changed after approval; inspect again")
	}
	desired, err := managedLockscreen(managed)
	if err != nil {
		return err
	}
	replacement, changed, err := removeLockscreenOverrides(original)
	if err != nil || !changed {
		return errors.New("Noctalia overrides changed; inspect again")
	}
	fmt.Fprintln(out, "-> stop Noctalia gracefully; leave Hyprland and applications running")
	if service == "" {
		err = stopLockscreenShell(ctx, pid, started, binary)
	} else {
		err = src.Stream(io.Discard, io.Discard, "systemctl", "--user", "stop", service)
		if err == nil {
			// A surviving child would keep the unit alive with ExitType=cgroup.
			data, checkErr := src.Run("systemctl", "--user", "show", service, "--property=ActiveState", "--value")
			if checkErr != nil || strings.TrimSpace(string(data)) != "inactive" {
				err = errors.New("Noctalia service did not stop completely")
			}
		}
	}
	if err != nil {
		return errors.New("Noctalia did not stop gracefully; settings untouched. Inspect the shell before retrying")
	}
	restarted := false
	defer func() {
		if !restarted {
			fmt.Fprintln(out, "-> restart Noctalia after interrupted repair")
			if err := restartLockscreenShell(src, p); err != nil {
				result = errors.Join(result, errors.New("Noctalia restart failed; retry the task or log in again"))
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	// Shutdown may flush preferences. Never overwrite those newly saved values.
	current, err := safeLockscreenRead(settings)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return errors.New("Noctalia saved settings while closing; no settings changed by Nimbus. Retry with its editor closed")
	}
	backup, err := backupLockscreenSettings(original)
	if backup != "" {
		fmt.Fprintf(out, "Private backup: %s\n", backup)
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = replaceLockscreenSettings(settings, original, replacement); err != nil {
		return err
	}
	fmt.Fprintln(out, "-> removed only lockscreen-widget overrides; restart Noctalia")
	restarted = true
	if err = restartLockscreenShell(src, p); err != nil {
		return errors.New("Noctalia restart failed after repair; backup retained")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = noctaliaUnlocked(src)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("Noctalia did not become ready after restart; backup retained, completion not recorded")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	current, err = safeLockscreenRead(settings)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, replacement) {
		return errors.New("Noctalia settings changed during restart; backup retained, completion not recorded")
	}
	managed, err = safeLockscreenRead(config)
	if err != nil || lockscreenDigest(managed) != p.ConfigDigest {
		return errors.New("Managed layout changed during repair; completion not recorded")
	}
	return verifyLockscreenLayout(src, desired)
}

// The fixed service is created by Chezmoi's Hyprland startup through UWSM.
// Reject other supervisors: killing their process could trigger a concurrent restart.
func noctaliaSessionService(src native.Source, pid int) (string, error) {
	data, err := src.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", errors.New("Cannot inspect Noctalia session ownership")
	}
	var group string
	for line := range strings.SplitSeq(string(data), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			group = path
		}
	}
	if group == "" {
		return "", errors.New("Cannot identify Noctalia session ownership")
	}
	unit := filepath.Base(group)
	for parent := group; parent != "/" && parent != "."; parent = filepath.Dir(parent) {
		candidate := filepath.Base(parent)
		if strings.HasSuffix(candidate, ".service") || strings.HasSuffix(candidate, ".scope") {
			unit = candidate
			break
		}
	}
	if unit != noctaliaService {
		if strings.HasSuffix(unit, ".service") && !strings.HasPrefix(unit, "wayland-wm@") {
			return "", errors.New("Noctalia has another service supervisor; use the managed UWSM startup and log in again")
		}
		return "", nil
	}
	data, err = src.Run("systemctl", "--user", "show", noctaliaService,
		"--property=ControlGroup,ActiveState,Restart,SendSIGKILL,TimeoutStopUSec,KillSignal,KillMode,ExecStop,Transient,TimeoutStopFailureMode")
	values := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if err != nil || values["ControlGroup"] != group || values["ActiveState"] != "active" ||
		values["TimeoutStopFailureMode"] != "terminate" || values["Restart"] != "no" || values["SendSIGKILL"] != "no" || values["TimeoutStopUSec"] != "10s" || values["KillSignal"] != "15" ||
		values["KillMode"] != "control-group" || values["ExecStop"] != "" || values["Transient"] != "yes" {
		return "", errors.New("Noctalia service does not match the managed graceful-stop policy; log in again first")
	}
	if _, err = src.LookPath("uwsm"); err != nil {
		return "", errors.New("UWSM is unavailable; cannot restart the managed Noctalia service")
	}
	if _, err = src.Run("uwsm", "check", "is-active", "compositor-only"); err != nil {
		return "", errors.New("UWSM session is unavailable; cannot restart Noctalia")
	}
	return noctaliaService, nil
}

const noctaliaService = "app-noctalia.service"

// LockscreenCommands exposes the exact lifecycle for the approval preview.
func LockscreenCommands(p *LockscreenRepair) [][]string {
	if p.Service == noctaliaService {
		return [][]string{
			{"systemctl", "--user", "stop", noctaliaService},
			{"uwsm", "app", "-s", "s", "-t", "service", "-u", noctaliaService,
				"-p", "TimeoutStopSec=10s", "-p", "SendSIGKILL=no", "--", p.Binary, "--daemon"},
		}
	}
	return [][]string{{p.Binary, "--daemon"}}
}

func restartLockscreenShell(src native.Source, p *LockscreenRepair) error {
	commands := LockscreenCommands(p)
	argv := commands[len(commands)-1]
	return src.Stream(io.Discard, io.Discard, argv[0], argv[1:]...)
}

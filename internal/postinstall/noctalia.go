package postinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

var pluginID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func noctaliaPlugins(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "noctalia-plugins", Owner: "package:" + pkg.Canonical,
		Title: "Install missing enabled Noctalia plugins", Status: Unknown,
		Prerequisites: []string{"Apply Chezmoi, then run this task inside the active Noctalia desktop session with network access."},
		Instructions:  []string{"Noctalia updates the affected sources, exports their enabled plugins and rebuilds the live registry and bar. This may also update already-installed plugins from those sources."},
		Verification:  "Check effective enabled IDs against the running shell's local catalog and readable runtime manifests and entry scripts. This verifies installation, not account sign-in or each widget's behavior.",
		Recovery:      "Retry this task after restoring connectivity. Noctalia owns downloads and partial results; Nimbus writes no plugin files or completion receipt.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	if _, err := src.LookPath("noctalia"); err != nil {
		t.Status, t.Detail = Blocked, "Noctalia is unavailable; repair the selected package with nimbus sync."
		return t
	}
	missing, sources, err := missingNoctaliaPlugins(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if len(missing) == 0 {
		t.Status, t.Detail = Complete, "All effectively enabled Noctalia plugins have readable runtime files."
		return t
	}
	t.Status, t.Detail = Pending, "Missing runtime files for: "+strings.Join(missing, ", ")+"."
	// Source updates also rescan the live registry. In Noctalia 5, enabling an
	// already-enabled ID can export files without refreshing that registry.
	t.Action = &Action{Kind: SyncNoctaliaPlugins}
	for _, source := range sources {
		t.Action.Commands = append(t.Action.Commands, []string{"noctalia", "msg", "plugins", "update", source})
	}
	return t
}

// Read the native merged config rather than maintaining a second plugin list.
// Export may include account settings: never retain it or render parse errors.
func missingNoctaliaPlugins(src native.Source) ([]string, []string, error) {
	data, err := src.Run("noctalia", "config", "export", "full")
	if err != nil {
		return nil, nil, errors.New("Noctalia's effective configuration could not be read; apply Chezmoi and check Noctalia configuration validation.")
	}
	var config struct {
		Plugins *struct {
			Enabled []string `toml:"enabled"`
			Source  []struct {
				Name, Kind, Location string
				Enabled              bool
			} `toml:"source"`
		} `toml:"plugins"`
	}
	if toml.Unmarshal(data, &config) != nil || config.Plugins == nil || config.Plugins.Enabled == nil {
		return nil, nil, errors.New("Noctalia returned an unrecognized effective plugin configuration.")
	}
	if len(config.Plugins.Enabled) == 0 {
		return nil, nil, nil
	}
	data, err = src.Run("noctalia", "msg", "plugins", "list")
	if err != nil {
		return nil, nil, errors.New("Noctalia's running instance is unavailable; run this task from a terminal in the Noctalia desktop session.")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, errors.New("The invoking user's home directory could not be determined.")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(stateHome) {
		stateHome = filepath.Join(home, ".local", "state")
	}
	roots := map[string]string{}
	for _, source := range config.Plugins.Source {
		if !source.Enabled {
			continue
		}
		// Apply the native flat source-name constraint before constructing paths.
		if !pluginID.MatchString(source.Name + "/placeholder") {
			return nil, nil, errors.New("Noctalia has an invalid plugin source name.")
		}
		switch source.Kind {
		case "git":
			roots[source.Name] = filepath.Join(stateHome, "noctalia", "plugins", "materialized", source.Name)
		case "path":
			location := source.Location
			if strings.HasPrefix(location, "~/") {
				location = filepath.Join(home, strings.TrimPrefix(location, "~/"))
			}
			if !filepath.IsAbs(location) {
				return nil, nil, errors.New("Noctalia has a plugin source with an unresolved relative path.")
			}
			roots[source.Name] = location
		}
	}
	// The CLI listing is explicitly local-only in Noctalia 5. It reports
	// catalog availability and enabled intent, not exported runtime presence.
	rows := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "(no plugins)" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || !pluginID.MatchString(fields[0]) || !strings.HasPrefix(fields[1], "[") || !strings.HasSuffix(fields[1], "]") || (fields[3] != "enabled" && fields[3] != "disabled") {
			return nil, nil, errors.New("Noctalia returned an unrecognized plugin listing.")
		}
		rows[fields[0]] = fields
	}
	var missing, sources []string
	for _, id := range config.Plugins.Enabled {
		if !pluginID.MatchString(id) {
			return nil, nil, errors.New("Noctalia has an invalid enabled plugin ID.")
		}
		row, exists := rows[id]
		if !exists {
			return nil, nil, fmt.Errorf("Noctalia has no cached catalog entry for %s; let Noctalia initialize its sources at desktop startup, then retry.", id)
		}
		if row[3] != "enabled" {
			return nil, nil, errors.New("Noctalia's running plugin selection differs from its effective config; reload Noctalia configuration before retrying.")
		}
		if slices.Contains(row[4:], "incompatible") {
			return nil, nil, fmt.Errorf("Plugin %s is incompatible with this Noctalia version; review it in Noctalia settings.", id)
		}
		root, ok := roots[strings.Trim(row[1], "[]")]
		if !ok {
			return nil, nil, fmt.Errorf("Plugin %s uses a source whose runtime path could not be verified.", id)
		}
		_, subdir, _ := strings.Cut(id, "/")
		if !noctaliaRuntimeReady(src, filepath.Join(root, subdir), id) {
			missing = append(missing, id)
			sourceName := strings.Trim(row[1], "[]")
			for _, source := range config.Plugins.Source {
				if source.Name == sourceName && source.Kind != "git" {
					return nil, nil, fmt.Errorf("Plugin %s has missing files in an externally managed path source; repair that source before retrying.", id)
				}
			}
			sources = append(sources, sourceName)
		}
	}
	slices.Sort(missing)
	slices.Sort(sources)
	return slices.Compact(missing), slices.Compact(sources), nil
}

func noctaliaRuntimeReady(src native.Source, root, id string) bool {
	data, err := src.ReadFile(filepath.Join(root, "plugin.toml"))
	var manifest map[string]any
	if err != nil || toml.Unmarshal(data, &manifest) != nil || manifest["id"] != id {
		return false
	}
	entries := 0
	for _, value := range manifest {
		rows, _ := value.([]any)
		for _, entry := range rows {
			fields, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			script, ok := fields["entry"].(string)
			if !ok {
				continue
			}
			if !filepath.IsLocal(script) {
				return false
			}
			if _, err := src.ReadFile(filepath.Join(root, script)); err != nil {
				return false
			}
			entries++
		}
	}
	return entries > 0
}

// NoctaliaCommands validates the fixed native workflow represented by a task.
func NoctaliaCommands(task Task) ([][]string, error) {
	if task.ID != "noctalia-plugins" || task.Status != Pending || task.Action == nil || task.Action.Kind != SyncNoctaliaPlugins || len(task.Action.Argv) != 0 || len(task.Action.Commands) == 0 {
		return nil, errors.New("task has no supported Noctalia action")
	}
	for _, argv := range task.Action.Commands {
		if len(argv) != 5 || !slices.Equal(argv[:4], []string{"noctalia", "msg", "plugins", "update"}) || !pluginID.MatchString(argv[4]+"/placeholder") {
			return nil, errors.New("unsupported Noctalia plugin source command")
		}
	}
	return task.Action.Commands, nil
}

// RunNoctaliaPlugins waits for native source updates to export missing plugins.
// Acknowledging a queued download is not evidence of installation.
func RunNoctaliaPlugins(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	commands, err := NoctaliaCommands(task)
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
	fmt.Fprintln(out, "Waiting for Noctalia to export the enabled plugins (up to two minutes)...")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastProblem := "No verification result is available."
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Noctalia plugin verification incomplete: %s Retry the task: %w", lastProblem, err)
		}
		missing, _, err := missingNoctaliaPlugins(src)
		if err != nil {
			// Source updates run in the background. An unreadable intermediate
			// result is not completion or proof that the update failed. Retry
			// only verification, retaining its sanitized error for the deadline.
			lastProblem = err.Error()
		} else if len(missing) == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("Noctalia plugin verification interrupted: %w", err)
			}
			return nil
		} else {
			lastProblem = fmt.Sprintf("Noctalia runtime files still missing for %s.", strings.Join(missing, ", "))
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

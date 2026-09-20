package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/selector"
)

func TestChannelReport(t *testing.T) {
	stable := &selector.Selector{Schema: selector.CurrentSchema, Channel: selector.ChannelStable,
		Checkout: "/home/pby/.local/share/nimbus", Machine: "vm", Origin: "github.com/Furyfree/nimbus"}
	legacy := *stable
	legacy.Schema = 1

	if lines, drift := channelReport(stable, "main", nil, selector.ChannelStable, nil, "0.6.0"); len(drift) != 0 {
		t.Fatalf("aligned stable reported drift: %v %v", lines, drift)
	}
	develop := *stable
	develop.Channel = selector.ChannelDevelop
	if _, drift := channelReport(&develop, "develop", nil, selector.ChannelDevelop, nil, "0.6.0~dev"); len(drift) != 0 {
		t.Fatalf("aligned develop reported drift: %v", drift)
	}
	lines, drift := channelReport(stable, "develop", nil, selector.ChannelDevelop, nil, "0.6.0")
	if len(drift) != 2 ||
		!strings.Contains(drift[0], "branch develop does not match channel stable") ||
		!strings.Contains(drift[1], "repository points at develop") {
		t.Fatalf("drift = %v, lines = %v", drift, lines)
	}
	lines, drift = channelReport(&legacy, "main", nil, selector.ChannelStable, nil, "0.6.0")
	if len(drift) != 0 || !strings.Contains(strings.Join(lines, "\n"), "schema 1") {
		t.Fatalf("legacy mismatch: %v %v", lines, drift)
	}
	if _, drift := channelReport(stable, "", errors.New("detached"), "unrecognized", nil, "0.6.0"); len(drift) != 2 {
		t.Fatalf("uninspectable state reported no drift: %v", drift)
	}
	if _, drift := channelReport(stable, "main", nil, "", errors.New("missing"), "0.6.0"); len(drift) != 1 ||
		!strings.Contains(drift[0], "could not be inspected") {
		t.Fatalf("missing repository drift = %v", drift)
	}
}

func TestMigrateSelector(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := selector.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// No selector (explicit --checkout and --machine) needs nothing.
	if detail, err := migrateSelector(false); err != nil || detail != "" {
		t.Fatalf("missing selector: %q %v", detail, err)
	}
	legacy := "schema = 1\ncheckout = \"/home/pby/.local/share/nimbus\"\nmachine = \"vm\"\norigin = \"github.com/Furyfree/nimbus\"\n"
	write(legacy)
	// A preview announces the migration and writes nothing.
	if detail, err := migrateSelector(true); err != nil || detail != "schema 2, channel stable" {
		t.Fatalf("plan: %q %v", detail, err)
	}
	if data, _ := os.ReadFile(path); string(data) != legacy {
		t.Fatalf("plan mode wrote the selector: %q", data)
	}
	// An apply records schema 2 on stable and keeps the selection.
	if detail, err := migrateSelector(false); err != nil || detail != "schema 2, channel stable" {
		t.Fatalf("apply: %q %v", detail, err)
	}
	sel, err := selector.Load(path)
	if err != nil || sel.Schema != selector.CurrentSchema || sel.Channel != selector.ChannelStable ||
		sel.Checkout != "/home/pby/.local/share/nimbus" || sel.Machine != "vm" || sel.Origin != "github.com/Furyfree/nimbus" {
		t.Fatalf("migrated selector = %+v %v", sel, err)
	}
	if detail, err := migrateSelector(false); err != nil || detail != "" {
		t.Fatalf("second run migrated again: %q %v", detail, err)
	}
	// A develop selector is current and stays develop.
	if err := selector.SetChannel(path, selector.ChannelDevelop); err != nil {
		t.Fatal(err)
	}
	if detail, err := migrateSelector(false); err != nil || detail != "" {
		t.Fatalf("develop selector touched: %q %v", detail, err)
	}
	if sel, _ = selector.Load(path); sel.Channel != selector.ChannelDevelop {
		t.Fatalf("develop selector became %q", sel.Channel)
	}
	// A selector Nimbus cannot read stops the run before anything changes.
	write("schema = 1\nchannel = \"develop\"\ncheckout = \"/x\"\nmachine = \"vm\"\norigin = \"o\"\n")
	if _, err := migrateSelector(false); err == nil {
		t.Fatal("a malformed selector was accepted")
	}
}

// The preview is where the owner learns about the migration; it must say so
// and leave the selector alone.
func TestSyncPlanAnnouncesTheSelectorMigration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	path, err := selector.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	legacy := "schema = 1\ncheckout = \"" + root + "\"\nmachine = \"vm\"\norigin = \"github.com/Furyfree/nimbus\"\n"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	_, out, errOut := run(t, "sync", "--plan", "--checkout", root, "--machine", "vm")
	if !strings.Contains(out, "Selector: recorded as schema 2, channel stable before anything else changes.") {
		t.Fatalf("preview lacks the migration line:\n%s%s", out, errOut)
	}
	if data, _ := os.ReadFile(path); string(data) != legacy {
		t.Fatalf("the preview wrote the selector: %q", data)
	}
}

func TestRepoChannel(t *testing.T) {
	stable := "[nimbus-engine]\nbaseurl=https://download.copr.fedorainfracloud.org/results/furyfree/nimbus/fedora-44-x86_64/\n"
	develop := strings.Replace(stable, "furyfree/nimbus/", "furyfree/nimbus-develop/", 1)
	if got, err := repoChannel(stable); err != nil || got != selector.ChannelStable {
		t.Fatalf("stable = %q, %v", got, err)
	}
	if got, err := repoChannel(develop); err != nil || got != selector.ChannelDevelop {
		t.Fatalf("develop = %q, %v", got, err)
	}
	if _, err := repoChannel("[nimbus-engine]\nenabled=1\n"); err == nil {
		t.Fatal("a repository without a baseurl was accepted")
	}
	if got, err := repoChannel("[nimbus-engine]\nbaseurl=https://example.test/repo/\n"); err != nil || got != "unrecognized" {
		t.Fatalf("unrecognized = %q, %v", got, err)
	}
}

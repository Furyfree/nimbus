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

func TestChannelNotice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	write := func(body string) {
		t.Helper()
		path, err := selector.DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	legacy := "schema = 1\ncheckout = \"/home/pby/.local/share/nimbus\"\nmachine = \"vm\"\norigin = \"github.com/Furyfree/nimbus\"\n"
	write(legacy)
	notice := channelNotice()
	if !strings.Contains(notice, "counts as stable") || !strings.Contains(notice, "install.sh --channel develop") {
		t.Fatalf("notice = %q", notice)
	}
	if repeat := channelNotice(); repeat != "" {
		t.Fatalf("notice repeated: %q", repeat)
	}
	write(strings.Replace(legacy, "schema = 1", "schema = 2\nchannel = \"stable\"", 1))
	if shown := channelNotice(); shown != "" {
		t.Fatalf("schema 2 showed a notice: %q", shown)
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

package plan

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

func TestFlatpakRemoteMustMatchTheDeclaredURL(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	f.Flatpak.Value.Remotes = []inspect.FlatpakRemote{{Name: "flathub", URL: "https://example.invalid/not-flathub/"}}
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "flatpak-remote:flathub"); op == nil || !strings.Contains(op.Blocked, "points to https://example.invalid/not-flathub/") {
		t.Fatalf("mismatched remote = %+v", op)
	}
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || !strings.Contains(op.Blocked, "cannot be used") {
		t.Fatalf("spotify = %+v", op)
	}
	f.Flatpak.Value.Remotes = []inspect.FlatpakRemote{{Name: "flathub", URL: "https://dl.flathub.org/repo/", GPGVerify: true, KeyFingerprints: []string{definitions.NormalizeFingerprint(c.Definitions().Repositories["flathub"].Key)}}}
	f.Flatpak.Value.Apps = []inspect.FlatpakApp{{ID: "com.spotify.Client", Version: "1", Origin: "fedora"}}
	p, _ = Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if op := find(p, "flatpak:com.spotify.Client"); op == nil || op.Action != ActionAdopt || op.Blocked != "" || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "remote fedora, not flathub") {
		t.Fatalf("foreign origin must be adopted with a note: %+v", op)
	}
}

func TestFlatpakWaitsForItsOwnInstallation(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// A fresh base install: no flatpak binary, so its state is unknown,
	// but common installs flatpak in this plan.
	f.Commands["flatpak"] = ""
	f.Flatpak = inspect.Section[inspect.Flatpak]{Error: `flatpak remotes: exec: "flatpak": executable file not found in $PATH`}
	p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, nil)
	remote := find(p, "flatpak-remote:flathub")
	if remote == nil || remote.Blocked != "" || remote.After != "packages:install" {
		t.Fatalf("remote = %+v", remote)
	}
	if app := find(p, "flatpak:com.spotify.Client"); app == nil || app.Blocked != "" || app.After != "flatpak-remote:flathub" {
		t.Fatalf("app = %+v", app)
	}
	if !p.Complete {
		t.Fatal("a fresh host must still get a complete plan")
	}
}

package definitions

import (
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	good := map[string]string{
		"ripgrep":                          "dnf:ripgrep",
		"dnf:ripgrep":                      "dnf:ripgrep",
		"xorg-x11-drv-nvidia-libs.i686":    "dnf:xorg-x11-drv-nvidia-libs.i686",
		"terra:ghostty":                    "terra:ghostty",
		"hyprland-copr:hyprland":           "hyprland-copr:hyprland",
		"flatpak:com.spotify.Client":       "flatpak:com.spotify.Client",
		"flatpak:md.obsidian.Obsidian":     "flatpak:md.obsidian.Obsidian",
		"rpmfusion-nonfree:akmod-nvidia":   "rpmfusion-nonfree:akmod-nvidia",
		"onepassword:1password-cli":        "onepassword:1password-cli",
		"gstreamer1-plugins-bad-freeworld": "dnf:gstreamer1-plugins-bad-freeworld",
	}
	for raw, want := range good {
		ref, err := ParseRef(raw)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if ref.Canonical() != want {
			t.Errorf("%q canonical = %q, want %q", raw, ref.Canonical(), want)
		}
	}
	bad := []string{"", " ripgrep", "ripgrep ", ":name", "terra:", "Terra:ghostty", "flatpak:spotify", "flatpak:com.spotify", "dnf:bad name", "a:b:c d"}
	for _, raw := range bad {
		if _, err := ParseRef(raw); err == nil {
			t.Errorf("%q: expected an error", raw)
		}
	}
	// The split happens at the first colon, so the rest is the name and a
	// second colon makes it an invalid RPM name rather than a nested prefix.
	if _, err := ParseRef("a:b:c"); err == nil || !strings.Contains(err.Error(), `"b:c"`) {
		t.Errorf("a:b:c should fail on the name b:c, got %v", err)
	}
}

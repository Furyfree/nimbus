package cli

import (
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

type launchSource struct {
	nativetest.FakeSource
	argv []string
}

func (s *launchSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.argv = append([]string{name}, args...)
	return nil
}
func TestLaunchUsesOnlyBrowserDiscoveryAndExactArgv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_DATA_DIRS", "/unused")
	src := &launchSource{FakeSource: nativetest.FakeSource{
		Commands: map[string][]byte{nativetest.Key("xdg-settings", "get", "default-web-browser"): []byte("brave.desktop")},
		Files:    map[string][]byte{filepath.Join(home, "data/applications/brave.desktop"): []byte("[Desktop Entry]\nType=Application\nExec=brave-browser %U\n")},
		Paths:    map[string]string{"brave-browser": "/fake/brave-browser"},
	}}
	old := newSource
	newSource = func() native.Source { return src }
	t.Cleanup(func() { newSource = old })
	target := "https://example.org/?q=$(touch%20bad)&x=1"
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"launch", "browser", target}, []string{"/fake/brave-browser", target}},
		{[]string{"launch", "browser", target, "--private"}, []string{"/fake/brave-browser", "--incognito", target}},
		{[]string{"launch", "webapp", target}, []string{"/fake/brave-browser", "--app=" + target}},
		{[]string{"launch", "webapp", target, "--private"}, []string{"/fake/brave-browser", "--incognito", "--app=" + target}},
	} {
		code, _, errOut := run(t, tc.args...)
		if code != 0 || !slices.Equal(src.argv, tc.want) {
			t.Fatalf("%d %q %s", code, src.argv, errOut)
		}
	}
	src.argv = nil
	for _, kind := range []string{"webapp", "browser"} {
		code, out, errOut := run(t, "launch", kind, target, "--json")
		if code != ExitUsage || src.argv != nil || !strings.Contains(errOut, "do not support --json") {
			t.Fatalf("%d %s %s", code, out, errOut)
		}
	}
	for _, args := range [][]string{{"launch", "webapp"}, {"launch", "browser", "one", "two"}, {"launch", "browser", "file:///tmp/x"}} {
		code, _, _ := run(t, args...)
		if code == 0 || src.argv != nil {
			t.Fatalf("invalid invocation launched: %q", args)
		}
	}
}

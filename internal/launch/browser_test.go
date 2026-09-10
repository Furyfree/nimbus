package launch

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

type browserSource struct{ nativetest.FakeSource }

func (s browserSource) ReadFile(path string) ([]byte, error) {
	if b, ok := s.Files[path]; ok {
		return b, nil
	}
	return nil, fs.ErrNotExist
}
func source(exec string) *browserSource {
	return &browserSource{nativetest.FakeSource{
		Commands: map[string][]byte{nativetest.Key("xdg-settings", "get", "default-web-browser"): []byte("test.desktop\n")},
		Files:    map[string][]byte{"/data/applications/test.desktop": []byte("[Desktop Entry]\nType=Application\nName=Test Browser\nIcon=browser\nExec=" + exec + "\n[Desktop Action Other]\nExec=ignored\n")},
		Paths:    map[string]string{"firefox": "/usr/bin/firefox", "microsoft-edge": "/usr/bin/microsoft-edge", "brave-browser": "/usr/bin/brave-browser", "/opt/Browser With Spaces": "/opt/Browser With Spaces"},
	}}
}
func TestBrowserArguments(t *testing.T) {
	for _, tc := range []struct {
		name, exec, url string
		private, webapp bool
		want            []string
	}{
		{"firefox", "firefox %u", "https://example.org/a?x=1&y=$(oops)", true, false, []string{"/usr/bin/firefox", "--private-window", "https://example.org/a?x=1&y=$(oops)"}},
		{"brave app", "brave-browser %U", "https://example.org/%20", false, true, []string{"/usr/bin/brave-browser", "--app=https://example.org/%20"}},
		{"fallback", "firefox %u", "https://example.org/", false, true, []string{"/usr/bin/brave-browser", "--app=https://example.org/"}},
		{"edge", "microsoft-edge %U", "https://example.org", true, false, []string{"/usr/bin/microsoft-edge", "--inprivate", "https://example.org"}},
		{"home", "firefox %U", "", false, false, []string{"/usr/bin/firefox"}},
		{"no placeholder", "firefox --new-window", "https://example.org", false, false, []string{"/usr/bin/firefox", "--new-window", "https://example.org"}},
		{"fields", "firefox %i %c %k %% %U", "https://example.org", false, false, []string{"/usr/bin/firefox", "--icon", "browser", "Test Browser", "/data/applications/test.desktop", "%", "https://example.org"}},
		{"quotes", `"/opt/Browser With Spaces" "argument with spaces" %u`, "https://example.org", false, false, []string{"/opt/Browser With Spaces", "argument with spaces", "https://example.org"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Browser(source(tc.exec), []string{"/missing", "relative", "/data"}, tc.url, tc.private, tc.webapp)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
func TestChromiumBrowserModes(t *testing.T) {
	for _, browser := range []struct {
		name, privateFlag string
	}{
		{"brave-origin", "--incognito"},
		{"brave-origin-stable", "--incognito"},
		{"brave-browser-stable", "--incognito"},
		{"chromium-browser", "--incognito"},
		{"google-chrome-stable", "--incognito"},
		{"microsoft-edge-stable", "--inprivate"},
		{"opera", "--private"},
		{"opera-stable", "--private"},
		{"vivaldi", "--incognito"},
		{"vivaldi-stable", "--incognito"},
		{"helium", "--incognito"},
	} {
		name := browser.name
		bin := "/usr/bin/" + name
		for _, tc := range []struct {
			mode            string
			private, webapp bool
			fallback        bool
			want            []string
		}{
			{"browser", false, false, false, []string{bin, "--profile-directory=Default", "https://example.org/"}},
			{"private", true, false, false, []string{bin, browser.privateFlag, "--profile-directory=Default", "https://example.org/"}},
			{"webapp", false, true, false, []string{bin, "--app=https://example.org/", "--profile-directory=Default"}},
			{"private webapp", true, true, false, []string{bin, browser.privateFlag, "--app=https://example.org/", "--profile-directory=Default"}},
			{"fallback", false, true, true, []string{bin, "--app=https://example.org/"}},
			{"private fallback", true, true, true, []string{bin, browser.privateFlag, "--app=https://example.org/"}},
		} {
			t.Run(name+"/"+tc.mode, func(t *testing.T) {
				src := source(bin + " --profile-directory=Default %U")
				if tc.fallback {
					src = source("firefox %U")
				}
				src.Paths = map[string]string{name: bin, bin: bin, "firefox": "/usr/bin/firefox"}
				got, err := Browser(src, []string{"/data"}, "https://example.org/", tc.private, tc.webapp)
				if err != nil || !slices.Equal(got, tc.want) {
					t.Fatalf("got %q, %v; want %q", got, err, tc.want)
				}
			})
		}
	}
}

func TestBrowserFallbacks(t *testing.T) {
	for _, condition := range []string{"unset default", "missing entry", "missing executable", "unsupported private"} {
		for _, private := range []bool{false, true} {
			name := condition + "/regular"
			if private {
				name = condition + "/private"
			}
			t.Run(name, func(t *testing.T) {
				src := source("unknown --profile=other %U")
				src.Paths = map[string]string{"firefox": "/usr/bin/firefox"}
				switch condition {
				case "unset default":
					src.Commands[nativetest.Key("xdg-settings", "get", "default-web-browser")] = nil
				case "missing entry":
					clear(src.Files)
				case "unsupported private":
					src.Paths["unknown"] = "/usr/bin/unknown"
				}
				want := []string{"/usr/bin/firefox", "https://example.org"}
				if private {
					want = []string{"/usr/bin/firefox", "--private-window", "https://example.org"}
				} else if condition == "unsupported private" {
					want = []string{"/usr/bin/unknown", "--profile=other", "https://example.org"}
				}
				got, err := Browser(src, []string{"/data"}, "https://example.org", private, false)
				if err != nil || !slices.Equal(got, want) {
					t.Fatalf("got %q, %v; want %q", got, err, want)
				}
			})
		}
	}
}

func TestBrowserRefusals(t *testing.T) {
	for _, url := range []string{"file:///tmp/test", "javascript:alert(1)", "--incognito", "https:///missing", "https://example.org\nfoo"} {
		if _, err := Browser(source("firefox %u"), []string{"/data"}, url, false, false); err == nil {
			t.Errorf("accepted %q", url)
		}
	}
	for _, exec := range []string{"", "firefox %Q", "firefox %u %U", "firefox %F", "firefox ; touch /tmp/bad", "firefox $(echo bad)", `firefox "broken`, `firefox "one"two`, `firefox "arg%u"`, "env=bad firefox"} {
		if _, err := Browser(source(exec), []string{"/data"}, "https://example.org", false, false); err == nil {
			t.Errorf("accepted command %q", exec)
		}
	}
	for _, id := range []string{"../test.desktop", "/tmp/test.desktop", "test.desktop\nother.desktop"} {
		s := source("firefox %u")
		s.Commands[nativetest.Key("xdg-settings", "get", "default-web-browser")] = []byte(id)
		if _, err := Browser(s, []string{"/data"}, "", false, false); err == nil {
			t.Errorf("accepted entry %q", id)
		}
	}
	for _, extra := range []string{"Hidden=true\n", "Terminal=true\n", "Exec=other\n"} {
		s := source("firefox %u")
		s.Files["/data/applications/test.desktop"] = []byte("[Desktop Entry]\nType=Application\nExec=firefox %u\n" + extra)
		if _, err := Browser(s, []string{"/data"}, "", false, false); err == nil {
			t.Errorf("accepted entry %q", extra)
		}
	}
	s := source("firefox %u")
	s.Paths = map[string]string{"firefox": "/usr/bin/firefox"}
	if _, err := Browser(s, []string{"/data"}, "https://example.org", false, true); err == nil {
		t.Error("missing fallback accepted")
	}
	s = source("unknown %u")
	s.Paths = map[string]string{"unknown": "/usr/bin/unknown"}
	if _, err := Browser(s, []string{"/data"}, "", true, false); err == nil {
		t.Error("unknown private mode accepted")
	}
}
func TestDesktopEscapes(t *testing.T) {
	e, err := desktopEntry("[Desktop Entry]\nExec=firefox \"a\\\\$b\" %U\n")
	if err != nil {
		t.Fatal(err)
	}
	words, err := execWords(e["Exec"])
	if err != nil || !slices.Equal(words, []string{"firefox", "a$b", "%U"}) {
		t.Fatalf("%q %v", words, err)
	}
}
func TestUnreadablePreferredEntryDoesNotFallThrough(t *testing.T) {
	s := unreadableSource{source("firefox %u")}
	_, err := Browser(s, []string{"/private", "/data"}, "", false, false)
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("%v", err)
	}
}

type unreadableSource struct{ *browserSource }

func (s unreadableSource) ReadFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, "/private/") {
		return nil, errors.New("permission denied")
	}
	return s.browserSource.ReadFile(path)
}

// Package launch resolves desktop launch commands without executing a shell.
package launch

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/Furyfree/nimbus/internal/native"
)

// Browser resolves the XDG browser, or an installed fallback for the requested mode.
// DataDirs are explicit XDG data roots; callers and tests supply them.
func Browser(src native.Source, dataDirs []string, target string, private, webapp bool) ([]string, error) {
	if target != "" {
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.ContainsFunc(target, unicode.IsControl) {
			return nil, errors.New("browser URL must be an absolute HTTP(S) URL without control characters")
		}
	} else if webapp {
		return nil, errors.New("webapp requires a URL")
	}
	desktopURL := target
	if webapp {
		desktopURL = ""
	}
	args, err := defaultBrowser(src, dataDirs, desktopURL)
	if err != nil {
		return nil, err
	}
	privateFlag := ""
	if len(args) > 0 {
		var app bool
		privateFlag, app = browserModes(args[0])
		bin, err := src.LookPath(args[0])
		if err != nil || (webapp && !app) || (private && privateFlag == "") {
			args = nil
		} else {
			args[0] = bin
		}
	}
	if len(args) == 0 {
		for _, family := range browsers {
			if (webapp && !family.webapp) || (private && family.privateFlag == "") {
				continue
			}
			for _, name := range family.commands {
				if bin, err := src.LookPath(name); err == nil {
					args = []string{bin}
					if desktopURL != "" {
						args = append(args, desktopURL)
					}
					privateFlag = family.privateFlag
					break
				}
			}
			if len(args) > 0 {
				break
			}
		}
	}
	if len(args) == 0 {
		return nil, errors.New("no installed browser supports the requested mode; install a supported browser or change the default browser")
	}
	argv := []string{args[0]}
	if private {
		argv = append(argv, privateFlag)
	}
	if webapp {
		argv = append(argv, "--app="+target)
	}
	return append(argv, args[1:]...), nil
}

// The same ordered list controls mode support and fallback discovery.
var browsers = []struct {
	commands    []string
	privateFlag string
	webapp      bool
}{
	{[]string{"brave-browser", "brave-browser-stable", "brave", "brave-origin", "brave-origin-stable"}, "--incognito", true},
	{[]string{"chromium", "chromium-browser"}, "--incognito", true},
	{[]string{"google-chrome", "google-chrome-stable", "google-chrome-beta", "google-chrome-unstable"}, "--incognito", true},
	{[]string{"microsoft-edge", "microsoft-edge-stable", "microsoft-edge-beta", "microsoft-edge-dev"}, "--inprivate", true},
	{[]string{"opera", "opera-stable", "opera-beta", "opera-developer"}, "--private", true},
	{[]string{"vivaldi", "vivaldi-stable", "vivaldi-snapshot"}, "--incognito", true},
	{[]string{"helium"}, "--incognito", true},
	{[]string{"firefox", "firefox-esr", "librewolf"}, "--private-window", false},
}

func browserModes(bin string) (privateFlag string, webapp bool) {
	for _, family := range browsers {
		if slices.Contains(family.commands, filepath.Base(bin)) {
			return family.privateFlag, family.webapp
		}
	}
	return "", false
}

func defaultBrowser(src native.Source, dataDirs []string, target string) ([]string, error) {
	raw, err := src.Run("xdg-settings", "get", "default-web-browser")
	if err != nil {
		return nil, fmt.Errorf("read default browser: %w", err)
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return nil, nil
	}
	if filepath.Base(id) != id || !strings.HasSuffix(id, ".desktop") || strings.ContainsAny(id, "\\\x00\r\n") {
		return nil, errors.New("default browser must name one desktop entry")
	}
	var entry map[string]string
	var path string
	for _, dir := range dataDirs {
		if !filepath.IsAbs(dir) {
			continue
		}
		path = filepath.Join(dir, "applications", id)
		data, readErr := src.ReadFile(path)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read browser entry: %w", readErr)
		}
		entry, err = desktopEntry(string(data))
		if err != nil {
			return nil, err
		}
		break
	}
	if entry == nil {
		return nil, nil
	}
	if entry["Type"] != "Application" || entry["Hidden"] == "true" || entry["Terminal"] == "true" {
		return nil, errors.New("default browser must be an enabled graphical application")
	}
	words, err := execWords(entry["Exec"])
	if err != nil {
		return nil, err
	}
	if len(words) == 0 || strings.ContainsAny(words[0], "=%") {
		return nil, errors.New("invalid browser executable")
	}
	args := []string{words[0]}
	usedURL := false
	for _, word := range words[1:] {
		switch word {
		case "%u", "%U":
			if usedURL {
				return nil, errors.New("browser entry repeats its URL field")
			}
			usedURL = true
			if target != "" {
				args = append(args, target)
			}
		case "%f", "%F":
			return nil, errors.New("browser entry requires local files instead of URLs")
		case "%i":
			if entry["Icon"] != "" {
				args = append(args, "--icon", entry["Icon"])
			}
		case "%c":
			args = append(args, entry["Name"])
		case "%k":
			args = append(args, path)
		case "%d", "%D", "%n", "%N", "%v", "%m":
		default:
			if strings.Contains(strings.ReplaceAll(word, "%%", ""), "%") {
				return nil, errors.New("unsupported browser desktop field code")
			}
			args = append(args, strings.ReplaceAll(word, "%%", "%"))
		}
	}
	if !usedURL && target != "" {
		args = append(args, target)
	}
	return args, nil
}

func desktopEntry(data string) (map[string]string, error) {
	entry := map[string]string{}
	active := false
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			active = line == "[Desktop Entry]"
			continue
		}
		if !active {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, errors.New("malformed browser desktop entry")
		}
		key = strings.TrimSpace(key)
		if _, exists := entry[key]; exists {
			return nil, errors.New("duplicate browser desktop key")
		}
		// Desktop string escapes are decoded before Exec quoting.
		var decoded strings.Builder
		for i := 0; i < len(value); i++ {
			if value[i] != '\\' {
				decoded.WriteByte(value[i])
				continue
			}
			i++
			if i == len(value) {
				return nil, errors.New("incomplete desktop escape")
			}
			switch value[i] {
			case 's':
				decoded.WriteByte(' ')
			case 'n':
				decoded.WriteByte('\n')
			case 't':
				decoded.WriteByte('\t')
			case 'r':
				decoded.WriteByte('\r')
			case '\\':
				decoded.WriteByte('\\')
			default:
				return nil, errors.New("unsupported desktop string escape")
			}
		}
		entry[key] = decoded.String()
	}
	return entry, nil
}

func execWords(value string) ([]string, error) {
	var words []string
	var word strings.Builder
	quoted, started, closed := false, false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == 0 || c == '\n' || c == '\r' {
			return nil, errors.New("control character in browser command")
		}
		if quoted {
			if c == '"' {
				quoted = false
				closed = true
				continue
			}
			if c == '\\' {
				i++
				if i == len(value) || !strings.ContainsRune("\"`$\\", rune(value[i])) {
					return nil, errors.New("invalid quoted browser escape")
				}
				word.WriteByte(value[i])
				continue
			}
			if c == '$' || c == '`' || c == '%' {
				return nil, errors.New("unescaped reserved character or field code in quoted browser argument")
			}
			word.WriteByte(c)
			continue
		}
		if c == ' ' || c == '\t' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
				closed = false
			}
			continue
		}
		if closed {
			return nil, errors.New("browser arguments must be quoted in whole")
		}
		if c == '"' {
			if started {
				return nil, errors.New("browser arguments must be quoted in whole")
			}
			started = true
			quoted = true
			continue
		}
		if strings.ContainsRune("'\\><~|&;$*?#()`", rune(c)) {
			return nil, errors.New("unquoted reserved character in browser command")
		}
		started = true
		word.WriteByte(c)
	}
	if quoted {
		return nil, errors.New("unclosed browser argument")
	}
	if started {
		words = append(words, word.String())
	}
	return words, nil
}

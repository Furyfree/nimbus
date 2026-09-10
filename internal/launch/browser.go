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

// Browser resolves the XDG browser, or a Chromium fallback for webapp mode.
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
	raw, err := src.Run("xdg-settings", "get", "default-web-browser")
	if err != nil {
		return nil, fmt.Errorf("read default browser: %w", err)
	}
	id := strings.TrimSpace(string(raw))
	if id == "" || filepath.Base(id) != id || !strings.HasSuffix(id, ".desktop") || strings.ContainsAny(id, "\\\x00\r\n") {
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
		return nil, fmt.Errorf("default browser entry %s was not found", id)
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
	chromium := chromiumBrowser(words[0])

	mode := ""
	if webapp {
		mode = "--app=" + target
		target = ""
	}
	if private {
		switch {
		case strings.HasPrefix(filepath.Base(words[0]), "microsoft-edge"):
			mode = "--inprivate"
		case chromium:
			mode = "--incognito"
		case slices.Contains([]string{"firefox", "firefox-esr", "librewolf"}, filepath.Base(words[0])):
			mode = "--private-window"
		default:
			return nil, errors.New("private mode is unsupported for the selected browser")
		}
	}
	args := []string{words[0]}
	if mode != "" {
		args = append(args, mode)
	}
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
	if webapp && !chromium {
		// The supported fallback order is explicit; no shell or desktop config edits.
		for _, name := range []string{"brave-browser", "brave", "chromium", "chromium-browser", "google-chrome"} {
			if bin, e := src.LookPath(name); e == nil {
				return []string{bin, mode}, nil
			}
		}
		return nil, errors.New("webapp needs a Chromium-family browser (Brave, Chromium, or Chrome)")
	}
	bin, err := src.LookPath(args[0])
	if err != nil {
		return nil, fmt.Errorf("find browser: %w", err)
	}
	args[0] = bin
	return args, nil
}

func chromiumBrowser(bin string) bool {
	return slices.Contains([]string{"brave-browser", "brave-browser-stable", "brave", "chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable"}, filepath.Base(bin))
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

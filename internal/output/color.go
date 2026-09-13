// Package output styles Nimbus text at the terminal boundary. Native commands
// use the original writers to retain their own terminal detection and output.
package output

import (
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/term"

	"github.com/charmbracelet/x/ansi"
)

// ColorEnabled uses ANSI palette colors only on a capable interactive stream.
func ColorEnabled(w io.Writer) bool {
	file, ok := Native(w).(*os.File)
	return ok && term.IsTerminal(int(file.Fd())) && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""
}

type colorWriter struct {
	mu           sync.Mutex
	out          io.Writer
	enabled      func() bool
	continuation bool
	width        func() int
}

// ColorWriter styles text and wraps complete lines to the terminal width.
// It never buffers a prompt or progress write waiting for a newline. The predicate is evaluated
// after flag parsing, so --json also disables styling on interactive streams.
func ColorWriter(out io.Writer, enabled func() bool) io.Writer {
	return &colorWriter{out: out, enabled: enabled, width: func() int { return terminalWidth(out) }}
}

// Native removes only the Nimbus color wrapper, retaining other writers such
// as log collectors. Returning the original file preserves child-process TTYs.
func Native(w io.Writer) io.Writer {
	if color, ok := w.(*colorWriter); ok {
		return color.out
	}
	return w
}

// AfterPrompt resets line tracking after input, whose terminal echo bypasses
// this writer. It emits no extra newline and leaves plain writers unchanged.
func AfterPrompt(w io.Writer) {
	if color, ok := w.(*colorWriter); ok {
		color.mu.Lock()
		defer color.mu.Unlock()
		color.continuation = false
	}
}

func (w *colorWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	color, width := w.enabled(), w.width()
	if !color && width == 0 {
		if len(p) > 0 {
			w.continuation = p[len(p)-1] != '\n'
		}
		return w.out.Write(p)
	}
	var b strings.Builder
	for line := range strings.Lines(string(p)) {
		start := !w.continuation
		wrapped := line
		if start && strings.HasSuffix(line, "\n") {
			wrapped = wrapLine(line, width)
		}
		for part := range strings.Lines(wrapped) {
			if color {
				b.WriteString(highlight(part, start))
			} else {
				b.WriteString(part)
			}
			start = true
		}
		w.continuation = !strings.HasSuffix(line, "\n")
	}
	styled := b.String()
	n, err := io.WriteString(w.out, styled)
	if err != nil {
		return 0, err
	}
	if n != len(styled) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

const (
	reset  = "\x1b[0m"
	good   = "\x1b[92m"
	warn   = "\x1b[93m"
	bad    = "\x1b[91m"
	accent = "\x1b[96m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
)

// Match output vocabulary, not arbitrary occurrences inside package names or
// prose. Tables are already aligned before styles are inserted.
var doctorRow = regexp.MustCompile(`^(?:pass|fail|unknown)[ \t]+\S+: `)
var bracketStatus = regexp.MustCompile(`\[(Previously verified|Unable to check|Verified|Pending|Blocked|Not applicable)\]`)
var counts = regexp.MustCompile(`\b[0-9]+ (?:passed|failed|unknown|pending|blocked|unchanged|available)\b`)
var phaseHeading = regexp.MustCompile(`^[1-9][0-9]*\. `)

var outcome = regexp.MustCompile(`^[0-9]+ (managed and unchanged|checks passed)`)

var leadingStatus = regexp.MustCompile(`(?i)^(succeeded|verified|ok|pass|current|unchanged|managed|installed|updated|pending|unknown|unmanaged|blocked|failed|failure|fail|error|skipped|adopt|desired|dependency|pre-existing|install|remove|retire|repair|note|notice|warning|reboot required|log out and back in)([ :\t]|$)`)
var taskStatus = regexp.MustCompile(`^(\S+[ \t]+)(Previously verified|Unable to check|Verified|Pending|Blocked)([ \t]+)`)
var helpEntry = regexp.MustCompile(`^([a-z][a-z0-9-]*|(?:-[a-zA-Z], )?--[a-zA-Z][a-zA-Z-]*)([ \t]{2,}|[ \t]+(?:string|int)\b)`)

func statusColor(status string) string {
	switch strings.ToLower(status) {
	case "failed", "failure", "fail", "error", "blocked", "remove":
		return bad + bold
	case "pending", "unknown", "unable to check", "unmanaged", "note", "notice", "warning", "reboot required", "log out and back in":
		return warn + bold
	case "succeeded", "verified", "ok", "pass", "installed", "updated", "install":
		return good + bold
	case "previously verified", "adopt", "desired", "repair":
		return accent + bold
	default:
		return dim
	}
}

func highlight(line string, start bool) string {
	// Never nest colors around already styled output or terminal controls.
	if strings.ContainsAny(line, "\x1b\r") {
		return line
	}
	text := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(text)]
	if start {
		if match := bracketStatus.FindStringSubmatchIndex(text); match != nil {
			return indent + text[:match[0]] + paint(text[match[0]:match[1]], statusColor(text[match[2]:match[3]])) + commands(text[match[1]:])
		}
		if match := phaseHeading.FindStringIndex(text); match != nil {
			return indent + paint(text[:match[1]], accent+bold) + commands(text[match[1]:])
		}
		if strings.HasPrefix(text, "- ") {
			return indent + paint("-", accent+bold) + commands(text[1:])
		}
		if outcome.MatchString(text) || strings.HasPrefix(text, "✓ ") {
			return indent + paint(text, good+bold)
		}
		if strings.HasPrefix(text, "* ") {
			selected, rest, _ := strings.Cut(text, "  <- ")
			if rest != "" {
				return indent + paint(selected, accent+bold) + paint("  <- "+rest, dim)
			}
			return indent + paint(text, accent+bold)
		}
		if strings.HasPrefix(text, "selected by ") || strings.HasPrefix(text, "package ") || strings.HasPrefix(text, "profile ") || strings.HasPrefix(text, "component ") {
			return indent + paint(text, accent+bold)
		}
		if strings.HasPrefix(text, "+") || (strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "->") && !strings.HasPrefix(text, "--") && !helpEntry.MatchString(text)) {
			color := good
			if text[0] == '-' {
				color = bad
			}
			return indent + paint(text, color)
		}
		if match := taskStatus.FindStringSubmatchIndex(text); match != nil {
			return indent + text[:match[4]] + paint(text[match[4]:match[5]], statusColor(text[match[4]:match[5]])) + commands(text[match[5]:])
		}
		if match := leadingStatus.FindStringSubmatchIndex(text); match != nil {
			status := text[match[2]:match[3]]
			return indent + paint(status, statusColor(status)) + commands(text[match[3]:])
		}
		if strings.HasPrefix(text, "-> ") || strings.HasPrefix(text, "$ ") || strings.HasPrefix(text, "Proceed?") {
			return indent + paint(text, accent+bold)
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "Remaining setup:" {
			return indent + paint(text, warn+bold)
		}
		if trimmed == "Verification problems:" {
			return indent + paint(text, bad+bold)
		}
		if strings.HasSuffix(trimmed, ":") || strings.HasPrefix(text, "plan for ") || strings.HasPrefix(text, "Setup for ") {
			return indent + paint(text, accent+bold)
		}
		if match := helpEntry.FindStringSubmatchIndex(text); indent != "" && match != nil {
			return indent + paint(text[:match[3]], accent+bold) + text[match[3]:]
		}
		if i := strings.Index(text, ": "); i > 0 && i < 32 {
			return indent + paint(text[:i+1], bold) + commands(text[i+1:])
		}
	}
	return commands(line)
}

func commands(text string) string {
	if strings.HasPrefix(strings.TrimLeft(text, " \t"), "nimbus ") {
		return paint(text, accent+bold)
	}
	// Highlight the tool name while retaining explanatory prose in the normal
	// foreground. Native action lines above highlight their complete preview.
	var b strings.Builder
	for {
		before, after, found := strings.Cut(text, "nimbus ")
		if !found {
			b.WriteString(text)
			break
		}
		b.WriteString(before)
		if len(before) == 0 || strings.ContainsRune(" \t`", rune(before[len(before)-1])) {
			b.WriteString(paint("nimbus", accent+bold))
		} else {
			b.WriteString("nimbus")
		}
		b.WriteByte(' ')
		text = after
	}
	return counts.ReplaceAllStringFunc(b.String(), func(count string) string {
		number, label, _ := strings.Cut(count, " ")
		color := good + bold
		if number == "0" {
			color = dim
		} else if label == "failed" || label == "blocked" {
			color = bad + bold
		} else if label == "unknown" || label == "pending" || label == "available" {
			color = warn + bold
		}
		return paint(count, color)
	})
}

func paint(text, style string) string {
	// Keep newline bytes outside the style so each line leaves the terminal in
	// its normal state, including partial prompt writes.
	body := strings.TrimRight(text, "\n")
	return style + body + reset + text[len(body):]
}

// Wrapping is presentation-only and follows the current terminal size. Pipes
// keep their original line structure; JSON and native processes bypass us.
func terminalWidth(out io.Writer) int {
	file, ok := Native(out).(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width < 10 {
		return 0
	}
	return width
}

func wrapLine(line string, width int) string {
	if width == 0 || strings.ContainsAny(line, "\x1b\r") || ansi.StringWidth(strings.TrimSuffix(line, "\n")) <= width {
		return line
	}
	text := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(text)]
	// Keep wrapped bullets, table descriptions and prose visibly attached.
	continuation := indent + "  "
	if doctorRow.MatchString(line) || indent == "        " {
		continuation = "        "
	}
	if match := taskStatus.FindStringSubmatchIndex(text); match != nil && match[1]+len(indent) < width-20 {
		continuation = strings.Repeat(" ", len(indent)+match[1])
	}
	if len(continuation) > width/2 {
		continuation = "  "
	}
	wrapped := ansi.Wrap(strings.TrimSuffix(line, "\n"), width-len(continuation), "")
	return strings.ReplaceAll(wrapped, "\n", "\n"+continuation) + "\n"
}

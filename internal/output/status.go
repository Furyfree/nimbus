package output

import (
	"fmt"
	"io"
	"strings"
)

// StatusLine supplies the meaning of a result explicitly, rather than asking
// the terminal writer to infer it from application names or explanatory text.
// Logs and nonterminal writers receive ordinary text.
func StatusLine(out io.Writer, status, message string) error {
	return writeStatus(out, "  ", status, fmt.Sprintf("%s %s\n", strings.Repeat(" ", max(0, 10-len(status))), message))
}

// StatusRow keeps task IDs separate from their explicitly classified result.
func StatusRow(out io.Writer, name, status, detail string) error {
	return writeStatus(out, fmt.Sprintf("  %-20s ", name), status, fmt.Sprintf("%s %s\n", strings.Repeat(" ", max(0, 14-len(status))), detail))
}

func writeStatus(out io.Writer, prefix, status, suffix string) error {
	plain := prefix + status + suffix
	w, ok := out.(*colorWriter)
	if !ok {
		_, err := io.WriteString(out, plain)
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	line := wrapLine(plain, w.width())
	if w.enabled() && strings.HasPrefix(line, prefix+status) {
		label := prefix
		if strings.TrimSpace(prefix) != "" {
			label = paint(prefix, heading+bold)
		}
		line = label + paint(status, statusColor(status)) + strings.TrimPrefix(line, prefix+status)
	}
	n, err := io.WriteString(w.out, line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	w.continuation = false
	return err
}

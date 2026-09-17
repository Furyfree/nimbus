package output

import (
	"strings"
	"testing"
)

func TestSemanticStatusDoesNotInferFromTaskNameOrMessage(t *testing.T) {
	for _, color := range []bool{false, true} {
		var out strings.Builder
		w := ColorWriter(&out, func() bool { return color })
		if err := StatusRow(w, "failed-task", "Verified", "error is part of an application name"); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if color && (!strings.Contains(text, good+bold+"Verified") || strings.Contains(text, bad)) {
			t.Fatal(text)
		}
		if !color && strings.Contains(text, "\x1b") {
			t.Fatal(text)
		}
		if !strings.Contains(ansiPattern.ReplaceAllString(text, ""), "failed-task") {
			t.Fatal(text)
		}
	}
}
func TestSemanticStatusNarrowWrappingDoesNotDuplicateText(t *testing.T) {
	var out strings.Builder
	w := ColorWriter(&out, func() bool { return true }).(*colorWriter)
	w.width = func() int { return 20 }
	if err := StatusRow(w, "noctalia-lockscreen", "Previously verified", "Recheck requires sudo."); err != nil {
		t.Fatal(err)
	}
	plain := ansiPattern.ReplaceAllString(out.String(), "")
	if strings.Count(strings.ReplaceAll(plain, "\n  ", ""), "noctalia-lockscreen") > 1 || strings.Count(plain, "Recheck") != 1 {
		t.Fatal(plain)
	}
}

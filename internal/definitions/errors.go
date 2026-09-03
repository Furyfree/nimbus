package definitions

import (
	"fmt"
	"strings"
)

// Error is one validation problem with its checkout-relative source location.
type Error struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

// ErrorList collects every problem found instead of stopping at the first.
type ErrorList []Error

func (l ErrorList) Error() string {
	parts := make([]string, len(l))
	for i, e := range l {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

// Add appends a formatted error for path.
func (l *ErrorList) Add(path, format string, args ...any) {
	*l = append(*l, Error{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Err returns the list as an error, or nil when it is empty.
func (l ErrorList) Err() error {
	if len(l) == 0 {
		return nil
	}
	return l
}

package cli

import (
	"io"
	"path/filepath"

	"github.com/Furyfree/nimbus/internal/native"
)

// Inspection retains its private output while slow work stays observable.
// Native streaming commands bypass this wrapper and retain their own prompts.
type progressSource struct {
	native.Source
	out io.Writer
}

func (s progressSource) Run(name string, args ...string) (data []byte, err error) {
	err = native.Activity(s.out, "waiting for "+filepath.Base(name), func() error {
		var runErr error
		data, runErr = s.Source.Run(name, args...)
		return runErr
	})
	return data, err
}

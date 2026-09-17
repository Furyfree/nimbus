package postinstall

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
)

type mokProcessSource struct {
	native.Source
	helper string
}

func (s mokProcessSource) ReadFile(path string) ([]byte, error) {
	if path != MOKCertificate {
		return nil, errors.New("unexpected file")
	}
	return []byte("fixture certificate"), nil
}
func (s mokProcessSource) Run(name string, args ...string) ([]byte, error) {
	if name != "mokutil" || !slices.Equal(args, []string{"--ignore-keyring", "--test-key", MOKCertificate}) {
		return nil, errors.New("unexpected command")
	}
	return (native.ExecSource{}).Run(s.helper, args...)
}

func TestMOKProductionRunnerExitSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       Status
	}{
		{"enrolled", `printf '%s is already enrolled\n' "$3"; exit 1`, Complete},
		{"not-enrolled", `printf '%s is not enrolled\n' "$3"; exit 0`, Pending},
		{"pending", `printf '%s is already in the enrollment request\n' "$3"; exit 1`, Pending},
		{"failure", `echo 'EFI variable read failed' >&2; exit 255`, Unknown},
		{"stderr-not-enrollment", `printf '%s is already in the built-in trusted keyring\n' "$3" >&2; exit 255`, Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper := filepath.Join(t.TempDir(), "fake-mokutil")
			if err := os.WriteFile(helper, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			task := VerifyMOK(mokProcessSource{helper: helper}, Task{ID: "nvidia-mok"})
			if task.Status != tc.want {
				t.Fatalf("got %+v, want %s", task, tc.want)
			}
		})
	}
}

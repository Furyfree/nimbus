package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

// Test hooks. The real recorder runs the hidden privileged action through
// sudo; the real fetcher uses HTTP.
var (
	newFetcher = func() func(string) ([]byte, error) {
		client := &http.Client{Timeout: 5 * time.Minute}
		return func(url string) ([]byte, error) {
			resp, err := client.Get(url)
			if err != nil {
				return nil, err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("%s: HTTP %s", url, resp.Status)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		}
	}
	newRecorder = func(src native.Source, stage string) func(string, *state.Stage) error {
		return func(digest string, st *state.Stage) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			data, err := json.Marshal(st)
			if err != nil {
				return err
			}
			path := filepath.Join(stage, "receipts-"+strings.TrimPrefix(digest, "sha256:")[:12]+".json")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}
			defer func() { _ = os.Remove(path) }()
			_, err = src.Run("sudo", exe, "internal", "record", "--plan", digest, "--stage", path)
			return err
		}
	}
	// sudoKeepalive asks for the sudo password once, before the first
	// privileged command, and renews the credential while apply runs, so a
	// long transaction does not ask again. Tests replace it.
	sudoKeepalive = func(src native.Source, out, errOut io.Writer) (func(), error) {
		if _, err := src.Run("sudo", "-n", "-v"); err != nil {
			if _, err := fmt.Fprintln(out, "sudo is needed for the privileged commands; the password is asked once"); err != nil {
				return nil, err
			}
			if err := src.Stream(out, errOut, "sudo", "-v"); err != nil {
				return nil, err
			}
		}
		done := make(chan struct{})
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			ticks := time.Tick(time.Minute)
			for {
				select {
				case <-done:
					return
				case <-ticks:
					_, _ = src.Run("sudo", "-n", "-v")
				}
			}
		}()
		return func() { close(done); <-stopped }, nil
	}
	// approver reads the interactive answer. Tests replace it.
	approver = func(in io.Reader, out io.Writer, _ string) bool {
		if _, err := fmt.Fprint(out, "Proceed? [Y/n] "); err != nil {
			return false
		}
		reader := bufio.NewReader(in)
		line, err := reader.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || strings.TrimSpace(line) == "") {
			return false
		}
		answer := strings.TrimSpace(strings.ToLower(line))
		return (answer == "") || answer == "y" || answer == "yes"
	}
)

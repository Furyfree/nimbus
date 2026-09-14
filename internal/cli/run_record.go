package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"syscall"
	"time"

	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/Furyfree/nimbus/internal/version"
)

type runPhase struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

// runRecord stores only typed workflow metadata. Never add native output,
// error strings, arguments, environment, or rendered configuration here.
type runRecord struct {
	ProcessID    int        `json:"pid"`
	Schema       int        `json:"schema"`
	Engine       string     `json:"engine"`
	Command      string     `json:"command"`
	Machine      string     `json:"machine"`
	Commit       string     `json:"definitions_commit,omitempty"`
	Started      time.Time  `json:"started"`
	Finished     time.Time  `json:"finished,omitzero"`
	DurationMS   int64      `json:"duration_ms"`
	Outcome      string     `json:"outcome"`
	FailedPhase  string     `json:"failed_phase,omitempty"`
	Phases       []runPhase `json:"phases,omitempty"`
	Executed     int        `json:"executed"`
	Failures     int        `json:"failures"`
	Skipped      int        `json:"skipped"`
	PendingSetup int        `json:"pending_setup"`
	Reboot       bool       `json:"reboot_required,omitzero"`
	Logout       bool       `json:"logout_required,omitzero"`
	path         string
}

func beginRunRecord(command, machine string) (*runRecord, error) {
	store, err := userstate.Default()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(store.Dir, "runs")
	if err := makeLogParents(dir); err != nil {
		return nil, err
	}
	if err := privateLogPath(store.Dir, true); err != nil {
		return nil, err
	}
	if err := privateLogPath(dir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Only finished, private records in our own namespace are retention targets.
	var finished []string
	for _, entry := range entries {
		if !runRecordName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if privateLogPath(path, false) != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 64*1024 {
			continue
		}
		data, err := os.ReadFile(path)
		record, valid := decodeRunRecord(data)
		if err == nil && valid && (!record.Finished.IsZero() || errors.Is(syscall.Kill(record.ProcessID, 0), syscall.ESRCH)) {
			finished = append(finished, path)
		}
	}
	slices.Sort(finished)
	for _, path := range finished[:max(0, len(finished)-19)] {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	f, err := os.CreateTemp(dir, "run-"+time.Now().UTC().Format("20060102T150405.000000000Z")+"-*.json")
	if err != nil {
		return nil, err
	}
	r := &runRecord{ProcessID: os.Getpid(), Schema: 1, Engine: version.Engine, Command: command, Machine: machine, Started: time.Now().UTC(), Outcome: "running", path: f.Name()}
	err = json.NewEncoder(f).Encode(r)
	return r, errors.Join(err, f.Close())
}

func (r *runRecord) finish(result *syncResult, phase string, failed bool) error {
	r.Finished = time.Now().UTC()
	r.DurationMS = r.Finished.Sub(r.Started).Milliseconds()
	if r.Outcome != "restarted" {
		r.Outcome = "succeeded"
	}
	if failed {
		r.Outcome, r.FailedPhase = "failed", recordFailurePhase(result, phase)
	}
	r.Executed, r.Failures, r.PendingSetup = len(result.Executed), len(result.Failures), len(result.Tasks)
	if failed && r.Failures == 0 {
		r.Failures = 1
	}
	r.Reboot, r.Logout = result.Reboot, result.Logout
	for _, step := range result.Steps {
		if step.Status == "skipped" {
			r.Skipped++
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("run record exceeds 64 KiB")
	}
	if err := privateLogPath(r.path, false); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.path), ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(data, '\n'))
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), r.path)
}

// A diagnostic-write error must not disappear behind the already-reported
// failure marker or a delegated child's exit status.
func finishRunRecord(out io.Writer, r *runRecord, result *syncResult, phase string, runErr error) error {
	err := r.finish(result, phase, runErr != nil)
	if err == nil {
		return runErr
	}
	err = fmt.Errorf("finish run record: %w", err)
	if errors.Is(runErr, reported{}) || isNativeExit(runErr) {
		_, printErr := fmt.Fprintln(out, "error:", err)
		return errors.Join(runErr, err, printErr)
	}
	return errors.Join(runErr, err)
}

var runRecordName = regexp.MustCompile(`^run-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9]+\.json$`)

func decodeRunRecord(data []byte) (runRecord, bool) {
	var r runRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF {
		return r, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return r, false
	}
	for _, key := range []string{"pid", "schema", "engine", "command", "machine", "started", "duration_ms", "outcome", "executed", "failures", "skipped", "pending_setup"} {
		if value, ok := fields[key]; !ok || bytes.Equal(value, []byte("null")) {
			return r, false
		}
	}
	if r.Schema != 1 || r.ProcessID <= 0 || r.Engine == "" || r.Started.IsZero() || r.DurationMS < 0 || r.Executed < 0 || r.Failures < 0 || r.Skipped < 0 || r.PendingSetup < 0 {
		return r, false
	}
	if !slices.Contains([]string{"sync", "sync --upgrade", "upgrade", "upgrade --system", "init"}, r.Command) {
		return r, false
	}
	switch r.Outcome {
	case "running":
		if !r.Finished.IsZero() {
			return r, false
		}
	case "succeeded", "failed", "restarted":
		if r.Finished.IsZero() || r.Finished.Before(r.Started) {
			return r, false
		}
	default:
		return r, false
	}
	for _, phase := range r.Phases {
		if phase.Name == "" || phase.DurationMS < 0 {
			return r, false
		}
	}
	return r, true
}

// Only fixed workflow names enter diagnostic metadata; task IDs, file paths
// and native error text remain outside the record.
func recordFailurePhase(result *syncResult, fallback string) string {
	for _, step := range result.Steps {
		if step.Status != "failed" {
			continue
		}
		switch step.Name {
		case "selection", "system installation", "dotfiles and tools":
			return step.Name
		}
	}
	if result.Failed != "" {
		switch result.Failed {
		case "snapper pre", "snapper post", "snapper cleanup", "snapper":
			return "snapper"
		default:
			return fallback
		}
	}
	for _, step := range result.Steps {
		if step.Status != "failed" {
			continue
		}
		switch step.Name {
		case "snapper pre", "snapper post", "snapper cleanup", "snapper":
			return "snapper"
		default:
			return fallback
		}
	}
	if len(result.Failures) > 0 {
		switch result.Failures[0].ID {
		case "snapper pre", "snapper post", "snapper cleanup":
			return "snapper"
		}
	}
	return fallback
}

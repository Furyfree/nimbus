// Package state is the applied state under /var/lib/nimbus: receipts for
// every verified operation, the baseline of packages that existed before
// Nimbus took over, and a journal. The normal user reads it; only the
// privileged record action writes it, and only data bound to the digest of
// the plan that produced it.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Schema is the state schema this engine reads and writes.
const Schema = 1

// Root is the state directory.
const Root = "/var/lib/nimbus"

// Layout below Root.
const (
	SchemaFile   = "schema"
	BaselineFile = "baseline.json"
	ReceiptsDir  = "receipts"
	JournalFile  = "journal.jsonl"
	StageDir     = "stage"
)

// Receipt records one verified operation on one resource. The receipt of a
// resource is replaced by the next operation on it and deleted by its
// removal; the journal keeps every entry.
type Receipt struct {
	Schema        int         `json:"schema"`
	Engine        string      `json:"engine"`
	Definitions   Definitions `json:"definitions"`
	Machine       string      `json:"machine"`
	Resource      string      `json:"resource"` // operation ID, such as package:dnf:ripgrep
	Provider      string      `json:"provider"` // dnf, flatpak, repository, flatpak-remote
	Paths         []string    `json:"paths,omitempty"`
	Previous      string      `json:"previous"`
	Intended      string      `json:"intended"`
	Operation     string      `json:"operation"` // install, adopt, remove, enable, repair
	PlanDigest    string      `json:"plan_digest"`
	Verified      bool        `json:"verified"`
	Verification  string      `json:"verification"`
	RecoveryPoint string      `json:"recovery_point,omitempty"`
	Timestamp     time.Time   `json:"timestamp"`
	Reboot        bool        `json:"reboot,omitempty"`
	Logout        bool        `json:"logout,omitempty"`
}

// Definitions identifies the checkout a receipt came from.
type Definitions struct {
	Origin string `json:"origin"`
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
	Digest string `json:"digest"`
}

// Baseline is the set of packages installed before Nimbus first applied
// anything. They are never prune candidates.
type Baseline struct {
	Schema   int       `json:"schema"`
	Recorded time.Time `json:"recorded"`
	Packages []string  `json:"packages"`
}

// Journal entries are receipts plus removals, appended in order.
type JournalEntry struct {
	Action   string    `json:"action"` // recorded, removed
	Receipt  *Receipt  `json:"receipt,omitempty"`
	Resource string    `json:"resource,omitempty"`
	Time     time.Time `json:"time"`
}

// Applied is what the normal user reads back.
type Applied struct {
	Present  bool
	Baseline *Baseline
	Receipts map[string]Receipt // by resource
}

// FileName maps a resource ID to its receipt file.
func FileName(resource string) string {
	r := strings.NewReplacer(":", "_", "/", "_", " ", "_")
	return r.Replace(resource) + ".json"
}

// Read loads the applied state below root. A missing directory means Nimbus
// has not applied anything yet; that is not an error.
func Read(root string) (*Applied, error) {
	a := &Applied{Receipts: map[string]Receipt{}}
	schemaData, err := os.ReadFile(filepath.Join(root, SchemaFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return a, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	if strings.TrimSpace(string(schemaData)) != fmt.Sprint(Schema) {
		return nil, fmt.Errorf("state schema %q is not supported; this engine reads %d", strings.TrimSpace(string(schemaData)), Schema)
	}
	a.Present = true
	if data, err := os.ReadFile(filepath.Join(root, BaselineFile)); err == nil {
		var b Baseline
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, fmt.Errorf("baseline: %w", err)
		}
		if b.Schema != Schema {
			return nil, fmt.Errorf("baseline schema %d is not supported", b.Schema)
		}
		a.Baseline = &b
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ReceiptsDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read receipts: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, ReceiptsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		var r Receipt
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("receipt %s: %w", e.Name(), err)
		}
		if r.Schema != Schema {
			return nil, fmt.Errorf("receipt %s: schema %d is not supported", e.Name(), r.Schema)
		}
		a.Receipts[r.Resource] = r
	}
	return a, nil
}

// InBaseline reports whether a package existed before Nimbus took over.
func (a *Applied) InBaseline(name string) bool {
	if a == nil || a.Baseline == nil {
		return false
	}
	i := sort.SearchStrings(a.Baseline.Packages, name)
	return i < len(a.Baseline.Packages) && a.Baseline.Packages[i] == name
}

// Stage is the data one record action writes. It is produced by the normal
// user, bound to the approved plan digest, and consumed by the privileged
// action, which refuses anything else.
type Stage struct {
	Schema     int       `json:"schema"`
	PlanDigest string    `json:"plan_digest"`
	Baseline   *Baseline `json:"baseline,omitempty"` // written only when none exists
	Receipts   []Receipt `json:"receipts,omitempty"`
	Remove     []string  `json:"remove,omitempty"` // resources whose receipts are deleted
	Time       time.Time `json:"time"`
}

// Record is the privileged action: it validates the stage against the plan
// digest and writes it atomically below root. It creates root with mode
// 0755 and files with 0644 so the normal user can read state back; state
// contains no secrets.
func Record(root, planDigest string, st *Stage) error {
	switch {
	case st.Schema != Schema:
		return fmt.Errorf("stage schema %d is not supported", st.Schema)
	case planDigest == "" || st.PlanDigest != planDigest:
		return fmt.Errorf("stage is bound to plan %q, not %q", st.PlanDigest, planDigest)
	}
	for _, r := range st.Receipts {
		if r.PlanDigest != planDigest {
			return fmt.Errorf("receipt %s is bound to plan %q", r.Resource, r.PlanDigest)
		}
		if !r.Verified {
			return fmt.Errorf("receipt %s is not verified; a failed operation never gets a receipt", r.Resource)
		}
		if r.Schema != Schema || r.Resource == "" || r.Operation == "" {
			return fmt.Errorf("receipt %s is incomplete", r.Resource)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ReceiptsDir), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(root, SchemaFile), []byte(fmt.Sprintln(Schema)), 0o644); err != nil {
		return err
	}
	if st.Baseline != nil {
		if _, err := os.Stat(filepath.Join(root, BaselineFile)); errors.Is(err, fs.ErrNotExist) {
			b := *st.Baseline
			b.Schema = Schema
			sort.Strings(b.Packages)
			data, _ := json.MarshalIndent(b, "", "  ")
			if err := writeAtomic(filepath.Join(root, BaselineFile), data, 0o644); err != nil {
				return err
			}
		}
	}
	journal, err := os.OpenFile(filepath.Join(root, JournalFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer journal.Close()
	enc := json.NewEncoder(journal)
	for _, r := range st.Receipts {
		data, _ := json.MarshalIndent(r, "", "  ")
		if err := writeAtomic(filepath.Join(root, ReceiptsDir, FileName(r.Resource)), data, 0o644); err != nil {
			return err
		}
		if err := enc.Encode(JournalEntry{Action: "recorded", Receipt: &r, Time: st.Time}); err != nil {
			return err
		}
	}
	for _, resource := range st.Remove {
		path := filepath.Join(root, ReceiptsDir, FileName(resource))
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := enc.Encode(JournalEntry{Action: "removed", Resource: resource, Time: st.Time}); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nimbus-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

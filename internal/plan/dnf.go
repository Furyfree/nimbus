// Package plan compares desired configuration with observed facts and
// produces the complete, non-mutating plan that apply later executes. It
// renders exact native commands and never runs a mutating one itself.
package plan

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// TxPackage is one row of a DNF transaction preview.
type TxPackage struct {
	Name       string `json:"name"`
	Arch       string `json:"arch"`
	EVR        string `json:"evr"`
	Repository string `json:"repository"`
	// Section is the preview heading the row came from, lowercased, such
	// as "installing", "installing dependencies", or "removing".
	Section string `json:"section"`
}

// Transaction is a parsed DNF5 preview.
type Transaction struct {
	Packages []TxPackage `json:"packages"`
	// Skipped holds packages DNF leaves unchanged because their update
	// conflicts or has broken dependencies. They are not part of the change.
	Skipped []TxPackage `json:"skipped,omitempty"`
	// NothingToDo is set when DNF reported nothing to change.
	NothingToDo bool `json:"nothing_to_do,omitzero"`
	// Download is DNF's own estimate of the inbound size, such as "3 GiB",
	// when the preview printed one.
	Download string `json:"download,omitempty"`
}

// Rows returns the packages of one section.
func (t *Transaction) Rows(section string) []TxPackage {
	var rows []TxPackage
	for _, p := range t.Packages {
		if p.Section == section {
			rows = append(rows, p)
		}
	}
	return rows
}

// Empty reports a preview that lists no package at all.
func (t *Transaction) Empty() bool {
	return len(t.Packages) == 0 && len(t.Skipped) == 0
}

// ResolveError is DNF's own explanation of an unresolvable transaction.
type ResolveError struct {
	Problems []string
}

func (e *ResolveError) Error() string {
	return "dnf5 could not resolve the transaction: " + strings.Join(e.Problems, "; ")
}

// Sections DNF5 prints in a preview. Any other heading is an error so a new
// DNF behavior is never silently accepted.
var knownSections = map[string]bool{
	"installing":                           true,
	"installing dependencies":              true,
	"installing weak dependencies":         true,
	"upgrading":                            true,
	"downgrading":                          true,
	"reinstalling":                         true,
	"removing":                             true,
	"removing dependent packages":          true,
	"removing unused dependencies":         true,
	skippingPrefix + "conflicts":           true,
	skippingPrefix + "broken dependencies": true,
}

// DNF5 upgrades print packages it cannot update under these headings.
const skippingPrefix = "skipping packages with "

// SectionReplaced holds the packages an upgrade or an obsoleting install
// replaces. DNF5 prints them indented below the new package as
// "replacing NAME ARCH EVR REPO SIZE", not as a section of their own.
const SectionReplaced = "replaced"

// ParsePreview reads the output of dnf5 --assumeno <install|remove> ...,
// which prints the resolved transaction table and then aborts.
func ParsePreview(out []byte) (*Transaction, error) {
	tx := &Transaction{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	section := ""
	inTable := false
	var problems []string
	collecting := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case collecting:
			if trimmed == "" || strings.HasPrefix(trimmed, "You can try") {
				collecting = false
				continue
			}
			problems = append(problems, trimmed)
			continue
		case strings.HasPrefix(trimmed, "Failed to resolve the transaction"):
			collecting = true
			continue
		case trimmed == "Nothing to do.":
			tx.NothingToDo = true
			continue
		case strings.HasPrefix(trimmed, "Total size of inbound packages is "):
			tx.Download = DownloadSize(trimmed)
			continue
		case strings.HasPrefix(trimmed, "Package ") && strings.HasSuffix(trimmed, "Arch   Version") || strings.HasPrefix(line, "Package") && strings.Contains(line, "Repository"):
			inTable = true
			continue
		case strings.HasPrefix(trimmed, "Transaction Summary"):
			inTable = false
			continue
		}
		if !inTable {
			continue
		}
		if heading, ok := strings.CutSuffix(trimmed, ":"); ok && !strings.HasPrefix(line, " ") {
			section = strings.ToLower(heading)
			if !knownSections[section] {
				return nil, fmt.Errorf("unknown preview section %q", trimmed)
			}
			continue
		}
		if !strings.HasPrefix(line, " ") || trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 {
			return nil, fmt.Errorf("unexpected preview row %q", trimmed)
		}
		if section == "" {
			return nil, fmt.Errorf("preview row before any section: %q", trimmed)
		}
		if fields[0] == "replacing" {
			if len(fields) < 5 {
				return nil, fmt.Errorf("unexpected preview row %q", trimmed)
			}
			tx.Packages = append(tx.Packages, TxPackage{Name: fields[1], Arch: fields[2], EVR: fields[3], Repository: fields[4], Section: SectionReplaced})
			continue
		}
		row := TxPackage{Name: fields[0], Arch: fields[1], EVR: fields[2], Repository: fields[3], Section: section}
		if strings.HasPrefix(section, skippingPrefix) {
			tx.Skipped = append(tx.Skipped, row)
			continue
		}
		tx.Packages = append(tx.Packages, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, &ResolveError{Problems: problems}
	}
	if !tx.NothingToDo && tx.Empty() {
		return nil, fmt.Errorf("no transaction table in dnf5 output")
	}
	return tx, nil
}

// SkipReason names why DNF skipped a row, such as "broken dependencies".
func SkipReason(row TxPackage) string {
	return strings.TrimPrefix(row.Section, skippingPrefix)
}

// DownloadSize extracts DNF's inbound size from the line that states it,
// "Total size of inbound packages is 3 GiB. Need to download 3 GiB.", which
// DNF prints on stderr and so may reach the planner outside the table.
func DownloadSize(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		size, ok := strings.CutPrefix(line, "Total size of inbound packages is ")
		if !ok {
			continue
		}
		if before, _, ok := strings.Cut(size, ". "); ok && before != "" {
			size = before
		}
		return strings.TrimSuffix(size, ".")
	}
	return ""
}

// Upgrade is one row of dnf5 check-upgrade.
type Upgrade struct {
	Name       string `json:"name"`
	Arch       string `json:"arch"`
	EVR        string `json:"evr"`
	Repository string `json:"repository"`
}

// ParseCheckUpgrade reads dnf5 check-upgrade output: an "Upgrades" heading
// followed by "name.arch evr repository" rows.
func ParseCheckUpgrade(out []byte) ([]Upgrade, error) {
	var ups []Upgrade
	scanner := bufio.NewScanner(bytes.NewReader(out))
	inList := false
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "Upgrades" {
			inList = true
			continue
		}
		if !inList || trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected check-upgrade row %q", trimmed)
		}
		name, arch, ok := strings.Cut(fields[0], ".")
		if i := strings.LastIndexByte(fields[0], '.'); i > 0 {
			name, arch, ok = fields[0][:i], fields[0][i+1:], true
		}
		if !ok {
			return nil, fmt.Errorf("unexpected package spec %q", fields[0])
		}
		ups = append(ups, Upgrade{Name: name, Arch: arch, EVR: fields[1], Repository: fields[2]})
	}
	return ups, scanner.Err()
}

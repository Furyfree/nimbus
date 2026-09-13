package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/spf13/cobra"
)

type setupNote struct {
	RequiresDotfiles bool     `json:"requires_dotfiles,omitempty"`
	ID               string   `json:"id"`
	Revision         int      `json:"revision"`
	Text             string   `json:"text"`
	Profiles         []string `json:"profiles,omitempty"`
}
type noteCatalog struct {
	Schema int         `json:"schema"`
	Notes  []setupNote `json:"notes"`
}

func setupNotes(src native.Source, s *selected) ([]setupNote, error) {
	data, err := src.ReadFile(filepath.Join(s.Root, "setup-notes.json"))
	if err != nil {
		return nil, fmt.Errorf("read Nimbus setup notes: %w", err)
	}
	var catalog noteCatalog
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&catalog); err != nil {
		return nil, fmt.Errorf("invalid setup-note catalog: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing setup-note data")
	}
	if catalog.Schema != 1 {
		return nil, errors.New("unsupported setup-note schema")
	}
	var notes []setupNote
	ids := map[string]bool{}
	for _, n := range catalog.Notes {
		if (!strings.HasPrefix(n.ID, "dotfiles.") && !strings.HasPrefix(n.ID, "nimbus.")) || ids[n.ID] || n.Revision < 1 || strings.TrimSpace(n.Text) == "" || strings.ContainsFunc(n.Text, unicode.IsControl) {
			return nil, errors.New("invalid or duplicate setup note")
		}
		ids[n.ID] = true
		if n.RequiresDotfiles && s.Checkout.Machines[s.Resolved.Machine].Dotfiles == nil {
			continue
		}
		if len(n.Profiles) == 0 || slices.ContainsFunc(n.Profiles, func(p string) bool { return slices.Contains(s.Resolved.Profiles, p) }) {
			notes = append(notes, n)
		}
	}
	return notes, nil
}
func newSetupNotes(opts *options) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{Use: "setup-notes", Short: "Show all applicable setup guidance without changing state", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := loadSelected(flags)
		if err != nil {
			return err
		}
		notes, err := setupNotes(newSource(), s)
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), notes, nil)
		}
		return displayNotes(cmd.OutOrStdout(), notes)
	}}
	addMachineFlags(&flags, cmd.Flags())
	return cmd
}
func displayNotes(out io.Writer, notes []setupNote) error {
	if len(notes) == 0 {
		return nil
	}
	var b bytes.Buffer
	fmt.Fprintln(&b, "\nSetup notes:")
	for _, n := range notes {
		fmt.Fprintf(&b, "  - %s\n", n.Text)
	}
	fmt.Fprintln(&b, "View all guidance: nimbus setup-notes")
	_, err := b.WriteTo(out)
	return err
}
func pendingNotes(src native.Source, s *selected, all bool) ([]setupNote, error) {
	notes, err := setupNotes(src, s)
	if err != nil {
		return nil, err
	}
	store, err := userstate.Default()
	if err != nil {
		return nil, err
	}
	state, err := store.Read("setup-notes")
	if err != nil {
		return nil, err
	}
	if !all {
		notes = slices.DeleteFunc(notes, func(n setupNote) bool { return state.Has(s.Resolved.Machine, n.ID, n.Revision, "displayed") })
	}
	return notes, nil
}
func rememberNotes(machine string, notes []setupNote) error {
	if len(notes) == 0 {
		return nil
	}
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	changes := map[string]userstate.Evidence{}
	for _, n := range notes {
		changes[n.ID] = userstate.Evidence{Revision: n.Revision, Source: "displayed"}
	}
	return store.Update("setup-notes", machine, changes, nil)
}

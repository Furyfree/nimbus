package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/doctor"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/selector"
)

// chezmoiHandoff initializes a missing source, then applies its local state.
// Chezmoi owns conflict handling and secrets; Nimbus never forces overwrites.
func chezmoiHandoff(src native.Source, out io.Writer, machine string, profiles []string, dotfiles *definitions.Dotfiles, onePasswordSSH bool) error {
	if dotfiles == nil {
		_, err := fmt.Fprintln(out, "no dotfiles repository is declared; dotfiles skipped")
		return err
	}
	wantOrigin, err := selector.NormalizeOrigin(dotfiles.Repo)
	if err != nil {
		return fmt.Errorf("dotfiles repository must be an explicit Git URL: %w", err)
	}
	if _, err := src.LookPath("chezmoi"); err != nil {
		return errors.New("chezmoi is not installed yet; run nimbus init again after fixing the system installation")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	initialized, err := inspect.ChezmoiInitialized(src, home)
	if err != nil {
		return fmt.Errorf("inspect Chezmoi source before handoff: %w", err)
	}
	flags := []string{"--promptString", "Machine=" + machine, "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=" + strings.Join(profiles, "/")}
	if !initialized {
		flags = append(flags, "--promptBool", fmt.Sprintf("Enable 1Password SSH integration=%t", onePasswordSSH))
		argv := append([]string{"init"}, append(flags, "--", dotfiles.Repo)...)
		if _, err := fmt.Fprintf(out, "-> initialize Chezmoi from %s\n   $ chezmoi %s\n", dotfiles.Repo, strings.Join(argv, " ")); err != nil {
			return err
		}
		if err := src.Stream(out, out, "chezmoi", argv...); err != nil {
			return fmt.Errorf("chezmoi init: %w", err)
		}
	} else {
		if _, err := fmt.Fprintln(out, "Chezmoi is already initialized; applying its existing local source"); err != nil {
			return err
		}
	}
	sourcePath, err := src.Run("chezmoi", "source-path")
	if err != nil {
		return fmt.Errorf("read Chezmoi source path: %w", err)
	}
	root := strings.TrimSpace(string(sourcePath))
	if !filepath.IsAbs(root) {
		return errors.New("chezmoi returned no absolute source path")
	}
	origin, err := src.Run("git", inspect.GitArgs(root, "config", "--get", "remote.origin.url")...)
	if err != nil {
		return fmt.Errorf("read Chezmoi source origin: %w", err)
	}
	actualOrigin, err := selector.NormalizeOrigin(strings.TrimSpace(string(origin)))
	if err != nil || actualOrigin != wantOrigin {
		return errors.New("the existing Chezmoi source does not match the declared dotfiles repository; inspect it before retrying")
	}
	if logged, ok := src.(installSource); ok {
		logged.log.checkoutIdentity(logged.Source, "dotfiles", root, actualOrigin)
	}
	data, err := src.Run("chezmoi", inspect.ChezmoiDataArgs...)
	if err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	selection, err := inspect.ParseChezmoiData(data)
	if err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	if selection.Machine != machine || !selection.ManagedByNimbus || !slices.Equal(slices.Sorted(slices.Values(selection.Profiles)), slices.Sorted(slices.Values(profiles))) {
		return fmt.Errorf("chezmoi's stored selection differs; refresh it before retrying: %s", doctor.ChezmoiRefresh(machine, profiles, selection.OnePasswordSSH))
	}
	if onePasswordSSH && !selection.OnePasswordSSH {
		return fmt.Errorf("the existing Chezmoi configuration has 1Password SSH disabled; enable it explicitly with: %s", doctor.ChezmoiRefresh(machine, profiles, true))
	}
	if _, err := fmt.Fprintf(out, "Setup note: To change your Chezmoi answers, run: %s\n", doctor.ChezmoiRefresh(machine, profiles, selection.OnePasswordSSH)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "-> apply user configuration and install its declared tools\n   $ chezmoi apply"); err != nil {
		return err
	}
	if err := src.Stream(out, out, "chezmoi", "apply"); err != nil {
		return fmt.Errorf("chezmoi apply: %w", err)
	}
	return nil
}

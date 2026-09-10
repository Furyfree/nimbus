package apply

import (
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/plan"
)

// userTool runs a user-scope step as the user, with its output visible: a
// maker's installer fetched to the stage directory and shown by digest.
// Presence afterwards is the check; nothing is recorded.
func (ex *executor) userTool(op plan.Operation) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, st := range op.Steps {
		if st.Argv == nil {
			continue
		}
		argv := slices.Clone(st.Argv)
		for i, a := range argv {
			if a == plan.InstallerScript {
				path, err := ex.fetchInstaller(op)
				if err != nil {
					return err
				}
				argv[i] = path
			}
		}
		if _, err := fmt.Fprintf(ex.opts.Out, "   $ %s\n", strings.Join(argv, " ")); err != nil {
			return fmt.Errorf("write command progress: %w", err)
		}
		errOut := cmp.Or(ex.opts.ErrOut, ex.opts.Out)
		if err := ex.opts.Source.Stream(ex.opts.Out, errOut, argv[0], argv[1:]...); err != nil {
			return err
		}
	}
	return ex.verifyUserTool(op, home)
}

// fetchInstaller downloads the installer named in the operation's first
// step, keeps it below the stage directory, and prints its digest.
func (ex *executor) fetchInstaller(op plan.Operation) (string, error) {
	url := ""
	for _, st := range op.Steps {
		if u, ok := strings.CutPrefix(st.Description, "download "); ok {
			url, _, _ = strings.Cut(u, " ")
		}
	}
	if url == "" {
		return "", errors.New("the installer step names no URL")
	}
	data, err := ex.opts.Fetch(url)
	if err != nil {
		return "", fmt.Errorf("download installer: %w", err)
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(ex.opts.Stage, "installer-"+strings.TrimPrefix(op.ID, "user:")+".sh")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(ex.opts.Out, "   downloaded %s, sha256 %x\n", url, sha256.Sum256(data)); err != nil {
		return "", fmt.Errorf("show installer digest: %w", err)
	}
	return path, nil
}

// verifyUserTool checks that the installer left its declared binary.
func (ex *executor) verifyUserTool(op plan.Operation, home string) error {
	for _, st := range op.Steps {
		if rel, ok := strings.CutPrefix(st.Description, "verify ~/"); ok {
			rel = strings.TrimSuffix(rel, " exists")
			names, err := ex.opts.Source.ReadDir(filepath.Join(home, filepath.Dir(rel)))
			if err != nil {
				return fmt.Errorf("verification: inspect ~/%s after the installer ran: %w", rel, err)
			}
			if !slices.Contains(names, filepath.Base(rel)) {
				return fmt.Errorf("verification: ~/%s does not exist after the installer ran", rel)
			}
		}
	}
	return nil
}

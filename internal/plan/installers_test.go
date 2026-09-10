package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestMiseBootstrapLeavesConfiguredToolsToChezmoi(t *testing.T) {
	c, _ := repository(t)
	// The global dotfiles apply must also work without development selected.
	c.Machines["common-only"] = &definitions.Machine{Schema: 1, ID: "common-only", Profiles: []string{"common"}}
	r, errs := definitions.Resolve(c, "common-only")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	src, f := readyHost(t, c)
	home := "/home/tester"
	f.User = inspect.Section[inspect.User]{Value: inspect.User{Home: home}}
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// A fresh home: Nimbus installs Mise itself. Chezmoi owns its tools.
	p := answerInstall(t, src, in, nil)
	if op := find(p, "user:mise"); op == nil || op.Action != ActionInstall || op.Steps[1].Argv[0] != "sh" || op.Steps[1].Argv[1] != InstallerScript || op.Steps[1].Privileged {
		t.Fatalf("mise installer = %+v", op)
	}
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not install the Chezmoi-owned tools: %+v", op)
	}
	// Mise present, config not written yet: no runtime operation is planned.
	src.Dirs[home+"/.local/bin"] = []string{"mise"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:mise"); op == nil || op.Action != ActionKeep {
		t.Fatalf("mise present = %+v", op)
	}
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not wait for the Mise config: %+v", op)
	}
	// Even with applied configuration and missing Rust, sync leaves the
	// tool installation to Chezmoi.
	src.Dirs[home+"/.config/mise"] = []string{"config.toml"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:mise:install"); op != nil {
		t.Fatalf("Nimbus must not duplicate the Chezmoi install script: %+v", op)
	}
	for _, op := range p.Operations {

		if op.Kind == KindUser && slices.ContainsFunc(op.Steps, func(st Step) bool { return st.Privileged }) {
			t.Fatalf("user-scope step marked privileged: %+v", op)
		}
	}
	// Unknown user-scope state blocks instead of dropping the tools.
	f.User = inspect.Section[inspect.User]{Error: "home inspection: broken"}
	p = answerInstall(t, src, in, nil)
	if op := find(p, "user:tools"); op == nil || op.Blocked == "" || !strings.Contains(op.Blocked, "broken") || p.Complete {
		t.Fatalf("unknown user state = %+v complete %v", op, p.Complete)
	}
	if find(p, "user:mise") != nil {
		t.Fatal("user tools planned without their facts")
	}
}

type userDirectorySource struct {
	*nativetest.FakeSource
	readErrors map[string]error
}

func (s userDirectorySource) ReadDir(path string) ([]string, error) {
	if err := s.readErrors[path]; err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return s.FakeSource.ReadDir(path)
}

func TestInstallerPlanningDistinguishesMissingAndUnreadableFiles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		binaryDir  []string
		binaryErr  error
		wantAction string
	}{
		{name: "missing binary directory", wantAction: ActionInstall},
		{name: "missing binary", binaryDir: []string{}, wantAction: ActionInstall},
		{name: "unreadable binary directory", binaryErr: os.ErrPermission, wantAction: ActionInstall},
		{name: "binary inspection failure", binaryErr: errors.New("filesystem unavailable"), wantAction: ActionInstall},
		{name: "ready", binaryDir: []string{"mise"}, wantAction: ActionKeep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			binaryDir := filepath.Join(home, ".local/bin")
			src := userDirectorySource{FakeSource: &nativetest.FakeSource{Dirs: map[string][]string{}, Commands: map[string][]byte{
				nativetest.Key("dnf5", "--cacheonly", "check-upgrade"): nil,
			}}, readErrors: map[string]error{binaryDir: tc.binaryErr}}
			if tc.binaryDir != nil {
				src.Dirs[binaryDir] = tc.binaryDir
			}
			resolved := &definitions.Resolved{Machine: "vm", Installers: []definitions.ResolvedInstaller{{Component: "mise", Installer: definitions.Installer{
				URL: "https://mise.run", Binary: ".local/bin/mise",
			}}}}
			p, err := Build(Inputs{Resolved: resolved, Facts: &inspect.Facts{User: inspect.Section[inspect.User]{Value: inspect.User{Home: home}}}, Source: src})
			if err != nil {
				t.Fatal(err)
			}
			if p.Complete != (tc.binaryErr == nil) {
				t.Fatalf("plan completeness does not reflect inspection failure: %+v", p)
			}
			op := find(p, "user:mise")
			if op == nil || op.Action != tc.wantAction {
				t.Fatalf("installer plan changed: %+v", op)
			}
			if tc.binaryErr != nil && (!strings.Contains(op.Blocked, ".local/bin/mise") || !strings.Contains(op.Blocked, tc.binaryErr.Error())) {
				t.Fatalf("inspection failure lost path or cause: %+v", op)
			}
		})
	}
}

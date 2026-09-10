package plan

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestPlanNotesWhatGoesBeyondTheDefinitions(t *testing.T) {
	c, r := repository(t)
	cases := []struct {
		name   string
		mutate func([]TxPackage) []TxPackage
		want   string
	}{
		{"undeclared install", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "surprise", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing"})
		}, "would install surprise"},
		{"undeclared removal", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "bzip2", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"})
		}, "would remove bzip2"},
		{"wrong repository", func(rows []TxPackage) []TxPackage {
			rows[0].Repository = "terra"
			return rows
		}, "would come from repository terra"},
		{"requested dependency from wrong repository", func(rows []TxPackage) []TxPackage {
			rows[0].Repository = "terra"
			rows[0].Section = "installing dependencies"
			return rows
		}, "would come from repository terra"},
		{"requested weak dependency from wrong repository", func(rows []TxPackage) []TxPackage {
			rows[0].Repository = "terra"
			rows[0].Section = "installing weak dependencies"
			return rows
		}, "would come from repository terra"},
		{"upgrade smuggled in", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "bash", Arch: "x86_64", EVR: "0:6-1", Repository: "updates", Section: "downgrading"})
		}, "would downgrade bash"},
		{"obsoleting an undeclared package", func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "old-tool", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: SectionReplaced})
		}, "would replace old-tool, which no component declares in removes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, f := readyHost(t, c)
			p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, tc.mutate)
			// DNF's resolution runs as it is; what goes beyond the
			// definitions is shown for review, not refused.
			op := find(p, "packages:install")
			if op.Blocked != "" || !strings.Contains(strings.Join(op.Notes, "\n"), tc.want) {
				t.Fatalf("blocked %q notes %v", op.Blocked, op.Notes)
			}
		})
	}
	t.Run("dependencies are allowed", func(t *testing.T) {
		src, f := readyHost(t, c)
		p := answerInstall(t, src, Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}, func(rows []TxPackage) []TxPackage {
			return append(rows, TxPackage{Name: "libfoo", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing dependencies"})
		})
		if op := find(p, "packages:install"); op.Blocked != "" || len(op.Notes) != 0 {
			t.Fatalf("dependency noted: %s %v", op.Blocked, op.Notes)
		}
	})
	t.Run("preview failure blocks with the reason", func(t *testing.T) {
		src, f := readyHost(t, c)
		p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
		if err != nil {
			t.Fatal(err)
		}
		if op := find(p, "packages:install"); op.Blocked == "" || !strings.Contains(op.Blocked, "not recorded") {
			t.Fatalf("unrecorded preview = %+v", op)
		}
	})
}

func TestAdoptionTakesWhatIsInstalled(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	// Whatever source a desired package came from, it is installed and
	// desired: Nimbus adopts it and records the source in the receipt.
	f.Packages.Value = append(f.Packages.Value,
		inspect.Package{Name: "ripgrep", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		inspect.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "terra", Reason: "user"},
		inspect.Package{Name: "git", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "anaconda", Reason: "user"},
	)
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	for _, id := range []string{"package:dnf:ripgrep", "package:terra:ghostty", "package:dnf:git"} {
		if op := find(p, id); op == nil || op.Blocked != "" || op.Action != ActionAdopt {
			t.Fatalf("%s = %+v", id, op)
		}
	}
}

func TestPreviewFailureReportsTheResolutionProblem(t *testing.T) {
	stderr := errors.New("dnf5 --assumeno --cacheonly install foo: Updating and loading repositories:\nRepositories loaded.\nFailed to resolve the transaction:\nNo match for argument: foo\nYou can try to add to command line:\n  --skip-unavailable to skip unavailable packages")
	got := previewFailure(nil, stderr, errors.New("no transaction table in dnf5 output"))
	if got != "dnf5 could not resolve the transaction: No match for argument: foo" {
		t.Fatalf("reason = %q", got)
	}
	got = previewFailure(nil, errors.New("dnf5: something else\nlast line"), errors.New("no table"))
	if got != "dnf5 preview failed: last line" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestARequestedProvideMayResolveToAnotherName(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// DNF resolves the first requested name to a differently named package,
	// the way pipewire-pulse resolved to pipewire-pulseaudio on Fedora 44.
	var requested string
	p := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		requested = rows[0].Name
		rows[0].Name = requested + "-real"
		src.Commands[nativetest.Key("dnf5", "--cacheonly", "repoquery", "--available", "--whatprovides", requested, "--queryformat", "%{name}|%{arch}|%{evr}|%{repoid}\\n")] = []byte(rows[0].Name + "|" + rows[0].Arch + "|" + rows[0].EVR + "|" + rows[0].Repository + "\n")
		return rows
	})
	op := find(p, "packages:install")
	if op.Blocked != "" || len(op.Notes) != 1 || op.Notes[0] != requested+" resolves to the package "+requested+"-real" {
		t.Fatalf("substitution = blocked %q notes %v", op.Blocked, op.Notes)
	}
	p = answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows, TxPackage{Name: "extra", Arch: "x86_64", EVR: "0:1-1", Repository: "fedora", Section: "installing"})
	})
	if op := find(p, "packages:install"); op.Blocked != "" || !strings.Contains(strings.Join(op.Notes, "\n"), "would install extra") {
		t.Fatalf("an extra row without an unmatched request must be noted: %q %v", op.Blocked, op.Notes)
	}
}

func TestNoMatchFromAnEnabledRepositoryAsksForARefresh(t *testing.T) {
	c, r := repository(t)
	// Every repository was enabled by an apply that stopped before its
	// refresh: the files are correct, so their packages join the preview,
	// but the local cache has never held Docker metadata. DNF matches
	// nothing from it, and one Fedora name is wrong as well.
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	args := installArgs(first)
	if args == nil || !slices.Contains(args, "docker-ce") {
		t.Fatalf("docker-ce is not in the preview: %v", args)
	}
	src.Commands[nativetest.Key("dnf5", args...)] = nil
	src.Failures[nativetest.Key("dnf5", args...)] = "dnf5 --assumeno --cacheonly install ...: Updating and loading repositories:\nRepositories loaded.\nFailed to resolve the transaction:\nNo match for argument: docker-ce\nNo match for argument: docker-ce-cli\nNo match for argument: no-such-fedora-package\nYou can try to add to command line:\n  --skip-unavailable to skip unavailable packages"
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "packages:install")
	if op == nil || op.Blocked == "" {
		t.Fatalf("install = %+v", op)
	}
	if !strings.HasPrefix(op.Blocked, "the enabled repositories docker have no cached metadata; sync again to refresh it (") || !strings.Contains(op.Blocked, "No match for argument: no-such-fedora-package") {
		t.Fatalf("blocked = %q", op.Blocked)
	}
}

func TestNeededUpgradesAreAcceptedAndNoted(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	// A requested package needs a newer openssl-libs than the installed
	// one, so DNF upgrades it and lists the old version below it.
	p := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows,
			TxPackage{Name: "openssl-libs", Arch: "x86_64", EVR: "1:3.5.8-1.fc44", Repository: "updates", Section: "upgrading"},
			TxPackage{Name: "openssl-libs", Arch: "x86_64", EVR: "1:3.5.7-2.fc44", Repository: "updates", Section: SectionReplaced})
	})
	op := find(p, "packages:install")
	if op.Blocked != "" || len(op.Notes) != 1 || op.Notes[0] != "1 installed packages are upgraded because the requested packages need the newer versions: openssl-libs" {
		t.Fatalf("needed upgrade = blocked %q notes %v", op.Blocked, op.Notes)
	}
}

func TestAnotherSourceIsNotedOnAdoptionAndKeep(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	f.Packages.Value = append(f.Packages.Value,
		inspect.Package{Name: "ripgrep", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "nimbus-terra", Reason: "user"},
		inspect.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "copr:copr.fedorainfracloud.org:someone:ghostty", Reason: "user"},
		inspect.Package{Name: "git", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "anaconda", Reason: "user"},
	)
	p, _ := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: applied("package:dnf:ripgrep")})
	if op := find(p, "package:dnf:ripgrep"); op == nil || op.Action != ActionKeep || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "installed from nimbus-terra, not from fedora") {
		t.Fatalf("kept from another source = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || op.Action != ActionAdopt || len(op.Notes) != 1 || !strings.Contains(op.Notes[0], "not from nimbus-terra") {
		t.Fatalf("adopted from another source = %+v", op)
	}
	if op := find(p, "package:dnf:git"); op == nil || len(op.Notes) != 0 {
		t.Fatalf("the installer's source needs no note: %+v", op)
	}
	if steps := repositorySteps("hyprland-copr", c.Definitions().Repositories["hyprland-copr"]); steps[2].Argv[0] != "rpm" || steps[2].Argv[1] != "--import" || !steps[2].Privileged {
		t.Fatalf("the COPR key must be imported before the enable: %+v", steps)
	}
}

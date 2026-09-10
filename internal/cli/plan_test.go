package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPlanRendersSectionsAndReportsIncomplete(t *testing.T) {
	root := repoRoot(t)
	src := fixtureSource(t, root)
	withForeignTerra(t, src)
	src.Commands[nativetest.Key("dnf5", "--cacheonly", "check-upgrade")] = []byte("Repositories loaded.\n")
	withSource(t, src)
	code, out, errOut := run(t, "sync", "-p", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure {
		t.Fatalf("an incomplete plan must exit 1, got %d\n%s%s", code, out, errOut)
	}
	for _, want := range []string{"plan for laptop", "sources to prepare:", "enable repository docker", "install ", "packages (exact versions once sources are prepared):", "docker-ce", "problems:", "repository terra is already enabled through terra.repo", "incomplete: fix the problems above"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sudo ") || strings.Contains(out, "sha256:") {
		t.Fatalf("the plan is for reading, not commands and digests:\n%s", out)
	}
	if strings.Contains(out, "prune ") {
		t.Fatal("prune section shown without --prune")
	}
	if _, out, _ := run(t, "sync", "-p", "-r", "--checkout", root, "--machine", "laptop"); !strings.Contains(out, "prune: prune needs the baseline") {
		t.Fatalf("plan --prune without a baseline must say why:\n%s", out)
	}
	code, out, _ = run(t, "sync", "-p", "--checkout", root, "--machine", "laptop", "-j")
	if code != ExitFailure {
		t.Fatalf("json exit %d", code)
	}
	var env struct {
		Data struct {
			Machine    string `json:"machine"`
			Complete   bool   `json:"complete"`
			Digest     string `json:"digest"`
			Operations []struct {
				ID      string `json:"id"`
				Blocked string `json:"blocked"`
			} `json:"operations"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Machine != "laptop" || env.Data.Complete || !strings.HasPrefix(env.Data.Digest, "sha256:") || len(env.Data.Operations) == 0 {
		t.Fatalf("envelope = %+v", env.Data)
	}
}

func TestStatusSummarizesThePlan(t *testing.T) {
	root := repoRoot(t)
	src := fixtureSource(t, root)
	withForeignTerra(t, src)
	withSource(t, src)
	code, out, _ := run(t, "status", "--checkout", root, "--machine", "desktop")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	c, err := definitions.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, errs := definitions.Resolve(c, "desktop")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want := fmt.Sprintf("machine desktop: %d profiles, %d components, %d desired packages", len(resolved.Profiles), len(resolved.Components), len(resolved.Packages))
	if !strings.Contains(out, want) || !strings.Contains(out, "plan incomplete") {
		t.Fatalf("status output:\n%s", out)
	}
	code, out, _ = run(t, "status", "--checkout", root, "--machine", "desktop", "--json")
	if code != ExitOK || !strings.Contains(out, `"repositories_to_enable"`) {
		t.Fatalf("json status: %d\n%s", code, out)
	}
}

func TestStatusCountsRetainedSourcesAsManaged(t *testing.T) {
	s := &selected{Resolved: &definitions.Resolved{Machine: "vm"}}
	p := &plan.Plan{Machine: "vm", Complete: true, Operations: []plan.Operation{
		{ID: "repository:retained", Kind: plan.KindRepository, Action: plan.ActionKeep, Summary: "retain source retained while installed software needs it"},
		{ID: "flatpak-remote:retained", Kind: plan.KindFlatpakRemote, Action: plan.ActionKeep, Summary: "retain source retained while installed software needs it"},
		{ID: "repository:new", Kind: plan.KindRepository, Action: plan.ActionEnable},
	}}
	st := summarize(s, p)
	if st.Managed != 2 || st.Repositories != 1 || st.ToInstall != 0 || st.ToRemove != 0 {
		t.Fatalf("retained sources are not pending preparation: %+v", st)
	}
}

func TestMachineOverridesWithoutSelector(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	if code, _, errOut := run(t, "status", "--checkout", root); code != ExitFailure || !strings.Contains(errOut, "--machine") {
		t.Fatalf("missing machine without a selector: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "status", "--checkout", root, "--machine", "nope"); code != ExitFailure || !strings.Contains(errOut, "unknown machine") {
		t.Fatalf("unknown machine: %d %q", code, errOut)
	}
}

func TestPlanReadsTheCacheAndARunRefreshesIt(t *testing.T) {
	root := repoRoot(t)
	src := fixtureSource(t, root)
	withForeignTerra(t, src)
	src.Failures[nativetest.Key("dnf5", "makecache")] = "no network"
	withSource(t, src)
	// Plan-only never refreshes: the failure is not even reached.
	code, out, errOut := run(t, "sync", "-p", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || strings.Contains(errOut, "metadata") || !strings.Contains(out, "from the local metadata cache") {
		t.Fatalf("sync -p: %d %q\n%s", code, errOut, out)
	}
	// A run refreshes first; a failed refresh is reported, not fatal.
	code, _, errOut = run(t, "sync", "-y", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(errOut, "metadata not refreshed: no network") {
		t.Fatalf("sync without network: %d %q", code, errOut)
	}
}

func TestPlanReadsLikeAnInstaller(t *testing.T) {
	tx := &plan.Transaction{Download: "2.8 GiB"}
	for i := range 40 {
		tx.Packages = append(tx.Packages,
			plan.TxPackage{Section: "installing", Name: fmt.Sprintf("requested-package-%02d", i), EVR: "0:1.0-1.fc44", Repository: "fedora"},
			plan.TxPackage{Section: "installing dependencies", Name: fmt.Sprintf("dependency-%02d", i), EVR: "0:2.0-1.fc44", Repository: "updates"},
		)
	}
	tx.Packages = append(tx.Packages, plan.TxPackage{Section: "upgrading", Name: "openssl-libs", EVR: "1:3.5.8-1.fc44", Repository: "updates"})
	var items []string
	for i := range 40 {
		items = append(items, fmt.Sprintf("dnf:requested-package-%02d", i))
	}
	p := &plan.Plan{Machine: "laptop", Complete: true, Operations: []plan.Operation{
		{ID: "repository:terra", Kind: plan.KindRepository, Action: plan.ActionEnable, Summary: "enable repository terra (https://example.invalid)"},
		{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install 40 packages", Items: items, Transaction: tx, Notes: []string{"1 installed packages are upgraded because the requested packages need the newer versions: openssl-libs"}},
		{ID: "package:terra:ghostty", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install ghostty", After: "repository:terra"},
		{ID: "flatpak:com.spotify.Client", Kind: plan.KindFlatpak, Action: plan.ActionInstall, Summary: "install Flatpak com.spotify.Client"},
		{ID: "package:dnf:bash", Kind: plan.KindPackage, Action: plan.ActionAdopt, Summary: "adopt bash"},
		{ID: "package:dnf:zsh", Kind: plan.KindPackage, Action: plan.ActionKeep, Summary: "zsh is managed"},
		{ID: "user:mise", Kind: plan.KindUser, Action: plan.ActionInstall, Summary: "install mise from https://mise.run as the user"},
	}}
	out := string(renderPlan(p, false, true))
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) > planWidth {
			t.Errorf("line longer than %d columns: %q", planWidth, line)
		}
	}
	for _, want := range []string{
		"sources to prepare:\n  enable repository terra",
		"install 41 packages, 2.8 GiB to download:\n  requested-package-00-0:1.0-1.fc44 (fedora),",
		"ghostty (after its repository is prepared)",
		"  plus 40 dependencies and 0 weak dependencies\n",
		"  upgrades needed: openssl-libs-1:3.5.8-1.fc44\n",
		"install 1 Flatpaks: com.spotify.Client\n",
		"note: 1 installed packages are upgraded",
		"user tools, as the user without sudo:\n  install mise from https://mise.run as the user\n",
		"adopt 1 already installed\n1 managed and unchanged\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dependency-00") || strings.Contains(out, "[") {
		t.Errorf("dependencies and tags do not belong in the plan:\n%s", out)
	}
}

func TestWaitingRunsNeedNoSudo(t *testing.T) {
	p := &plan.Plan{Operations: []plan.Operation{{Kind: plan.KindUser, Action: plan.ActionInstall, After: "packages:install"}}}
	if runnable(p) != 0 || needsSudo(p, false, true) {
		t.Fatal("waiting bootstrap needs no sudo")
	}
	if !needsSudo(p, true, true) || !needsSudo(p, false, false) {
		t.Fatal("upgrade and first run need sudo")
	}
}

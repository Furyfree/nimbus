package facts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectFedora44Fixture(t *testing.T) {
	src := fedora44(t)
	root := gitCheckout(t, src, false)
	f := Inspect(src, root)

	if !f.Platform.Known() || f.Platform.Value.ID != "fedora" || f.Platform.Value.VersionID != "44" || f.Platform.Value.Arch != "x86_64" {
		t.Fatalf("platform = %+v", f.Platform)
	}
	if !f.Packages.Known() || len(f.Packages.Value) != 150 {
		t.Fatalf("packages = %d known=%v %s", len(f.Packages.Value), f.Packages.Known(), f.Packages.Error)
	}
	reasons := map[string]int{}
	for _, p := range f.Packages.Value {
		reasons[p.Reason]++
	}
	if reasons["user"] != 25 || reasons["dependency"] != 125 || reasons["unknown"] != 0 {
		t.Fatalf("reasons = %v", reasons)
	}
	bash := findPackage(f, "bash")
	if bash == nil || bash.EVR() != "5.3.9-3.fc44" || bash.Reason != "user" {
		t.Fatalf("bash = %+v", bash)
	}

	if !f.Repositories.Known() {
		t.Fatal(f.Repositories.Error)
	}
	byID := map[string]Repository{}
	for _, r := range f.Repositories.Value {
		byID[r.ID] = r
	}
	if r := byID["fedora"]; !r.Enabled || r.GPGCheck != "1" || r.File != "fedora.repo" {
		t.Fatalf("fedora = %+v", r)
	}
	if r := byID["terra"]; !r.Enabled || r.GPGCheck != "1" || !strings.Contains(r.GPGKey, "terra") {
		t.Fatalf("terra = %+v", r)
	}
	if r := byID["terra-source"]; r.Enabled {
		t.Fatalf("terra-source should be disabled: %+v", r)
	}
	if r := byID["updates-testing"]; r.Enabled {
		t.Fatalf("updates-testing should be disabled: %+v", r)
	}

	if !f.Flatpak.Known() || len(f.Flatpak.Value.Remotes) != 1 || f.Flatpak.Value.Remotes[0].Name != "flathub" || len(f.Flatpak.Value.Apps) != 2 {
		t.Fatalf("flatpak = %+v", f.Flatpak)
	}
	if f.SecureBoot.Value != SecureBootEnabled || f.SELinux.Value != SELinuxEnforcing || f.Firewalld.Value != "active" {
		t.Fatalf("security = %s %s %s", f.SecureBoot.Value, f.SELinux.Value, f.Firewalld.Value)
	}
	if !f.Checkout.Known() || f.Checkout.Value.Origin != "github.com/furyfree-org/nimbus" || f.Checkout.Value.Dirty || !strings.HasPrefix(f.Checkout.Value.Commit, "0123456789ab") {
		t.Fatalf("checkout = %+v", f.Checkout)
	}
	for _, name := range RequiredCommands {
		if f.Commands[name] == "" {
			t.Fatalf("command %s missing", name)
		}
	}
}

func findPackage(f *Facts, name string) *Package {
	for i := range f.Packages.Value {
		if f.Packages.Value[i].Name == name {
			return &f.Packages.Value[i]
		}
	}
	return nil
}

func TestUnknownFactsAreReportedNotGuessed(t *testing.T) {
	src := fedora44(t)
	delete(src.Files, OSReleasePath)
	delete(src.Files, SecureBootPath)
	delete(src.Files, SELinuxPath)
	src.Failures[Key("dnf5", PackageQueryArgs...)] = "dnf5: cannot open the package database"
	src.Failures[Key("systemctl", "is-active", "firewalld")] = "inactive"
	delete(src.Commands, Key("systemctl", "is-active", "firewalld"))
	delete(src.Paths, "flatpak")
	delete(src.Commands, Key("flatpak", "remotes", "--system", "--columns=name,url"))
	f := Inspect(src, "")

	if f.Platform.Known() {
		t.Fatal("platform should be unknown without os-release")
	}
	if f.Packages.Known() || !strings.Contains(f.Packages.Error, "package database") {
		t.Fatalf("packages = %+v", f.Packages)
	}
	if f.SecureBoot.Value != SecureBootUnavailable || f.SELinux.Value != SELinuxDisabled {
		t.Fatalf("missing kernel files: %s %s", f.SecureBoot.Value, f.SELinux.Value)
	}
	if f.Firewalld.Known() || !strings.Contains(f.Firewalld.Error, "inactive") {
		t.Fatalf("firewalld = %+v", f.Firewalld)
	}
	if f.Flatpak.Known() || !strings.Contains(f.Flatpak.Error, ErrNotRecorded.Error()) {
		t.Fatalf("flatpak = %+v", f.Flatpak)
	}
	if f.Checkout.Known() || f.Checkout.Error != "no checkout selected" {
		t.Fatalf("checkout = %+v", f.Checkout)
	}
	if f.Commands["flatpak"] != "" {
		t.Fatal("flatpak should be reported missing")
	}
}

func TestFirewalldStateFromFailingExit(t *testing.T) {
	src := fedora44(t)
	// systemctl prints the state and exits non-zero for anything but active;
	// the fake models that as recorded output plus a failure.
	src.Commands[Key("systemctl", "is-active", "firewalld")] = []byte("inactive\n")
	delete(src.Failures, Key("systemctl", "is-active", "firewalld"))
	f := Inspect(src, "")
	if f.Firewalld.Value != "inactive" {
		t.Fatalf("firewalld = %+v", f.Firewalld)
	}
}

func TestSecureBootDisabledAndDirtyCheckout(t *testing.T) {
	src := fedora44(t)
	src.Files[SecureBootPath] = []byte{6, 0, 0, 0, 0}
	src.Files[SELinuxPath] = []byte("0\n")
	root := gitCheckout(t, src, true)
	f := Inspect(src, root)
	if f.SecureBoot.Value != SecureBootDisabled || f.SELinux.Value != SELinuxPermissive {
		t.Fatalf("security = %s %s", f.SecureBoot.Value, f.SELinux.Value)
	}
	if !f.Checkout.Value.Dirty {
		t.Fatal("dirty checkout not detected")
	}
}

func TestParsersRejectMalformedOutput(t *testing.T) {
	if _, err := parsePackages([]byte("bash|0|5.3.9\n")); err == nil || !strings.Contains(err.Error(), "expected 7 fields") {
		t.Fatalf("short package line accepted: %v", err)
	}
	if _, err := parseColumns([]byte("only-one-column\n"), 2); err == nil {
		t.Fatal("short column row accepted")
	}
	repos := parseRepoFile("x.repo", []byte("junk before section\n[a]\nenabled=0\ngpgcheck=true\n[b]\n"))
	if len(repos) != 2 || repos[0].Enabled || repos[0].GPGCheck != "1" || !repos[1].Enabled || repos[1].GPGCheck != "" {
		t.Fatalf("repos = %+v", repos)
	}
	values := parseOSRelease([]byte("ID=fedora\nPRETTY_NAME=\"Fedora Linux 44\"\n# comment\nBROKEN\n"))
	if values["ID"] != "fedora" || values["PRETTY_NAME"] != "Fedora Linux 44" {
		t.Fatalf("os-release = %v", values)
	}
	if (Package{Epoch: "1", Version: "2", Release: "3"}).EVR() != "1:2-3" {
		t.Fatal("epoch rendering")
	}
}

func TestExecSourceReturnsOutputOnFailure(t *testing.T) {
	out, err := (ExecSource{}).Run("sh", "-c", "echo inactive; exit 3")
	if err == nil {
		t.Fatal("non-zero exit must be an error")
	}
	if strings.TrimSpace(string(out)) != "inactive" {
		t.Fatalf("stdout lost on failure: %q", out)
	}
	if _, err := (ExecSource{}).Run("sh", "-c", "echo bad >&2; exit 1"); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}

func TestFakeFailureKeepsRecordedOutput(t *testing.T) {
	src := fedora44(t)
	src.Commands[Key("systemctl", "is-active", "firewalld")] = []byte("inactive\n")
	src.Failures[Key("systemctl", "is-active", "firewalld")] = "exit status 3"
	f := Inspect(src, "")
	if f.Firewalld.Value != "inactive" || !f.Firewalld.Known() {
		t.Fatalf("firewalld = %+v", f.Firewalld)
	}
}

func TestCheckoutReadsOnlyThroughSource(t *testing.T) {
	src := fedora44(t)
	root := gitCheckout(t, src, false)
	// Nothing was written below root; the .git directory exists only in the
	// fixture, so a read outside the source would fail.
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("test wrote into %s", root)
	}
	f := Inspect(src, root)
	if !f.Checkout.Known() || f.Checkout.Value.Origin != "github.com/furyfree-org/nimbus" {
		t.Fatalf("checkout = %+v", f.Checkout)
	}
	for _, key := range []string{Key("git", GitArgs(root, "rev-parse", "HEAD")...), Key("git", GitArgs(root, "status", "--porcelain")...)} {
		if !strings.HasPrefix(key, "git --no-optional-locks ") {
			t.Fatalf("git invoked without --no-optional-locks: %s", key)
		}
	}
	plain := t.TempDir()
	if f := Inspect(src, plain); f.Checkout.Known() || !strings.Contains(f.Checkout.Error, "not a Git checkout") {
		t.Fatalf("non-repository = %+v", f.Checkout)
	}
}

func TestCheckoutFollowsWorktreePointer(t *testing.T) {
	src := fedora44(t)
	main := t.TempDir()
	wt := t.TempDir()
	wtDir := filepath.Join(main, ".git", "worktrees", "wt")
	src.Files[filepath.Join(wt, ".git")] = []byte("gitdir: " + wtDir + "\n")
	src.Files[filepath.Join(wtDir, "commondir")] = []byte("../..\n")
	src.Files[filepath.Join(main, ".git", "config")] = []byte("[remote \"origin\"]\n\turl = https://github.com/furyfree-org/nimbus\n")
	src.Commands[Key("git", GitArgs(wt, "rev-parse", "HEAD")...)] = []byte("abc\n")
	src.Commands[Key("git", GitArgs(wt, "status", "--porcelain")...)] = []byte("")
	f := Inspect(src, wt)
	if !f.Checkout.Known() || f.Checkout.Value.Origin != "github.com/furyfree-org/nimbus" {
		t.Fatalf("worktree checkout = %+v", f.Checkout)
	}
}

func TestExecSourceReportsDirectories(t *testing.T) {
	_, err := (ExecSource{}).ReadFile(t.TempDir())
	if !IsDirectoryError(err) {
		t.Fatalf("directory read = %v", err)
	}
}

func TestRepositoryOverridesWin(t *testing.T) {
	src := fedora44(t)
	src.Dirs[RepoOverride] = []string{"99-config_manager.repo"}
	src.Files[filepath.Join(RepoOverride, "99-config_manager.repo")] = []byte("# Generated by libdnf5.\n[rpmfusion-free]\npriority=100\n[rpmfusion-free-updates]\npriority=100\n")
	f := Inspect(src, "")
	for _, r := range f.Repositories.Value {
		switch r.ID {
		case "rpmfusion-free", "rpmfusion-free-updates":
			if r.Priority != "100" || len(r.Overrides) != 1 {
				t.Fatalf("%s = %+v", r.ID, r)
			}
		case "fedora":
			if r.Priority != "" || len(r.Overrides) != 0 {
				t.Fatalf("fedora must be untouched: %+v", r)
			}
		}
	}
}

func TestReplayedPackagesKeepTheirSourceRepository(t *testing.T) {
	pkgs, err := parsePackages([]byte("hyprland|0|0.56.2|2.fc44|x86_64|@stored_transaction(copr:copr.fedorainfracloud.org:lionheartp:Hyprland)|User\nbat|0|0.26.1|4.fc44|x86_64|@stored_transaction(updates)|Dependency\nzsh|0|5.9|21.fc44|x86_64|updates|User\n"))
	if err != nil {
		t.Fatal(err)
	}
	if pkgs[0].FromRepo != "copr:copr.fedorainfracloud.org:lionheartp:Hyprland" || pkgs[1].FromRepo != "updates" || pkgs[2].FromRepo != "updates" {
		t.Fatalf("from_repo = %q %q %q", pkgs[0].FromRepo, pkgs[1].FromRepo, pkgs[2].FromRepo)
	}
}

func TestAnOverrideCanDisableARepository(t *testing.T) {
	src := &FakeSource{
		Files: map[string][]byte{
			filepath.Join(RepoDir, "1password.repo"):              []byte("[1password]\nname=1Password\nbaseurl=https://example.invalid/1p\nenabled=1\n"),
			filepath.Join(RepoOverride, "99-config_manager.repo"): []byte("[1password]\nenabled=0\n"),
		},
		Dirs: map[string][]string{RepoDir: {"1password.repo"}, RepoOverride: {"99-config_manager.repo"}},
	}
	repos, err := repositories(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Enabled || len(repos[0].Overrides) != 1 {
		t.Fatalf("repos = %+v", repos)
	}
}

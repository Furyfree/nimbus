// Package doctor turns facts into health checks. Each failure carries its
// observation, impact, and remediation; doctor never repairs.
package doctor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Furyfree/nimbus/internal/facts"
)

// Status of one check.
const (
	Pass    = "pass"
	Fail    = "fail"
	Unknown = "unknown" // the fact could not be established; never a guess
)

// Check is one health check result.
type Check struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Observation string `json:"observation"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// Report is the complete doctor result.
type Report struct {
	Checks  []Check `json:"checks"`
	Failed  int     `json:"failed"`
	Unknown int     `json:"unknown"`
}

// Config is what doctor knows beyond the facts.
type Config struct {
	// SupportedReleases comes from the checkout's nimbus.toml; nil when the
	// checkout could not be loaded.
	SupportedReleases []string
	// DefinitionsError explains why the checkout definitions are unusable.
	DefinitionsError string
	// SelectorError explains why the selector could not be used.
	SelectorError string
	// CheckoutOverride is true when --checkout replaced the selector.
	CheckoutOverride bool
	// ApprovedOrigin is the selector's origin when the selector was used.
	ApprovedOrigin string
	// Machine and Profiles are the selected machine's, for the Chezmoi
	// check; empty when no machine is selected.
	Machine  string
	Profiles []string
	// Dotfiles is true when the selected machine declares a dotfiles
	// repository, so a handoff is expected.
	Dotfiles bool
}

// Run evaluates every check against the facts.
func Run(f *facts.Facts, cfg Config) Report {
	var r Report
	add := func(c Check) {
		r.Checks = append(r.Checks, c)
		switch c.Status {
		case Fail:
			r.Failed++
		case Unknown:
			r.Unknown++
		}
	}
	add(platform(f, cfg))
	add(selector(f, cfg))
	add(definitions(cfg))
	add(commands(f))
	add(packages(f))
	add(repositorySignatures(f))
	add(secureBoot(f))
	add(selinux(f))
	add(firewalld(f))
	add(chezmoiSelection(f, cfg))
	return r
}

func platform(f *facts.Facts, cfg Config) Check {
	c := Check{ID: "platform"}
	if !f.Platform.Known() {
		c.Status, c.Observation = Unknown, f.Platform.Error
		c.Remediation = "make " + facts.OSReleasePath + " and uname readable"
		return c
	}
	p := f.Platform.Value
	c.Observation = fmt.Sprintf("%s %s on %s", p.ID, p.VersionID, p.Arch)
	switch {
	case p.ID != "fedora":
		c.Status, c.Impact = Fail, "Nimbus manages Fedora only"
		c.Remediation = "run Nimbus on a supported Fedora installation"
	case p.Arch != "x86_64":
		c.Status, c.Impact = Fail, "only x86_64 is supported"
		c.Remediation = "run Nimbus on an x86_64 machine"
	case cfg.SupportedReleases == nil:
		c.Status = Unknown
		c.Observation += "; supported releases unknown because the checkout definitions are unavailable"
		c.Remediation = "fix the checkout, then rerun doctor"
	case !contains(cfg.SupportedReleases, p.VersionID):
		c.Status = Fail
		c.Impact = "Nimbus refuses every mutation on an unsupported release"
		c.Remediation = fmt.Sprintf("use a checkout that supports Fedora %s (this one supports %s) or upgrade Fedora with its native tooling", p.VersionID, strings.Join(cfg.SupportedReleases, ", "))
	default:
		c.Status = Pass
	}
	return c
}

func selector(f *facts.Facts, cfg Config) Check {
	c := Check{ID: "selector"}
	switch {
	case cfg.CheckoutOverride && !f.Checkout.Known() && f.Commands["git"] == "":
		c.Status, c.Observation = Unknown, "--checkout override in use; git is not installed, so the checkout's commit cannot be read yet"
		c.Remediation = "apply installs git through the common profile; run doctor again afterwards"
	case cfg.CheckoutOverride && !f.Checkout.Known():
		c.Status, c.Observation = Fail, "--checkout override in use; "+f.Checkout.Error
		c.Impact = "the override skips selector approval only; a checkout whose origin and commit cannot be read is not inspectable"
		c.Remediation = "point --checkout at a Git clone of the Nimbus repository"
	case cfg.CheckoutOverride:
		c.Status, c.Observation = Pass, "--checkout override in use; the selector was not consulted; "+describeCheckout(f.Checkout.Value)
	case cfg.SelectorError != "":
		c.Status, c.Observation = Fail, cfg.SelectorError
		c.Impact = "no machine is selected, so nothing can be resolved or applied"
		c.Remediation = "run nimbus init to select a checkout and machine"
	case !f.Checkout.Known() && f.Commands["git"] == "":
		c.Status, c.Observation = Unknown, "git is not installed, so the checkout's commit cannot be read yet"
		c.Remediation = "apply installs git through the common profile; run doctor again afterwards"
	case !f.Checkout.Known():
		c.Status, c.Observation = Fail, f.Checkout.Error
		c.Impact = "the selected checkout cannot be trusted"
		c.Remediation = "make sure the checkout is a Git clone of the approved origin"
	case f.Checkout.Value.Origin != cfg.ApprovedOrigin:
		c.Status = Fail
		c.Observation = fmt.Sprintf("checkout origin %s, approved origin %s", f.Checkout.Value.Origin, cfg.ApprovedOrigin)
		c.Impact = "the checkout is not the approved repository"
		c.Remediation = "point the selector at the approved checkout or review the trust change explicitly"
	default:
		c.Status, c.Observation = Pass, describeCheckout(f.Checkout.Value)
	}
	return c
}

func describeCheckout(co facts.Checkout) string {
	state := "clean"
	if co.Dirty {
		state = "dirty"
	}
	commit := co.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return fmt.Sprintf("%s at %s (%s)", co.Origin, commit, state)
}

func definitions(cfg Config) Check {
	c := Check{ID: "definitions"}
	if cfg.DefinitionsError != "" {
		c.Status, c.Observation = Fail, cfg.DefinitionsError
		c.Impact = "no desired state can be resolved from this checkout"
		c.Remediation = "run nimbus validate and fix the reported definitions"
		return c
	}
	c.Status, c.Observation = Pass, "checkout definitions validate"
	return c
}

func commands(f *facts.Facts) Check {
	c := Check{ID: "commands"}
	var missing []string
	for _, name := range facts.RequiredCommands {
		if f.Commands[name] == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		c.Status = Fail
		c.Observation = "missing: " + strings.Join(missing, ", ")
		c.Impact = "Nimbus drives these native tools and cannot inspect or apply without them"
		c.Remediation = "install the missing packages with dnf5"
		return c
	}
	c.Status, c.Observation = Pass, "every required native command is present"
	return c
}

func packages(f *facts.Facts) Check {
	c := Check{ID: "packages"}
	if !f.Packages.Known() {
		c.Status, c.Observation = Fail, f.Packages.Error
		c.Impact = "installed packages cannot be compared with desired state"
		c.Remediation = "make sure dnf5 works for the normal user"
		return c
	}
	c.Status = Pass
	c.Observation = fmt.Sprintf("%d installed packages read from DNF", len(f.Packages.Value))
	return c
}

func repositorySignatures(f *facts.Facts) Check {
	c := Check{ID: "repository-signatures"}
	if !f.Repositories.Known() {
		c.Status, c.Observation = Unknown, f.Repositories.Error
		c.Remediation = "make " + facts.RepoDir + " readable"
		return c
	}
	var unchecked, unset []string
	enabled := 0
	for _, r := range f.Repositories.Value {
		if !r.Enabled {
			continue
		}
		enabled++
		switch r.GPGCheck {
		case "1":
		case "0":
			unchecked = append(unchecked, r.ID)
		case "":
			unset = append(unset, r.ID)
		default:
			unset = append(unset, r.ID+" (gpgcheck="+r.GPGCheck+")")
		}
	}
	if len(unchecked) > 0 {
		c.Status = Fail
		c.Observation = "signature checking is off for: " + strings.Join(unchecked, ", ")
		c.Impact = "packages from these repositories install without a verified signature"
		c.Remediation = "set gpgcheck=1 in the repository file or disable the repository"
		return c
	}
	if len(unset) > 0 {
		c.Status = Unknown
		c.Observation = "gpgcheck is not set for: " + strings.Join(unset, ", ") + "; doctor does not vouch for DNF's default"
		c.Remediation = "set gpgcheck=1 explicitly in the repository file"
		return c
	}
	c.Status = Pass
	c.Observation = fmt.Sprintf("%d enabled repositories, all with signature checking", enabled)
	return c
}

func secureBoot(f *facts.Facts) Check {
	c := Check{ID: "secure-boot"}
	switch {
	case !f.SecureBoot.Known():
		c.Status, c.Observation = Unknown, f.SecureBoot.Error
	case f.SecureBoot.Value == facts.SecureBootEnabled:
		c.Status, c.Observation = Pass, "Secure Boot is enabled"
	case f.SecureBoot.Value == facts.SecureBootUnavailable:
		c.Status, c.Observation = Unknown, "no EFI variables: legacy boot or a container"
		c.Remediation = "boot in UEFI mode with Secure Boot enabled"
	default:
		c.Status, c.Observation = Fail, "Secure Boot is disabled"
		c.Impact = "the policy requires Secure Boot; TPM2 unlock bound to PCR 7 protects nothing without it"
		c.Remediation = "enable Secure Boot in firmware; Nimbus never changes it"
	}
	return c
}

func selinux(f *facts.Facts) Check {
	c := Check{ID: "selinux"}
	switch {
	case !f.SELinux.Known():
		c.Status, c.Observation = Unknown, f.SELinux.Error
	case f.SELinux.Value == facts.SELinuxEnforcing:
		c.Status, c.Observation = Pass, "SELinux is enforcing"
	default:
		c.Status, c.Observation = Fail, "SELinux is "+f.SELinux.Value
		c.Impact = "the policy requires enforcing mode; denials must be fixed with typed resources, not by relaxing SELinux"
		c.Remediation = "set SELINUX=enforcing in /etc/selinux/config and reboot"
	}
	return c
}

func firewalld(f *facts.Facts) Check {
	c := Check{ID: "firewalld"}
	switch {
	case !f.Firewalld.Known():
		c.Status, c.Observation = Unknown, f.Firewalld.Error
	case f.Firewalld.Value == "active":
		c.Status, c.Observation = Pass, "firewalld is active"
	default:
		c.Status, c.Observation = Fail, "firewalld is "+f.Firewalld.Value
		c.Impact = "no firewall drops inbound traffic"
		c.Remediation = "systemctl enable --now firewalld"
	}
	return c
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// chezmoiSelection compares what Chezmoi stored at its initialization with
// the selected machine and its profiles; a profile change after the handoff
// is refreshed by hand with the command profiles add prints.
func chezmoiSelection(f *facts.Facts, cfg Config) Check {
	c := Check{ID: "chezmoi"}
	switch {
	case cfg.Machine == "" || !cfg.Dotfiles:
		c.Status, c.Observation = Pass, "no dotfiles repository is declared; nothing to compare"
	case f.Commands["chezmoi"] == "":
		c.Status, c.Observation = Unknown, "chezmoi is not installed"
		c.Remediation = "sync installs chezmoi through the common profile; init then performs the handoff"
	case !f.Chezmoi.Known():
		c.Status, c.Observation = Unknown, f.Chezmoi.Error
	case !f.Chezmoi.Value.Initialized:
		c.Status, c.Observation = Fail, "Chezmoi is not initialized"
		c.Impact = "the user configuration is not managed yet"
		c.Remediation = "run nimbus init for the one-time Chezmoi handoff"
	case !f.Chezmoi.Value.ManagedByNimbus || f.Chezmoi.Value.Machine != cfg.Machine || !sameSet(f.Chezmoi.Value.Profiles, cfg.Profiles):
		c.Status = Fail
		c.Observation = fmt.Sprintf("Chezmoi stores machine %q, managed_by_nimbus %v, profiles %s; the manifest selects %s with profiles %s", f.Chezmoi.Value.Machine, f.Chezmoi.Value.ManagedByNimbus, strings.Join(f.Chezmoi.Value.Profiles, "/"), cfg.Machine, strings.Join(cfg.Profiles, "/"))
		c.Impact = "the user configuration follows a stale selection"
		c.Remediation = "refresh with: " + ChezmoiRefresh(cfg.Machine, cfg.Profiles)
	default:
		c.Status, c.Observation = Pass, "Chezmoi stores the selected machine and profiles"
	}
	return c
}

// ChezmoiRefresh is the direct command that re-answers the Nimbus prompts
// of an initialized Chezmoi.
func ChezmoiRefresh(machine string, profiles []string) string {
	return fmt.Sprintf("chezmoi init --prompt --promptString Machine=%s --promptBool ManagedByNimbus=true --promptMultichoice Profiles=%s", machine, strings.Join(profiles, "/"))
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if !set[y] {
			return false
		}
	}
	return true
}

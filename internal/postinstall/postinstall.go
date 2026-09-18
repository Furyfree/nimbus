// Package postinstall inspects manual setup with read-only observations.
// Native action runners require caller approval and never store completion receipts.
package postinstall

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/agentproxy"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

type Status string

const (
	Pending       Status = "pending"
	Complete      Status = "complete"
	Unknown       Status = "unknown"
	Blocked       Status = "blocked"
	NotApplicable Status = "not-applicable"
)

type ActionKind string

const (
	RestoreNoctaliaLockscreen ActionKind = "restore-noctalia-lockscreen"
	OpenApplication           ActionKind = "open-application"
	EnrollFingerprint         ActionKind = "enroll-fingerprint"
	InstallApplication        ActionKind = "install-application"
	SetTailscaleOperator      ActionKind = "set-tailscale-operator"
	LoginTailscale            ActionKind = "login-tailscale"
	ConfigureDTUNetwork       ActionKind = "configure-dtu-network"
	InstallDTUCertificate     ActionKind = "install-dtu-certificate"
	SyncNoctaliaPlugins       ActionKind = "sync-noctalia-plugins"
	SyncHyprlandPlugins       ActionKind = "sync-hyprland-plugins"
	SetAccountPicture         ActionKind = "set-account-picture"
	SetupVoxtype              ActionKind = "setup-voxtype"
	SetHostname               ActionKind = "set-hostname"
	SetupNVIDIA               ActionKind = "setup-nvidia"
	SetupFDE                  ActionKind = "setup-fde"
	EnrollFDE                 ActionKind = "enroll-fde"
)

// Action describes native commands offered for explicit user selection.
// Argv is used for a single command; Commands is an ordered native workflow.
type Action struct {
	DTUProfile *DTUProfile       `json:"dtu_profile,omitempty"`
	DTU        *DTUCertificate   `json:"dtu,omitempty"`
	Lockscreen *LockscreenRepair `json:"lockscreen,omitempty"`
	Hyprland   *HyprlandSetup    `json:"hyprland,omitempty"`
	Kind       ActionKind        `json:"kind"`
	Argv       []string          `json:"argv,omitempty"`
	Commands   [][]string        `json:"commands,omitempty"`
	User       string            `json:"user,omitempty"`
	Hostname   string            `json:"hostname,omitempty"`
	Picture    *AccountPicture   `json:"picture,omitempty"`
}

type Task struct {
	PreviouslyVerified    bool     `json:"previously_verified,omitzero"`
	VerificationNeedsRoot bool     `json:"verification_needs_root,omitzero"`
	ID                    string   `json:"id"`
	Owner                 string   `json:"owner"`
	Title                 string   `json:"title"`
	Status                Status   `json:"status"`
	Detail                string   `json:"detail"`
	Prerequisites         []string `json:"prerequisites,omitempty"`
	Instructions          []string `json:"instructions"`
	Verification          string   `json:"verification"`
	Recovery              string   `json:"recovery"`
	Action                *Action  `json:"action,omitempty"`
	Reboot                bool     `json:"reboot,omitzero"`
	Logout                bool     `json:"logout,omitzero"`
	// fdeSecure remembers the inspected Secure Boot state for the FDE task's
	// preview; RunFDESetup re-reads it before any mutation.
	fdeSecure bool
}

// FDESecure reports the inspected Secure Boot state for the FDE task.
func (t Task) FDESecure() bool { return t.fdeSecure }

type Inputs struct {
	Task     string // Empty inspects the complete checklist; a task ID inspects only its prerequisites.
	Resolved *definitions.Resolved
	Facts    inspect.Facts
	Applied  state.Applied
}

// Inspect returns selected tasks, including completed and unknown tasks, in
// stable order. Callers may filter presentation but must not promote unknown
// observations to completion. Source must describe the invoking user's system.
func Inspect(src native.Source, in Inputs) []Task {
	result := []Task{}
	if in.Resolved == nil {
		return result
	}
	add := func(id string, check func() Task) {
		if in.Task == "" || in.Task == id {
			result = append(result, check())
		}
	}
	for _, pkg := range in.Resolved.Packages {
		switch {
		case pkg.Name == "1password" && pkg.Prefix == "onepassword":
			add("onepassword", func() Task { return onePassword(src, in, pkg) })
		case pkg.Name == "fprintd" && pkg.Prefix == "dnf":
			add("fingerprint", func() Task { return fingerprint(src, in, pkg) })
		case pkg.Name == "tailscale" && pkg.Prefix != "flatpak":
			add("tailscale-operator", func() Task { return tailscaleOperator(src, in, pkg) })
		case pkg.Name == "github-copilot-installer" || pkg.Name == "wowup-cf-installer":
			id := "copilot"
			if pkg.Name == "wowup-cf-installer" {
				id = "wowup"
			}
			add(id, func() Task { return installerHelper(src, in, pkg) })
			if pkg.Name == "github-copilot-installer" && (in.Task == "" || in.Task == "agent-proxy") {
				observed := agentproxy.Inspect(src, in.Resolved.Machine)
				status := Pending
				if observed.Status == "configured" {
					status = Complete
				}
				if observed.Status == "blocked" {
					status = Unknown
				}
				result = append(result, Task{ID: "agent-proxy", Owner: "agent-proxy", Title: "Connect Copilot to local agents", Status: status, Detail: observed.Detail, Instructions: []string{"Run nimbus postinstall agent-proxy for approved native setup and model refresh."}, Verification: "Local registration and installation files; runtime access is checked only by the explicit task.", Recovery: "Retry the task with Copilot open. Existing models are retained when discovery fails."})
			}
		case pkg.Name == "noctalia" && pkg.Prefix != "flatpak":
			add("noctalia-plugins", func() Task { return noctaliaPlugins(src, in, pkg) })
			add("noctalia-lockscreen", func() Task { return noctaliaLockscreen(src, in, pkg) })
		case pkg.Name == "hyprland-devel" && pkg.Prefix != "flatpak":
			add("hyprland-plugins", func() Task { return hyprlandPlugins(src, in, pkg) })
		case pkg.Name == "accountsservice" && pkg.Prefix != "flatpak":
			add("account-picture", func() Task { return accountPicture(src, in, pkg) })
		case pkg.Name == "voxtype":
			add("voxtype", func() Task { return voxtypeSetup(src, in, pkg) })
		case pkg.Name == "protonplus":
			for _, steam := range in.Resolved.Packages {
				if steam.Name == "steam" && steam.Prefix != "flatpak" {
					add("proton-cachyos", func() Task { return protonCachyOS(src, in, pkg, steam) })
					break
				}
			}
		}
	}
	if slices.ContainsFunc(in.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "nvidia" }) {
		add("nvidia-mok", func() Task { return mok(src, in) })
	}
	if slices.ContainsFunc(in.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "dtu-network" }) {
		add("dtu-network", func() Task { return dtuNetwork(src, in) })
	}
	add("hostname", func() Task { return hostnameTask(src, in) })
	if slices.ContainsFunc(in.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "fde" }) {
		add("fde", func() Task { return fdeTask(src, in) })
	}
	if in.Task == "" {
		result = append(result, sessionTasks(src, in)...)
	}
	slices.SortFunc(result, func(a, b Task) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func protonCachyOS(src native.Source, in Inputs, proton, steam definitions.ResolvedPackage) Task {
	t := Task{
		ID: "proton-cachyos", Owner: "package:" + proton.Canonical,
		Title: "Install Proton-CachyOS Latest for Steam", Status: Unknown,
		Detail:        "ProtonPlus owns the compatibility tool and its rolling updates. Package presence does not prove that a runner is installed or current.",
		Prerequisites: []string{"Start native Steam once to create its user directories, then close running games."},
		Instructions: []string{
			"Install the rolling Latest entry through ProtonPlus. Restart Steam afterwards to make the compatibility tool available.",
			"For an existing Latest installation, use protonplus update steam-system proton-cachyos. ProtonPlus preferences control background updates.",
		},
		Verification: "Use protonplus list steam-system and check ProtonPlus for updates. Nimbus does not query remote releases or read Steam account data during inspection.",
		Recovery:     "Use ProtonPlus to retry a failed download or remove the compatibility tool. Nimbus does not remove Steam data or runner files.",
	}
	for _, pkg := range []definitions.ResolvedPackage{proton, steam} {
		if status, detail := packageReady(in, pkg); status != Complete {
			t.Status, t.Detail = status, detail
			return t
		}
	}
	if _, err := src.LookPath("protonplus"); err != nil {
		t.Status, t.Detail = Blocked, "ProtonPlus is unavailable; repair the selected package with nimbus sync."
		return t
	}
	output, err := src.Run("protonplus", "list", "steam-system")
	if err != nil {
		t.Detail = "ProtonPlus could not inspect Steam runners; start Steam once, then inspect protonplus list steam-system."
		return t
	}
	listing := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(output), "")
	if !strings.HasPrefix(listing, "Installed runners for Steam:\n") {
		t.Detail = "Unrecognized ProtonPlus runner listing; inspect protonplus list steam-system."
		return t
	}
	for line := range strings.SplitSeq(listing, "\n") {
		if strings.TrimSpace(line) == "Proton-CachyOS Latest" {
			t.Status, t.Detail = Complete, "ProtonPlus lists Proton-CachyOS Latest for native Steam."
			return t
		}
	}
	t.Status, t.Detail = Pending, "Proton-CachyOS Latest is not installed for native Steam."
	t.Action = &Action{Kind: InstallApplication, Argv: []string{"protonplus", "install", "steam-system", "proton-cachyos", "latest"}}
	return t
}

func installerHelper(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	id, title := "copilot", "Install GitHub Copilot"
	if pkg.Name == "wowup-cf-installer" {
		id, title = "wowup", "Install WoWUp CurseForge"
	}
	helper := "/usr/bin/" + pkg.Name
	t := Task{
		ID: id, Owner: "package:" + pkg.Canonical, Title: title, Status: Unknown,
		Detail:       "The helper RPM does not include the application. The helper owns downloads, verification, installation and removal.",
		Instructions: []string{"Inspect the helper's status before installing. Use Topgrade for configured application updates."},
		Verification: "Check the installer helper's status and launch the application. Nimbus does not record application completion.",
		Recovery:     "Use the helper's documented uninstall command before removing its RPM; user data remains outside Nimbus ownership.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
	} else if _, err := src.LookPath(helper); err != nil {
		t.Status, t.Detail = Blocked, "The installer helper is unavailable; repair the selected package with nimbus sync."
	} else if id == "wowup" {
		output, err := src.Run(helper, "status", "--json")
		if err != nil {
			t.Detail = "The installer helper could not inspect application state; inspect its native status."
			return t
		}
		var status struct {
			Installed      bool   `json:"installed"`
			Verified       bool   `json:"verified"`
			CleanupPending bool   `json:"cleanup_pending"`
			Version        string `json:"version"`
		}
		if json.Unmarshal(output, &status) != nil {
			t.Detail = "Installer helper returned no recognized application status."
			return t
		}
		switch {
		case status.CleanupPending:
			t.Status = Blocked
			t.Detail = "WoWUp removal cleanup is pending; run wowup-cf-installer status --json and finish removal or reinstall before installing."
		case status.Installed && status.Verified:
			t.Status, t.Detail = Complete, "WoWUp CurseForge "+status.Version+" is installed."
		default:
			t.Status, t.Detail = Pending, "WoWUp CurseForge is not installed."
			t.Instructions = append(t.Instructions, "The helper verifies the official AppImage and installs it. Installing or reinstalling the helper RPM queues the same job through systemd.")
			t.Action = &Action{Kind: InstallApplication, Argv: []string{"sudo", "--", helper, "install", "--assumeyes"}}
		}
	} else {
		output, err := src.Run(helper, "status")
		if err != nil {
			t.Detail = "The installer helper could not inspect application state; inspect its native status."
			return t
		}
		found := false
		for line := range strings.SplitSeq(string(output), "\n") {
			if value, ok := strings.CutPrefix(line, "Installed GitHub Copilot: "); ok {
				found = true
				if value != "not installed" && regexp.MustCompile(`^[0-9][A-Za-z0-9.+~^-]*$`).MatchString(value) {
					t.Status, t.Detail = Complete, "GitHub Copilot "+value+" is installed."
					return t
				}
				if value != "not installed" {
					t.Detail = "Unrecognized Copilot installation identity."
					return t
				}
			}
		}
		if !found {
			t.Detail = "Installer helper returned no recognized application status."
			return t
		}
		t.Status, t.Detail = Pending, "GitHub Copilot is not installed."
		t.Instructions = append(t.Instructions, "The helper selects and verifies the application release, then asks DNF to install it. Native prompts remain enabled.")
		t.Action = &Action{Kind: InstallApplication, Argv: []string{"sudo", "--", helper, "install"}}
	}
	return t
}

func onePassword(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "onepassword", Owner: "package:" + pkg.Canonical, Title: "Set up 1Password",
		Status: Unknown, Detail: "Sign-in and unlock readiness require confirmation in 1Password; no account or secret data was queried.",
		Prerequisites: []string{"The selected 1Password package is installed and recorded by Nimbus."},
		Instructions:  []string{"Open 1Password, sign in if needed, and unlock it.", "Enable any desired browser or SSH integration in 1Password and the corresponding user configuration."},
		Verification:  "Confirm the intended account is usable in the app. Package presence does not establish sign-in readiness.",
		Recovery:      "Close the app or lock it; account and integration settings remain owned by 1Password and the user.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
	} else if _, err := src.LookPath("1password"); err != nil {
		t.Status, t.Detail = Blocked, "The 1Password executable is unavailable; repair the selected package with nimbus sync."
	} else {
		t.Action = &Action{Kind: OpenApplication, Argv: []string{"1password"}}
	}
	return t
}

func packageReady(in Inputs, pkg definitions.ResolvedPackage) (Status, string) {
	if !in.Facts.Packages.Known() {
		return Unknown, "Installed package state could not be inspected."
	}
	installed := slices.ContainsFunc(in.Facts.Packages.Value, func(p inspect.Package) bool { return p.Name == pkg.Name && (p.Arch == "x86_64" || p.Arch == "noarch") })
	if !installed {
		return Blocked, "The selected " + pkg.Name + " package is not installed; run nimbus sync."
	}
	id := "package:" + pkg.Canonical
	r, ok := in.Applied.Receipts[id]
	if !ok || !validReceipt(in, id, r) || r.Provider != "dnf" {
		return Blocked, "The selected " + pkg.Name + " package has no verified Nimbus ownership receipt; run nimbus sync."
	}
	if r.Package != "" && !slices.ContainsFunc(in.Facts.Packages.Value, func(p inspect.Package) bool {
		return p.Name == pkg.Name && p.ID() == r.Package && (p.Arch == "x86_64" || p.Arch == "noarch")
	}) {
		return Blocked, "The installed " + pkg.Name + " package differs from its recorded native identity; inspect nimbus status."
	}
	return Complete, ""
}

func validReceipt(in Inputs, id string, r state.Receipt) bool {
	return in.Applied.Present && r.Verified && r.Resource == id && r.Machine == in.Resolved.Machine && (r.Schema == 1 || r.Schema == state.ReceiptSchema) && slices.Contains([]string{"install", "adopt", "repair", "enable"}, r.Operation)
}

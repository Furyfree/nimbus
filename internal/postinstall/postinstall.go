// Package postinstall inspects manual setup with read-only observations.
// Native action runners require caller approval and never store completion receipts.
package postinstall

import (
	"slices"
	"strings"

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
	OpenApplication      ActionKind = "open-application"
	EnrollFingerprint    ActionKind = "enroll-fingerprint"
	InstallApplication   ActionKind = "install-application"
	SetTailscaleOperator ActionKind = "set-tailscale-operator"
	SyncNoctaliaPlugins  ActionKind = "sync-noctalia-plugins"
)

// Action describes native commands offered for explicit user selection.
// Argv is used for a single command; Commands is the Noctalia source workflow.
type Action struct {
	Kind     ActionKind `json:"kind"`
	Argv     []string   `json:"argv,omitempty"`
	Commands [][]string `json:"commands,omitempty"`
	User     string     `json:"user,omitempty"`
}

type Task struct {
	ID            string   `json:"id"`
	Owner         string   `json:"owner"`
	Title         string   `json:"title"`
	Status        Status   `json:"status"`
	Detail        string   `json:"detail"`
	Prerequisites []string `json:"prerequisites,omitempty"`
	Instructions  []string `json:"instructions"`
	Verification  string   `json:"verification"`
	Recovery      string   `json:"recovery"`
	Action        *Action  `json:"action,omitempty"`
	Reboot        bool     `json:"reboot,omitzero"`
	Logout        bool     `json:"logout,omitzero"`
}

type Inputs struct {
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
	for _, pkg := range in.Resolved.Packages {
		switch {
		case pkg.Name == "1password" && pkg.Prefix == "onepassword":
			result = append(result, onePassword(src, in, pkg))
		case pkg.Name == "fprintd" && pkg.Prefix == "dnf":
			result = append(result, fingerprint(src, in, pkg))
		case pkg.Name == "tailscale" && pkg.Prefix != "flatpak":
			result = append(result, tailscaleOperator(src, in, pkg))
		case pkg.Name == "github-copilot-installer" || pkg.Name == "wowup-cf-installer":
			result = append(result, installerHelper(src, in, pkg))
		case pkg.Name == "noctalia" && pkg.Prefix != "flatpak":
			result = append(result, noctaliaPlugins(src, in, pkg))
		case pkg.Name == "protonplus":
			for _, steam := range in.Resolved.Packages {
				if steam.Name == "steam" && steam.Prefix != "flatpak" {
					result = append(result, protonCachyOS(src, in, pkg, steam))
					break
				}
			}
		}
	}
	if slices.ContainsFunc(in.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "nvidia" }) {
		result = append(result, mok(src, in))
	}
	result = append(result, sessionTasks(src, in)...)
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
		t.Status = Blocked
		t.Detail = "The WoWUp COPR helper needs a standalone install command before Nimbus can offer initial installation."
		t.Instructions = []string{"The current helper exposes prepare/apply only. Complete the standalone install flow in COPR; Nimbus will not manage application artifacts."}
	} else {
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

package plan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// flatpakRemote returns the operation for a missing or mismatched system
// remote. A remote with the declared name but another URL blocks.
func (b *builder) flatpakRemote(id string, r definitions.Repository) (Operation, bool) {
	op := Operation{
		ID: "flatpak-remote:" + id, Kind: KindFlatpakRemote, Action: ActionEnable, Risk: RiskMedium,
		Summary: "add system Flatpak remote " + id, Paths: b.repoPaths(id),
		Steps: []Step{
			{Description: "download " + r.URL + " and verify its sole primary key fingerprint is " + definitions.NormalizeFingerprint(r.Key)},
			{Description: "add the verified remote", Argv: []string{"flatpak", "remote-add", "--if-not-exists", "--system", "--from", id, "<verified remote definition>"}, Privileged: true},
			{Description: "verify the installed remote URL, signature checking and key fingerprint"},
		},
	}
	op.Source = b.sourceOwnership(op, []string{id})
	if !b.in.Facts.Flatpak.Known() {
		if b.in.Facts.Commands["flatpak"] == "" && b.installsPackage("flatpak") {
			// flatpak itself arrives with this plan's install transaction;
			// the remote and its applications wait for it.
			op.After = "packages:install"
			b.pendingRepo[id] = true
			return op, true
		}
		op.Blocked = "Flatpak state is unknown: " + b.in.Facts.Flatpak.Error
		b.blockedRepo[id] = op.Blocked
		return op, true
	}
	remotes := b.in.Facts.Flatpak.Value.Remotes
	i := slices.IndexFunc(remotes, func(remote inspect.FlatpakRemote) bool { return remote.Name == id })
	if i < 0 {
		return op, true
	}
	remote := remotes[i]
	if remote.URL == r.URL || strings.TrimSuffix(remote.URL, "/") == strings.TrimSuffix(strings.TrimSuffix(r.URL, "flathub.flatpakrepo"), "/") {
		if reason := FlatpakKeyDrift(r, remote); reason != "" {
			op.Blocked = "system remote " + id + ": " + reason + "; inspect and correct its signing keys with the native Flatpak tools before retrying"
			b.blockedRepo[id] = op.Blocked
			return op, true
		}
		b.ready[id] = true
		return Operation{}, false
	}
	op.Blocked = fmt.Sprintf("system remote %s points to %s, not the declared %s; remove or fix it first", id, remote.URL, r.URL)
	b.blockedRepo[id] = op.Blocked
	return op, true
}

func flatpakRepoID(root definitions.Root) string {
	for id, r := range root.Repositories {
		if r.Kind == "flatpak" {
			return id
		}
	}
	return ""
}

func (b *builder) flatpaks() []Operation {
	var ops []Operation
	installed := map[string]inspect.FlatpakApp{}
	if b.in.Facts.Flatpak.Known() {
		for _, app := range b.in.Facts.Flatpak.Value.Apps {
			installed[app.ID] = app
		}
	}
	remote := flatpakRepoID(b.in.Root)
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix != definitions.PrefixFlatpak {
			continue
		}
		if app, ok := installed[p.Name]; ok {
			op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt Flatpak %s %s from %s, already installed", p.Name, app.Version, app.Origin), Paths: p.Paths}
			if _, managed := b.in.Applied.Receipts[op.ID]; managed {
				op.Action, op.Summary = ActionKeep, fmt.Sprintf("Flatpak %s %s is managed and unchanged", p.Name, app.Version)
			} else if app.Origin != remote {
				op.Notes = append(op.Notes, fmt.Sprintf("installed from remote %s, not %s", app.Origin, remote))
			}
			ops = append(ops, op)
			continue
		}
		op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionInstall, Risk: RiskLow,
			Summary: "install Flatpak " + p.Name, Paths: p.Paths,
			Steps: []Step{{Description: "install from the system remote", Argv: []string{"flatpak", "install", "--system", "--noninteractive", remote, p.Name}, Privileged: true}}}
		// The command is exact without a preview, so the application runs
		// in the same round as its remote; it waits only when the remote
		// itself waits for the flatpak package, or is blocked.
		if !b.ready[remote] && (b.pendingRepo[remote] || b.blockedRepo[remote] != "") {
			op.After, op.Blocked = b.waitOrBlock(remote, KindFlatpakRemote)
		}
		ops = append(ops, op)
	}
	return ops
}

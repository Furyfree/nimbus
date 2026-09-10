package apply

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// Upgrade brings the installed system current through the native tools,
// with their output on the terminal: dnf5 upgrade, and flatpak update when
// a Flatpak remote is declared and the tool is present. Upgrades change no
// ownership, so they record no receipts.
func Upgrade(opts Options, root definitions.Root) *Result {
	r := &Result{}
	ex := &executor{opts: opts}
	ex.opts.Out = cmp.Or(ex.opts.Out, io.Discard)
	if err := ex.snapshotPackages(); err != nil {
		r.Failed, r.Error = "upgrade:dnf", "verification before upgrade: "+err.Error()
		r.Failures = append(r.Failures, Failure{ID: r.Failed, Error: r.Error})
		return r
	}
	installed, err := ex.packageTransaction([]string{"dnf5", "-y", "upgrade"}, opts.UpgradePreview)
	if err == nil {
		for _, c := range opts.Constraints {
			found := false
			for _, p := range installed {
				if p.Name != c.Name {
					continue
				}
				found = true
				if !c.Matches(p.EVR()) {
					err = errors.Join(err, fmt.Errorf("%s remains at %s outside %s; a compatible package or explicit migration is required", p.Name, p.EVR(), c.Family))
				}
			}
			if !found {
				err = errors.Join(err, fmt.Errorf("constrained package %s is missing after upgrade", c.Name))
			}
		}
	}
	r.Differences = append(r.Differences, ex.differences...)
	if err != nil {
		r.Failed, r.Error = "upgrade:dnf", err.Error()
		r.Failures = append(r.Failures, Failure{ID: r.Failed, Error: r.Error})
	} else {
		r.Executed = append(r.Executed, "upgrade:dnf")
	}
	for repo := range maps.Values(root.Repositories) {
		if repo.Kind != "flatpak" {
			continue
		}
		if _, err := opts.Source.LookPath("flatpak"); err != nil {
			r.Pending = append(r.Pending, "upgrade:flatpak")
			break
		}
		before := inspect.SystemFlatpak(opts.Source)
		if !before.Known() {
			r.Failures = append(r.Failures, Failure{ID: "upgrade:flatpak", Error: "Flatpak verification before update: " + before.Error})
			r.Pending = append(r.Pending, "upgrade:flatpak")
			if r.Error == "" {
				r.Failed, r.Error = "upgrade:flatpak", "Flatpak verification before update: "+before.Error
			}
			break
		}
		nativeErr := ex.sudo("flatpak", "update", "--system", "--noninteractive")
		after := inspect.SystemFlatpak(opts.Source)
		var verifyErr error
		if !after.Known() {
			verifyErr = errors.New("Flatpak verification incomplete: " + after.Error)
			r.Differences = append(r.Differences, verifyErr.Error())
		} else {
			old := map[string]inspect.FlatpakApp{}
			for _, app := range before.Value.Apps {
				old[app.ID] = app
			}
			for _, app := range after.Value.Apps {
				previous, existed := old[app.ID]
				if !existed || previous.Version != app.Version || previous.Origin != app.Origin {
					r.Differences = append(r.Differences, fmt.Sprintf("Flatpak updated %s: %s (%s) -> %s (%s)", app.ID, previous.Version, previous.Origin, app.Version, app.Origin))
				}
				delete(old, app.ID)
			}
			for _, id := range slices.Sorted(maps.Keys(old)) {
				r.Differences = append(r.Differences, "Flatpak removed "+id)
			}
		}
		if err := errors.Join(nativeErr, verifyErr); err != nil {
			r.Failures = append(r.Failures, Failure{ID: "upgrade:flatpak", Error: err.Error()})
			if r.Error == "" {
				r.Failed, r.Error = "upgrade:flatpak", err.Error()
			} else {
				r.Error += "; Flatpak update: " + err.Error()
			}
		} else {
			r.Executed = append(r.Executed, "upgrade:flatpak")
		}
		break
	}
	return r
}

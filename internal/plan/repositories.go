package plan

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// DNFRepoIDs returns the repository IDs a declared repository creates on the
// host. Nimbus writes nimbus-<id>.repo itself; a release package and a COPR
// use the IDs their own tooling creates.
func DNFRepoIDs(id string, r definitions.Repository) []string {
	switch r.Kind {
	case "dnf":
		if r.ReleasePackage != "" {
			return []string{id, id + "-updates"}
		}
		return []string{"nimbus-" + id}
	case "copr":
		owner, project, _ := strings.Cut(r.Project, "/")
		return []string{"copr:copr.fedorainfracloud.org:" + owner + ":" + project}
	}
	return nil
}

// fedoraRepos are the host repository IDs a bare Fedora package may come
// from.
var fedoraRepos = []string{"fedora", "updates", "updates-testing", "fedora-cisco-openh264"}

// expectedRepos returns the host repository IDs a package with this prefix
// may come from.
func (b *builder) expectedRepos(prefix string) []string {
	if prefix == definitions.PrefixDNF {
		return fedoraRepos
	}
	return DNFRepoIDs(prefix, b.in.Root.Repositories[prefix])
}

// inspectRepo decides whether the host already provides a declared DNF or
// COPR repository as declared. It returns ready, a repair description for
// a Nimbus-owned file that drifted, or a blocking problem.
func (b *builder) inspectRepo(id string, r definitions.Repository) (ready bool, repair string, blocked string) {
	ids := DNFRepoIDs(id, r)
	var enabled []inspect.Repository
	for _, host := range ids {
		for _, have := range b.repos[host] {
			if have.Enabled {
				enabled = append(enabled, have)
			}
		}
	}
	if len(enabled) == 0 {
		// An earlier retirement may have retained the native files while
		// disabling them. Check their identity through the normal inspector
		// before proposing an explicit enable/key reconciliation.
		virtual := *b
		virtual.repos = make(map[string][]inspect.Repository, len(b.repos))
		found := false
		for host, repositories := range b.repos {
			virtual.repos[host] = slices.Clone(repositories)
			if slices.Contains(ids, host) {
				for i := range virtual.repos[host] {
					virtual.repos[host][i].Enabled = true
					found = true
				}
			}
		}
		if found {
			_, _, blocked := virtual.inspectRepo(id, r)
			if blocked != "" {
				return false, "", blocked
			}
			return false, "reconcile signing key: repository is disabled", ""
		}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			if i := slices.IndexFunc(b.repos[id], func(have inspect.Repository) bool { return have.Enabled }); i >= 0 {
				return false, "", fmt.Sprintf("repository %s is already enabled through %s, which Nimbus does not own; remove that file or keep the repository unmanaged", id, b.repos[id][i].File)
			}
		}
		return false, "", ""
	}
	var drift []string
	var keyDrift []string
	for _, have := range enabled {
		for _, key := range []string{"baseurl", "metalink", "mirrorlist", "sslverify"} {
			if value, overridden := have.OverrideOptions[key]; overridden {
				want := have.Options[key]
				if r.Kind == "dnf" && r.ReleasePackage == "" {
					want = ""
					if key == "baseurl" {
						want = r.BaseURL
					}
				}
				if key == "sslverify" {
					want = "1"
					switch strings.ToLower(value) {
					case "true", "yes":
						value = "1"
					}
				}
				if value != want {
					return false, "", fmt.Sprintf("repository %s has a conflicting %s override in %s; correct the native override before retrying", have.ID, key, strings.Join(have.Overrides, ", "))
				}
			}
		}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			if have.File != "nimbus-"+id+".repo" {
				return false, "", fmt.Sprintf("repository %s is provided by %s, not by nimbus-%s.repo, which Nimbus would own; remove that file first", have.ID, have.File, id)
			}
			if reason := RepositoryKeyDrift(id, r, have); reason != "" {
				keyDrift = append(keyDrift, have.ID+": "+reason)
			}
			if fileDrift := ownedFileDrift(id, r, have); len(fileDrift) > 0 {
				drift = append(drift, fileDrift...)
				continue
			}
		}
		if r.Kind != "dnf" || r.ReleasePackage != "" {
			if reason := RepositoryKeyDrift(id, r, have); reason != "" {
				keyDrift = append(keyDrift, have.ID+": "+reason)
			}
		}
		// The effective values include overrides below repos.override.d,
		// which is where a maker's file is corrected and where a file
		// Nimbus owns could still be changed from outside.
		if have.GPGCheck != "1" {
			drift = append(drift, have.ID+" has gpgcheck="+have.GPGCheck)
		}
		if r.Priority != nil && have.Priority != strconv.Itoa(*r.Priority) {
			drift = append(drift, fmt.Sprintf("%s has priority %q, declared %d", have.ID, have.Priority, *r.Priority))
		}
	}
	if r.Kind == "dnf" && r.ReleasePackage == "" {
		for _, host := range slices.Sorted(maps.Keys(b.repos)) {
			if slices.Contains(ids, host) {
				continue
			}
			for _, have := range b.repos[host] {
				if have.Enabled && have.BaseURL == r.BaseURL {
					b.duplicates[id] = append(b.duplicates[id], host)
					drift = append(drift, fmt.Sprintf("%s from %s also serves the declared baseurl and is disabled through an override", host, have.File))
				}
			}
		}
	}
	if len(keyDrift) > 0 {
		return false, "reconcile signing key: " + strings.Join(append(keyDrift, drift...), "; "), ""
	}
	if len(drift) > 0 {
		switch {
		case len(b.duplicates[id]) == len(drift):
			return false, strings.Join(drift, "; "), ""
		case r.Kind == "dnf" && r.ReleasePackage == "":
			return false, "rewrite nimbus-" + id + ".repo: " + strings.Join(drift, "; "), ""
		}
		return false, "correct the repository file: " + strings.Join(drift, "; "), ""
	}
	return true, "", ""
}

// disableDuplicateSteps renders the override that disables each host
// repository serving a declared baseurl under another ID.
func (b *builder) disableDuplicateSteps(id string) []Step {
	var opts []string
	for _, host := range b.duplicates[id] {
		opts = append(opts, host+".enabled=0")
	}
	if len(opts) == 0 {
		return nil
	}
	return []Step{{Description: DisableDuplicateDescription, Argv: append([]string{"dnf5", "config-manager", "setopt"}, opts...), Privileged: true}}
}

// DisableDuplicateDescription marks the override step that disables a
// duplicate host repository; apply runs it after the repository's own steps.
const DisableDuplicateDescription = "disable the duplicate repository through a DNF override"

// IsReleasePackage reports whether an installed package is the release
// package of a declared repository, which Nimbus installs while enabling
// it and which therefore belongs to that repository, never to prune.
func IsReleasePackage(root definitions.Root, name string) bool {
	for r := range maps.Values(root.Repositories) {
		if r.ReleasePackage != "" && ReleasePackageName(r.ReleasePackage) == name {
			return true
		}
	}
	return false
}

// ReleasePackageName derives the package name from a release package URL
// such as .../rpmfusion-free-release-44.noarch.rpm: the file name without
// .rpm, the architecture, and the version and release fields, which are
// the trailing dash-separated fields that start with a digit.
func ReleasePackageName(url string) string {
	name := strings.TrimSuffix(path.Base(url), ".rpm")
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	for range 2 {
		i := strings.LastIndexByte(name, '-')
		if i <= 0 || i+1 >= len(name) || name[i+1] < '0' || name[i+1] > '9' {
			break
		}
		name = name[:i]
	}
	return name
}

func (b *builder) repositories() []Operation {
	var ops []Operation
	for _, id := range b.in.Resolved.Repositories {
		r := b.in.Root.Repositories[id]
		if r.Kind == "flatpak" {
			if op, missing := b.flatpakRemote(id, r); missing {
				ops = append(ops, op)
			}
			continue
		}
		ready, repair, blocked := b.inspectRepo(id, r)
		if ready {
			b.ready[id] = true
			continue
		}
		op := Operation{
			ID: "repository:" + id, Kind: KindRepository, Action: ActionEnable, Risk: RiskMedium,
			Summary: fmt.Sprintf("enable repository %s (%s)", id, repoLocation(r)),
			Paths:   b.repoPaths(id),
			Steps:   repositorySteps(id, r),
		}
		switch {
		case blocked != "":
			op.Blocked = blocked
			b.blockedRepo[id] = blocked
		case repair != "":
			op.Action, op.Summary = ActionRepair, fmt.Sprintf("repair repository %s: %s", id, repair)
			switch {
			case strings.HasPrefix(repair, "reconcile signing key:"):
				op.Steps = repositorySteps(id, r)
				if r.Kind == "dnf" && r.ReleasePackage == "" {
					op.Steps[len(op.Steps)-1] = AddRepoStep(id, r, true)
					op.Steps = append(op.Steps, PrioritySteps(id, r)...)
				} else {
					var steps []Step
					for _, step := range op.Steps {
						if len(step.Argv) > 1 && step.Argv[0] == "dnf5" && (step.Argv[1] == "install" || step.Argv[1] == "copr") {
							continue
						}
						steps = append(steps, step)
					}
					op.Steps = steps
				}
			case strings.HasPrefix(repair, "rewrite "):
				op.Steps = []Step{AddRepoStep(id, r, true)}
			case strings.HasPrefix(repair, "correct "):
				op.Steps = PrioritySteps(id, r)
			default:
				op.Steps = nil
			}
			op.Steps = append(op.Steps, b.disableDuplicateSteps(id)...)
		}
		if op.Blocked == "" {
			b.repoChanges = append(b.repoChanges, op.ID)
		}
		op.Source = b.sourceOwnership(op, DNFRepoIDs(id, r))
		ops = append(ops, op)
	}
	return ops
}

func repoLocation(r definitions.Repository) string {
	switch {
	case r.ReleasePackage != "":
		return r.ReleasePackage
	case r.Kind == "copr":
		return "copr " + r.Project
	default:
		return r.BaseURL
	}
}

// KeyPath is where a verified key for a Nimbus-owned repository lives.
func KeyPath(id string) string { return "/etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus-" + id }

// PrioritySteps renders dnf5 config-manager setopt for every host ID of a
// repository, which writes an override file instead of touching the
// maker's repository file. The override enables signature checking and
// points to the verified local key that the enable or key repair installed.
func PrioritySteps(id string, r definitions.Repository) []Step {
	var opts []string
	for _, host := range DNFRepoIDs(id, r) {
		opts = append(opts, host+".enabled=1", host+".gpgcheck=1", host+".gpgkey=file://"+KeyPath(id))
		if r.Priority != nil {
			opts = append(opts, fmt.Sprintf("%s.priority=%d", host, *r.Priority))
		}
	}
	return []Step{{Description: "set signature checking, the verified local key and priority through a DNF override", Argv: append([]string{"dnf5", "config-manager", "setopt"}, opts...), Privileged: true}}
}

// CheckRepository decides whether the host provides a declared DNF or COPR
// repository as declared. Apply uses it to verify an enable or repair.
func CheckRepository(root definitions.Root, id string, repos []inspect.Repository) (ready bool, repair string, blocked string) {
	b := &builder{in: Inputs{Root: root}, repos: map[string][]inspect.Repository{}, duplicates: map[string][]string{}}
	for _, r := range repos {
		b.repos[r.ID] = append(b.repos[r.ID], r)
	}
	return b.inspectRepo(id, root.Repositories[id])
}

// RepoOption is one key of the repository file Nimbus owns.
type RepoOption struct{ Key, Value string }

// OwnedRepoOptions is the complete content of nimbus-<id>.repo: what
// AddRepoStep writes and what inspectRepo verifies, so the two cannot
// drift apart. A key the file has beyond these is drift too.
func OwnedRepoOptions(id string, r definitions.Repository) []RepoOption {
	opts := []RepoOption{
		{"name", id + " (Nimbus)"},
		{"enabled", "1"},
		{"baseurl", r.BaseURL},
		{"gpgcheck", "1"},
		{"gpgkey", "file://" + KeyPath(id)},
	}
	if r.Priority != nil {
		opts = append(opts, RepoOption{"priority", strconv.Itoa(*r.Priority)})
	}
	return opts
}

// ownedFileDrift compares a section of nimbus-<id>.repo with what Nimbus
// would write.
func ownedFileDrift(id string, r definitions.Repository, have inspect.Repository) []string {
	var drift []string
	expected := map[string]bool{}
	for _, o := range OwnedRepoOptions(id, r) {
		expected[o.Key] = true
		switch got, ok := have.Options[o.Key]; {
		case !ok:
			drift = append(drift, fmt.Sprintf("%s lacks %s=%s", have.File, o.Key, o.Value))
		case got != o.Value:
			drift = append(drift, fmt.Sprintf("%s has %s=%s, declared %s", have.File, o.Key, got, o.Value))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(have.Options)) {
		if !expected[key] {
			drift = append(drift, fmt.Sprintf("%s has %s=%s, which Nimbus does not write", have.File, key, have.Options[key]))
		}
	}
	return drift
}

// AddRepoStep renders the native command that writes nimbus-<id>.repo.
func AddRepoStep(id string, r definitions.Repository, overwrite bool) Step {
	argv := []string{"dnf5", "config-manager", "addrepo", "--id=nimbus-" + id}
	for _, o := range OwnedRepoOptions(id, r) {
		argv = append(argv, "--set="+o.Key+"="+o.Value)
	}
	if overwrite {
		argv = append(argv, "--overwrite")
	}
	return Step{Description: "write /etc/yum.repos.d/nimbus-" + id + ".repo through DNF", Argv: argv, Privileged: true}
}

// repositorySteps renders the exact enabling steps per repository kind.
// Placeholders in angle brackets are paths apply fills in from its stage
// directory after the internal verification step succeeds.
func repositorySteps(id string, r definitions.Repository) []Step {
	fp := definitions.NormalizeFingerprint(r.Key)
	switch {
	case r.Kind == "copr":
		return append([]Step{
			{Description: "download the COPR key and verify its fingerprint is " + fp, Argv: append([]string{"gpg"}, inspect.KeyInspectArgs("<verified key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<verified key>", KeyPath(id)}, Privileged: true},
			{Description: "import the verified key into the RPM database", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			{Description: "enable the COPR through DNF", Argv: []string{"dnf5", "copr", "enable", "-y", r.Project}, Privileged: true},
		}, PrioritySteps(id, r)...)
	case r.ReleasePackage != "":
		return append([]Step{
			{Description: "download " + r.ReleasePackage + " and verify sha256 " + r.SHA256},
			{Description: "unpack the verified package to find its key", Argv: []string{"rpm2archive", "<verified package>"}},
			{Description: "verify the extracted key fingerprint is " + fp, Argv: append([]string{"gpg"}, inspect.KeyInspectArgs("<extracted key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<extracted key>", KeyPath(id)}, Privileged: true},
			{Description: "import the verified key", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			{Description: "install the release package with signature checking on", Argv: []string{"dnf5", "install", "-y", "<verified package>"}, Privileged: true},
		}, PrioritySteps(id, r)...)
	default:
		keySource := "download " + r.KeyURL
		if r.KeyFile != "" {
			keySource = "read system/" + r.KeyFile + " from the checkout"
		}
		return []Step{
			{Description: keySource + " and verify its fingerprint is " + fp, Argv: append([]string{"gpg"}, inspect.KeyInspectArgs("<verified key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<verified key>", KeyPath(id)}, Privileged: true},
			{Description: "import the key into the RPM database", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			AddRepoStep(id, r, false),
		}
	}
}

func (b *builder) repoPaths(id string) []string {
	var paths []string
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == id || (p.Prefix == definitions.PrefixFlatpak && b.in.Root.Repositories[id].Kind == "flatpak") {
			paths = append(paths, p.Canonical)
		}
	}
	return paths
}

// waitOrBlock returns the After or Blocked value for a package whose
// repository is not ready.
func (b *builder) waitOrBlock(repoID, opKind string) (after, blocked string) {
	if msg, ok := b.blockedRepo[repoID]; ok {
		return "", "repository " + repoID + " cannot be used: " + msg
	}
	return opKind + ":" + repoID, ""
}

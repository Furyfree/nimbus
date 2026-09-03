package apply

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// Options are everything the executor needs beyond the plan. Every side
// effect goes through one of these so tests replace them.
type Options struct {
	Source facts.Source
	// Fetch downloads a URL. Apply is the one command allowed to reach the
	// network for keys, release packages, and remote definitions.
	Fetch func(url string) ([]byte, error)
	// Record is the privileged record action: the real one runs
	// `sudo nimbus internal record`; tests write to a temporary root.
	Record func(planDigest string, st *state.Stage) error
	// Keys extracts the key files from a downloaded release RPM. The real
	// one runs rpm2archive through Source and reads the archive.
	Keys func(rpmPath string) (map[string][]byte, error)
	// Stage is a user-owned directory for verified downloads, below the
	// runtime directory.
	Stage string
	// Checkout supplies key files stored in the definition tree.
	Checkout *definitions.Checkout
	// Root is the checkout's definitions, for repository declarations.
	Root definitions.Root
	// FirstApply records the baseline of installed packages with the first
	// receipt, marking everything present now as pre-existing.
	FirstApply  bool
	Engine      string
	Definitions state.Definitions
	Out         io.Writer
	Now         func() time.Time
}

// Result reports what ran. A failure stops the run; receipts of completed
// operations stay.
type Result struct {
	Executed []string `json:"executed"`
	Pending  []string `json:"pending"`
	Failed   string   `json:"failed,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Run executes every runnable operation of a complete plan in order. It
// assumes the caller holds the operation lock and has re-verified the plan
// digest immediately before.
func Run(p *plan.Plan, opts Options) *Result {
	r := &Result{}
	if !p.Complete {
		r.Error = "the plan is incomplete; resolve its blocked operations first"
		return r
	}
	ex := &executor{p: p, opts: opts, seen: map[string]facts.Package{}}
	if ex.opts.Now == nil {
		ex.opts.Now = time.Now
	}
	if ex.opts.Out == nil {
		ex.opts.Out = io.Discard
	}
	if err := ex.snapshotPackages(); err != nil {
		r.Error = err.Error()
		return r
	}
	for _, op := range p.Operations {
		if op.After != "" {
			r.Pending = append(r.Pending, op.ID)
			continue
		}
		if op.Action == plan.ActionKeep {
			continue
		}
		fmt.Fprintf(ex.opts.Out, "-> %s\n", op.Summary)
		receipts, remove, err := ex.execute(op)
		if err != nil {
			r.Failed = op.ID
			r.Error = err.Error()
			fmt.Fprintf(ex.opts.Out, "   failed: %v\n", err)
			return r
		}
		st := &state.Stage{Schema: state.Schema, PlanDigest: p.Digest, Receipts: receipts, Remove: remove, Time: ex.opts.Now().UTC()}
		if ex.opts.FirstApply && !ex.baselineDone {
			st.Baseline = &state.Baseline{Schema: state.Schema, Recorded: st.Time, Packages: ex.baseline()}
			ex.baselineDone = true
		}
		if len(receipts) > 0 || len(remove) > 0 || st.Baseline != nil {
			if err := ex.opts.Record(p.Digest, st); err != nil {
				r.Failed = op.ID
				r.Error = "record receipt: " + err.Error()
				return r
			}
		}
		r.Executed = append(r.Executed, op.ID)
	}
	return r
}

type executor struct {
	p            *plan.Plan
	opts         Options
	seen         map[string]facts.Package // installed packages at start
	baselineDone bool
}

func (ex *executor) snapshotPackages() error {
	pkgs, err := ex.installed()
	if err != nil {
		return err
	}
	for _, p := range pkgs {
		ex.seen[p.Name] = p
	}
	return nil
}

func (ex *executor) baseline() []string {
	names := make([]string, 0, len(ex.seen))
	for name := range ex.seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// installed re-reads the installed packages through the same query facts
// uses, so verification sees the real result of a transaction.
func (ex *executor) installed() ([]facts.Package, error) {
	f := facts.Inspect(ex.opts.Source, "")
	if !f.Packages.Known() {
		return nil, errors.New("installed packages are unknown: " + f.Packages.Error)
	}
	return f.Packages.Value, nil
}

// sudo runs a privileged native command exactly as the plan showed it.
func (ex *executor) sudo(argv ...string) error {
	fmt.Fprintf(ex.opts.Out, "   $ sudo %s\n", strings.Join(argv, " "))
	_, err := ex.opts.Source.Run("sudo", argv...)
	return err
}

func (ex *executor) receipt(op plan.Operation, provider, previous, intended, verification string) state.Receipt {
	return state.Receipt{Schema: state.Schema, Engine: ex.opts.Engine, Definitions: ex.opts.Definitions, Machine: ex.p.Machine,
		Resource: op.ID, Provider: provider, Paths: op.Paths, Previous: previous, Intended: intended, Operation: op.Action,
		PlanDigest: ex.p.Digest, Verified: true, Verification: verification, Timestamp: ex.opts.Now().UTC()}
}

func (ex *executor) execute(op plan.Operation) (receipts []state.Receipt, remove []string, err error) {
	switch {
	case op.Kind == plan.KindRepository:
		return ex.repository(op)
	case op.Kind == plan.KindFlatpakRemote:
		return ex.flatpakRemote(op)
	case op.Kind == plan.KindFlatpak && op.Action == plan.ActionAdopt:
		return []state.Receipt{ex.receipt(op, "flatpak", "installed", "installed", "flatpak list shows the application")}, nil, nil
	case op.Kind == plan.KindFlatpak && op.Action == plan.ActionInstall:
		return ex.flatpakInstall(op)
	case op.Kind == plan.KindFlatpak && op.Action == plan.ActionRemove:
		return ex.flatpakRemove(op)
	case op.Kind == plan.KindPackage && op.Action == plan.ActionRetire:
		return nil, []string{op.ID}, nil
	case op.Kind == plan.KindPackage && op.Action == plan.ActionAdopt:
		name := op.ID[strings.LastIndexByte(op.ID, ':')+1:]
		inst, ok := ex.seen[name]
		if !ok {
			return nil, nil, fmt.Errorf("%s is no longer installed", name)
		}
		return []state.Receipt{ex.receipt(op, "dnf", "installed "+inst.EVR()+" ("+inst.FromRepo+")", "installed", "dnf5 repoquery --installed lists it")}, nil, nil
	case op.ID == "packages:install":
		return ex.installTransaction(op)
	case op.Kind == plan.KindPackage && (op.Action == plan.ActionRemove || op.Action == plan.ActionPrune):
		return ex.removeTransaction(op)
	}
	return nil, nil, fmt.Errorf("operation %s (%s %s) has no executor", op.ID, op.Kind, op.Action)
}

// --- keys and repositories -------------------------------------------------

// fingerprint runs gpg over a key file through Source and returns the
// primary fingerprint.
func (ex *executor) fingerprint(path string) (string, error) {
	out, err := ex.opts.Source.Run("gpg", "--batch", "--show-keys", "--with-colons", path)
	if err != nil {
		return "", fmt.Errorf("gpg: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" {
			return strings.ToUpper(fields[9]), nil
		}
	}
	return "", errors.New("gpg printed no fingerprint")
}

// verifiedKey writes key material to the stage and checks its fingerprint.
func (ex *executor) verifiedKey(name string, data []byte, want string) (string, error) {
	path := filepath.Join(ex.opts.Stage, name)
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	got, err := ex.fingerprint(path)
	if err != nil {
		return "", err
	}
	if got != definitions.NormalizeFingerprint(want) {
		os.Remove(path)
		return "", fmt.Errorf("key fingerprint %s does not match the declared %s", got, definitions.NormalizeFingerprint(want))
	}
	return path, nil
}

func (ex *executor) repository(op plan.Operation) ([]state.Receipt, []string, error) {
	id := strings.TrimPrefix(op.ID, "repository:")
	r, ok := ex.opts.Root.Repositories[id]
	if !ok {
		return nil, nil, fmt.Errorf("repository %s is not declared", id)
	}
	var err error
	switch {
	case r.Kind == "copr":
		err = ex.enableCOPR(id, r, op)
	case r.ReleasePackage != "":
		err = ex.enableReleasePackage(id, r, op)
	default:
		err = ex.enableBaseURL(id, r, op)
	}
	if err != nil {
		return nil, nil, err
	}
	f := facts.Inspect(ex.opts.Source, "")
	if !f.Repositories.Known() {
		return nil, nil, errors.New("verification: repositories are unknown: " + f.Repositories.Error)
	}
	ready, repair, blocked := plan.CheckRepository(ex.opts.Root, id, f.Repositories.Value)
	if !ready {
		reason := repair
		if blocked != "" {
			reason = blocked
		}
		if reason == "" {
			reason = "not enabled"
		}
		return nil, nil, fmt.Errorf("verification: repository %s is not as declared after the operation: %s", id, reason)
	}
	return []state.Receipt{ex.receipt(op, "repository", "absent", "enabled with key "+definitions.NormalizeFingerprint(r.Key), "repository present, enabled, signature checking on, priority as declared")}, nil, nil
}

func (ex *executor) enableBaseURL(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair {
		return ex.sudo(plan.AddRepoStep(id, r, true).Argv...)
	}
	var data []byte
	var err error
	if r.KeyFile != "" {
		entry, ok := ex.opts.Checkout.Entry("system/" + r.KeyFile)
		if !ok {
			return fmt.Errorf("key file system/%s is missing from the checkout", r.KeyFile)
		}
		data = entry.Content
	} else {
		data, err = ex.opts.Fetch(r.KeyURL)
		if err != nil {
			return fmt.Errorf("download key: %w", err)
		}
	}
	key, err := ex.verifiedKey("key-"+id+".asc", data, r.Key)
	if err != nil {
		return err
	}
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	return ex.sudo(plan.AddRepoStep(id, r, false).Argv...)
}

func (ex *executor) enableReleasePackage(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	data, err := ex.opts.Fetch(r.ReleasePackage)
	if err != nil {
		return fmt.Errorf("download release package: %w", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != r.SHA256 {
		return fmt.Errorf("release package sha256 %s does not match the declared %s", got, r.SHA256)
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return err
	}
	rpm := filepath.Join(ex.opts.Stage, id+"-release.rpm")
	if err := os.WriteFile(rpm, data, 0o600); err != nil {
		return err
	}
	keys, err := ex.opts.Keys(rpm)
	if err != nil {
		return fmt.Errorf("extract keys: %w", err)
	}
	var key string
	for name, content := range keys {
		if path, err := ex.verifiedKey("key-"+id+"-"+filepath.Base(name), content, r.Key); err == nil {
			key = path
			break
		}
	}
	if key == "" {
		return fmt.Errorf("no key in %s has the declared fingerprint %s", r.ReleasePackage, definitions.NormalizeFingerprint(r.Key))
	}
	if err := ex.sudo("rpm", "--import", key); err != nil {
		return err
	}
	if err := ex.sudo("dnf5", "install", "-y", rpm); err != nil {
		return err
	}
	return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
}

func (ex *executor) enableCOPR(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	data, err := ex.opts.Fetch("https://download.copr.fedorainfracloud.org/results/" + r.Project + "/pubkey.gpg")
	if err != nil {
		return fmt.Errorf("download COPR key: %w", err)
	}
	if _, err := ex.verifiedKey("key-"+id+".gpg", data, r.Key); err != nil {
		return err
	}
	if err := ex.sudo("dnf5", "copr", "enable", "-y", r.Project); err != nil {
		return err
	}
	return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
}

// ExtractKeysWithRPM2Archive is the real key extractor: rpm2archive writes
// <rpm>.tgz beside the package, and the key files live below
// ./etc/pki/rpm-gpg/ inside it.
func ExtractKeysWithRPM2Archive(src facts.Source) func(string) (map[string][]byte, error) {
	return func(rpmPath string) (map[string][]byte, error) {
		if _, err := src.Run("rpm2archive", rpmPath); err != nil {
			return nil, err
		}
		return readKeysFromArchive(rpmPath + ".tgz")
	}
}

func (ex *executor) flatpakRemote(op plan.Operation) ([]state.Receipt, []string, error) {
	id := strings.TrimPrefix(op.ID, "flatpak-remote:")
	r := ex.opts.Root.Repositories[id]
	data, err := ex.opts.Fetch(r.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("download remote definition: %w", err)
	}
	encoded := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "GPGKey=") {
			encoded = strings.TrimPrefix(line, "GPGKey=")
		}
	}
	if encoded == "" {
		return nil, nil, errors.New("the remote definition carries no GPGKey")
	}
	keyData, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("remote key: %w", err)
	}
	if _, err := ex.verifiedKey("remote-"+id+".gpg", keyData, r.Key); err != nil {
		return nil, nil, err
	}
	repoFile := filepath.Join(ex.opts.Stage, id+".flatpakrepo")
	if err := os.WriteFile(repoFile, data, 0o600); err != nil {
		return nil, nil, err
	}
	if err := ex.sudo("flatpak", "remote-add", "--if-not-exists", "--system", "--from", id, repoFile); err != nil {
		return nil, nil, err
	}
	f := facts.Inspect(ex.opts.Source, "")
	if !f.Flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + f.Flatpak.Error)
	}
	for _, remote := range f.Flatpak.Value.Remotes {
		if remote.Name == id {
			return []state.Receipt{ex.receipt(op, "flatpak-remote", "absent", "present with key "+definitions.NormalizeFingerprint(r.Key), "flatpak remotes lists it")}, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("verification: remote %s is not present after the operation", id)
}

// --- packages --------------------------------------------------------------

// storedTransaction mirrors DNF5's transaction.json.
type storedTransaction struct {
	RPMs []struct {
		NEVRA  string `json:"nevra"`
		Action string `json:"action"`
	} `json:"rpms"`
}

func (ex *executor) installTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Transaction == nil || len(op.Steps) < 3 {
		return nil, nil, errors.New("the install operation carries no reviewed transaction")
	}
	store, replay, cleanup := op.Steps[0].Argv, op.Steps[1].Argv, op.Steps[2].Argv
	stage := replay[len(replay)-1]
	if err := ex.sudo(store...); err != nil {
		return nil, nil, err
	}
	data, err := ex.opts.Source.ReadFile(filepath.Join(stage, "transaction.json"))
	if err != nil {
		_ = ex.sudo(cleanup...)
		return nil, nil, fmt.Errorf("read stored transaction: %w", err)
	}
	var stored storedTransaction
	if err := json.Unmarshal(data, &stored); err != nil {
		_ = ex.sudo(cleanup...)
		return nil, nil, fmt.Errorf("stored transaction: %w", err)
	}
	if err := compareStored(op.Transaction, stored); err != nil {
		_ = ex.sudo(cleanup...)
		return nil, nil, err
	}
	if err := ex.sudo(replay...); err != nil {
		_ = ex.sudo(cleanup...)
		return nil, nil, err
	}
	if err := ex.sudo(cleanup...); err != nil {
		return nil, nil, err
	}
	installed, err := ex.installed()
	if err != nil {
		return nil, nil, err
	}
	have := map[string]facts.Package{}
	for _, p := range installed {
		have[p.Name] = p
	}
	var receipts []state.Receipt
	for _, canonical := range op.Paths {
		name := canonical[strings.LastIndexByte(canonical, ':')+1:]
		inst, ok := have[name]
		if !ok {
			return nil, nil, fmt.Errorf("verification: %s is not installed after the transaction", name)
		}
		sub := plan.Operation{ID: "package:" + canonical, Action: plan.ActionInstall, Paths: pathsFor(ex.p, "package:"+canonical)}
		receipts = append(receipts, ex.receipt(sub, "dnf", "absent", "installed "+inst.EVR(), "dnf5 repoquery --installed lists "+inst.EVR()))
	}
	return receipts, nil, nil
}

// compareStored checks that the stored transaction is exactly the reviewed
// one: the same complete RPM identities (name, epoch:version-release, arch)
// with the same actions, counted, so a changed build, a different
// architecture, or a duplicated multilib entry is caught before replay.
func compareStored(preview *plan.Transaction, stored storedTransaction) error {
	want := map[string]int{}
	for _, row := range preview.Packages {
		want[identity(row.Name, row.EVR, row.Arch, actionOf(row.Section))]++
	}
	got := map[string]int{}
	for _, rpm := range stored.RPMs {
		name, evr, arch := splitNEVRA(rpm.NEVRA)
		got[identity(name, evr, arch, strings.ToLower(rpm.Action))]++
	}
	var diffs []string
	for id, n := range want {
		if g := got[id]; g != n {
			diffs = append(diffs, fmt.Sprintf("reviewed %s x%d, stored x%d", id, n, g))
		}
	}
	for id, n := range got {
		if _, ok := want[id]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s x%d appeared in the stored transaction without review", id, n))
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		return errors.New("the downloaded transaction differs from the reviewed plan: " + strings.Join(diffs, "; "))
	}
	return nil
}

// identity renders one comparable RPM identity. A zero epoch is dropped,
// since DNF's NEVRA omits it while the preview prints it.
func identity(name, evr, arch, action string) string {
	return action + " " + name + "-" + strings.TrimPrefix(evr, "0:") + "." + arch
}

// splitNEVRA separates name, epoch:version-release, and arch. Version and
// release never contain a dash, so the last two dashes delimit them.
func splitNEVRA(nevra string) (name, evr, arch string) {
	s := nevra
	if i := strings.LastIndexByte(s, '.'); i > 0 {
		arch, s = s[i+1:], s[:i]
	}
	if i := strings.LastIndexByte(s, '-'); i > 0 {
		release := s[i+1:]
		s = s[:i]
		if j := strings.LastIndexByte(s, '-'); j > 0 {
			return s[:j], s[j+1:] + "-" + release, arch
		}
	}
	return s, "", arch
}

func actionOf(section string) string {
	switch {
	case strings.HasPrefix(section, "installing"):
		return "install"
	case strings.HasPrefix(section, "removing"), section == "replacing":
		return "remove"
	case section == "upgrading":
		return "upgrade"
	case section == "downgrading":
		return "downgrade"
	case section == "reinstalling":
		return "reinstall"
	}
	return section
}

// nevraName strips ".arch" and "-version-release" from a NEVRA.
func nevraName(nevra string) string {
	name, _, _ := splitNEVRA(nevra)
	return name
}

func pathsFor(p *plan.Plan, id string) []string {
	for _, op := range p.Operations {
		if op.ID == id {
			return op.Paths
		}
	}
	return nil
}

func (ex *executor) removeTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if len(op.Steps) == 0 {
		return nil, nil, errors.New("the removal carries no command")
	}
	argv := op.Steps[0].Argv
	names := argv[3:] // dnf5 -y remove <names>
	if err := ex.sudo(argv...); err != nil {
		return nil, nil, err
	}
	installed, err := ex.installed()
	if err != nil {
		return nil, nil, err
	}
	for _, p := range installed {
		if contains(names, p.Name) {
			return nil, nil, fmt.Errorf("verification: %s is still installed after the removal", p.Name)
		}
	}
	var remove []string
	if op.ID == "packages:remove-owned" {
		for _, path := range op.Paths {
			if strings.HasPrefix(path, "package:") {
				remove = append(remove, path)
			}
		}
	}
	return nil, remove, nil
}

func (ex *executor) flatpakInstall(op plan.Operation) ([]state.Receipt, []string, error) {
	if err := ex.sudo(op.Steps[0].Argv...); err != nil {
		return nil, nil, err
	}
	id := strings.TrimPrefix(op.ID, "flatpak:")
	f := facts.Inspect(ex.opts.Source, "")
	if !f.Flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + f.Flatpak.Error)
	}
	for _, app := range f.Flatpak.Value.Apps {
		if app.ID == id {
			return []state.Receipt{ex.receipt(op, "flatpak", "absent", "installed "+app.Version+" from "+app.Origin, "flatpak list shows the application")}, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("verification: %s is not installed after the operation", id)
}

func (ex *executor) flatpakRemove(op plan.Operation) ([]state.Receipt, []string, error) {
	if err := ex.sudo(op.Steps[0].Argv...); err != nil {
		return nil, nil, err
	}
	id := strings.TrimPrefix(op.ID, "flatpak:")
	f := facts.Inspect(ex.opts.Source, "")
	if !f.Flatpak.Known() {
		return nil, nil, errors.New("verification: Flatpak state is unknown: " + f.Flatpak.Error)
	}
	for _, app := range f.Flatpak.Value.Apps {
		if app.ID == id {
			return nil, nil, fmt.Errorf("verification: %s is still installed", id)
		}
	}
	return nil, []string{op.ID}, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

package apply

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	FirstApply bool
	Engine     string
	// ErrOut receives the stderr of native commands; nil means Out.
	ErrOut      io.Writer
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
	// Differences lists what a DNF transaction did beyond or instead of
	// its preview: a package the preview did not name, one it named that
	// did not happen, or another version than shown.
	Differences []string `json:"differences,omitempty"`
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
		r.Differences = append(r.Differences, ex.differences...)
		ex.differences = nil
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
	differences  []string // what the last transaction did beyond its preview
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
// sudo runs one privileged native command with its output on the terminal,
// so DNF's and Flatpak's own progress stays visible.
func (ex *executor) sudo(argv ...string) error {
	fmt.Fprintf(ex.opts.Out, "   $ sudo %s\n", strings.Join(argv, " "))
	errOut := ex.opts.ErrOut
	if errOut == nil {
		errOut = ex.opts.Out
	}
	return ex.opts.Source.Stream(ex.opts.Out, errOut, "sudo", argv...)
}

func (ex *executor) receipt(op plan.Operation, provider, previous, intended, verification string) state.Receipt {
	return state.Receipt{Schema: state.Schema, Engine: ex.opts.Engine, Definitions: ex.opts.Definitions, Machine: ex.p.Machine,
		Resource: op.ID, Provider: provider, Paths: op.Paths, Previous: previous, Intended: intended, Operation: op.Action,
		PlanDigest: ex.p.Digest, Verified: true, Verification: verification, Timestamp: ex.opts.Now().UTC()}
}

func (ex *executor) execute(op plan.Operation) (receipts []state.Receipt, remove []string, err error) {
	switch {
	case op.Kind == plan.KindDNFConfig:
		return ex.dnfConfig(op)
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

// dnfConfig writes, adopts, or removes the libdnf5 drop-in and verifies
// the file afterwards by reading it back.
func (ex *executor) dnfConfig(op plan.Operation) ([]state.Receipt, []string, error) {
	want := plan.DNFDropIn(ex.opts.Root)
	readBack := func() (string, error) {
		data, err := ex.opts.Source.ReadFile(facts.DNFDropInPath)
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return string(data), err
	}
	switch op.Action {
	case plan.ActionAdopt:
		return []state.Receipt{ex.receipt(op, "dnf-config", "present as declared", "drop-in rendered from nimbus.toml [dnf]", "file content matches the rendered drop-in")}, nil, nil
	case plan.ActionRemove:
		if err := ex.sudo(op.Steps[0].Argv...); err != nil {
			return nil, nil, err
		}
		if have, err := readBack(); err != nil || have != "" {
			return nil, nil, fmt.Errorf("verification: %s still exists after removal", facts.DNFDropInPath)
		}
		return nil, []string{op.ID}, nil
	}
	previous := "absent"
	if op.Action == plan.ActionRepair {
		previous = "present with other content"
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return nil, nil, err
	}
	staged := filepath.Join(ex.opts.Stage, "dnf-drop-in.conf")
	if err := os.WriteFile(staged, []byte(want), 0o600); err != nil {
		return nil, nil, err
	}
	for _, st := range op.Steps {
		if st.Argv == nil {
			continue
		}
		argv := slices.Clone(st.Argv)
		for i, a := range argv {
			if a == plan.DNFDropInPlaceholder {
				argv[i] = staged
			}
		}
		if err := ex.sudo(argv...); err != nil {
			return nil, nil, err
		}
	}
	have, err := readBack()
	if err != nil {
		return nil, nil, fmt.Errorf("verification: %w", err)
	}
	if have != want {
		return nil, nil, fmt.Errorf("verification: %s does not hold the rendered drop-in", facts.DNFDropInPath)
	}
	return []state.Receipt{ex.receipt(op, "dnf-config", previous, "drop-in rendered from nimbus.toml [dnf]", "file content matches the rendered drop-in")}, nil, nil
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
	for _, st := range op.Steps {
		if st.Description == plan.DisableDuplicateDescription {
			if err := ex.sudo(st.Argv...); err != nil {
				return nil, nil, err
			}
		}
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
		// The plan shows exactly which steps the repair needs: a rewrite
		// of the owned file, an override, or both; the duplicate override
		// runs afterwards with the other kinds.
		for _, st := range op.Steps {
			if st.Argv != nil && st.Description != plan.DisableDuplicateDescription {
				if err := ex.sudo(st.Argv...); err != nil {
					return err
				}
			}
		}
		return nil
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

// ExtractKeysWithRPM2Archive is the real key extractor. rpm2archive writes
// the gzip tar to standard output when that is a pipe, which Source.Run
// captures; the key files live below ./etc/pki/rpm-gpg/ inside it.
func ExtractKeysWithRPM2Archive(src facts.Source) func(string) (map[string][]byte, error) {
	return func(rpmPath string) (map[string][]byte, error) {
		archive, err := src.Run("rpm2archive", rpmPath)
		if err != nil {
			return nil, err
		}
		return readKeysFromArchive(archive)
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
func (ex *executor) installTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Transaction == nil || len(op.Steps) < 1 {
		return nil, nil, errors.New("the install operation carries no previewed transaction")
	}
	if err := ex.sudo(op.Steps[0].Argv...); err != nil {
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
	for _, canonical := range op.Items {
		name := canonical[strings.LastIndexByte(canonical, ':')+1:]
		inst, ok := have[name]
		if !ok {
			return nil, nil, fmt.Errorf("verification: %s is not installed after the transaction", name)
		}
		sub := plan.Operation{ID: "package:" + canonical, Action: plan.ActionInstall, Paths: pathsFor(ex.p, "package:"+canonical)}
		receipts = append(receipts, ex.receipt(sub, "dnf", "absent", "installed "+inst.EVR(), "dnf5 repoquery --installed lists "+inst.EVR()))
	}
	ex.differences = transactionDifferences(op.Transaction, ex.seen, have)
	for _, p := range installed {
		ex.seen[p.Name] = p
	}
	for name := range ex.seen {
		if _, ok := have[name]; !ok {
			delete(ex.seen, name)
		}
	}
	return receipts, nil, nil
}

// transactionDifferences compares what a transaction changed, the installed
// set before against after, with what its preview showed. It is a report
// for the owner, not a check that stops anything: DNF resolved again at
// install time, and this says where that resolution differed.
func transactionDifferences(tx *plan.Transaction, before, after map[string]facts.Package) []string {
	previewed := map[string]plan.TxPackage{}
	for _, row := range tx.Packages {
		if row.Section != plan.SectionReplaced {
			previewed[row.Name] = row
		}
	}
	var diffs []string
	names := map[string]bool{}
	for n := range before {
		names[n] = true
	}
	for n := range after {
		names[n] = true
	}
	for _, name := range sortedNames(names) {
		was, had := before[name]
		now, has := after[name]
		row, shown := previewed[name]
		switch {
		case had && has && was.EVR() == now.EVR():
			// A previewed removal that did not happen is reported below.
			if shown && (strings.HasPrefix(row.Section, "upgrading") || strings.HasPrefix(row.Section, "downgrading")) {
				diffs = append(diffs, fmt.Sprintf("%s was previewed for %s but is unchanged", name, row.Section))
			}
		case !had && has && !shown:
			diffs = append(diffs, fmt.Sprintf("DNF also installed %s %s (%s)", name, now.EVR(), now.FromRepo))
		case !had && has && strings.TrimPrefix(row.EVR, "0:") != strings.TrimPrefix(now.EVR(), "0:"):
			diffs = append(diffs, fmt.Sprintf("%s was installed as %s, the preview showed %s", name, now.EVR(), row.EVR))
		case had && !has && !shown:
			diffs = append(diffs, fmt.Sprintf("DNF also removed %s %s", name, was.EVR()))
		case had && has && !shown:
			diffs = append(diffs, fmt.Sprintf("DNF also changed %s from %s to %s", name, was.EVR(), now.EVR()))
		}
	}
	for name, row := range previewed {
		_, had := before[name]
		_, has := after[name]
		if strings.HasPrefix(row.Section, "installing") && !has {
			diffs = append(diffs, fmt.Sprintf("%s was previewed for installation but is not installed", name))
		}
		if strings.HasPrefix(row.Section, "removing") && had && has {
			diffs = append(diffs, fmt.Sprintf("%s was previewed for removal but is still installed", name))
		}
	}
	sort.Strings(diffs)
	return diffs
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
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
	if len(argv) < 4 {
		return nil, nil, errors.New("the removal command names no packages")
	}
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

// Upgrade brings the installed system current through the native tools,
// with their output on the terminal: dnf5 upgrade, and flatpak update when
// a Flatpak remote is declared and the tool is present. Upgrades change no
// ownership, so they record no receipts.
func Upgrade(opts Options, root definitions.Root) error {
	ex := &executor{opts: opts}
	if ex.opts.Out == nil {
		ex.opts.Out = io.Discard
	}
	if err := ex.sudo("dnf5", "-y", "upgrade"); err != nil {
		return err
	}
	for _, r := range root.Repositories {
		if r.Kind == "flatpak" {
			if _, err := opts.Source.LookPath("flatpak"); err == nil {
				return ex.sudo("flatpak", "update", "--system", "--noninteractive")
			}
		}
	}
	return nil
}

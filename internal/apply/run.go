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
	UpgradePreview *plan.Transaction

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

// Failure identifies one unsuccessful operation.
type Failure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// Result preserves completed work and every failure. Independent user tools
// continue after a failure; a system operation failure stops the run.
type Result struct {
	Reboot   bool      `json:"reboot,omitempty"`
	Logout   bool      `json:"logout,omitempty"`
	Failures []Failure `json:"failures,omitempty"`
	Executed []string  `json:"executed"`
	Pending  []string  `json:"pending"`
	Failed   string    `json:"failed,omitempty"`
	Error    string    `json:"error,omitempty"`
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
	var deferredFileRemovals []string
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
		r.Differences = append(r.Differences, ex.differences...)
		ex.differences = nil
		if err != nil {
			r.Failures = append(r.Failures, Failure{ID: op.ID, Error: err.Error()})
			if r.Error == "" {
				r.Failed, r.Error = op.ID, err.Error()
			}
			fmt.Fprintf(ex.opts.Out, "   failed: %v\n", err)
			if op.Kind == plan.KindUser {
				continue
			}
			return r
		}
		for _, receipt := range receipts {
			r.Reboot = r.Reboot || receipt.Reboot
			r.Logout = r.Logout || receipt.Logout
		}
		if op.Kind == plan.KindFile && op.Action == plan.ActionRemove && op.File != nil && len(op.File.Triggers) > 0 {
			deferredFileRemovals = append(deferredFileRemovals, remove...)
			remove = nil
		}
		st := &state.Stage{Schema: state.Schema, PlanDigest: p.Digest, Receipts: receipts, Remove: remove, Time: ex.opts.Now().UTC()}
		if ex.opts.FirstApply && !ex.baselineDone {
			st.Baseline = &state.Baseline{Schema: state.BaselineSchema, Recorded: st.Time, Packages: ex.baseline()}
			ex.baselineDone = true
		}
		if len(receipts) > 0 || len(remove) > 0 || st.Baseline != nil {
			if err := ex.opts.Record(p.Digest, st); err != nil {
				r.Failed = op.ID
				r.Error = "operation applied and verified, but its receipt was not recorded: " + err.Error()
				fmt.Fprintf(ex.opts.Out, "   %s; inspect the live resource and restore its reviewed previous state before retrying.\n", r.Error)
				r.Failures = append(r.Failures, Failure{ID: op.ID, Error: r.Error})
				return r
			}
		}
		r.Executed = append(r.Executed, op.ID)
	}
	if len(deferredFileRemovals) > 0 {
		if err := ex.opts.Record(p.Digest, &state.Stage{Schema: state.Schema, PlanDigest: p.Digest, Remove: deferredFileRemovals, Time: ex.opts.Now().UTC()}); err != nil {
			r.Failed = deferredFileRemovals[0]
			r.Executed = slices.DeleteFunc(r.Executed, func(id string) bool { return slices.Contains(deferredFileRemovals, id) })
			r.Error = "file retirement was applied but not recorded; inspect the live resource and restore its reviewed previous state before retrying: " + err.Error()
			for _, id := range deferredFileRemovals {
				r.Failures = append(r.Failures, Failure{ID: id, Error: r.Error})
			}
			fmt.Fprintf(ex.opts.Out, "   %s\n", r.Error)
		}
	}
	return r
}

type executor struct {
	p            *plan.Plan
	opts         Options
	seen         map[string]facts.Package // installed packages, kept current
	before       []string                 // package names at the start, sorted
	baselineDone bool
	differences  []string // what the last transaction did beyond its preview
}

func (ex *executor) snapshotPackages() error {
	pkgs, err := ex.installed()
	if err != nil {
		return err
	}
	ex.seen = facts.PackageMap(pkgs)
	// The baseline is what existed before this run, frozen here: a
	// transaction later in the run updates seen, never before.
	ex.before = make([]string, 0, len(ex.seen))
	for _, p := range ex.seen {
		ex.before = append(ex.before, p.ID())
	}
	sort.Strings(ex.before)
	ex.before = slices.Compact(ex.before)
	return nil
}

func (ex *executor) baseline() []string {
	return append([]string(nil), ex.before...)
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
	return state.Receipt{Schema: state.ReceiptSchema, Engine: ex.opts.Engine, Definitions: ex.opts.Definitions, Machine: ex.p.Machine,
		Resource: op.ID, Provider: provider, Paths: op.Paths, Previous: previous, Intended: intended, Operation: op.Action,
		PlanDigest: ex.p.Digest, Verified: true, Verification: verification, Timestamp: ex.opts.Now().UTC()}
}

func (ex *executor) execute(op plan.Operation) (receipts []state.Receipt, remove []string, err error) {
	switch {
	case (op.Kind == plan.KindRepository || op.Kind == plan.KindFlatpakRemote) && (op.Action == plan.ActionRemove || op.Action == plan.ActionRetire):
		return ex.sourceRetirement(op)
	case op.Kind == plan.KindFile || op.Kind == plan.KindService || op.Kind == plan.KindGroup || op.Kind == plan.KindTarget || op.Kind == plan.KindTrigger:
		return ex.systemResource(op)
	case op.Kind == plan.KindUser:
		return nil, nil, ex.userTool(op)
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
	case (op.Kind == plan.KindPackage || op.Kind == plan.KindFlatpak) && op.Action == plan.ActionRetire:
		return nil, []string{op.ID}, nil
	case op.Kind == plan.KindPackage && op.Action == plan.ActionAdopt:
		name := op.ID[strings.LastIndexByte(op.ID, ':')+1:]
		if resolved := op.Resolved[name]; resolved != "" {
			name = resolved
		}
		inst, ok := facts.FindPackage(packageValues(ex.seen), name)
		if !ok {
			return nil, nil, fmt.Errorf("%s is no longer installed", name)
		}
		r := ex.receipt(op, "dnf", "installed "+inst.EVR()+" ("+inst.FromRepo+")", "installed", "dnf5 repoquery --installed lists it")
		r.Package = inst.ID()
		return []state.Receipt{r}, nil, nil
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
	keys, err := facts.KeyFingerprints(ex.opts.Source, path)
	if err != nil {
		return "", fmt.Errorf("gpg: %w", err)
	}
	if len(keys) != 1 {
		return "", fmt.Errorf("key file has %d primary keys; exactly the declared key is required", len(keys))
	}
	return keys[0], nil
}

// verifiesKey distinguishes a complete key reconciliation from a repair
// limited to already verified repository options.
func verifiesKey(op plan.Operation) bool {
	for _, step := range op.Steps {
		if len(step.Argv) > 0 && step.Argv[0] == "gpg" {
			return true
		}
	}
	return false
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
	receipt := ex.receipt(op, "repository", "absent", "enabled with key "+definitions.NormalizeFingerprint(r.Key), "repository enabled, signature checking on, local key fingerprint and priority as declared")
	recordSourceOwnership(&receipt, op, plan.DNFRepoIDs(id, r), f)
	return []state.Receipt{receipt}, nil, nil
}

func (ex *executor) enableBaseURL(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
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
	if err := ex.sudo(plan.AddRepoStep(id, r, op.Action == plan.ActionRepair).Argv...); err != nil {
		return err
	}
	if op.Action == plan.ActionRepair {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	return nil
}

func (ex *executor) enableReleasePackage(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
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
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	if op.Action != plan.ActionRepair {
		if _, err := ex.packageTransaction([]string{"dnf5", "install", "-y", rpm}, nil); err != nil {
			return err
		}
	}
	return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
}

func (ex *executor) enableCOPR(id string, r definitions.Repository, op plan.Operation) error {
	if op.Action == plan.ActionRepair && !verifiesKey(op) {
		return ex.sudo(plan.PrioritySteps(id, r)[0].Argv...)
	}
	data, err := ex.opts.Fetch("https://download.copr.fedorainfracloud.org/results/" + r.Project + "/pubkey.gpg")
	if err != nil {
		return fmt.Errorf("download COPR key: %w", err)
	}
	key, err := ex.verifiedKey("key-"+id+".gpg", data, r.Key)
	if err != nil {
		return err
	}
	if err := ex.sudo("install", "-m", "0644", key, plan.KeyPath(id)); err != nil {
		return err
	}
	if err := ex.sudo("rpm", "--import", plan.KeyPath(id)); err != nil {
		return err
	}
	if op.Action != plan.ActionRepair {
		if err := ex.sudo("dnf5", "copr", "enable", "-y", r.Project); err != nil {
			return err
		}
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
			if reason := plan.FlatpakKeyDrift(r, remote); reason != "" {
				return nil, nil, fmt.Errorf("verification: remote %s: %s", id, reason)
			}
			url := ""
			for _, line := range strings.Split(string(data), "\n") {
				if value, ok := strings.CutPrefix(line, "Url="); ok {
					url = strings.TrimSpace(value)
				}
			}
			if url == "" || strings.TrimSuffix(remote.URL, "/") != strings.TrimSuffix(url, "/") {
				return nil, nil, fmt.Errorf("verification: remote %s URL differs from the verified remote definition", id)
			}
			receipt := ex.receipt(op, "flatpak-remote", "absent", "present with key "+definitions.NormalizeFingerprint(r.Key), "remote URL, signature checking and installed key match")
			recordSourceOwnership(&receipt, op, []string{id}, f)
			return []state.Receipt{receipt}, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("verification: remote %s is not present after the operation", id)
}

// --- packages --------------------------------------------------------------

func packageValues(packages map[string]facts.Package) []facts.Package {
	out := make([]facts.Package, 0, len(packages))
	for _, p := range packages {
		out = append(out, p)
	}
	return out
}

// packageTransaction always inspects the result, including a native failure
// after partial work. Unknown verification never becomes an empty success.
func (ex *executor) packageTransaction(argv []string, tx *plan.Transaction) ([]facts.Package, error) {
	nativeErr := ex.sudo(argv...)
	installed, verifyErr := ex.installed()
	if verifyErr != nil {
		ex.differences = append(ex.differences, "DNF verification incomplete: "+verifyErr.Error())
		return nil, errors.Join(nativeErr, fmt.Errorf("verification: %w", verifyErr))
	}
	after := facts.PackageMap(installed)
	ex.differences = append(ex.differences, transactionDifferences(tx, ex.seen, after)...)
	ex.seen = after
	return installed, nativeErr
}

func (ex *executor) installTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Transaction == nil || len(op.Steps) < 1 {
		return nil, nil, errors.New("the install operation carries no previewed transaction")
	}
	installed, err := ex.packageTransaction(op.Steps[0].Argv, op.Transaction)
	if err != nil {
		return nil, nil, err
	}
	var receipts []state.Receipt
	for _, canonical := range op.Items {
		name := plan.PackageName(canonical)
		if resolved := op.Resolved[name]; resolved != "" {
			name = resolved
		}
		inst, ok := facts.FindPackage(installed, name)
		if !ok {
			return nil, nil, fmt.Errorf("verification: %s is not installed after the transaction", name)
		}
		sub := plan.Operation{ID: "package:" + canonical, Action: plan.ActionInstall, Paths: pathsFor(ex.p, "package:"+canonical)}
		r := ex.receipt(sub, "dnf", "absent", "installed "+inst.ID()+" "+inst.EVR(), "dnf5 repoquery --installed lists "+inst.ID()+" "+inst.EVR())
		r.Package = inst.ID()
		receipts = append(receipts, r)
	}
	return receipts, nil, nil
}

// transactionDifferences compares complete RPM sets, retaining both multilib
// architectures and simultaneous installonly versions. A preview describes
// changes to the initial set; every other package must remain as observed.
func transactionDifferences(tx *plan.Transaction, before, after map[string]facts.Package) []string {
	before = facts.PackageMap(packageValues(before))
	after = facts.PackageMap(packageValues(after))
	expected := facts.PackageMap(packageValues(before))
	shown := map[string]bool{}
	if tx != nil {
		for _, row := range tx.Packages {
			id := facts.PackageID(row.Name, row.Arch)
			shown[id] = true
			if strings.HasPrefix(row.Section, "removing") || row.Section == plan.SectionReplaced {
				delete(expected, id+" "+strings.TrimPrefix(row.EVR, "0:"))
				continue
			}
			// Upgrade and downgrade replace the installed version. Explicit
			// replacing rows also cover obsoletes and installonly changes.
			if row.Section == "upgrading" || row.Section == "downgrading" {
				for key, p := range expected {
					if p.ID() == id {
						delete(expected, key)
					}
				}
			}
			version, release, _ := strings.Cut(strings.TrimPrefix(row.EVR, "0:"), "-")
			epoch := ""
			if e, v, ok := strings.Cut(version, ":"); ok {
				epoch, version = e, v
			}
			p := facts.Package{Name: row.Name, Arch: row.Arch, Epoch: epoch, Version: version, Release: release, FromRepo: row.Repository}
			expected[id+" "+p.EVR()] = p
		}
	}
	ids := map[string]bool{}
	for _, set := range []map[string]facts.Package{before, expected, after} {
		for _, p := range set {
			ids[p.ID()] = true
		}
	}
	versions := func(set map[string]facts.Package, id string) []string {
		var out []string
		for _, p := range set {
			if p.ID() == id {
				out = append(out, p.EVR())
			}
		}
		sort.Strings(out)
		return out
	}
	var diffs []string
	for _, id := range sortedNames(ids) {
		want, got := versions(expected, id), versions(after, id)
		if slices.Equal(want, got) {
			continue
		}
		was := versions(before, id)
		switch {
		case !shown[id] && len(was) == 0:
			diffs = append(diffs, fmt.Sprintf("DNF also installed %s %s", id, strings.Join(got, ", ")))
		case !shown[id] && len(got) == 0:
			diffs = append(diffs, fmt.Sprintf("DNF also removed %s %s", id, strings.Join(was, ", ")))
		case !shown[id]:
			diffs = append(diffs, fmt.Sprintf("DNF also changed %s from %s to %s", id, strings.Join(was, ", "), strings.Join(got, ", ")))
		default:
			show := func(v []string) string {
				if len(v) == 0 {
					return "absent"
				}
				return strings.Join(v, ", ")
			}
			diffs = append(diffs, fmt.Sprintf("%s is %s after DNF; the preview expected %s", id, show(got), show(want)))
		}
	}
	if tx != nil {
		for _, row := range tx.Packages {
			if strings.HasPrefix(row.Section, "removing") || row.Section == plan.SectionReplaced || row.Repository == "" {
				continue
			}
			id := facts.PackageID(row.Name, row.Arch)
			if p, ok := after[id+" "+strings.TrimPrefix(row.EVR, "0:")]; ok && p.FromRepo != row.Repository {
				diffs = append(diffs, fmt.Sprintf("%s %s came from %s; the preview showed %s", id, p.EVR(), p.FromRepo, row.Repository))
			}
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
	if len(op.Steps) == 0 || len(op.Steps[0].Argv) < 5 {
		return nil, nil, errors.New("the removal command names no packages")
	}
	argv := op.Steps[0].Argv
	if argv[3] != "--no-autoremove" {
		return nil, nil, errors.New("the removal command must use --no-autoremove")
	}
	installed, err := ex.packageTransaction(argv, op.Transaction)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range argv[4:] {
		if p, ok := facts.FindPackage(installed, name); ok {
			return nil, nil, fmt.Errorf("verification: %s is still installed after the removal", p.ID())
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
func Upgrade(opts Options, root definitions.Root) *Result {
	r := &Result{}
	ex := &executor{opts: opts}
	if ex.opts.Out == nil {
		ex.opts.Out = io.Discard
	}
	if err := ex.snapshotPackages(); err != nil {
		r.Failed, r.Error = "upgrade:dnf", "verification before upgrade: "+err.Error()
		r.Failures = append(r.Failures, Failure{ID: r.Failed, Error: r.Error})
		return r
	}
	_, err := ex.packageTransaction([]string{"dnf5", "-y", "upgrade"}, opts.UpgradePreview)
	r.Differences = append(r.Differences, ex.differences...)
	if err != nil {
		r.Failed, r.Error = "upgrade:dnf", err.Error()
		r.Failures = append(r.Failures, Failure{ID: r.Failed, Error: r.Error})
	} else {
		r.Executed = append(r.Executed, "upgrade:dnf")
	}
	for _, repo := range root.Repositories {
		if repo.Kind != "flatpak" {
			continue
		}
		if _, err := opts.Source.LookPath("flatpak"); err != nil {
			r.Pending = append(r.Pending, "upgrade:flatpak")
			break
		}
		before := facts.Inspect(opts.Source, "").Flatpak
		if !before.Known() {
			r.Failures = append(r.Failures, Failure{ID: "upgrade:flatpak", Error: "Flatpak verification before update: " + before.Error})
			r.Pending = append(r.Pending, "upgrade:flatpak")
			if r.Error == "" {
				r.Failed, r.Error = "upgrade:flatpak", "Flatpak verification before update: "+before.Error
			}
			break
		}
		nativeErr := ex.sudo("flatpak", "update", "--system", "--noninteractive")
		after := facts.Inspect(opts.Source, "").Flatpak
		var verifyErr error
		if !after.Known() {
			verifyErr = errors.New("Flatpak verification incomplete: " + after.Error)
			r.Differences = append(r.Differences, verifyErr.Error())
		} else {
			old := map[string]facts.FlatpakApp{}
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
			removed := make([]string, 0, len(old))
			for id := range old {
				removed = append(removed, id)
			}
			sort.Strings(removed)
			for _, id := range removed {
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

// userTool runs a user-scope step as the user, with its output visible: a
// maker's installer fetched to the stage directory and shown by digest, a
// runtimes command, or a cargo install. Presence afterwards is the check;
// nothing is recorded.
func (ex *executor) userTool(op plan.Operation) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, st := range op.Steps {
		if st.Argv == nil {
			continue
		}
		argv := slices.Clone(st.Argv)
		for i, a := range argv {
			switch {
			case a == plan.InstallerScript:
				path, err := ex.fetchInstaller(op)
				if err != nil {
					return err
				}
				argv[i] = path
			case strings.HasPrefix(a, plan.HomeDir):
				argv[i] = home + strings.TrimPrefix(a, plan.HomeDir)
			}
		}
		fmt.Fprintf(ex.opts.Out, "   $ %s\n", strings.Join(argv, " "))
		errOut := ex.opts.ErrOut
		if errOut == nil {
			errOut = ex.opts.Out
		}
		if err := ex.opts.Source.Stream(ex.opts.Out, errOut, argv[0], argv[1:]...); err != nil {
			return err
		}
	}
	return ex.verifyUserTool(op, home)
}

// fetchInstaller downloads the installer named in the operation's first
// step, keeps it below the stage directory, and prints its digest.
func (ex *executor) fetchInstaller(op plan.Operation) (string, error) {
	url := ""
	for _, st := range op.Steps {
		if u, ok := strings.CutPrefix(st.Description, "download "); ok {
			url, _, _ = strings.Cut(u, " ")
		}
	}
	if url == "" {
		return "", errors.New("the installer step names no URL")
	}
	data, err := ex.opts.Fetch(url)
	if err != nil {
		return "", fmt.Errorf("download installer: %w", err)
	}
	if err := os.MkdirAll(ex.opts.Stage, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(ex.opts.Stage, "installer-"+strings.TrimPrefix(op.ID, "user:")+".sh")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	fmt.Fprintf(ex.opts.Out, "   downloaded %s, sha256 %x\n", url, sha256.Sum256(data))
	return path, nil
}

// verifyUserTool re-reads what the step should have left: the installer's
// binary, or the crate in cargo's list. A runtimes command is verified by
// its own exit status.
func (ex *executor) verifyUserTool(op plan.Operation, home string) error {
	switch {
	case strings.HasPrefix(op.ID, "package:cargo:"):
		crate := strings.TrimPrefix(op.ID, "package:cargo:")
		f := facts.Inspect(ex.opts.Source, "")
		if !f.User.Known() || !contains(f.User.Value.Crates, crate) {
			return fmt.Errorf("verification: cargo install --list does not show %s", crate)
		}
	case strings.HasPrefix(op.ID, "user:") && !strings.HasSuffix(op.ID, ":install"):
		for _, st := range op.Steps {
			if rel, ok := strings.CutPrefix(st.Description, "verify ~/"); ok {
				rel = strings.TrimSuffix(rel, " exists")
				names, err := ex.opts.Source.ReadDir(filepath.Join(home, filepath.Dir(rel)))
				if err != nil || !contains(names, filepath.Base(rel)) {
					return fmt.Errorf("verification: ~/%s does not exist after the installer ran", rel)
				}
			}
		}
	}
	return nil
}

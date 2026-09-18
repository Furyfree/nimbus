package postinstall

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/native"
)

// fdeEnrollmentPath records which TPM token Nimbus owns. It holds no
// credentials, only the native identifiers needed for scoped removal.
var fdeEnrollmentPath = "/var/lib/nimbus/fde/enrollment.json"

// fdeExecutable resolves the installed engine for root-only internal calls;
// tests override it.
var fdeExecutable = os.Executable

// fdeRootSource runs the root-observed record write; tests override it.
var fdeRootSource native.Source = native.ExecSource{}

var fdeUUIDRE = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

// FDEEnrollment is the ownership record written after a successful enrollment.
type FDEEnrollment struct {
	Schema    int               `json:"schema"`
	Device    string            `json:"device"`
	UUID      string            `json:"uuid"`
	Keyslot   string            `json:"keyslot"`
	Token     string            `json:"token"`
	PCRs      string            `json:"pcrs"`
	PCRValues map[string]string `json:"pcr_values,omitempty"`
	// TPMSRK fingerprints the TPM's SRK public key, so verification can tell
	// a different TPM from the enrolled one.
	TPMSRK      string `json:"tpm_srk,omitempty"`
	SecureBoot  bool   `json:"secure_boot"`
	Fingerprint string `json:"fingerprint"`
	EnrolledAt  string `json:"enrolled_at"`
}

// fdeDevice derives the LUKS2 backing device from the reviewed command line.
// The UUID must be one path component, so a forged command line cannot escape
// the by-uuid directory.
func fdeDevice(src native.Source) (string, string, error) {
	cmdline, err := fdeCmdline(src)
	if err != nil {
		return "", "", err
	}
	uuid := ""
	for _, option := range strings.Fields(cmdline) {
		if rest, ok := strings.CutPrefix(option, "rd.luks.uuid=luks-"); ok {
			uuid = rest
			break
		}
	}
	if uuid == "" {
		return "", "", errors.New("the command line names no LUKS root; enrollment cannot identify the device")
	}
	if !fdeUUIDRE.MatchString(uuid) {
		return "", "", fmt.Errorf("the LUKS UUID %q is not a plain identifier", uuid)
	}
	return "/dev/disk/by-uuid/" + uuid, uuid, nil
}

// fdeToken is one systemd-tpm2 token observed in the LUKS2 metadata.
type fdeToken struct {
	ID      string
	Keyslot string
	Slots   int
}

type fdeLUKSMetadata struct {
	Tokens map[string]struct {
		Type     string   `json:"type"`
		Keyslots []string `json:"keyslots"`
	} `json:"tokens"`
	Keyslots map[string]struct {
		Type string `json:"type"`
	} `json:"keyslots"`
}

// fdeLUKSState reads the LUKS2 metadata once and returns the systemd-tpm2
// tokens plus every keyslot type.
func fdeLUKSState(src native.Source) ([]fdeToken, map[string]string, map[string]bool, error) {
	device, _, err := fdeDevice(src)
	if err != nil {
		return nil, nil, nil, err
	}
	out, err := src.Run("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", device)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read the LUKS2 metadata: %w", err)
	}
	var metadata fdeLUKSMetadata
	if err := json.Unmarshal(out, &metadata); err != nil {
		return nil, nil, nil, fmt.Errorf("parse the LUKS2 metadata: %w", err)
	}
	var tokens []fdeToken
	referenced := map[string]bool{}
	for tokenID, token := range metadata.Tokens {
		for _, slot := range token.Keyslots {
			referenced[slot] = true
		}
		if token.Type != "systemd-tpm2" {
			continue
		}
		observed := fdeToken{ID: tokenID, Slots: len(token.Keyslots)}
		if len(token.Keyslots) > 0 {
			observed.Keyslot = token.Keyslots[0]
		}
		tokens = append(tokens, observed)
	}
	slices.SortFunc(tokens, func(a, b fdeToken) int {
		if a.Keyslot != b.Keyslot {
			return strings.Compare(a.Keyslot, b.Keyslot)
		}
		return strings.Compare(a.ID, b.ID)
	})
	keyslots := make(map[string]string, len(metadata.Keyslots))
	for id, keyslot := range metadata.Keyslots {
		keyslots[id] = keyslot.Type
	}
	return tokens, keyslots, referenced, nil
}

// fdeTokens lists every systemd-tpm2 token deterministically, so ownership
// never depends on map iteration order.
func fdeTokens(src native.Source) ([]fdeToken, error) {
	tokens, _, _, err := fdeLUKSState(src)
	return tokens, err
}

// fdeEnrollment reads the ownership record through the root-only helper. A
// missing record is not an error.
func fdeEnrollment(src native.Source) (FDEEnrollment, bool, error) {
	exe, err := fdeExecutable()
	if err != nil {
		return FDEEnrollment{}, false, err
	}
	out, err := src.Run("sudo", "-n", "--", exe, "internal", "fde-uki", "state")
	if err != nil {
		return FDEEnrollment{}, false, fmt.Errorf("read the FDE enrollment record: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return FDEEnrollment{}, false, nil
	}
	var record FDEEnrollment
	if err := json.Unmarshal(out, &record); err != nil {
		return FDEEnrollment{}, false, fmt.Errorf("parse the FDE enrollment record: %w", err)
	}
	if record.Schema != 1 || record.Device == "" || record.UUID == "" ||
		!numericID(record.Keyslot) || !numericID(record.Token) {
		return FDEEnrollment{}, false, errors.New("the FDE enrollment record is incomplete")
	}
	return record, true, nil
}

func numericID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// fdeBootNextID returns the one-shot BootNext entry id, if set.
func fdeBootNextID(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "BootNext:"); ok {
			return strings.ToUpper(strings.TrimSpace(rest))
		}
	}
	return ""
}

// fdeBootCurrentID returns the firmware's current boot entry id.
func fdeBootCurrentID(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "BootCurrent:"); ok {
			return strings.ToUpper(strings.TrimSpace(rest))
		}
	}
	return ""
}

// fdeEnrollEligible enforces the enrollment preconditions that native state
// can establish. The caller verifies the image and entry first.
func fdeEnrollEligible(src native.Source) error {
	ready, err := fdeMarkerReady(src)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("FDE setup is not active; run the approved setup first")
	}
	out, err := src.Run("efibootmgr")
	if err != nil {
		return fmt.Errorf("inspect the firmware entries: %w", err)
	}
	entries := fdeBootEntries(string(out))
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	if !fdeEntryReady(entries, secure) {
		return errors.New("the Nimbus firmware entry is missing or wrong; rerun the approved setup")
	}
	id, ok := fdeCorrectEntryID(entries, secure)
	if !ok {
		return errors.New("the Nimbus firmware entry could not be identified")
	}
	if current := fdeBootCurrentID(string(out)); !strings.EqualFold(current, id) {
		return fmt.Errorf("this boot used firmware entry %s, not the Nimbus image entry %s; reboot into the Nimbus image before enrolling", current, id)
	}
	if _, err := fdeSRKFingerprint(src); err != nil {
		return fmt.Errorf("the running TPM SRK public key is unavailable (%w); enrollment was not changed; retry after systemd-tpm2-setup completes", err)
	}
	if secure {
		keys := fdeKeys()
		enrolled, pending, err := fdeMOKState(src, keys.mokDER)
		if err != nil {
			return err
		}
		if !enrolled {
			if pending {
				return errors.New("MOK enrollment is pending; complete Enroll MOK at the next boot first")
			}
			return errors.New("the Nimbus MOK certificate is not enrolled; rerun the approved setup")
		}
	}
	return nil
}

// FDEEnrollCommands describes the reviewed enrollment operations.
func FDEEnrollCommands(task Task) ([][]string, error) {
	if err := validateFDEEnroll(task); err != nil {
		return nil, err
	}
	keys := fdeKeys()
	return [][]string{
		{"sudo", "--", "systemd-cryptenroll",
			"--tpm2-device=auto",
			"--tpm2-pcrs=7+14+12+13",
			"--tpm2-public-key=" + keys.pcrPublic,
			"--tpm2-public-key-pcrs=11",
			"--tpm2-pcrlock=",
			"<luks-device>"},
		{"sudo", "--", "nimbus", "internal", "fde-uki", "record", "<keyslot>", "<token>"},
	}, nil
}

func validateFDEEnroll(task Task) error {
	if task.ID != "fde" || (task.Status != Pending && task.Status != Unknown) || task.Action == nil || task.Action.Kind != EnrollFDE {
		return errors.New("invalid FDE enrollment action")
	}
	return nil
}

// FDERenewCommands describes the reviewed renewal operations.
func FDERenewCommands(task Task) ([][]string, error) {
	if err := validateFDERenew(task); err != nil {
		return nil, err
	}
	keys := fdeKeys()
	return [][]string{
		{"sudo", "--", "systemd-cryptenroll",
			"--tpm2-device=auto",
			"--tpm2-pcrs=7+14+12+13",
			"--tpm2-public-key=" + keys.pcrPublic,
			"--tpm2-public-key-pcrs=11",
			"--tpm2-pcrlock=",
			"--wipe-slot=<recorded-keyslot>",
			"<luks-device>"},
		{"sudo", "--", "nimbus", "internal", "fde-uki", "record", "<new-keyslot>", "<new-token>"},
	}, nil
}

func validateFDERenew(task Task) error {
	if task.ID != "fde" || task.Status != Pending || task.Action == nil || task.Action.Kind != RenewFDE {
		return errors.New("invalid FDE renewal action")
	}
	return nil
}

// orphanHint names the exact native command that removes a token Nimbus does
// not own, so a stranded enrollment is recoverable.
func orphanHint(token fdeToken) string {
	return fmt.Sprintf("remove it with: sudo systemd-cryptenroll --wipe-slot=%s /dev/disk/by-uuid/<uuid>", token.Keyslot)
}

// RunFDEEnroll performs the approved first enrollment. The passphrase is
// entered in systemd-cryptenroll's own native prompt and is never read here.
func RunFDEEnroll(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if err := validateFDEEnroll(task); err != nil {
		return err
	}
	return runFDEEnrollment(ctx, src, out, errOut, false)
}

// RunFDERenew replaces the recorded enrollment after the measured state
// changed, adding the new slot before wiping only the recorded one.
func RunFDERenew(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if err := validateFDERenew(task); err != nil {
		return err
	}
	return runFDEEnrollment(ctx, src, out, errOut, true)
}

func runFDEEnrollment(ctx context.Context, src native.Source, out, errOut io.Writer, renew bool) error {
	stream := func(name string, args ...string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "$ %s %s\n", name, strings.Join(args, " ")); err != nil {
			return err
		}
		return src.Stream(out, errOut, name, args...)
	}
	if err := stream("sudo", "--validate"); err != nil {
		return err
	}
	if err := fdeEnrollEligible(src); err != nil {
		return err
	}
	device, uuid, err := fdeDevice(src)
	if err != nil {
		return err
	}
	before, err := fdeTokens(src)
	if err != nil {
		return err
	}
	record, owned, err := fdeEnrollment(src)
	if err != nil {
		return err
	}
	wipe := ""
	wipeFirst := false
	switch {
	case renew:
		if len(before) != 1 || before[0].Slots != 1 || !owned ||
			record.Keyslot != before[0].Keyslot || record.Token != before[0].ID || record.UUID != uuid {
			return errors.New("the observed TPM token is not the single recorded token; inspect the LUKS2 tokens and the ownership record before renewing")
		}
		match, err := fdePCRsMatch(src, record)
		if err != nil {
			return fmt.Errorf("read the measured state before renewing: %w", err)
		}
		if match {
			live, err := fdeSRKFingerprint(src)
			if err != nil {
				return fmt.Errorf("read the running TPM SRK before renewing: %w", err)
			}
			if record.TPMSRK == "" {
				// The policy is already enrolled; only the ownership record
				// lacked the TPM identity.
				return refreshFDEEnrollment(src, out, errOut, record, before[0], true)
			}
			if record.TPMSRK == live {
				return refreshFDEEnrollment(src, out, errOut, record, before[0], false)
			}
			// A different TPM with identical policy metadata would be
			// refused as already enrolled, so the recorded slot is removed
			// before the new token is added.
			wipeFirst = true
		} else {
			wipe = record.Keyslot
		}
	case len(before) > 1:
		return fmt.Errorf("more than one systemd-tpm2 token exists; inspect the LUKS2 tokens and %s", orphanHint(before[0]))
	case len(before) == 1 && (!owned || record.Keyslot != before[0].Keyslot || record.Token != before[0].ID || record.UUID != uuid):
		return fmt.Errorf("a TPM token exists that Nimbus does not own; %s", orphanHint(before[0]))
	case len(before) == 1:
		return errors.New("TPM automatic unlock is already enrolled and recorded; nothing to do")
	}
	keys := fdeKeys()
	addArgs := []string{"sudo", "--", "systemd-cryptenroll",
		"--tpm2-device=auto",
		"--tpm2-pcrs=7+14+12+13",
		"--tpm2-public-key=" + keys.pcrPublic,
		"--tpm2-public-key-pcrs=11",
		"--tpm2-pcrlock=",
	}
	if wipeFirst {
		if err := stream("sudo", "--", "systemd-cryptenroll", "--wipe-slot="+record.Keyslot, device); err != nil {
			return fmt.Errorf("remove the token bound to the other TPM: %w; automatic unlock stays off until enrollment succeeds; rerun this task", err)
		}
		left, err := fdeTokens(src)
		if err != nil {
			return err
		}
		if len(left) != 0 {
			return errors.New("the old TPM token was not removed; inspect the LUKS2 tokens")
		}
	} else if wipe != "" {
		addArgs = append(addArgs, "--wipe-slot="+wipe)
	}
	addArgs = append(addArgs, device)
	if _, err := fmt.Fprintf(out, "$ %s %s\n", addArgs[0], strings.Join(addArgs[1:], " ")); err != nil {
		return err
	}
	if err := stream(addArgs[0], addArgs[1:]...); err != nil {
		if wipeFirst {
			return fmt.Errorf("systemd-cryptenroll failed after the old TPM token was removed: %w; automatic unlock is off until enrollment succeeds; rerun this task", err)
		}
		return fmt.Errorf("systemd-cryptenroll failed: %w", err)
	}
	after, err := fdeTokens(src)
	if err != nil {
		return err
	}
	if len(after) != 1 || after[0].Keyslot == "" || after[0].Slots != 1 {
		return errors.New("enrollment reported success but no single TPM token was observed; inspect the LUKS2 tokens")
	}
	if wipe != "" && after[0].Keyslot == wipe {
		return errors.New("renewal reported success but the recorded slot was reused; inspect the LUKS2 tokens")
	}
	exe, err := fdeExecutable()
	if err != nil {
		return err
	}
	recordArgs := []string{"sudo", "--", exe, "internal", "fde-uki", "record", after[0].Keyslot, after[0].ID}
	recordErr := src.Stream(out, errOut, recordArgs[0], recordArgs[1:]...)
	if recordErr != nil {
		// The token exists but is unrecorded; retry the root-observed write
		// once before reporting, because nothing else changed.
		recordErr = src.Stream(out, errOut, recordArgs[0], recordArgs[1:]...)
	}
	if recordErr != nil {
		if renew {
			return fmt.Errorf("the new token in keyslot %s is enrolled and the recorded slot %s was wiped, but its ownership record could not be written (%w); finish with: sudo %s internal fde-uki record %s %s", after[0].Keyslot, record.Keyslot, recordErr, exe, after[0].Keyslot, after[0].ID)
		}
		return fmt.Errorf("the token in keyslot %s is enrolled but its ownership record could not be written (%w); finish with: sudo %s internal fde-uki record %s %s", after[0].Keyslot, recordErr, exe, after[0].Keyslot, after[0].ID)
	}
	if renew {
		_, err = fmt.Fprintln(out, "TPM automatic unlock renewed and recorded. Reboot to verify that the disk unlocks without the passphrase; the disk passphrase remains the fallback.")
	} else {
		_, err = fmt.Fprintln(out, "TPM enrollment recorded. Reboot to verify that the disk unlocks without the passphrase; the disk passphrase remains the fallback.")
	}
	return err
}

// refreshFDEEnrollment rewrites the ownership record for the observed token
// without touching enrollment. It is the repair when the token still matches
// the measured state and only record metadata was missing. tpmChanged reports
// whether the record is being bound to the running TPM for the first time.
func refreshFDEEnrollment(src native.Source, out, errOut io.Writer, record FDEEnrollment, token fdeToken, tpmMissing bool) error {
	exe, err := fdeExecutable()
	if err != nil {
		return err
	}
	args := []string{"sudo", "--", exe, "internal", "fde-uki", "record", token.Keyslot, token.ID}
	if err := src.Stream(out, errOut, args[0], args[1:]...); err != nil {
		return fmt.Errorf("the token is unchanged but its ownership record could not be rewritten (%w); finish with: sudo %s internal fde-uki record %s %s", err, exe, token.Keyslot, token.ID)
	}
	if tpmMissing {
		_, err = fmt.Fprintln(out, "The enrollment policy was already in place; the ownership record now identifies this TPM. Reboot to verify automatic unlock; the disk passphrase remains the fallback.")
	} else {
		_, err = fmt.Fprintln(out, "The enrollment and ownership record already match this TPM; nothing to do. Reboot to verify automatic unlock; the disk passphrase remains the fallback.")
	}
	return err
}

// WriteFDEEnrollment observes the native token state as root and writes the
// ownership record. Identifiers come from the observed metadata, never from
// the invoking user.
func WriteFDEEnrollment(keyslot, token string) error {
	if !numericID(keyslot) || !numericID(token) {
		return errors.New("the keyslot and token must be numeric")
	}
	device, uuid, err := fdeDevice(fdeRootSource)
	if err != nil {
		return err
	}
	tokens, err := fdeTokens(fdeRootSource)
	if err != nil {
		return err
	}
	found := len(tokens) == 1 && tokens[0].ID == token && tokens[0].Keyslot == keyslot && tokens[0].Slots == 1
	if !found {
		return fmt.Errorf("no lone systemd-tpm2 token %s in keyslot %s was observed; the record was not written", token, keyslot)
	}
	values, err := fdePCRValues(fdeRootSource)
	if err != nil {
		return err
	}
	srk, err := fdeSRKFingerprint(fdeRootSource)
	if err != nil {
		return err
	}
	secure, err := fdeSecureBoot(fdeRootSource)
	if err != nil {
		return err
	}
	fingerprint, err := fdePublicKeyDigest(fdeKeys().pcrPublic)
	if err != nil {
		return err
	}
	entry := FDEEnrollment{
		Schema: 1, Device: device, UUID: uuid, Keyslot: keyslot, Token: token,
		PCRs: "7+14+12+13+11", PCRValues: values, TPMSRK: srk, SecureBoot: secure,
		Fingerprint: fingerprint, EnrolledAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fdeEnrollmentPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temp := fdeEnrollmentPath + ".new"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := fdeSyncFile(temp); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, fdeEnrollmentPath); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Chmod(fdeEnrollmentPath, 0o600)
}

// ReadFDEEnrollment prints the ownership record for root-only inspection. A
// missing record prints nothing so the caller can treat it as absent.
func ReadFDEEnrollment() ([]byte, error) {
	data, err := os.ReadFile(fdeEnrollmentPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// fdePCRValues captures the literal PCR values the enrollment bound, so a
// later boot can detect a changed measured state without guessing.
func fdePCRValues(src native.Source) (map[string]string, error) {
	values := map[string]string{}
	for _, pcr := range []string{"7", "12", "13", "14"} {
		data, err := src.ReadFile("/sys/class/tpm/tpm0/pcr-sha256/" + pcr)
		if err != nil {
			return nil, fmt.Errorf("read PCR %s: %w", pcr, err)
		}
		values[pcr] = strings.TrimSpace(string(data))
	}
	return values, nil
}

// fdePCRsMatch reports whether the current literal PCR values match every
// value the record captured. A record without the full set (older schema or
// a partial write) does not match, so status can offer renewal and the
// record is rewritten with the observed values.
func fdePCRsMatch(src native.Source, record FDEEnrollment) (bool, error) {
	if len(record.PCRValues) == 0 {
		return false, nil
	}
	current, err := fdePCRValues(src)
	if err != nil {
		return false, err
	}
	for _, pcr := range []string{"7", "12", "13", "14"} {
		value, ok := record.PCRValues[pcr]
		if !ok || current[pcr] != value {
			return false, nil
		}
	}
	return true, nil
}

// fdeTPMSRKFile is the live TPM's SRK public key as published by
// systemd-tpm2-setup-early on tmpfs. A disk copy never carries it, and it is
// world-readable, so it identifies the running TPM without a privileged read.
const fdeTPMSRKFile = "/run/systemd/tpm2-srk-public-key.tpm2b_public"

// fdeTPMSRKPersistentFile is the persistent copy systemd keeps on the root
// filesystem for the next boot. It is only a fallback: a disk copy carries
// the old value until a boot refreshes it.
const fdeTPMSRKPersistentFile = "/var/lib/systemd/tpm2-srk-public-key.tpm2b_public"

// fdeSRKFingerprint hashes the running TPM's SRK public key from the live
// tmpfs copy, falling back to the persistent copy only when the live one is
// absent. An empty record value means the record predates the fingerprint.
func fdeSRKFingerprint(src native.Source) (string, error) {
	var errs []error
	for _, path := range []string{fdeTPMSRKFile, fdeTPMSRKPersistentFile} {
		data, err := src.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", path, err))
			continue
		}
		if len(data) == 0 {
			errs = append(errs, fmt.Errorf("%s is empty", path))
			continue
		}
		sum := sha256.Sum256(data)
		return fmt.Sprintf("sha256:%x", sum), nil
	}
	return "", errors.Join(errs...)
}

// fdeTPMSRKMatches reports whether the live TPM is the one the record bound.
func fdeTPMSRKMatches(src native.Source, record FDEEnrollment) (bool, error) {
	current, err := fdeSRKFingerprint(src)
	if err != nil {
		return false, err
	}
	return record.TPMSRK != "" && record.TPMSRK == current, nil
}

// fdePublicKeyDigest fingerprints the PCR public key for the ownership record.
func fdePublicKeyDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

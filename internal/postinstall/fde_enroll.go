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
	Schema      int    `json:"schema"`
	Device      string `json:"device"`
	UUID        string `json:"uuid"`
	Keyslot     string `json:"keyslot"`
	Token       string `json:"token"`
	PCRs        string `json:"pcrs"`
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
}

type fdeLUKSMetadata struct {
	Tokens map[string]struct {
		Type     string   `json:"type"`
		Keyslots []string `json:"keyslots"`
	} `json:"tokens"`
}

// fdeTokens lists every systemd-tpm2 token deterministically, so ownership
// never depends on map iteration order.
func fdeTokens(src native.Source) ([]fdeToken, error) {
	device, _, err := fdeDevice(src)
	if err != nil {
		return nil, err
	}
	out, err := src.Run("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", device)
	if err != nil {
		return nil, fmt.Errorf("read the LUKS2 metadata: %w", err)
	}
	var metadata fdeLUKSMetadata
	if err := json.Unmarshal(out, &metadata); err != nil {
		return nil, fmt.Errorf("parse the LUKS2 metadata: %w", err)
	}
	var tokens []fdeToken
	for tokenID, token := range metadata.Tokens {
		if token.Type != "systemd-tpm2" {
			continue
		}
		observed := fdeToken{ID: tokenID}
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
	return tokens, nil
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
	device, _, err := fdeDevice(src)
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
	switch {
	case len(before) > 1:
		return fmt.Errorf("more than one systemd-tpm2 token exists; inspect the LUKS2 tokens and %s", orphanHint(before[0]))
	case len(before) == 1 && (!owned || record.Keyslot != before[0].Keyslot || record.Token != before[0].ID):
		return fmt.Errorf("a TPM token exists that Nimbus does not own; %s", orphanHint(before[0]))
	case len(before) == 1:
		return errors.New("TPM automatic unlock is already enrolled and recorded; nothing to do")
	}
	keys := fdeKeys()
	args := []string{"sudo", "--", "systemd-cryptenroll",
		"--tpm2-device=auto",
		"--tpm2-pcrs=7+14+12+13",
		"--tpm2-public-key=" + keys.pcrPublic,
		"--tpm2-public-key-pcrs=11",
		"--tpm2-pcrlock=",
		device,
	}
	if err := stream(args[0], args[1:]...); err != nil {
		return fmt.Errorf("systemd-cryptenroll failed: %w", err)
	}
	after, err := fdeTokens(src)
	if err != nil {
		return err
	}
	if len(after) != 1 || after[0].Keyslot == "" {
		return errors.New("enrollment reported success but no single TPM token was observed; inspect the LUKS2 tokens")
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
		return fmt.Errorf("the token is enrolled but its ownership record could not be written (%w); rerun this task to record it", recordErr)
	}
	_, err = fmt.Fprintln(out, "TPM enrollment recorded. Reboot to verify that the disk unlocks without the passphrase; the disk passphrase remains the fallback.")
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
	found := false
	for _, observed := range tokens {
		if observed.ID == token && observed.Keyslot == keyslot {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no systemd-tpm2 token %s in keyslot %s was observed; the record was not written", token, keyslot)
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
		PCRs: "7+14+12+13+11", SecureBoot: secure, Fingerprint: fingerprint,
		EnrolledAt: time.Now().UTC().Format(time.RFC3339),
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

// fdePublicKeyDigest fingerprints the PCR public key for the ownership record.
func fdePublicKeyDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

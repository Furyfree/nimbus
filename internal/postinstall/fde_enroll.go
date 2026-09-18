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
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// fdeEnrollmentPath records which TPM token Nimbus owns. It holds no
// credentials, only the native identifiers needed for scoped removal.
var fdeEnrollmentPath = "/var/lib/nimbus/fde/enrollment.json"

// fdeExecutable resolves the installed engine for root-only internal calls;
// tests override it.
var fdeExecutable = os.Executable

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
	return "/dev/disk/by-uuid/" + uuid, uuid, nil
}

// fdeTokenInfo describes the native TPM token state of the root volume.
type fdeTokenInfo struct {
	Present    bool
	Keyslot    string
	Token      string
	Unreadable bool
}

type fdeLUKSMetadata struct {
	Tokens map[string]struct {
		Type     string   `json:"type"`
		Keyslots []string `json:"keyslots"`
	} `json:"tokens"`
}

// fdeTokenState reads the LUKS2 token metadata read-only and reports the
// systemd-tpm2 token. Native metadata is authoritative; an unreadable device
// stays explicit instead of guessing.
func fdeTokenState(src native.Source) (fdeTokenInfo, error) {
	device, _, err := fdeDevice(src)
	if err != nil {
		return fdeTokenInfo{}, err
	}
	out, err := src.Run("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", device)
	if err != nil {
		return fdeTokenInfo{Unreadable: true}, fmt.Errorf("read the LUKS2 metadata: %w", err)
	}
	var metadata fdeLUKSMetadata
	if err := json.Unmarshal(out, &metadata); err != nil {
		return fdeTokenInfo{Unreadable: true}, fmt.Errorf("parse the LUKS2 metadata: %w", err)
	}
	for tokenID, token := range metadata.Tokens {
		if token.Type != "systemd-tpm2" {
			continue
		}
		info := fdeTokenInfo{Present: true, Token: tokenID}
		if len(token.Keyslots) > 0 {
			info.Keyslot = token.Keyslots[0]
		}
		return info, nil
	}
	return fdeTokenInfo{}, nil
}

// fdeEnrollment reads the ownership record through the root-only helper.
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
	if record.Schema != 1 || record.Device == "" || record.UUID == "" || record.Keyslot == "" {
		return FDEEnrollment{}, false, errors.New("the FDE enrollment record is incomplete")
	}
	return record, true, nil
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
	if current := fdeBootCurrentID(string(out)); current != id {
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
	args := []string{"sudo", "--", "systemd-cryptenroll",
		"--tpm2-device=auto",
		"--tpm2-pcrs=7+14+12+13",
		"--tpm2-public-key=" + keys.pcrPublic,
		"--tpm2-public-key-pcrs=11",
		"--tpm2-pcrlock=",
	}
	if task.fdeRenewSlot != "" {
		args = append(args, "--wipe-slot="+task.fdeRenewSlot)
	}
	args = append(args, "<luks-device>")
	return [][]string{args, {"sudo", "--", "nimbus", "internal", "fde-uki", "record", "<staged-enrollment>"}}, nil
}

func validateFDEEnroll(task Task) error {
	if task.ID != "fde" || (task.Status != Pending && task.Status != Unknown) || task.Action == nil || task.Action.Kind != EnrollFDE {
		return errors.New("invalid FDE enrollment action")
	}
	return nil
}

// RunFDEEnroll performs the approved enrollment or renewal. The passphrase is
// entered in systemd-cryptenroll's own native prompt and never read here.
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
	device, uuid, err := fdeDevice(src)
	if err != nil {
		return err
	}
	info, err := fdeTokenState(src)
	if err != nil {
		return err
	}
	keys := fdeKeys()
	record, owned, err := fdeEnrollment(src)
	if err != nil {
		return err
	}
	if info.Present && (!owned || record.Keyslot != info.Keyslot) {
		return errors.New("a TPM token exists that Nimbus does not own; inspect it before enrolling")
	}
	if !info.Present && owned {
		// The token was removed outside Nimbus; renew to a fresh slot.
		owned = false
	}
	args := []string{"sudo", "--", "systemd-cryptenroll",
		"--tpm2-device=auto",
		"--tpm2-pcrs=7+14+12+13",
		"--tpm2-public-key=" + keys.pcrPublic,
		"--tpm2-public-key-pcrs=11",
		"--tpm2-pcrlock=",
	}
	if owned {
		args = append(args, "--wipe-slot="+record.Keyslot)
	}
	args = append(args, device)
	if err := stream(args[0], args[1:]...); err != nil {
		return fmt.Errorf("systemd-cryptenroll failed: %w", err)
	}
	after, err := fdeTokenState(src)
	if err != nil {
		return err
	}
	if !after.Present || after.Keyslot == "" {
		return errors.New("enrollment reported success but no TPM token was observed")
	}
	if owned && after.Keyslot == record.Keyslot {
		return errors.New("the renewed enrollment reused the wiped slot; inspect the LUKS2 tokens")
	}
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return err
	}
	fingerprint, err := fdePublicKeyDigest(keys.pcrPublic)
	if err != nil {
		return err
	}
	entry := FDEEnrollment{
		Schema: 1, Device: device, UUID: uuid, Keyslot: after.Keyslot, Token: after.Token,
		PCRs: "7+14+12+13+11", SecureBoot: secure, Fingerprint: fingerprint,
	}
	exe, err := fdeExecutable()
	if err != nil {
		return err
	}
	staged, cleanup, err := stageFDEEnrollment(entry)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := stream("sudo", "--", exe, "internal", "fde-uki", "record", staged); err != nil {
		return fmt.Errorf("record the FDE enrollment: %w", err)
	}
	if owned {
		_, err = fmt.Fprintln(out, "Automatic unlock renewed. Reboot to verify that the disk unlocks without the passphrase; the passphrase remains the fallback.")
	} else {
		_, err = fmt.Fprintln(out, "TPM enrollment recorded. Reboot to verify that the disk unlocks without the passphrase; the passphrase remains the fallback.")
	}
	return err
}

// stageFDEEnrollment writes the ownership record to a private staged file the
// root-only recorder validates before installing it.
func stageFDEEnrollment(entry FDEEnrollment) (string, func(), error) {
	dir, err := os.MkdirTemp("", "nimbus-fde-enroll-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	staged := filepath.Join(dir, "enrollment.json")
	data, err := json.Marshal(entry)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.WriteFile(staged, data, 0600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return staged, cleanup, nil
}

// WriteFDEEnrollment validates and installs the staged ownership record. It
// runs as root through the internal command; Nimbus never trusts the caller's
// content beyond these native identifiers.
func WriteFDEEnrollment(staged string) error {
	data, err := os.ReadFile(staged)
	if err != nil {
		return err
	}
	var entry FDEEnrollment
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("parse the staged enrollment record: %w", err)
	}
	if entry.Schema != 1 || !strings.HasPrefix(entry.Device, "/dev/disk/by-uuid/") || entry.UUID == "" ||
		entry.Keyslot == "" || entry.Token == "" || entry.Fingerprint == "" || entry.PCRs == "" ||
		strings.ContainsAny(entry.Keyslot+entry.Token, "/\\ \t\n") {
		return errors.New("the staged enrollment record is invalid")
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
	if err := os.Rename(temp, fdeEnrollmentPath); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Chmod(fdeEnrollmentPath, 0o600); err != nil {
		return err
	}
	return nil
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

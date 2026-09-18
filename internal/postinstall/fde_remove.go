package postinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// FDERemoveCommands describes the reviewed removal operations.
func FDERemoveCommands(task Task) ([][]string, error) {
	if err := validateFDERemove(task); err != nil {
		return nil, err
	}
	return [][]string{
		{"sudo", "--", "systemd-cryptenroll", "--wipe-slot=<recorded-keyslot>", "<luks-device>"},
		{"sudo", "--", "efibootmgr", "-b", "<nimbus-entry>", "-B"},
		{"sudo", "--", "rm", "-f", FDEUKIPath},
		{"sudo", "--", "rm", "-f", FDEUKIMarker},
		{"sudo", "--", "rm", "-f", fdeKeys().mokKey, fdeKeys().mokCert, fdeKeys().mokDER, fdeKeys().pcrPrivate, fdeKeys().pcrPublic},
		{"sudo", "--", "rm", "-f", fdeEnrollmentPath},
		{"sudo", "--", "rmdir", fdeKeyDir},
	}, nil
}

func validateFDERemove(task Task) error {
	if task.ID != "fde" || task.Action == nil || task.Action.Kind != RemoveFDE {
		return errors.New("invalid FDE removal action")
	}
	return nil
}

// fdeOtherUnlockExists reports whether a non-TPM keyslot remains once the
// recorded slot is wiped, so removal can never leave no way in.
func fdeOtherUnlockExists(src native.Source, device, wiped string) (bool, error) {
	out, err := src.Run("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", device)
	if err != nil {
		return false, fmt.Errorf("read the LUKS2 keyslots: %w", err)
	}
	var metadata struct {
		Keyslots map[string]struct {
			Type string `json:"type"`
		} `json:"keyslots"`
	}
	if err := json.Unmarshal(out, &metadata); err != nil {
		return false, fmt.Errorf("parse the LUKS2 keyslots: %w", err)
	}
	for slot, keyslot := range metadata.Keyslots {
		if slot != wiped && keyslot.Type != "" {
			return true, nil
		}
	}
	return false, nil
}

// RunFDERemove removes only what Nimbus owns: the recorded TPM token, the
// Nimbus firmware entry and image, the marker and the key material. The disk
// passphrase, unrelated tokens and Fedora's boot entries stay.
func RunFDERemove(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if err := validateFDERemove(task); err != nil {
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
	device, _, err := fdeDevice(src)
	if err != nil {
		return err
	}
	tokens, err := fdeTokens(src)
	if err != nil {
		return err
	}
	record, owned, err := fdeEnrollment(src)
	if err != nil {
		return err
	}
	if len(tokens) > 0 {
		if len(tokens) > 1 || !owned || record.Keyslot != tokens[0].Keyslot || record.Token != tokens[0].ID {
			return fmt.Errorf("the TPM token is not the one Nimbus recorded; refusing to wipe it (%s)", tokens[0].Keyslot)
		}
		other, err := fdeOtherUnlockExists(src, device, record.Keyslot)
		if err != nil {
			return err
		}
		if !other {
			return errors.New("removing this enrollment would leave no other unlock method; add or keep a disk passphrase first")
		}
		args := []string{"sudo", "--", "systemd-cryptenroll", "--wipe-slot=" + record.Keyslot, device}
		if err := stream(args[0], args[1:]...); err != nil {
			return fmt.Errorf("remove the recorded TPM keyslot: %w", err)
		}
		after, err := fdeTokens(src)
		if err != nil {
			return err
		}
		if len(after) != 0 {
			return errors.New("the recorded enrollment still reports a TPM token; inspect the LUKS2 tokens")
		}
	}
	entries, err := fdeBootEntriesOutput(src)
	if err != nil {
		return err
	}
	for _, entry := range fdeNimbusEntries(entries) {
		if err := stream("sudo", "--", "efibootmgr", "-b", entry.ID, "-B"); err != nil {
			return fmt.Errorf("remove the Nimbus firmware entry %s: %w", entry.ID, err)
		}
	}
	for _, path := range []string{
		FDEUKIPath,
		FDEUKIMarker,
		fdeKeys().mokKey, fdeKeys().mokCert, fdeKeys().mokDER, fdeKeys().pcrPrivate, fdeKeys().pcrPublic,
		fdeEnrollmentPath,
	} {
		if err := stream("sudo", "--", "rm", "-f", path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if err := stream("sudo", "--", "rmdir", fdeKeyDir); err != nil {
		return fmt.Errorf("remove %s: %w", fdeKeyDir, err)
	}
	_, err = fmt.Fprintln(out, "Nimbus automatic unlock was removed. The disk passphrase and Fedora's GRUB entries remain the unlock and boot path.")
	return err
}

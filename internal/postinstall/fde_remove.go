package postinstall

import (
	"context"
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

// fdeOtherUnlockExists reports whether a keyslot remains that no token
// references once the recorded slot is wiped, so removal can never leave no
// way in.
func fdeOtherUnlockExists(src native.Source, wiped string) (bool, error) {
	_, keyslots, referenced, err := fdeLUKSState(src)
	if err != nil {
		return false, err
	}
	for slot, kind := range keyslots {
		if slot != wiped && kind != "" && !referenced[slot] {
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
	device, uuid, err := fdeDevice(src)
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
		if len(tokens) > 1 || !owned || record.Keyslot != tokens[0].Keyslot || record.Token != tokens[0].ID || record.UUID != uuid {
			return fmt.Errorf("the TPM token is not the one Nimbus recorded; refusing to wipe it (%s)", tokens[0].Keyslot)
		}
		other, err := fdeOtherUnlockExists(src, record.Keyslot)
		if err != nil {
			return err
		}
		if !other {
			return errors.New("removing this enrollment would leave no other unlock method; add or keep a disk passphrase first")
		}
	}
	// The boot path is disarmed before the token is wiped, so a partial
	// failure can never leave the Nimbus entry as the default without its key.
	entries, err := fdeBootEntriesOutput(src)
	if err != nil {
		return err
	}
	for _, entry := range fdeNimbusEntries(entries) {
		if err := stream("sudo", "--", "efibootmgr", "-b", entry.ID, "-B"); err != nil {
			return fmt.Errorf("remove the Nimbus firmware entry %s: %w", entry.ID, err)
		}
	}
	if err := stream("sudo", "--", "rm", "-f", FDEUKIMarker); err != nil {
		return fmt.Errorf("remove %s: %w", FDEUKIMarker, err)
	}
	if len(tokens) > 0 {
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
	for _, path := range []string{
		FDEUKIPath,
		fdeKeys().mokKey, fdeKeys().mokCert, fdeKeys().mokDER, fdeKeys().pcrPrivate, fdeKeys().pcrPublic,
		fdeEnrollmentPath,
	} {
		if err := stream("sudo", "--", "rm", "-f", path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if _, err := src.Run("sudo", "-n", "--", "test", "-d", fdeKeyDir); err == nil {
		if err := stream("sudo", "--", "rmdir", fdeKeyDir); err != nil {
			return fmt.Errorf("remove %s: %w", fdeKeyDir, err)
		}
	}
	_, err = fmt.Fprintln(out, "Nimbus automatic unlock was removed. The disk passphrase and Fedora's GRUB entries remain the unlock and boot path. The MOK certificate stays enrolled; remove it with mokutil if desired.")
	return err
}

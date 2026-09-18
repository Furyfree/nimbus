package postinstall

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

const mokPrivateKey = "/etc/pki/akmods/private/private_key.priv"

// RunNVIDIAMOK runs only after the caller previews, approves, locks and rechecks
// the selected task. Password prompts belong to sudo and mokutil, not Nimbus.
func RunNVIDIAMOK(ctx context.Context, src native.Source, out, stderr io.Writer) error {
	stream := func(name string, args ...string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "$ %s %s\n", name, strings.Join(args, " ")); err != nil {
			return err
		}
		return src.Stream(out, stderr, name, args...)
	}
	boot, err := src.Run("mokutil", "--sb-state")
	if err != nil || strings.TrimSpace(string(boot)) != "SecureBoot enabled" {
		return errors.New("NVIDIA MOK setup requires confirmed enabled Secure Boot; no changes were made")
	}
	kernelOut, err := src.Run("uname", "-r")
	if err != nil {
		return err
	}
	kernel := strings.TrimSpace(string(kernelOut))
	if kernel == "" || strings.HasPrefix(kernel, "-") || strings.ContainsAny(kernel, "/\\ \t\r\n") {
		return errors.New("cannot determine a safe running kernel release")
	}
	if err := stream("sudo", "--validate"); err != nil {
		return err
	}
	files, err := src.Run("sudo", "-n", "--", "find", "/etc/pki/akmods", "-maxdepth", "2", "(", "-path", MOKCertificate, "-o", "-path", mokPrivateKey, ")", "-print")
	if err != nil {
		return fmt.Errorf("inspect akmods key files: %w; no key generation was attempted", err)
	}
	paths := strings.Fields(string(files))
	certExists := slices.Contains(paths, MOKCertificate)
	keyExists := slices.Contains(paths, mokPrivateKey)
	if certExists != keyExists {
		return errors.New("akmods signing key pair is incomplete; inspect /etc/pki/akmods before retrying; neither file was replaced")
	}
	if !certExists {
		return errors.New("the akmods signing key pair is missing; reboot once so akmods-keygen generates it before building modules, then retry; no keys were created")
	}
	// Read only the public certificate. The private key stays with native tools.
	der, err := src.Run("sudo", "-n", "--", "cat", MOKCertificate)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil || cert.SerialNumber.Sign() <= 0 {
		return errors.New("akmods public certificate is invalid or has no positive serial number; existing keys were preserved")
	}
	state, err := mokEnrollment(src, MOKCertificate)
	if err != nil {
		return err
	}
	keyID := hex.EncodeToString(cert.SerialNumber.Bytes())
	if !nvidiaSignaturesMatch(src, kernel, keyID) {
		if err := stream("sudo", "--", "akmods", "--force", "--rebuild", "--akmod", "nvidia", "--kernels", kernel); err != nil {
			return fmt.Errorf("rebuild NVIDIA modules: %w; enrollment was not changed", err)
		}
		if !nvidiaSignaturesMatch(src, kernel, keyID) {
			return errors.New("NVIDIA modules do not all report signatures matching the akmods certificate after rebuilding; inspect akmods signing before retrying; enrollment was not changed")
		}
	}
	// Refresh even on a retry with already signed modules: an earlier run might
	// have stopped between the module build and dracut finishing.
	if err := stream("sudo", "--", "dracut", "--force", "--kver", kernel); err != nil {
		return fmt.Errorf("refresh boot image: %w; enrollment was not changed; correct the error and retry", err)
	}
	rebuild, err := FDEUKIRebuildArgv(src, kernel)
	if err != nil {
		return fmt.Errorf("inspect the FDE marker: %w; enrollment was not changed", err)
	}
	if rebuild != nil {
		if err := stream(rebuild[0], rebuild[1:]...); err != nil {
			return fmt.Errorf("rebuild the signed Nimbus image after the initramfs refresh: %w; enrollment was not changed", err)
		}
	}
	if state == mokAbsent {
		if _, err := fmt.Fprintln(out, "Choose a temporary MOK password in the native prompt. The reboot enrollment screen uses US/QWERTY. Nimbus does not record the password."); err != nil {
			return err
		}
		if err := stream("sudo", "--", "mokutil", "--import", MOKCertificate); err != nil {
			return fmt.Errorf("request MOK enrollment: %w; signed modules and boot image remain; retry after correcting the error", err)
		}
		state, err = mokEnrollment(src, MOKCertificate)
		if err != nil {
			return err
		}
		if state == mokAbsent {
			return errors.New("mokutil finished but the enrollment request could not be confirmed; inspect mokutil before rebooting")
		}
	}
	if state == mokRequested {
		_, err = fmt.Fprintln(out, "NVIDIA modules are signed and the boot image is refreshed. Enrollment is pending, not complete.\nReboot when ready. In MOK Manager: Enroll MOK -> Continue -> Yes -> temporary password -> Reboot. Keyboard: US/QWERTY.")
	} else {
		_, err = fmt.Fprintln(out, "NVIDIA modules are signed, the boot image is refreshed, and the certificate is trusted. Reboot when ready to load the driver.")
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "After reboot, verify with: nvidia-smi; mokutil --sb-state. No automatic reboot or Secure Boot changes were made.")
	return err
}

type mokState int

const (
	mokAbsent mokState = iota
	mokRequested
	mokTrusted
)

func mokEnrollment(src native.Source, certificate string) (mokState, error) {
	data, err := src.Run("sudo", "-n", "--", "mokutil", "--ignore-keyring", "--test-key", certificate)
	code := 0
	if err != nil {
		code = -1
		if exit, ok := errors.AsType[interface {
			error
			ExitCode() int
		}](err); ok {
			code = exit.ExitCode()
		}
	}
	// mokutil --test-key exits 0 when the certificate is not enrolled and 1
	// when it is enrolled, pending or blocked, matching VerifyMOK. The
	// --ignore-keyring flag keeps the kernel-keyring shortcut from reporting a
	// key that is not enrolled in the firmware.
	switch strings.TrimSpace(string(data)) {
	case certificate + " is already enrolled", certificate + " is already in db":
		if code == 1 {
			return mokTrusted, nil
		}
	case certificate + " is already in the enrollment request":
		if code == 1 {
			return mokRequested, nil
		}
	case certificate + " is not enrolled":
		if code == 0 {
			return mokAbsent, nil
		}
	}
	return mokAbsent, fmt.Errorf("cannot establish MOK enrollment: %s: %w", strings.TrimSpace(string(data)), errors.Join(err, errors.New("unexpected mokutil result")))
}

func nvidiaSignaturesMatch(src native.Source, kernel, keyID string) bool {
	for _, module := range []string{"nvidia", "nvidia_modeset", "nvidia_drm", "nvidia_uvm"} {
		for _, field := range []string{"signer", "sig_key"} {
			data, err := src.Run("modinfo", "-k", kernel, "-F", field, module)
			value := strings.TrimSpace(string(data))
			if err != nil || value == "" || (field == "sig_key" && strings.TrimLeft(strings.ToLower(strings.ReplaceAll(value, ":", "")), "0") != strings.TrimLeft(keyID, "0")) {
				return false
			}
		}
	}
	return true
}

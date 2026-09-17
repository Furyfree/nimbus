package postinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// The engine package ships the inert kernel-install hook; approved setup owns
// only the marker and the single image the firmware entry points at.
const (
	FDEUKIMarker   = "/etc/nimbus/fde-uki.enabled"
	FDEUKIPath     = "/boot/efi/EFI/Linux/nimbus.efi"
	FDEHookPath    = "/etc/kernel/install.d/90-nimbus-uki.install"
	FDEBootLabel   = "Nimbus UKI"
	FDEBootLoader  = `\EFI\Linux\nimbus.efi`
	fdeESPMount    = "/boot/efi"
	fdeCmdlineFile = "/etc/kernel/cmdline"
	fdeKernelDir   = "/usr/lib/modules"
	fdeMinimumSize = 8 << 20
)

var fdeVersionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// fdeImageSize is a seam so tests can pin the atomic-install contract.
var fdeImageSize = func(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// fdeCmdline selects the embedded command line: the native /etc/kernel/cmdline
// when present, otherwise the running command line without boot-loader
// artifacts, matching systemd's own ukify hook.
func fdeCmdline(src native.Source) (string, error) {
	if data, err := src.ReadFile(fdeCmdlineFile); err == nil {
		var fields []string
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields = append(fields, strings.Fields(line)...)
		}
		if len(fields) > 0 {
			return strings.Join(fields, " "), nil
		}
	}
	data, err := src.ReadFile("/proc/cmdline")
	if err != nil {
		return "", fmt.Errorf("read /proc/cmdline: %w", err)
	}
	var fields []string
	for _, option := range strings.Fields(string(data)) {
		if strings.HasPrefix(option, "BOOT_IMAGE=") || strings.HasPrefix(option, "initrd=") {
			continue
		}
		fields = append(fields, option)
	}
	if len(fields) == 0 {
		return "", errors.New("no usable kernel command line was observed")
	}
	return strings.Join(fields, " "), nil
}

type fdeSigning struct {
	key  string
	cert string
}

// fdeSigningPair returns the akmods MOK key pair when both halves exist. The
// private key is only probed for existence; its content is never retained.
func fdeSigningPair(src native.Source) *fdeSigning {
	if _, err := src.ReadFile(mokPrivateKey); err != nil {
		return nil
	}
	if _, err := src.ReadFile(MOKCertificate); err != nil {
		return nil
	}
	return &fdeSigning{key: mokPrivateKey, cert: MOKCertificate}
}

// fdeSecureBoot reports the firmware SecureBoot variable. A missing variable
// means the firmware does not enforce signatures.
func fdeSecureBoot(src native.Source) (bool, error) {
	names, err := src.ReadDir("/sys/firmware/efi/efivars")
	if err != nil {
		return false, err
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "SecureBoot-") {
			continue
		}
		data, err := src.ReadFile("/sys/firmware/efi/efivars/" + name)
		if err != nil {
			return false, err
		}
		return len(data) > 4 && data[4] == 1, nil
	}
	return false, nil
}

// fdeUKIArgv pins the reviewed ukify invocation.
func fdeUKIArgv(version, cmdline, output string, signing *fdeSigning) []string {
	argv := []string{
		"build",
		"--linux=" + fdeKernelDir + "/" + version + "/vmlinuz",
		"--initrd=/boot/initramfs-" + version + ".img",
		"--cmdline=" + cmdline,
		"--output=" + output,
	}
	if signing != nil {
		argv = append(argv,
			"--secureboot-private-key="+signing.key,
			"--secureboot-certificate="+signing.cert)
	}
	return argv
}

// BuildFDEUKI rebuilds the firmware image for one kernel through native
// ukify. It runs as root from the kernel-install hook and from approved
// setup, and never changes firmware entries or enrollment.
func BuildFDEUKI(src native.Source, version string, out io.Writer) error {
	return buildFDEUKI(src, version, out, FDEUKIPath)
}

func buildFDEUKI(src native.Source, version string, out io.Writer, target string) error {
	if !fdeVersionRE.MatchString(version) {
		return fmt.Errorf("invalid kernel version %q", version)
	}
	if _, err := src.ReadFile(FDEUKIMarker); err != nil {
		return errors.New("the FDE marker is absent; approved setup has not enabled image builds")
	}
	cmdline, err := fdeCmdline(src)
	if err != nil {
		return err
	}
	signing := fdeSigningPair(src)
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	if secure && signing == nil {
		return errors.New("Secure Boot is enabled and no akmods signing key pair exists; enroll a MOK signing key before building a bootable image")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp := target + ".new"
	defer func() { _ = os.Remove(temp) }()
	argv := fdeUKIArgv(version, cmdline, temp, signing)
	if _, err := fmt.Fprintf(out, "ukify %s\n", strings.Join(argv, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, out, "ukify", argv...); err != nil {
		return fmt.Errorf("ukify failed: %w", err)
	}
	size, err := fdeImageSize(temp)
	if err != nil {
		return fmt.Errorf("ukify produced no image: %w", err)
	}
	if size < fdeMinimumSize {
		return fmt.Errorf("ukify image is implausibly small (%d bytes); the previous image is unchanged", size)
	}
	if err := os.Rename(temp, target); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Installed %s for kernel %s.\n", target, version)
	return err
}

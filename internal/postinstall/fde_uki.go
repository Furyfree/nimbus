package postinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
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
	FDEUKITool     = "/usr/bin/ukify"
	fdeMinimumSize = 8 << 20
)

var fdeVersionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

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

// fdeSecureBoot reports whether the firmware enforces signatures, reusing the
// strict efivar parsing from inspection. A missing variable is not enforcing.
func fdeSecureBoot(src native.Source) (bool, error) {
	state, err := inspect.SecureBootState(src)
	if err != nil {
		return false, err
	}
	return state == inspect.SecureBootEnabled, nil
}

// fdeUKIArgv pins the reviewed ukify invocation.
func fdeUKIArgv(version, cmdline, output string) []string {
	return []string{
		"build",
		"--linux=" + fdeKernelDir + "/" + version + "/vmlinuz",
		"--initrd=/boot/initramfs-" + version + ".img",
		"--cmdline=" + cmdline,
		"--output=" + output,
	}
}

// fdeSyncFile flushes a staged image before it replaces the booted one, so a
// crash cannot leave a truncated default boot path on the FAT partition.
func fdeSyncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
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
	ready, err := fdeMarkerReady(src)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("the FDE marker is absent; approved setup has not enabled image builds")
	}
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	if secure {
		return errors.New("Secure Boot is enabled; the signed shim chain is not implemented yet, so no image was built")
	}
	cmdline, err := fdeCmdline(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp := target + ".new"
	defer func() { _ = os.Remove(temp) }()
	argv := fdeUKIArgv(version, cmdline, temp)
	if _, err := fmt.Fprintf(out, "ukify %s\n", strings.Join(argv, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, out, FDEUKITool, argv...); err != nil {
		return fmt.Errorf("ukify failed: %w", err)
	}
	info, err := os.Stat(temp)
	if err != nil {
		return fmt.Errorf("ukify produced no image: %w", err)
	}
	if info.Size() < fdeMinimumSize {
		return fmt.Errorf("ukify image is implausibly small (%d bytes); the previous image is unchanged", info.Size())
	}
	if err := fdeSyncFile(temp); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	_, err = fmt.Fprintf(out, "Installed %s for kernel %s.\n", target, version)
	return err
}

// RemoveFDEUKI reconciles the image when a kernel is removed: if the image
// embeds that kernel, it is rebuilt for the running kernel so the default
// boot entry does not reference a kernel without modules.
func RemoveFDEUKI(src native.Source, version string, out io.Writer) error {
	return removeFDEUKI(src, version, out, FDEUKIPath)
}

func removeFDEUKI(src native.Source, version string, out io.Writer, target string) error {
	ready, err := fdeMarkerReady(src)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	if !fdeVersionRE.MatchString(version) {
		return fmt.Errorf("invalid kernel version %q", version)
	}
	inspected, err := src.Run(FDEUKITool, "inspect", target)
	if err != nil {
		return fmt.Errorf("inspect the current image: %w", err)
	}
	if fdeInspect(string(inspected)).uname != version {
		return nil
	}
	running, err := src.Run("uname", "-r")
	if err != nil {
		return err
	}
	kernel := strings.TrimSpace(string(running))
	if !fdeVersionRE.MatchString(kernel) {
		return fmt.Errorf("cannot determine a safe running kernel release %q", kernel)
	}
	if _, err := fmt.Fprintf(out, "The Nimbus image embedded kernel %s; rebuilding it for %s.\n", version, kernel); err != nil {
		return err
	}
	return buildFDEUKI(src, kernel, out, target)
}

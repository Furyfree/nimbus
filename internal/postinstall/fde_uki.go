package postinstall

import (
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
)

// The engine package ships the inert kernel-install hook; approved setup owns
// only the marker, the key material and the single image the firmware entry
// points at.
const (
	FDEUKIMarker   = plan.FDEMarkerPath
	FDEUKIPath     = "/boot/efi/EFI/Linux/nimbus.efi"
	FDEHookPath    = "/etc/kernel/install.d/90-nimbus-uki.install"
	FDEBootLabel   = "Nimbus UKI"
	FDEBootLoader  = `\EFI\Linux\nimbus.efi`
	fdeESPMount    = "/boot/efi"
	fdeCmdlineFile = "/etc/kernel/cmdline"
	fdeKernelDir   = "/usr/lib/modules"
	FDEUKITool     = "/usr/bin/ukify"
	fdeMinimumSize = 8 << 20
	fdeCrypttab    = "/etc/crypttab"
)

// fdeKeyDir holds the generated key material. It is a variable so tests can
// isolate it; production always uses the applied-state path.
var fdeKeyDir = "/var/lib/nimbus/fde"

var fdeVersionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func fdeKeys() fdeKeyPaths {
	return fdeKeyPaths{
		dir:        fdeKeyDir,
		mokKey:     filepath.Join(fdeKeyDir, "mok.key"),
		mokCert:    filepath.Join(fdeKeyDir, "mok.crt"),
		mokDER:     filepath.Join(fdeKeyDir, "mok.cer"),
		pcrPrivate: filepath.Join(fdeKeyDir, "pcr-private.pem"),
		pcrPublic:  filepath.Join(fdeKeyDir, "pcr-public.pem"),
	}
}

type fdeKeyPaths struct {
	dir        string
	mokKey     string
	mokCert    string
	mokDER     string
	pcrPrivate string
	pcrPublic  string
}

// ensureFDEMOKDER derives the public DER copy mokutil needs. It is public,
// derived data, so a missing copy is always regenerated.
func ensureFDEMOKDER(keys fdeKeyPaths) error {
	if _, err := os.Stat(keys.mokDER); err == nil {
		return nil
	}
	data, err := os.ReadFile(keys.mokCert)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return errors.New("the MOK certificate is not a usable PEM certificate")
	}
	return os.WriteFile(keys.mokDER, block.Bytes, 0o644)
}

// ensureFDEKeys returns the existing key material or generates one complete
// pair set with ukify genkey. Partial material is never completed silently.
func ensureFDEKeys(src native.Source, out io.Writer) (fdeKeyPaths, error) {
	keys := fdeKeys()
	exists := 0
	for _, path := range []string{keys.mokKey, keys.mokCert, keys.pcrPrivate, keys.pcrPublic} {
		if _, err := os.Stat(path); err == nil {
			exists++
		} else if !errors.Is(err, os.ErrNotExist) {
			return keys, err
		}
	}
	if exists == 4 {
		return keys, ensureFDEMOKDER(keys)
	}
	if exists != 0 {
		return keys, errors.New("partial FDE key material under " + keys.dir + "; inspect it before retrying")
	}
	if err := os.MkdirAll(keys.dir, 0o700); err != nil {
		return keys, err
	}
	if err := os.Chmod(keys.dir, 0o700); err != nil {
		return keys, err
	}
	argv := []string{
		"genkey",
		"--secureboot-private-key=" + keys.mokKey,
		"--secureboot-certificate=" + keys.mokCert,
		"--pcr-private-key=" + keys.pcrPrivate,
		"--pcr-public-key=" + keys.pcrPublic,
	}
	if _, err := fmt.Fprintf(out, "ukify %s\n", strings.Join(argv, " ")); err != nil {
		return keys, err
	}
	if err := src.Stream(out, out, FDEUKITool, argv...); err != nil {
		for _, path := range []string{keys.mokKey, keys.mokCert, keys.pcrPrivate, keys.pcrPublic} {
			_ = os.Remove(path)
		}
		return keys, fmt.Errorf("ukify genkey failed: %w", err)
	}
	for _, path := range []string{keys.mokKey, keys.pcrPrivate} {
		if err := os.Chmod(path, 0o400); err != nil {
			return keys, err
		}
	}
	for _, path := range []string{keys.mokCert, keys.pcrPublic} {
		if err := os.Chmod(path, 0o644); err != nil {
			return keys, err
		}
	}
	return keys, ensureFDEMOKDER(keys)
}

// requireFDEKeys returns the setup-generated key material. The kernel-install
// hook never generates secrets; approved setup does that once.
func requireFDEKeys() (fdeKeyPaths, error) {
	keys := fdeKeys()
	for _, path := range []string{keys.mokKey, keys.mokCert, keys.pcrPrivate, keys.pcrPublic} {
		if _, err := os.Stat(path); err != nil {
			return keys, errors.New("the FDE key material is missing or unreadable; run the approved FDE setup to generate it")
		}
	}
	return keys, nil
}

// EnsureFDEKeys is the root-only setup entry point for generating the key
// material once. The kernel-install hook never calls it.
func EnsureFDEKeys(src native.Source, out io.Writer) error {
	_, err := ensureFDEKeys(src, out)
	return err
}

// FDEMOKCertificate returns the DER certificate path for review previews.
func FDEMOKCertificate() string { return fdeKeys().mokDER }

// fdeSigningArgs embeds the signed PCR 11 policy; the Secure Boot pair is only
// used when the firmware enforces signatures.
func fdeSigningArgs(keys fdeKeyPaths, secure bool) []string {
	args := []string{
		"--pcr-private-key=" + keys.pcrPrivate,
		"--pcr-public-key=" + keys.pcrPublic,
		"--phases=enter-initrd",
		"--pcr-banks=sha256",
	}
	if secure {
		args = append([]string{
			"--secureboot-private-key=" + keys.mokKey,
			"--secureboot-certificate=" + keys.mokCert,
		}, args...)
	}
	return args
}

// fdeUnlockOption derives the per-volume unlock option from the native
// crypttab line, so its existing options (discard, x-initrd.attach) survive.
func fdeUnlockOption(src native.Source, cmdline string) (string, error) {
	uuid := ""
	for _, option := range strings.Fields(cmdline) {
		if rest, ok := strings.CutPrefix(option, "rd.luks.uuid=luks-"); ok {
			uuid = rest
			break
		}
	}
	if uuid == "" {
		return "", errors.New("the command line names no LUKS root (rd.luks.uuid); the automatic-unlock option cannot be added")
	}
	data, err := src.ReadFile(fdeCrypttab)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fdeCrypttab, err)
	}
	options := ""
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] != "luks-"+uuid && fields[1] != "UUID="+uuid {
			continue
		}
		found = true
		if len(fields) >= 4 {
			options = strings.Trim(fields[3], `"`)
		}
	}
	if !found {
		return "", fmt.Errorf("%s has no entry for luks-%s; inspect it before retrying", fdeCrypttab, uuid)
	}
	if options == "none" {
		options = ""
	}
	if !strings.Contains(options, "tpm2-device=") {
		if options != "" {
			options += ","
		}
		options += "tpm2-device=auto"
	}
	return "rd.luks.options=" + uuid + "=" + options, nil
}

// fdeAddOptions replaces any earlier value of each option key and appends the
// reviewed values, so a rebuild from the running command line stays stable.
func fdeAddOptions(cmdline string, options ...string) string {
	var prefixes []string
	for _, option := range options {
		if rest, ok := strings.CutPrefix(option, "rd.luks.options="); ok {
			uuid, _, _ := strings.Cut(rest, "=")
			prefixes = append(prefixes, "rd.luks.options="+uuid+"=")
			continue
		}
		key, _, _ := strings.Cut(option, "=")
		prefixes = append(prefixes, key+"=")
	}
	var fields []string
	for _, field := range strings.Fields(cmdline) {
		drop := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(field, prefix) {
				drop = true
				break
			}
		}
		if !drop {
			fields = append(fields, field)
		}
	}
	return strings.Join(append(fields, options...), " ")
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
func fdeUKIArgv(version, cmdline, output string, signing []string) []string {
	argv := []string{
		"build",
		"--linux=" + fdeKernelDir + "/" + version + "/vmlinuz",
		"--initrd=/boot/initramfs-" + version + ".img",
		"--cmdline=" + cmdline,
		"--os-release=@/etc/os-release",
	}
	argv = append(argv, signing...)
	return append(argv, "--output="+output)
}

// fdeBuildCmdline is the exact command line the image embeds: the native base
// plus the reviewed unlock option and hardening. Verification compares against
// this, never the raw base.
func fdeBuildCmdline(src native.Source) (string, error) {
	cmdline, err := fdeCmdline(src)
	if err != nil {
		return "", err
	}
	unlock, err := fdeUnlockOption(src, cmdline)
	if err != nil {
		return "", err
	}
	return fdeAddOptions(cmdline, unlock, "rd.shell=0", "rd.emergency=reboot"), nil
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

// FDEUKIRebuildArgv returns the approved command that rebuilds the signed
// image for one kernel after an initramfs change, or nil when the marker shows
// no approved FDE setup. Callers run it through their own privileged runner.
func FDEUKIRebuildArgv(src native.Source, version string) ([]string, error) {
	ready, err := fdeMarkerReady(src)
	if err != nil || !ready {
		return nil, err
	}
	exe, err := fdeExecutable()
	if err != nil {
		return nil, err
	}
	return []string{"sudo", "--", exe, "internal", "fde-uki", "add", version, "--only-if-current"}, nil
}

// BuildFDEUKI rebuilds the firmware image for one kernel through native
// ukify. It runs as root from the kernel-install hook and from approved
// setup, and never changes firmware entries or enrollment.
func BuildFDEUKI(src native.Source, version string, out io.Writer) error {
	return buildFDEUKI(src, version, out, FDEUKIPath, false)
}

// BuildFDEUKICurrent rebuilds the image only when it does not already embed a
// different kernel. Initramfs reconciles use it so they cannot replace an
// image the kernel-install hook built for a newer installed kernel.
func BuildFDEUKICurrent(src native.Source, version string, out io.Writer) error {
	return buildFDEUKI(src, version, out, FDEUKIPath, true)
}

func buildFDEUKI(src native.Source, version string, out io.Writer, target string, onlyIfCurrent bool) error {
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
	if onlyIfCurrent {
		if _, statErr := os.Stat(target); statErr == nil {
			current, err := src.Run(FDEUKITool, "inspect", target)
			if err != nil {
				return fmt.Errorf("the existing image could not be inspected; it was not replaced: %w", err)
			}
			embedded := fdeInspect(string(current)).uname
			if embedded == "" {
				return errors.New("the existing image does not identify its kernel; it was not replaced")
			}
			if embedded != version {
				_, err := fmt.Fprintf(out, "the image already targets kernel %s; it was not rebuilt for %s\n", embedded, version)
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("the existing image could not be examined; it was not replaced: %w", statErr)
		}
	}
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	cmdline, err := fdeBuildCmdline(src)
	if err != nil {
		return err
	}
	keys, err := requireFDEKeys()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp := target + ".new"
	defer func() { _ = os.Remove(temp) }()
	argv := fdeUKIArgv(version, cmdline, temp, fdeSigningArgs(keys, secure))
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
	if secure {
		if _, err := src.Run("sbverify", "--cert", keys.mokCert, temp); err != nil {
			return fmt.Errorf("the signed image did not verify: %w", err)
		}
	}
	inspected, err := src.Run(FDEUKITool, "inspect", temp)
	if err != nil {
		return fmt.Errorf("inspect the staged image: %w", err)
	}
	sections := fdeInspect(string(inspected)).sections
	for _, want := range []string{".pcrsig:", ".pcrpkey:"} {
		if !slices.Contains(sections, want) {
			return fmt.Errorf("the staged image lacks the %s section; the previous image is unchanged", strings.TrimSuffix(want, ":"))
		}
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
	return buildFDEUKI(src, kernel, out, target, false)
}

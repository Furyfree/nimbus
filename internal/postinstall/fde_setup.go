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
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
)

const fdeMarkerText = `# Nimbus owns this marker. Its presence activates the kernel-install
# hook /etc/kernel/install.d/90-nimbus-uki.install, which rebuilds
# /boot/efi/EFI/Linux/nimbus.efi for new kernels through the installed
# engine. Removing the file stops rebuilds and keeps the current image.
`

const fdeSizeSlack = 64 << 20

// FDEEvidence records a verified image and stays distinct from the enrollment
// evidence that the next milestone adds.
const FDEEvidence = "fde.uki"

// fdeMarkerReady reports whether the marker already matches the reviewed
// content. A foreign file is never overwritten.
func fdeMarkerReady(src native.Source) (bool, error) {
	data, err := src.ReadFile(FDEUKIMarker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(data) != fdeMarkerText {
		return false, errors.New("/etc/nimbus/fde-uki.enabled exists with unfamiliar content; inspect it before retrying")
	}
	return true, nil
}

// fdeMarkerChange returns the reviewed marker write and the digest the
// privileged helper binds it to.
func fdeMarkerChange(src native.Source) (plan.FileChange, string, error) {
	have, err := inspect.ObserveFile(src, FDEUKIMarker)
	if err != nil {
		return plan.FileChange{}, "", fmt.Errorf("inspect %s: %w", FDEUKIMarker, err)
	}
	if have.Exists {
		return plan.FileChange{}, "", errors.New("the FDE marker changed after approval; inspect it before retrying")
	}
	after := inspect.SystemFile{Exists: true, Content: []byte(fdeMarkerText), Owner: "root", Group: "root", Mode: "0644"}
	data, err := json.Marshal(struct {
		Before inspect.SystemFile
		After  inspect.SystemFile
	}{have, after})
	if err != nil {
		return plan.FileChange{}, "", err
	}
	return plan.FileChange{Target: FDEUKIMarker, Before: have, After: after}, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

type fdeBootEntry struct {
	ID     string
	Loader string
}

// fdeBootEntries returns entries whose description is the Nimbus label from
// plain efibootmgr output.
func fdeBootEntries(output string) []fdeBootEntry {
	var entries []fdeBootEntry
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "Boot") {
			continue
		}
		rawID, rest, ok := strings.Cut(line[4:], " ")
		if !ok {
			continue
		}
		id := strings.TrimSuffix(rawID, "*")
		if _, err := strconv.ParseUint(id, 16, 16); err != nil {
			continue
		}
		label, device, ok := strings.Cut(rest, "\t")
		if !ok || strings.TrimSpace(label) != FDEBootLabel {
			continue
		}
		entries = append(entries, fdeBootEntry{ID: id, Loader: fdeLoaderPath(device)})
	}
	return entries
}

// fdeLoaderPath normalizes the loader file path in an efibootmgr device path.
// Long device-path forms may contain earlier ")/" pairs, so only the last one
// separates the ESP from the loader file.
func fdeLoaderPath(device string) string {
	index := strings.LastIndex(device, ")/")
	if index < 0 {
		return ""
	}
	return strings.TrimPrefix(strings.ReplaceAll(device[index+2:], "/", `\`), `\`)
}

func fdeEntryCorrect(entries []fdeBootEntry) bool {
	want := strings.TrimPrefix(FDEBootLoader, `\`)
	return slices.ContainsFunc(entries, func(entry fdeBootEntry) bool {
		return strings.EqualFold(entry.Loader, want)
	})
}

func fdeBootEntriesOutput(src native.Source) ([]fdeBootEntry, error) {
	out, err := src.Run("efibootmgr")
	if err != nil {
		return nil, fmt.Errorf("inspect firmware boot entries: %w", err)
	}
	return fdeBootEntries(string(out)), nil
}

var fdeESPDeviceRE = []*regexp.Regexp{
	regexp.MustCompile(`^(nvme\d+n\d+)p(\d+)$`),
	regexp.MustCompile(`^(mmcblk\d+)p(\d+)$`),
	regexp.MustCompile(`^([a-z]+)(\d+)$`),
}

// fdeESPDevice resolves the ESP partition to the disk and partition number
// efibootmgr needs.
func fdeESPDevice(src native.Source) (string, string, error) {
	data, err := src.ReadFile("/proc/mounts")
	if err != nil {
		return "", "", err
	}
	source := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == fdeESPMount {
			source = fields[0]
		}
	}
	device, ok := strings.CutPrefix(source, "/dev/")
	if !ok || strings.ContainsAny(device, "\\\"' \t") {
		return "", "", fmt.Errorf("%s is not a mounted partition device (%q)", fdeESPMount, source)
	}
	for _, re := range fdeESPDeviceRE {
		if match := re.FindStringSubmatch(device); match != nil {
			return "/dev/" + match[1], match[2], nil
		}
	}
	return "", "", fmt.Errorf("unrecognized ESP device %q", source)
}

// fdeEnsureEntry makes the firmware entry point at the current image. Only
// same-label entries are replaced; everything else is left untouched.
func fdeEnsureEntry(src native.Source, out, errOut io.Writer) error {
	entries, err := fdeBootEntriesOutput(src)
	if err != nil {
		return err
	}
	if fdeEntryCorrect(entries) {
		_, err := fmt.Fprintln(out, "Firmware entry already points at the Nimbus image.")
		return err
	}
	disk, part, err := fdeESPDevice(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(out, "$ sudo -- efibootmgr -b %s -B\n", entry.ID); err != nil {
			return err
		}
		if err := src.Stream(out, errOut, "sudo", "--", "efibootmgr", "-b", entry.ID, "-B"); err != nil {
			return fmt.Errorf("remove stale firmware entry %s: %w", entry.ID, err)
		}
	}
	args := []string{"sudo", "--", "efibootmgr", "-c", "-d", disk, "-p", part, "-L", FDEBootLabel, "-l", FDEBootLoader}
	if _, err := fmt.Fprintf(out, "$ %s\n", strings.Join(args, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, errOut, args[0], args[1:]...); err != nil {
		return fmt.Errorf("create firmware entry: %w", err)
	}
	after, err := fdeBootEntriesOutput(src)
	if err != nil {
		return err
	}
	if !fdeEntryCorrect(after) {
		return errors.New("the firmware entry was created but does not reference the Nimbus image")
	}
	return nil
}

func fdeFileSize(src native.Source, path string) (int64, error) {
	out, err := src.Run("stat", "--format=%s", "--", path)
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", path, err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("unrecognized size for %s", path)
	}
	return size, nil
}

// fdeSpaceCheck requires room for the image plus slack before any change.
func fdeSpaceCheck(src native.Source, kernel string, out io.Writer) error {
	var need int64
	for _, path := range []string{fdeKernelDir + "/" + kernel + "/vmlinuz", "/boot/initramfs-" + kernel + ".img"} {
		size, err := fdeFileSize(src, path)
		if err != nil {
			return err
		}
		need += size
	}
	avail, err := src.Run("df", "--output=avail", "-B1", fdeESPMount)
	if err != nil {
		return fmt.Errorf("inspect the EFI system partition: %w", err)
	}
	fields := strings.Fields(string(avail))
	if len(fields) < 2 {
		return errors.New("unrecognized df output for the EFI system partition")
	}
	free, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil {
		return errors.New("unrecognized free space on the EFI system partition")
	}
	if free < need+fdeSizeSlack {
		return fmt.Errorf("the EFI system partition has %d bytes free; the image needs about %d bytes plus %d bytes of slack", free, need, int64(fdeSizeSlack))
	}
	_, err = fmt.Fprintf(out, "EFI system partition: %d bytes free, image needs about %d bytes.\n", free, need)
	return err
}

func validateFDESetup(task Task) error {
	if task.ID != "fde" || (task.Status != Pending && task.Status != Unknown) || task.Action == nil || task.Action.Kind != SetupFDE {
		return errors.New("invalid FDE setup action")
	}
	return nil
}

// FDESetupCommands describes the fixed operations in the approval preview.
func FDESetupCommands(task Task) ([][]string, error) {
	if err := validateFDESetup(task); err != nil {
		return nil, err
	}
	return [][]string{
		{"sudo", "--", "nimbus", "internal", "system-file", "--plan", "<approved-digest>", "--payload", "<verified-marker-change>"},
		{"sudo", "--", "nimbus", "internal", "fde-uki", "add", "<running-kernel>"},
		{"sudo", "--", "efibootmgr", "-b", "<stale-nimbus-entry>", "-B"},
		{"sudo", "--", "efibootmgr", "-c", "-d", "<esp-disk>", "-p", "<esp-partition>", "-L", FDEBootLabel, "-l", FDEBootLoader},
	}, nil
}

// RunFDESetup runs only after the caller previews, approves, locks and
// rechecks the task. It writes the marker, builds the image and
// ensures the firmware entry. It never enrolls a TPM or removes anything.
func RunFDESetup(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if err := validateFDESetup(task); err != nil {
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
	// Rollback must run even when the context was cancelled after the marker
	// was written, so the hook never stays armed without an image.
	rollback := func(name string, args ...string) error {
		if _, err := fmt.Fprintf(out, "$ %s %s\n", name, strings.Join(args, " ")); err != nil {
			return err
		}
		return src.Stream(out, errOut, name, args...)
	}
	if err := stream("sudo", "--validate"); err != nil {
		return err
	}
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	if secure {
		return errors.New("Secure Boot is enabled; the signed shim chain is not implemented yet, and no changes were made")
	}
	kernelOut, err := src.Run("uname", "-r")
	if err != nil {
		return err
	}
	kernel := strings.TrimSpace(string(kernelOut))
	if !fdeVersionRE.MatchString(kernel) {
		return fmt.Errorf("cannot determine a safe running kernel release %q", kernel)
	}
	if err := fdeSpaceCheck(src, kernel, out); err != nil {
		return err
	}
	ready, err := fdeMarkerReady(src)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wroteMarker := false
	if !ready {
		change, digest, err := fdeMarkerChange(src)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(apply.FilePayload{PlanDigest: digest, Change: change})
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "nimbus-fde-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		staged := filepath.Join(dir, "marker.json")
		if err := os.WriteFile(staged, payload, 0600); err != nil {
			return err
		}
		if err := stream("sudo", "--", exe, "internal", "system-file", "--plan", digest, "--payload", staged); err != nil {
			return fmt.Errorf("write the FDE marker: %w", err)
		}
		wroteMarker = true
	}
	if err := stream("sudo", "--", exe, "internal", "fde-uki", "add", kernel); err != nil {
		if wroteMarker {
			if rollbackErr := fdeRemoveMarker(src, rollback, exe); rollbackErr != nil {
				return errors.Join(fmt.Errorf("build the Nimbus image: %w", err), fmt.Errorf("remove the FDE marker after the failed build: %w", rollbackErr))
			}
			return fmt.Errorf("build the Nimbus image: %w; the FDE marker was removed and the hook stays inert", err)
		}
		return fmt.Errorf("build the Nimbus image: %w", err)
	}
	if err := fdeEnsureEntry(src, out, errOut); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Reboot when ready to boot the Nimbus image. Fedora's GRUB entries and the disk passphrase remain the fallback path.")
	return err
}

// fdeRemoveMarker reverses only the marker this run wrote, bound to its
// observed content and metadata.
func fdeRemoveMarker(src native.Source, stream func(string, ...string) error, exe string) error {
	have, err := inspect.ObserveFile(src, FDEUKIMarker)
	if err != nil {
		return err
	}
	if !have.Exists || string(have.Content) != fdeMarkerText {
		return errors.New("the marker changed after the failed build; inspect it manually")
	}
	change := plan.FileChange{Target: FDEUKIMarker, Before: have, After: inspect.SystemFile{}}
	data, err := json.Marshal(change)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	payload, err := json.Marshal(apply.FilePayload{PlanDigest: digest, Change: change})
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "nimbus-fde-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	staged := filepath.Join(dir, "marker-removal.json")
	if err := os.WriteFile(staged, payload, 0600); err != nil {
		return err
	}
	return stream("sudo", "--", exe, "internal", "system-file", "--plan", digest, "--payload", staged)
}

// VerifyFDE performs the approved read-only checks that unprivileged
// inspection cannot: the image parses and embeds the expected command line.
func VerifyFDE(src native.Source, t Task) Task {
	t.PreviouslyVerified = false
	t.VerificationNeedsRoot = false
	t.Status = Unknown
	ready, err := fdeMarkerReady(src)
	if err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	if !ready {
		t.Status, t.Detail = Pending, "Setup is not active; run the approved FDE setup first."
		return t
	}
	entries, err := fdeBootEntriesOutput(src)
	if err != nil {
		t.Detail = "The firmware entries could not be inspected: " + err.Error()
		return t
	}
	if !fdeEntryCorrect(entries) {
		t.Status, t.Detail = Pending, "No correct Nimbus firmware entry was observed; rerun the approved setup to repair it."
		return t
	}
	expected, err := fdeCmdline(src)
	if err != nil {
		t.Detail = "The expected command line could not be observed: " + err.Error()
		return t
	}
	out, err := src.Run(FDEUKITool, "inspect", FDEUKIPath)
	if err != nil {
		t.VerificationNeedsRoot = true
		t.Detail = "The Nimbus image could not be read or inspected; retry when sudo is available."
		return t
	}
	inspected := fdeInspect(string(out))
	damaged := func(detail string) Task {
		t.Status, t.Detail = Pending, detail
		t.Action = &Action{Kind: SetupFDE}
		t.Reboot = true
		return t
	}
	for _, want := range []string{".linux:", ".initrd:", ".cmdline:", ".osrel:", ".uname:"} {
		if !slices.Contains(inspected.sections, want) {
			return damaged("The Nimbus image lacks the " + strings.TrimSuffix(want, ":") + " section; rebuild it with the approved setup.")
		}
	}
	if inspected.cmdline != expected {
		return damaged("The Nimbus image does not embed the expected command line; rebuild it with the approved setup.")
	}
	if !fdeVersionRE.MatchString(inspected.uname) {
		return damaged("The Nimbus image does not identify its kernel release; rebuild it with the approved setup.")
	}
	if _, err := src.Run("stat", "--format=%s", "--", fdeKernelDir+"/"+inspected.uname+"/vmlinuz"); err != nil {
		return damaged("The Nimbus image embeds kernel " + inspected.uname + ", whose kernel package is no longer installed; rebuild it for an installed kernel with the approved setup.")
	}
	t.Status, t.Detail = Complete, "The Nimbus image parses, embeds the expected command line for an installed kernel and its firmware entry is correct. TPM enrollment and automatic unlock are still not configured."
	return t
}

// fdeInspectResult captures the expected sections and the embedded command
// line and kernel release from ukify inspect output.
type fdeInspectResult struct {
	sections []string
	cmdline  string
	uname    string
}

// fdeInspect extracts section names and the text payloads Nimbus verifies.
func fdeInspect(output string) fdeInspectResult {
	var result fdeInspectResult
	section, inText := "", false
	var text []string
	flush := func() {
		switch section {
		case ".cmdline:":
			result.cmdline = strings.TrimSpace(strings.Join(text, " "))
		case ".uname:":
			result.uname = strings.TrimSpace(strings.Join(text, " "))
		}
		text = nil
	}
	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ".") && strings.HasSuffix(trimmed, ":") {
			flush()
			section, inText = trimmed, false
			result.sections = append(result.sections, trimmed)
			continue
		}
		if !inText {
			inText = trimmed == "text:"
			continue
		}
		text = append(text, trimmed)
	}
	flush()
	return result
}

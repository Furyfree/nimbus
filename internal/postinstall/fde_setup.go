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

const fdeMarkerText = plan.FDEMarkerText

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
	ID      string
	Label   string
	Loader  string
	Options string
	Active  bool
}

// fdeBootEntries returns every firmware boot entry with its loader and load
// option data, so the Fedora shim entry can be resolved as well as Nimbus's.
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
		active := strings.HasSuffix(rawID, "*")
		id := strings.TrimSuffix(rawID, "*")
		if _, err := strconv.ParseUint(id, 16, 16); err != nil {
			continue
		}
		label, device, ok := strings.Cut(rest, "\t")
		if !ok {
			continue
		}
		loader, options := fdeSplitDevice(device)
		if loader == "" {
			continue
		}
		entries = append(entries, fdeBootEntry{ID: id, Label: strings.TrimSpace(label), Loader: loader, Options: options, Active: active})
	}
	return entries
}

// fdeBootOrder returns the BootOrder ids, or nil when the line is absent.
func fdeBootOrder(output string) []string {
	for line := range strings.SplitSeq(output, "\n") {
		rest, ok := strings.CutPrefix(line, "BootOrder:")
		if !ok {
			continue
		}
		var ids []string
		for _, id := range strings.Split(strings.TrimSpace(rest), ",") {
			if id != "" {
				ids = append(ids, strings.ToUpper(id))
			}
		}
		return ids
	}
	return nil
}

// fdeCorrectEntryID returns the id of one active, matching Nimbus entry.
func fdeCorrectEntryID(entries []fdeBootEntry, secure bool) (string, bool) {
	for _, entry := range fdeNimbusEntries(entries) {
		if fdeEntryMatches(entry, entries, secure) {
			return entry.ID, true
		}
	}
	return "", false
}

// fdeSplitDevice separates the loader file path from the trailing optional
// data in an efibootmgr device path. It accepts both the raw form
// (`...)/\EFI\fedora\shimx64.efi<options>`) and the `File(...)` form some
// builds print.
func fdeSplitDevice(device string) (string, string) {
	index := strings.LastIndex(device, ")/")
	if index < 0 {
		return "", ""
	}
	rest := strings.TrimPrefix(strings.ReplaceAll(device[index+2:], "/", `\`), `\`)
	if file := strings.Index(rest, "File("); file >= 0 {
		loader, options := "", ""
		if end := strings.Index(rest[file+len("File("):], ")"); end >= 0 {
			loader = strings.TrimPrefix(rest[file+len("File("):file+len("File(")+end], ".")
			tail := rest[file+len("File(")+end+1:]
			if next := strings.Index(tail, "File("); next >= 0 {
				if end2 := strings.Index(tail[next+len("File("):], ")"); end2 >= 0 {
					options = strings.TrimPrefix(tail[next+len("File("):next+len("File(")+end2], ".")
				}
			}
		}
		return loader, options
	}
	end := strings.Index(strings.ToLower(rest), ".efi")
	if end < 0 {
		return rest, ""
	}
	end += len(".efi")
	return rest[:end], rest[end:]
}

func fdeLoaderPath(device string) string {
	loader, _ := fdeSplitDevice(device)
	return loader
}

// fdeNimbusEntries filters the firmware entries Nimbus owns by label.
func fdeNimbusEntries(entries []fdeBootEntry) []fdeBootEntry {
	var nimbus []fdeBootEntry
	for _, entry := range entries {
		if entry.Label == FDEBootLabel {
			nimbus = append(nimbus, entry)
		}
	}
	return nimbus
}

// fdeShimLoader resolves the installed Fedora shim from its own firmware
// entry, so the signed chain follows the distribution instead of a hardcoded
// path.
func fdeShimLoader(entries []fdeBootEntry) (string, bool) {
	for _, entry := range entries {
		if entry.Label == "Fedora" && strings.Contains(strings.ToLower(entry.Loader), "shim") {
			return entry.Loader, true
		}
	}
	return "", false
}

// fdeOptionsEqual matches the load option data as efibootmgr prints it: raw
// UCS-2 hex when unterminated, or text when the firmware kept a terminator.
func fdeOptionsEqual(options, want string) bool {
	if options == "" {
		return want == ""
	}
	if strings.HasPrefix(options, "0x") || strings.HasPrefix(options, "0X") {
		options = options[2:]
	}
	hex := len(options) > 0 && len(options)%4 == 0
	for _, r := range options {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			hex = false
			break
		}
	}
	if hex {
		// efibootmgr prints the raw UTF-16LE bytes in order, so each
		// four-digit group is low byte first.
		var decoded strings.Builder
		for i := 0; i+4 <= len(options); i += 4 {
			low, errLow := strconv.ParseUint(options[i:i+2], 16, 8)
			high, errHigh := strconv.ParseUint(options[i+2:i+4], 16, 8)
			if errLow != nil || errHigh != nil {
				return false
			}
			decoded.WriteRune(rune(low | high<<8))
		}
		return decoded.String() == want
	}
	return options == want
}

// fdeEntryCorrect reports whether one active Nimbus entry matches the
// reviewed loader, with the shim plus load option when Secure Boot is
// enforced.
func fdeEntryCorrect(entries []fdeBootEntry, secure bool) bool {
	_, ok := fdeCorrectEntryID(entries, secure)
	return ok
}

// fdeEntryMatches reports whether one entry matches the reviewed form.
func fdeEntryMatches(entry fdeBootEntry, entries []fdeBootEntry, secure bool) bool {
	if !entry.Active {
		return false
	}
	if secure {
		shim, ok := fdeShimLoader(entries)
		if !ok {
			return false
		}
		return strings.EqualFold(entry.Loader, shim) && fdeOptionsEqual(entry.Options, FDEBootLoader+" ")
	}
	want := strings.TrimPrefix(FDEBootLoader, `\`)
	return strings.EqualFold(entry.Loader, want) && entry.Options == ""
}

// fdeEntryReady reports exactly one Nimbus entry pointing at the current
// image; a same-label duplicate still needs cleanup.
func fdeEntryReady(entries []fdeBootEntry, secure bool) bool {
	return len(fdeNimbusEntries(entries)) == 1 && fdeEntryCorrect(entries, secure)
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
// same-label entries are replaced; everything else is left untouched. A stale
// same-label entry is removed even when another same-label entry is correct,
// so no shadowed duplicate keeps an earlier position in BootOrder.
func fdeEnsureEntry(src native.Source, out, errOut io.Writer) error {
	raw, err := src.Run("efibootmgr")
	if err != nil {
		return fmt.Errorf("inspect firmware boot entries: %w", err)
	}
	entries := fdeBootEntries(string(raw))
	secure, err := fdeSecureBoot(src)
	if err != nil {
		return fmt.Errorf("read the Secure Boot state: %w", err)
	}
	if fdeEntryReady(entries, secure) {
		if id, ok := fdeCorrectEntryID(entries, secure); ok {
			if err := fdePromoteEntry(src, out, errOut, fdeBootOrder(string(raw)), id); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(out, "Firmware entry already points at the Nimbus image.")
		return err
	}
	disk, part, err := fdeESPDevice(src)
	if err != nil {
		return err
	}
	shim := ""
	if secure {
		var ok bool
		if shim, ok = fdeShimLoader(entries); !ok {
			return errors.New("no Fedora shim boot entry was found; the signed chain cannot be created")
		}
	}
	for _, entry := range fdeNimbusEntries(entries) {
		if _, err := fmt.Fprintf(out, "$ sudo -- efibootmgr -b %s -B\n", entry.ID); err != nil {
			return err
		}
		if err := src.Stream(out, errOut, "sudo", "--", "efibootmgr", "-b", entry.ID, "-B"); err != nil {
			return fmt.Errorf("remove stale firmware entry %s: %w", entry.ID, err)
		}
	}
	args := []string{"sudo", "--", "efibootmgr", "-c", "-d", disk, "-p", part, "-L", FDEBootLabel, "-l"}
	if secure {
		args = append(args, `\`+shim, "-u", FDEBootLoader+" ")
	} else {
		args = append(args, FDEBootLoader)
	}
	if _, err := fmt.Fprintf(out, "$ %s\n", strings.Join(args, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, errOut, args[0], args[1:]...); err != nil {
		return fmt.Errorf("create firmware entry: %w", err)
	}
	raw, err = src.Run("efibootmgr")
	if err != nil {
		return err
	}
	entries = fdeBootEntries(string(raw))
	if !fdeEntryCorrect(entries, secure) {
		return errors.New("the firmware entry was created but does not reference the Nimbus image")
	}
	id, ok := fdeCorrectEntryID(entries, secure)
	if !ok {
		return errors.New("the Nimbus firmware entry disappeared after creation")
	}
	return fdePromoteEntry(src, out, errOut, fdeBootOrder(string(raw)), id)
}

// fdePromoteEntry puts the Nimbus entry first in BootOrder, so the firmware
// boots the signed image by default while Fedora's entry stays next.
func fdePromoteEntry(src native.Source, out, errOut io.Writer, order []string, id string) error {
	if len(order) > 0 && strings.EqualFold(order[0], id) {
		_, err := fmt.Fprintln(out, "The Nimbus image is already the default boot target.")
		return err
	}
	next := []string{id}
	for _, existing := range order {
		if !strings.EqualFold(existing, id) {
			next = append(next, existing)
		}
	}
	args := append([]string{"sudo", "--", "efibootmgr", "-o"}, strings.Join(next, ","))
	if _, err := fmt.Fprintf(out, "$ %s\n", strings.Join(args, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, errOut, args[0], args[1:]...); err != nil {
		return fmt.Errorf("set the default boot target: %w", err)
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
	commands := [][]string{
		{"sudo", "--", "nimbus", "internal", "fde-uki", "genkey"},
	}
	if task.fdeSecure {
		commands = append(commands, []string{"sudo", "--", "mokutil", "--import", FDEMOKCertificate()})
	}
	commands = append(commands,
		[]string{"sudo", "--", "nimbus", "internal", "system-file", "--plan", "<approved-digest>", "--payload", "<verified-marker-change>"},
		[]string{"sudo", "--", "nimbus", "internal", "fde-uki", "add", "<running-kernel>"},
		[]string{"sudo", "--", "efibootmgr", "-b", "<stale-nimbus-entry>", "-B"},
		[]string{"sudo", "--", "efibootmgr", "-o", "<nimbus-entry>,<remaining-boot-order>"},
	)
	entry := []string{"sudo", "--", "efibootmgr", "-c", "-d", "<esp-disk>", "-p", "<esp-partition>", "-L", FDEBootLabel, "-l"}
	if task.fdeSecure {
		entry = append(entry, "<fedora-shim-loader>", "-u", FDEBootLoader+" ")
	} else {
		entry = append(entry, FDEBootLoader)
	}
	return append(commands, entry), nil
}

// fdeMOKState maps the repository's MOK parser onto the FDE certificate.
func fdeMOKState(src native.Source, der string) (bool, bool, error) {
	state, err := mokEnrollment(src, der)
	if err != nil {
		return false, false, err
	}
	switch state {
	case mokTrusted:
		return true, false, nil
	case mokRequested:
		return false, true, nil
	default:
		return false, false, nil
	}
}

// fdeEnsureMOK requests the native MOK enrollment once. Nimbus never sees the
// temporary password; mokutil and MokManager prompt for it directly.
func fdeEnsureMOK(src native.Source, out, errOut io.Writer, keys fdeKeyPaths) error {
	enrolled, pending, err := fdeMOKState(src, keys.mokDER)
	if err != nil {
		return err
	}
	if enrolled {
		_, err := fmt.Fprintln(out, "The Nimbus MOK certificate is already enrolled.")
		return err
	}
	if pending {
		_, err := fmt.Fprintln(out, "A MOK enrollment for this certificate is already pending; complete Enroll MOK at the next boot.")
		return err
	}
	args := []string{"sudo", "--", "mokutil", "--import", keys.mokDER}
	if _, err := fmt.Fprintf(out, "$ %s\n", strings.Join(args, " ")); err != nil {
		return err
	}
	if err := src.Stream(out, errOut, args[0], args[1:]...); err != nil {
		return fmt.Errorf("request MOK enrollment: %w", err)
	}
	_, err = fmt.Fprintln(out, "Complete Enroll MOK in MokManager at the next boot with the temporary password you entered.")
	return err
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
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := stream("sudo", "--", exe, "internal", "fde-uki", "genkey"); err != nil {
		return fmt.Errorf("generate the FDE key material: %w", err)
	}
	keys := fdeKeys()
	if secure {
		if err := fdeEnsureMOK(src, out, errOut, keys); err != nil {
			return err
		}
	}
	ready, err := fdeMarkerReady(src)
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
	if secure {
		_, err = fmt.Fprintln(out, "Reboot when ready. Complete Enroll MOK in MokManager with the temporary password, then boot the Nimbus image; Fedora's GRUB entries and the disk passphrase remain the fallback path.")
	} else {
		_, err = fmt.Fprintln(out, "Reboot when ready to boot the Nimbus image. Fedora's GRUB entries and the disk passphrase remain the fallback path.")
	}
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
	bootOutput, err := src.Run("efibootmgr")
	if err != nil {
		t.Detail = "The firmware entries could not be inspected: " + err.Error()
		return t
	}
	entries := fdeBootEntries(string(bootOutput))
	secure, err := fdeSecureBoot(src)
	if err != nil {
		t.Detail = "The Secure Boot state could not be read: " + err.Error()
		return t
	}
	t.fdeSecure = secure
	if !fdeEntryReady(entries, secure) {
		t.Status, t.Detail = Pending, "No correct Nimbus firmware entry was observed; rerun the approved setup to repair it."
		return t
	}
	expected, err := fdeBuildCmdline(src)
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
	for _, want := range []string{".linux:", ".initrd:", ".cmdline:", ".osrel:", ".uname:", ".pcrsig:", ".pcrpkey:"} {
		if !slices.Contains(inspected.sections, want) {
			return damaged("The Nimbus image lacks the " + strings.TrimSuffix(want, ":") + " section; rebuild it with the approved setup.")
		}
	}
	if secure {
		if !slices.Contains(inspected.sections, ".sbat:") {
			return damaged("The Nimbus image lacks its SBAT metadata; rebuild it with the approved setup.")
		}
		keys := fdeKeys()
		enrolled, pending, err := fdeMOKState(src, keys.mokDER)
		if err != nil {
			t.VerificationNeedsRoot = true
			t.Detail = "The MOK enrollment state could not be read; retry when sudo is available."
			return t
		}
		if !enrolled {
			t.Status = Pending
			t.Action = &Action{Kind: SetupFDE}
			t.Reboot = true
			if pending {
				t.Detail = "MOK enrollment is pending; complete Enroll MOK at the next boot."
			} else {
				t.Detail = "The Nimbus MOK certificate is not enrolled; rerun the approved setup."
			}
			return t
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
	if inspected.initrd == "" {
		return damaged("The Nimbus image does not identify its initramfs; rebuild it with the approved setup.")
	}
	sum, err := src.Run("sudo", "-n", "--", "sha256sum", "/boot/initramfs-"+inspected.uname+".img")
	if err != nil {
		t.VerificationNeedsRoot = true
		t.Detail = "The embedded initramfs could not be compared with /boot: " + err.Error()
		return t
	}
	fields := strings.Fields(string(sum))
	if len(fields) == 0 {
		t.VerificationNeedsRoot = true
		t.Detail = "The embedded initramfs could not be compared with /boot: sha256sum returned no digest."
		return t
	}
	if !strings.EqualFold(fields[0], inspected.initrd) {
		return damaged("The Nimbus image embeds an initramfs that differs from /boot; rebuild it with the approved setup.")
	}
	tokens, err := fdeTokens(src)
	if err != nil {
		t.VerificationNeedsRoot = true
		t.Detail = "The LUKS2 TPM token state could not be read: " + err.Error()
		return t
	}
	record, owned, err := fdeEnrollment(src)
	if err != nil {
		t.VerificationNeedsRoot = true
		t.Detail = "The FDE enrollment record could not be read: " + err.Error()
		return t
	}
	entryID, _ := fdeCorrectEntryID(entries, secure)
	switch {
	case len(tokens) == 1 && owned && tokens[0].Slots == 1 && record.Keyslot == tokens[0].Keyslot && record.Token == tokens[0].ID:
		current := fdeBootCurrentID(string(bootOutput))
		if current == "" {
			t.VerificationNeedsRoot = true
			t.Detail = "The current firmware boot entry could not be determined; verify the Nimbus entry with root access."
			return t
		}
		if !strings.EqualFold(current, entryID) {
			t.Status = Complete
			t.Detail = "The recorded TPM keyslot is present. This boot used another firmware entry, where the policy does not apply; reboot through the Nimbus image to unlock automatically."
			return t
		}
		match, err := fdePCRsMatch(src, record)
		if err != nil {
			t.VerificationNeedsRoot = true
			t.Detail = "The measured PCR values could not be read: " + err.Error()
			return t
		}
		if !match {
			t.Status = Pending
			t.Action = &Action{Kind: RenewFDE}
			t.Reboot = true
			t.Detail = "The recorded measured state is missing or differs from this boot, so the TPM policy no longer matches. Renew the enrollment to record the current values; until then the disk passphrase unlocks."
			return t
		}
		t.Status = Complete
		t.Detail = "The signed Nimbus image is booting and the recorded TPM keyslot matches the recorded measured state. Reboots unlock automatically; the disk passphrase remains the fallback."
	case len(tokens) == 1 && (!owned || tokens[0].Slots != 1):
		t.Status = Blocked
		t.Detail = "A systemd-tpm2 token exists that Nimbus does not own as a single-key slot; " + orphanHint(tokens[0])
	case len(tokens) > 1:
		t.Status = Blocked
		t.Detail = "More than one systemd-tpm2 token exists; inspect the LUKS2 tokens before changing enrollment."
	case len(tokens) == 1:
		t.Status = Blocked
		t.Detail = "A systemd-tpm2 token exists that Nimbus does not own; " + orphanHint(tokens[0])
	default:
		t.Status = Pending
		t.Action = &Action{Kind: EnrollFDE}
		t.Reboot = true
		t.Detail = "The signed Nimbus image is booting. Reboot into it if this boot did not, then enroll TPM automatic unlock; the disk passphrase remains available."
	}
	return t
}

// fdeInspectResult captures the expected sections and the embedded command
// line and kernel release from ukify inspect output.
type fdeInspectResult struct {
	sections []string
	cmdline  string
	uname    string
	initrd   string
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
			if section == ".initrd:" {
				if rest, ok := strings.CutPrefix(trimmed, "sha256:"); ok {
					result.initrd = strings.TrimSpace(rest)
				}
			}
			inText = trimmed == "text:"
			continue
		}
		text = append(text, trimmed)
	}
	flush()
	return result
}

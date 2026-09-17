package postinstall

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
)

// fdeTask inspects the machine read-only and explains the native TPM
// automatic-unlock flow. Setup and enrollment are separate approved work; this
// task never enrolls, unlocks or writes. It is selected with the fde component.
func fdeTask(src native.Source, in Inputs) Task {
	t := Task{
		ID: "fde", Owner: "component:fde",
		Title: "Set up TPM automatic disk unlock", Status: Unknown,
		Prerequisites: []string{
			"The root filesystem is a LUKS2 volume on an EFI system with a TPM2 device.",
			"The selected fde packages are applied and recorded by Nimbus.",
		},
		Instructions: []string{
			"Approved setup will build and sign a Unified Kernel Image with ukify, install it with kernel-install and enroll the TPM2 device through systemd-cryptenroll.",
			"The existing disk passphrase stays valid; enrollment adds a scoped TPM keyslot and removal targets only that slot.",
			"Sync and upgrades never enroll a machine or change the unlock policy.",
		},
		Verification: "Inspection reads the mounted root, the TPM2 device, the firmware mode and the installed tools. Shim presence, EFI space and keyslots need an approved privileged check.",
		Recovery:     "The disk passphrase always remains a valid unlock path. Removal and policy renewal are offered by the explicit task only.",
	}
	mounts, err := src.ReadFile("/proc/mounts")
	if err != nil {
		t.Detail = "The mounted root could not be inspected; read /proc/mounts and retry"
		return t
	}
	if !encryptedRoot(src, rootSource(string(mounts))) {
		t.Status, t.Detail = NotApplicable, "The root filesystem is not a LUKS2 mapper; automatic unlock keeps the passphrase only."
		return t
	}
	switch in.Facts.SecureBoot.Value {
	case inspect.SecureBootEnabled:
		setup, err := setupMode(src)
		if err != nil {
			t.Detail = "The firmware Setup Mode state could not be read; inspect bootctl status, then retry"
			return t
		}
		if setup {
			t.Status, t.Detail = NotApplicable, "The firmware is in Setup Mode; enroll Secure Boot keys before automatic unlock."
			return t
		}
	case inspect.SecureBootDisabled:
	case inspect.SecureBootUnavailable:
		t.Status, t.Detail = NotApplicable, "EFI Secure Boot state is unavailable; this setup keeps the passphrase only."
		return t
	default:
		t.Detail = "Secure Boot state could not be read; inspect bootctl status, then retry"
		return t
	}
	names, err := src.ReadDir("/sys/class/tpm")
	if errors.Is(err, os.ErrNotExist) {
		t.Status, t.Detail = NotApplicable, "No TPM device was found; the disk passphrase remains the only unlock path."
		return t
	}
	if err != nil {
		t.Detail = "The TPM device class could not be inspected; retry"
		return t
	}
	if !slices.Contains(names, "tpm0") {
		t.Status, t.Detail = NotApplicable, "No TPM device was found; the disk passphrase remains the only unlock path."
		return t
	}
	version, err := src.ReadFile("/sys/class/tpm/tpm0/tpm_version_major")
	if err != nil {
		t.Detail = "The TPM version could not be read; inspect /sys/class/tpm, then retry"
		return t
	}
	if strings.TrimSpace(string(version)) != "2" {
		t.Status, t.Detail = NotApplicable, "The TPM is not version 2; automatic unlock keeps the passphrase only."
		return t
	}
	for _, name := range []string{"systemd-ukify", "sbsigntools"} {
		found := false
		for _, pkg := range in.Resolved.Packages {
			if pkg.Name != name {
				continue
			}
			found = true
			if status, detail := packageReady(in, pkg); status != Complete {
				t.Status, t.Detail = status, detail
				return t
			}
		}
		if !found {
			t.Status, t.Detail = Blocked, "The selected fde component is missing the "+name+" package prerequisite."
			return t
		}
	}
	var missing []string
	for _, tool := range []string{"systemd-cryptenroll", "ukify", "sbsign", "kernel-install", "dracut"} {
		if _, err := src.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		t.Status, t.Detail = Blocked, "The setup tools are missing ("+strings.Join(missing, ", ")+"); repair the selected fde packages with nimbus sync."
		return t
	}
	t.Status = Pending
	t.Detail = "LUKS2 root, TPM2 and the signed-UKI tools are present. Shim presence and the keyslot policy need the approved privileged check. The approved setup action is not implemented yet; no changes are offered."
	return t
}

// rootSource returns the device mounted at / from /proc/mounts contents. The
// last match wins, because later mounts shadow earlier ones.
func rootSource(mounts string) string {
	root := ""
	for line := range strings.SplitSeq(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "/" {
			root = fields[0]
		}
	}
	return root
}

// encryptedRoot reports whether the mounted root device is a LUKS2 mapper.
// The device-mapper UUID is authoritative, so any mapper name works.
func encryptedRoot(src native.Source, source string) bool {
	uuid, err := dmUUID(src, source)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(uuid), "CRYPT-LUKS2-")
}

// dmUUID resolves the device-mapper UUID for a mapper mount source. Sysfs
// exposes dm devices as dm-N, so /dev/mapper names are matched through
// dm/name; /dev/dm-N sources are read directly.
func dmUUID(src native.Source, source string) (string, error) {
	if name, ok := strings.CutPrefix(source, "/dev/mapper/"); ok {
		name, _, _ = strings.Cut(name, "[")
		names, err := src.ReadDir("/sys/class/block")
		if err != nil {
			return "", err
		}
		for _, entry := range names {
			if !strings.HasPrefix(entry, "dm-") {
				continue
			}
			deviceName, err := src.ReadFile("/sys/class/block/" + entry + "/dm/name")
			if err != nil || strings.TrimSpace(string(deviceName)) != name {
				continue
			}
			data, err := src.ReadFile("/sys/class/block/" + entry + "/dm/uuid")
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		return "", errors.New("mapper not found in /sys/class/block")
	}
	if device, ok := strings.CutPrefix(source, "/dev/"); ok && strings.HasPrefix(device, "dm-") {
		data, err := src.ReadFile("/sys/class/block/" + device + "/dm/uuid")
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", errors.New("not a device-mapper source")
}

// setupMode reports whether the firmware is in Setup Mode, which disables
// Secure Boot enforcement without clearing the SecureBoot variable.
func setupMode(src native.Source) (bool, error) {
	names, err := src.ReadDir("/sys/firmware/efi/efivars")
	if err != nil {
		return false, err
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "SetupMode-") {
			continue
		}
		data, err := src.ReadFile("/sys/firmware/efi/efivars/" + name)
		if err != nil {
			return false, err
		}
		return len(data) > 4 && data[4] == 1, nil
	}
	return false, errors.New("SetupMode variable not found")
}

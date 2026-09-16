package postinstall

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

//go:embed dtu/network.py
var dtuNetworkHelper string

// DTUProfile contains only non-secret evidence, never the native settings.
type DTUExistingProfile struct {
	UUID string `json:"uuid"`
	SSID string `json:"ssid"`
}

type DTUProfile struct {
	Existing   []DTUExistingProfile `json:"existing,omitempty"`
	Observed   string               `json:"observed"`
	Configured bool                 `json:"configured"`
	Connected  bool                 `json:"connected"`
}

var dtuItemPattern = regexp.MustCompile(`^[a-z0-9]{26}$`)

func ValidDTUItem(item string) bool { return dtuItemPattern.MatchString(item) }

// DTU uses these packages as runtime prerequisites; it does not modify them.
// A recorded baseline identity can establish availability without claiming
// package ownership. Existing ownership receipts still take precedence.
func dtuPackageReady(in Inputs, pkg definitions.ResolvedPackage) (Status, string) {
	status, detail := packageReady(in, pkg)
	if status == Complete || !in.Facts.Packages.Known() {
		return status, detail
	}
	if _, recorded := in.Applied.Receipts["package:"+pkg.Canonical]; recorded {
		return status, detail
	}
	if pkg.Canonical != "dnf:"+pkg.Name || !in.Applied.Present || in.Applied.Baseline == nil || in.Applied.Baseline.Schema != state.BaselineSchema {
		return status, detail
	}
	matches := slices.DeleteFunc(slices.Clone(in.Facts.Packages.Value), func(p inspect.Package) bool {
		return p.Name != pkg.Name || (p.Arch != "x86_64" && p.Arch != "noarch")
	})
	if len(matches) == 1 && in.Applied.InBaseline(matches[0].ID()) {
		return Complete, "Installed prerequisite matches the recorded package baseline; package ownership is unchanged."
	}
	return status, detail
}

// DTUProfileCommand runs the embedded, reviewed helper in isolated Python mode.
// Only code and public arguments enter argv. Credentials stay inside Python.
func DTUProfileCommand(action string, args ...string) []string {
	return append([]string{"python3", "-I", "-B", "-c", dtuNetworkHelper, action}, args...)
}

func inspectDTUProfile(src native.Source) (DTUProfile, error) {
	argv := DTUProfileCommand("inspect")
	data, err := src.Run(argv[0], argv[1:]...)
	if err != nil {
		return DTUProfile{}, errors.New("cannot inspect DTU profile; check NetworkManager, Python DBus support and profile ownership")
	}
	var profile DTUProfile
	if json.Unmarshal(data, &profile) != nil || !pictureHash.MatchString(profile.Observed) {
		return profile, errors.New("invalid DTU profile observation")
	}
	return profile, nil
}

func dtuNetwork(src native.Source, in Inputs) Task {
	t := dtuCertificateTask(src, in)
	t.Title = "Set up DTU eduroam on Linux"
	t.Instructions = []string{
		"Install or repair the reviewed DTU CAT CA bundle for NetworkManager; no global trust changes.",
		"Create the Nimbus DTU eduroam profile only when no eduroam/DTUsecure profile exists, or after explicit deletion/replacement approval. Other Wi-Fi profiles are preserved.",
		"Choose manual entry or a 1Password item UUID (not a vault UUID), using its username and password fields. A bare username receives @dtu.dk; another realm is rejected.",
		"Use PEAP/MSCHAPv2 with the pinned CA and exact DTU server names. NetworkManager saves the password in its root-only system connection file (unencrypted within that file); Nimbus keeps no copy.",
		"Enable automatic connection after saving and verifying the profile/password; NetworkManager may connect when eduroam is available. Ask separately whether to try connecting now. If eduroam is not found, keep setup configured for later.",
	}
	t.Verification = "Verify the CA, ownership, labels and native profile security settings. Profile configured is not proof of password acceptance, Internet access or reconnection after reboot."
	t.Recovery = "Rerun after partial setup; existing profiles are kept by default. Approved deletion cannot restore old credentials if recreation fails. Edit credentials through NetworkManager or remove only your Nimbus DTU eduroam profile there before recreating it. Reset/component removal preserves profiles and the CA; review references before removing the certificate."
	if t.Status != Pending && t.Status != Complete {
		return t
	}
	for _, name := range []string{"python3", "python3-dbus"} {
		i := slices.IndexFunc(in.Resolved.Packages, func(p definitions.ResolvedPackage) bool { return p.Canonical == "dnf:"+name })
		if i < 0 {
			t.Status, t.Detail, t.Action = Blocked, "Sync dtu-network to install Python and its native DBus bindings first.", nil
			return t
		}
		if status, detail := dtuPackageReady(in, in.Resolved.Packages[i]); status != Complete {
			t.Status, t.Detail, t.Action = status, detail, nil
			return t
		}
	}
	profile, err := inspectDTUProfile(src)
	if err != nil {
		t.Status, t.Detail, t.Action = Unknown, err.Error(), nil
		return t
	}
	action := &Action{Kind: ConfigureDTUNetwork, DTUProfile: &profile}
	if t.Action != nil {
		action.DTU = t.Action.DTU
	}
	if t.Status == Complete {
		t.Detail = "Prepare DTU eduroam through the native NetworkManager settings API."
	}
	verified := t.Status == Complete && profile.Configured
	t.Status, t.Action = Pending, action
	if len(profile.Existing) > 0 {
		t.Status = Complete
		t.Detail = fmt.Sprintf("%d existing eduroam/DTUsecure profile(s) found. Setup is skipped unless you explicitly approve deletion and replacement; existing settings are not certified by Nimbus.", len(profile.Existing))
		if verified {
			t.Detail = "Nimbus DTU profile and CA configuration verified. Rerunning keeps them unless replacement is explicitly approved; Internet access and reboot reconnection are not verified."
		}
	}
	return t
}

func dtuCertificateAction(task Task) Task {
	action := *task.Action
	action.Kind, action.DTUProfile = InstallDTUCertificate, nil
	task.Action = &action
	return task
}

func DTUSetupCommands(task Task) ([][]string, error) {
	if task.ID != "dtu-network" || task.Status != Pending || task.Action == nil || task.Action.Kind != ConfigureDTUNetwork || task.Action.DTUProfile == nil || !pictureHash.MatchString(task.Action.DTUProfile.Observed) || len(task.Action.Argv) != 0 || len(task.Action.Commands) != 0 {
		return nil, errors.New("invalid DTU network action")
	}
	var commands [][]string
	if task.Action.DTU != nil {
		var err error
		commands, err = DTUCommands(dtuCertificateAction(task))
		if err != nil {
			return nil, err
		}
	}
	return append(commands, []string{"python3", "<embedded DTU CAT adaptation>", "configure", "(private credential prompts; separate connection confirmation)"}), nil
}

func RunDTUSetup(ctx context.Context, src native.Source, out, errOut io.Writer, task Task, item string, replaceExisting bool) error {
	if _, err := DTUSetupCommands(task); err != nil {
		return err
	}
	if len(task.Action.DTUProfile.Existing) > 0 && !replaceExisting {
		return errors.New("existing DTU profiles require explicit replacement approval")
	}
	if item != "" && !ValidDTUItem(item) {
		return errors.New("invalid 1Password item UUID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	profile, err := inspectDTUProfile(src)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(profile, *task.Action.DTUProfile) {
		return errors.New("DTU profile changed after approval; inspect and retry")
	}
	if task.Action.DTU != nil {
		if err := RunDTUCertificate(ctx, src, out, errOut, dtuCertificateAction(task)); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	argv := DTUProfileCommand("configure", "--expected", profile.Observed)
	if replaceExisting {
		argv = append(argv, "--replace-existing")
	}
	if item != "" {
		argv = append(argv, "--onepassword-item", item)
	}
	if err := src.Stream(out, errOut, argv[0], argv[1:]...); err != nil {
		return errors.New("DTU guided setup did not finish; inspect its message and rerun to recover partial setup")
	}
	profile, err = inspectDTUProfile(src)
	if err != nil {
		return err
	}
	if !profile.Configured {
		return errors.New("DTU profile configuration could not be verified")
	}
	return nil
}

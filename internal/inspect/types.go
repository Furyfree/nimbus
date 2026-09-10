package inspect

// Section is one fact family. A collection failure leaves Value empty and
// records why in Error; an unknown fact is never a guessed default.
type Section[T any] struct {
	Value T      `json:"value"`
	Error string `json:"error,omitempty"`
}

// Known reports whether the section was collected.
func (s Section[T]) Known() bool { return s.Error == "" }

// Facts is the structured observation of one system.
type Facts struct {
	Platform     Section[Platform]     `json:"platform"`
	Packages     Section[[]Package]    `json:"packages"`
	Repositories Section[[]Repository] `json:"repositories"`
	Flatpak      Section[Flatpak]      `json:"flatpak"`
	SecureBoot   Section[string]       `json:"secure_boot"`
	SELinux      Section[string]       `json:"selinux"`
	Firewalld    Section[string]       `json:"firewalld"`
	Checkout     Section[Checkout]     `json:"checkout"`
	// DNFDropIn is the content of Nimbus's libdnf5 drop-in, empty when the
	// file is absent.
	DNFDropIn Section[string]   `json:"dnf_drop_in"`
	Hardware  Section[Hardware] `json:"hardware"`
	Chezmoi   Section[Chezmoi]  `json:"chezmoi"`
	User      Section[User]     `json:"user"`
	Commands  map[string]string `json:"commands"`
}

// User identifies the invoking user and bootstrap home.
type User struct {
	Name string `json:"name,omitempty"`
	Home string `json:"home"`
}

// Chezmoi is the state of the user's Chezmoi initialization, read through
// its own data output: whether it exists, and the machine and profiles it
// stored at initialization.
type Chezmoi struct {
	OnePasswordSSH  bool     `json:"one_password_ssh"`
	Initialized     bool     `json:"initialized"`
	Machine         string   `json:"machine,omitempty"`
	ManagedByNimbus bool     `json:"managed_by_nimbus,omitzero"`
	Profiles        []string `json:"profiles,omitempty"`
}

// Hardware is the machine identity from DMI and the display adapters from
// PCI: what init uses to propose a tracked machine and its components.
type Hardware struct {
	Product string `json:"product"` // DMI product name
	Board   string `json:"board"`   // DMI board name
	// Chassis is "laptop" or "desktop" from the SMBIOS chassis type, or
	// empty when the type says neither.
	Chassis string      `json:"chassis"`
	Display []PCIDevice `json:"display"`
}

// PCIDevice is one display-class PCI device by vendor and device ID.
type PCIDevice struct {
	Vendor string `json:"vendor"`
	Device string `json:"device"`
}

// Platform is the operating system identity.
type Platform struct {
	ID         string `json:"id"`
	VersionID  string `json:"version_id"`
	PrettyName string `json:"pretty_name"`
	Arch       string `json:"arch"`
}

// Package is one installed RPM as DNF records it.
type Package struct {
	Name     string `json:"name"`
	Epoch    string `json:"epoch"`
	Version  string `json:"version"`
	Release  string `json:"release"`
	Arch     string `json:"arch"`
	FromRepo string `json:"from_repo"`
	Reason   string `json:"reason"` // user, dependency, group, weak-dependency, external, unknown
}

// EVR renders epoch:version-release with the epoch omitted when zero.
func (p Package) EVR() string {
	if p.Epoch == "" || p.Epoch == "0" {
		return p.Version + "-" + p.Release
	}
	return p.Epoch + ":" + p.Version + "-" + p.Release
}

// Repository is one section of a file below /etc/yum.repos.d.
type Repository struct {
	ID              string   `json:"id"`
	File            string   `json:"file"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	GPGCheck        string   `json:"gpgcheck"` // "1", "0", or "" when unset
	GPGKey          string   `json:"gpgkey"`
	KeyFingerprints []string `json:"key_fingerprints,omitempty"`
	KeyError        string   `json:"key_error,omitempty"`
	Priority        string   `json:"priority"`
	BaseURL         string   `json:"baseurl,omitempty"`
	Metalink        string   `json:"metalink,omitempty"`
	Mirrorlist      string   `json:"mirrorlist,omitempty"`
	// Options is every key of the section as written in its file, so a
	// file Nimbus owns can be compared whole with what Nimbus would write.
	Options map[string]string `json:"options,omitempty"`
	// Overrides lists the files below /etc/dnf/repos.override.d that
	// changed this repository's effective values.
	Overrides       []string          `json:"overrides,omitempty"`
	OverrideOptions map[string]string `json:"override_options,omitempty"`
}

// Flatpak is the system installation's remotes and applications.
type Flatpak struct {
	Remotes []FlatpakRemote `json:"remotes"`
	Apps    []FlatpakApp    `json:"apps"`
}

// FlatpakRemote is one system remote.
type FlatpakRemote struct {
	Name            string   `json:"name"`
	URL             string   `json:"url"`
	GPGVerify       bool     `json:"gpg_verify"`
	KeyFingerprints []string `json:"key_fingerprints,omitempty"`
	KeyError        string   `json:"key_error,omitempty"`
}

// FlatpakApp is one system application.
type FlatpakApp struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Origin  string `json:"origin"`
}

// Checkout is the local Git state of the selected Nimbus checkout.
type Checkout struct {
	Root   string `json:"root"`
	Origin string `json:"origin"`
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

// Security states.
const (
	SecureBootEnabled     = "enabled"
	SecureBootDisabled    = "disabled"
	SecureBootUnavailable = "unavailable" // no EFI variables: legacy boot or a container

	SELinuxEnforcing  = "enforcing"
	SELinuxPermissive = "permissive"
	SELinuxDisabled   = "disabled"
)

// RequiredCommands are the native tools the engine needs on the host.
var RequiredCommands = []string{"dnf5", "rpm", "flatpak", "systemctl", "git"}

// OptionalCommands are tools the engine drives when present; their absence
// is a state, not a failure. Both lists are looked up into Facts.Commands.
var OptionalCommands = []string{"chezmoi"}

// Package definitions loads, validates, resolves, and hashes the desired
// configuration in a Nimbus checkout. It reads configuration only: it never
// inspects the system, runs a command, uses the network, or writes.
package definitions

// CurrentSchema is the definition schema this engine reads.
const CurrentSchema = 1

// Root is nimbus.toml.
type Root struct {
	Schema        int                   `toml:"schema"`
	Compatibility Compatibility         `toml:"compatibility"`
	Repositories  map[string]Repository `toml:"repositories"`
	// DNF holds libdnf5 [main] options, written verbatim to a drop-in
	// below /etc/dnf/libdnf5.conf.d before any package transaction. Values
	// are numbers, booleans, or strings.
	DNF map[string]any `toml:"dnf"`
}

// Compatibility declares the supported Fedora releases and the minimum engine.
type Compatibility struct {
	Fedora    []string `toml:"fedora"`
	MinEngine string   `toml:"min_engine"`
}

// Repository is one package source other than Fedora. Its table key is the
// prefix package references use.
type Repository struct {
	Kind           string `toml:"kind"`
	BaseURL        string `toml:"baseurl"`
	ReleasePackage string `toml:"release_package"`
	SHA256         string `toml:"sha256"`
	Project        string `toml:"project"`
	URL            string `toml:"url"`
	KeyURL         string `toml:"key_url"`
	KeyFile        string `toml:"key_file"`
	Key            string `toml:"key"`
	// Priority is a pointer so an omitted value is distinguishable from 0.
	Priority *int `toml:"priority"`
}

// Machine is machines/<id>.toml.
type Machine struct {
	Schema int    `toml:"schema"`
	ID     string `toml:"id"`
	// Hardware is a substring of the DMI product or board name that
	// identifies this machine, so init can propose it on that hardware.
	Hardware string `toml:"hardware,omitempty"`
	// Shell selects the invoking user's default login shell.
	Shell              string            `toml:"shell,omitempty"`
	Profiles           []string          `toml:"profiles"`
	Components         []string          `toml:"components"`
	Packages           []string          `toml:"packages"`
	PackageExclusions  []string          `toml:"package_exclusions"`
	PackageConstraints map[string]string `toml:"package_constraints"`
	Dotfiles           *Dotfiles         `toml:"dotfiles"`
}

// Dotfiles names the Chezmoi repository for the handoff.
type Dotfiles struct {
	Repo string `toml:"repo"`
}

// Profile is profiles/<id>.toml.
type Profile struct {
	Schema     int      `toml:"schema"`
	ID         string   `toml:"id"`
	Packages   []string `toml:"packages"`
	Components []string `toml:"components"`
}

// Component is components/<id>.toml.
type Component struct {
	Schema        int           `toml:"schema"`
	ID            string        `toml:"id"`
	Requires      []string      `toml:"requires"`
	Conflicts     []string      `toml:"conflicts"`
	Packages      []string      `toml:"packages"`
	Removes       []string      `toml:"removes"`
	Files         []FileDecl    `toml:"files"`
	Services      []ServiceDecl `toml:"services"`
	Groups        []GroupDecl   `toml:"groups"`
	DefaultTarget string        `toml:"default_target"`
	// Detect says which hardware makes init propose this component.
	Detect *Detect `toml:"detect"`
	// Installer is a user-scope tool the maker's installer script places
	// below the home directory, such as Mise.
	Installer *Installer `toml:"installer"`
}

// Installer describes a maker's installer script and the binary it leaves
// relative to the home directory. Chezmoi owns subsequent user configuration.
type Installer struct {
	URL    string `toml:"url"`
	Binary string `toml:"binary"`
}

// Detect is a hardware rule: the chassis kind, "laptop" or "desktop", or
// the PCI vendor ID of a display adapter, such as "1002" for AMD.
type Detect struct {
	Chassis       string `toml:"chassis"`
	DisplayVendor string `toml:"display_vendor"`
}

// FileDecl is one generic system file below /etc. Source is relative to
// system/root/ and the target is the same path below /.
type FileDecl struct {
	Source   string   `toml:"source"`
	Owner    string   `toml:"owner"`
	Group    string   `toml:"group"`
	Mode     string   `toml:"mode"`
	Triggers []string `toml:"triggers"`
}

// ServiceDecl keeps persistent enablement separate from current activation.
type ServiceDecl struct {
	Unit    string `toml:"unit" json:"unit"`
	Enabled *bool  `toml:"enabled" json:"enabled,omitempty"`
	Running *bool  `toml:"running" json:"running,omitempty"`
}

// GroupDecl adds one supplementary membership, preserving prior membership.
type GroupDecl struct {
	Name string `toml:"name" json:"name"`
	User string `toml:"user" json:"user"`
}

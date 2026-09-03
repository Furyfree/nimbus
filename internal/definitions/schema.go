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
	Priority       int    `toml:"priority"`
}

// Machine is machines/<id>.toml.
type Machine struct {
	Schema            int       `toml:"schema"`
	ID                string    `toml:"id"`
	Profiles          []string  `toml:"profiles"`
	Components        []string  `toml:"components"`
	Packages          []string  `toml:"packages"`
	PackageExclusions []string  `toml:"package_exclusions"`
	Dotfiles          *Dotfiles `toml:"dotfiles"`
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
	Schema    int        `toml:"schema"`
	ID        string     `toml:"id"`
	Requires  []string   `toml:"requires"`
	Conflicts []string   `toml:"conflicts"`
	Packages  []string   `toml:"packages"`
	Removes   []string   `toml:"removes"`
	Files     []FileDecl `toml:"files"`
}

// FileDecl is one generic system file below /etc. Source is relative to
// system/root/ and the target is the same path below /.
type FileDecl struct {
	Source string `toml:"source"`
	Owner  string `toml:"owner"`
	Group  string `toml:"group"`
	Mode   string `toml:"mode"`
}

package state

// SourceOwnership preserves native identity and the enablement Nimbus found.
// A missing value on an older receipt cannot authorize source retirement.
type SourceOwnership struct {
	Original []NativeSource `json:"original"`
	Applied  []NativeSource `json:"applied"`
}

// NativeSource describes a verified DNF section or system Flatpak remote.
type NativeSource struct {
	ID         string `json:"id"`
	File       string `json:"file,omitempty"`
	URL        string `json:"url,omitempty"`
	Metalink   string `json:"metalink,omitempty"`
	Mirrorlist string `json:"mirrorlist,omitempty"`
	GPGKey     string `json:"gpgkey,omitempty"`
	Verify     bool   `json:"verify"`
	Keys       string `json:"keys,omitempty"`
	Enabled    bool   `json:"enabled"`
}

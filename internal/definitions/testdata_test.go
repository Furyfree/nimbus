package definitions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseTree is the smallest valid checkout the fixtures mutate.
func baseTree() map[string]string {
	return map[string]string{
		"nimbus.toml": `schema = 1
[compatibility]
fedora = ["44"]
min_engine = "0.1.0"
[repositories.terra]
kind = "dnf"
baseurl = "https://example.invalid/terra"
key_url = "https://example.invalid/terra/key.asc"
key = "AE09157A4DE88B497EA1D5D300CDAB43DE226D6F"
priority = 100
[repositories.flathub]
kind = "flatpak"
url = "https://example.invalid/flathub.flatpakrepo"
key = "6E5C05D979C76DAF93C081354184DD4D907A7CAE"
`,
		"machines/one.toml": `schema = 1
id = "one"
profiles = ["common", "extra"]
components = ["hardware"]
packages = ["ripgrep", "flatpak:org.example.App"]
package_exclusions = []
`,
		"profiles/common.toml": `schema = 1
id = "common"
packages = ["git", "terra:ghostty"]
components = ["base"]
`,
		"profiles/extra.toml": `schema = 1
id = "extra"
packages = []
components = ["top"]
`,
		"components/base.toml": `schema = 1
id = "base"
packages = ["curl"]
removes = ["curl-minimal"]
[[files]]
source = "etc/example.conf"
owner = "root"
group = "root"
mode = "0644"
`,
		"components/top.toml": `schema = 1
id = "top"
requires = ["dep"]
packages = ["top-tool"]
`,
		"components/dep.toml": `schema = 1
id = "dep"
packages = ["dep-tool"]
`,
		"components/hardware.toml": `schema = 1
id = "hardware"
packages = ["ddcutil"]
`,
		"system/root/etc/example.conf": "example = true\n",
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadAndValidate(t *testing.T, root string) ErrorList {
	t.Helper()
	c, err := Load(root)
	if err != nil {
		if errs, ok := err.(ErrorList); ok {
			return errs
		}
		t.Fatalf("load: %v", err)
	}
	return Validate(c)
}

func requireError(t *testing.T, errs ErrorList, want string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), want) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got:\n%s", want, errs.Error())
}

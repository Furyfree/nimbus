package definitions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/version"
)

func TestBaseTreeIsValid(t *testing.T) {
	root := writeTree(t, baseTree())
	if errs := loadAndValidate(t, root); len(errs) > 0 {
		t.Fatalf("unexpected errors:\n%s", errs.Error())
	}
}

func TestRepositoryDefinitionsValidate(t *testing.T) {
	c, err := Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("load repository checkout: %v", err)
	}
	if errs := Validate(c); len(errs) > 0 {
		t.Fatalf("repository definitions are invalid:\n%s", errs.Error())
	}
	for _, id := range []string{"desktop", "laptop", "vm"} {
		if _, ok := c.Machines[id]; !ok {
			t.Fatalf("machine %s is not tracked", id)
		}
		resolved, errs := Resolve(c, id)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		fastmail := 0
		for _, p := range resolved.Packages {
			if p.Canonical == "flatpak:com.fastmail.Fastmail" {
				fastmail++
				if strings.Join(p.Paths, ",") != "profile:hyprland-noctalia" {
					t.Fatalf("%s Fastmail provenance = %v", id, p.Paths)
				}
			}
		}
		if fastmail != 1 || !contains(resolved.Repositories, "flathub") {
			t.Fatalf("%s: expected one Fastmail package from Flathub", id)
		}
	}
	r, errs := Resolve(c, "desktop")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	docker := findComponent(r, "docker")
	if docker == nil || strings.Join(docker.Paths, ",") != "component:windows-vm,profile:development" {
		t.Fatalf("docker provenance = %v", docker)
	}
	if !contains(r.Removes, "ffmpeg-free") {
		t.Fatalf("removes = %v", r.Removes)
	}
	if len(r.Files) < 1 || r.Files[0].Target != "/etc/docker/daemon.json" || r.Files[0].Source != "system/root/etc/docker/daemon.json" {
		t.Fatalf("files = %+v", r.Files)
	}
	if !contains(r.Repositories, "flathub") || !contains(r.Repositories, "hyprland-copr") {
		t.Fatalf("repositories = %v", r.Repositories)
	}
}

func findComponent(r *Resolved, id string) *ResolvedComponent {
	for i := range r.Components {
		if r.Components[i].ID == id {
			return &r.Components[i]
		}
	}
	return nil
}

func TestResolutionIsDeterministic(t *testing.T) {
	root := writeTree(t, baseTree())
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Resolve(c, "one")
	b, _ := Resolve(c, "one")
	if strings.Join(a.Profiles, ",") != "common,extra" {
		t.Fatalf("profile order changed: %v", a.Profiles)
	}
	for i := range a.Packages {
		if a.Packages[i].Canonical != b.Packages[i].Canonical {
			t.Fatal("package order differs between resolutions")
		}
		if i > 0 && a.Packages[i-1].Canonical > a.Packages[i].Canonical {
			t.Fatalf("packages are not sorted: %s after %s", a.Packages[i].Canonical, a.Packages[i-1].Canonical)
		}
	}
	dep := findComponent(a, "dep")
	if dep == nil || strings.Join(dep.Paths, ",") != "component:top" {
		t.Fatalf("dep provenance = %v", dep)
	}
	if strings.Join(a.Repositories, ",") != "flathub,terra" {
		t.Fatalf("repositories = %v", a.Repositories)
	}
}

func TestExclusionRemovesDirectlySelectedPackage(t *testing.T) {
	tree := baseTree()
	tree["machines/one.toml"] = strings.Replace(tree["machines/one.toml"], `package_exclusions = []`, `package_exclusions = ["git"]`, 1)
	root := writeTree(t, tree)
	c, _ := Load(root)
	if errs := Validate(c); len(errs) > 0 {
		t.Fatal(errs)
	}
	r, _ := Resolve(c, "one")
	for _, p := range r.Packages {
		if p.Canonical == "dnf:git" {
			t.Fatal("excluded package still selected")
		}
	}
}

func TestInvalidTrees(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"missing common", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `["common", "extra"]`, `["extra"]`, 1)
		}, `profiles must include "common"`},
		{"unknown field rejected", func(f map[string]string) {
			f["machines/one.toml"] += "[package_constraints]\n\"dnf:git\" = \"=1.0\"\n"
		}, "unknown field"},
		{"undeclared prefix", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["copr:thing"]`, 1)
		}, `names repository "copr"`},
		{"reserved prefix as repository", func(f map[string]string) {
			f["nimbus.toml"] += "[repositories.dnf]\nkind = \"copr\"\nproject = \"a/b\"\nkey = \"AE09157A4DE88B497EA1D5D300CDAB43DE226D6F\"\n"
		}, "reserved prefix"},
		{"requires cycle", func(f map[string]string) {
			f["components/dep.toml"] += "requires = [\"top\"]\n"
		}, "requires cycle"},
		{"conflict", func(f map[string]string) {
			f["components/hardware.toml"] += "conflicts = [\"base\"]\n"
		}, "conflicts with selected component"},
		{"same name from two repositories", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["terra:git"]`, 1)
		}, "more than one repository"},
		{"exclusion matches nothing", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `package_exclusions = []`, `package_exclusions = ["absent"]`, 1)
		}, "matches no selected package"},
		{"exclusion of a required component's package", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `package_exclusions = []`, `package_exclusions = ["dep-tool"]`, 1)
		}, "another selected component requires"},
		{"removed package is also selected", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["curl-minimal"]`, 1)
		}, "also selected"},
		{"file source outside etc", func(f map[string]string) {
			f["components/base.toml"] = strings.Replace(f["components/base.toml"], `etc/example.conf`, `usr/example.conf`, 1)
		}, "must start with etc/"},
		{"file source traversal", func(f map[string]string) {
			f["components/base.toml"] = strings.Replace(f["components/base.toml"], `etc/example.conf`, `etc/../example.conf`, 1)
		}, "clean relative path"},
		{"file source missing", func(f map[string]string) {
			f["components/base.toml"] = strings.Replace(f["components/base.toml"], `etc/example.conf`, `etc/absent.conf`, 1)
		}, "does not exist"},
		{"bad mode", func(f map[string]string) {
			f["components/base.toml"] = strings.Replace(f["components/base.toml"], `mode = "0644"`, `mode = "644"`, 1)
		}, "four octal digits"},
		{"id mismatch", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `id = "extra"`, `id = "other"`, 1)
		}, "does not match filename"},
		{"unknown component", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `["hardware"]`, `["ghost"]`, 1)
		}, `unknown component "ghost"`},
		{"two flatpak repositories", func(f map[string]string) {
			f["nimbus.toml"] += "[repositories.other]\nkind = \"flatpak\"\nurl = \"https://example.invalid/o\"\nkey = \"6E5C05D979C76DAF93C081354184DD4D907A7CAE\"\n"
		}, "only one flatpak repository"},
		{"missing key file", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], `key_url = "https://example.invalid/terra/key.asc"`, `key_file = "keys/terra.asc"`, 1)
		}, "does not exist"},
		{"stray file in definitions directory", func(f map[string]string) {
			f["profiles/notes.txt"] = "x"
		}, "unexpected file"},
		{"unsupported schema", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], "schema = 1", "schema = 2", 1)
		}, "schema 2 is not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := baseTree()
			tc.mutate(tree)
			root := writeTree(t, tree)
			requireError(t, loadAndValidate(t, root), tc.want)
		})
	}
}

func TestSymlinkInsideBoundaryIsRejected(t *testing.T) {
	root := writeTree(t, baseTree())
	if err := os.Symlink("example.conf", filepath.Join(root, "system", "root", "etc", "link.conf")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	requireError(t, loadAndValidate(t, root), "symlinks are not allowed")
}

func TestSymlinkedCheckoutRootIsResolved(t *testing.T) {
	real := writeTree(t, baseTree())
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	c, err := Load(link)
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != real {
		t.Fatalf("root = %s, want %s", c.Root, real)
	}
}

func TestLoadWritesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	root := writeTree(t, baseTree())
	var dirs []string
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(d, 0o755)
		}
	})
	if errs := loadAndValidate(t, root); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestReviewedInvariants(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"duplicate fedora release", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], `fedora = ["44"]`, `fedora = ["44", "44"]`, 1)
		}, `lists "44" twice`},
		{"missing priority", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], "priority = 100\n", "", 1)
		}, "priority is required"},
		{"priority not above fedora", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], "priority = 100", "priority = 99", 1)
		}, "must be above Fedora's 99"},
		{"copr key_url rejected", func(f map[string]string) {
			f["nimbus.toml"] += "[repositories.c]\nkind = \"copr\"\nproject = \"a/b\"\nkey_url = \"https://example.invalid/k\"\nkey = \"AE09157A4DE88B497EA1D5D300CDAB43DE226D6F\"\npriority = 100\n"
		}, "key URL derives from the project"},
		{"two repositories with one priority rejected", func(f map[string]string) {
			f["nimbus.toml"] += "[repositories.other]\nkind = \"dnf\"\nbaseurl = \"https://example.invalid/other\"\nkey_url = \"https://example.invalid/other/key.asc\"\nkey = \"AE09157A4DE88B497EA1D5D300CDAB43DE226D6F\"\npriority = 100\n"
		}, "priority 100 is also used by repository other"},
		{"dnf option with a bad name rejected", func(f map[string]string) {
			f["nimbus.toml"] += "[dnf]\n\"Max-Parallel\" = 10\n"
		}, "dnf.Max-Parallel: option names are lowercase"},
		{"dnf option with a table value rejected", func(f map[string]string) {
			f["nimbus.toml"] += "[dnf]\nmax_parallel_downloads = [10]\n"
		}, "dnf.max_parallel_downloads: value must be a number, boolean, or string"},
		{"key_url with a DNF variable rejected", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], `key_url = "https://example.invalid/terra/key.asc"`, `key_url = "https://example.invalid/terra$releasever/key.asc"`, 1)
		}, "concrete URL without DNF variables"},
		{"key file must be a public key", func(f map[string]string) {
			f["nimbus.toml"] = strings.Replace(f["nimbus.toml"], `key_url = "https://example.invalid/terra/key.asc"`, `key_file = "keys/terra.asc"`, 1)
			f["system/keys/terra.asc"] = "not a key\n"
		}, "not an armored PGP public key"},
		{"one file lists two sources for one name", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["git", "terra:git"]`, 1)
		}, "one package has one source"},
		{"flatpak and rpm share a name", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["org.example.App"]`, 1)
		}, "more than one repository"},
		{"exclusion of a machine package", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `package_exclusions = []`, `package_exclusions = ["ripgrep"]`, 1)
		}, "remove it there instead"},
		{"exclusion of a required component also selected directly", func(f map[string]string) {
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `["hardware"]`, `["hardware", "dep"]`, 1)
			f["machines/one.toml"] = strings.Replace(f["machines/one.toml"], `package_exclusions = []`, `package_exclusions = ["dep-tool"]`, 1)
		}, "another selected component requires"},
		{"removal of a package selected from another repository", func(f map[string]string) {
			f["profiles/extra.toml"] = strings.Replace(f["profiles/extra.toml"], `packages = []`, `packages = ["terra:curl-minimal"]`, 1)
		}, "also selected as terra:curl-minimal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := baseTree()
			tc.mutate(tree)
			requireError(t, loadAndValidate(t, writeTree(t, tree)), tc.want)
		})
	}
}

func TestTwoDotsInsideANameAreAllowed(t *testing.T) {
	tree := baseTree()
	tree["components/base.toml"] = strings.Replace(tree["components/base.toml"], `etc/example.conf`, `etc/example..conf`, 1)
	tree["system/root/etc/example..conf"] = "x\n"
	if errs := loadAndValidate(t, writeTree(t, tree)); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestMinimumEngine(t *testing.T) {
	root := writeTree(t, baseTree())
	saved := version.Engine
	t.Cleanup(func() { version.Engine = saved })
	version.Engine = "0.0.0-dev"
	if errs := loadAndValidate(t, root); len(errs) > 0 {
		t.Fatalf("development build must skip the check: %v", errs)
	}
	version.Engine = "0.0.9"
	requireError(t, loadAndValidate(t, root), "newer than this engine")
	version.Engine = "0.1.0"
	if errs := loadAndValidate(t, root); len(errs) > 0 {
		t.Fatalf("equal release must pass: %v", errs)
	}
}

func TestInvalidIDsAndReservedCargo(t *testing.T) {
	for _, kind := range []string{"machines", "profiles", "components"} {
		t.Run(kind, func(t *testing.T) {
			tree := baseTree()
			tree[kind+"/bad;name.toml"] = "schema = 1\nid = \"bad;name\"\nprofiles = [\"common\"]\n"
			if kind != "machines" {
				tree[kind+"/bad;name.toml"] = "schema = 1\nid = \"bad;name\"\n"
			}
			if errs := loadAndValidate(t, writeTree(t, tree)); len(errs) == 0 {
				t.Fatal("invalid identifier accepted")
			}
		})
	}
	t.Run("cargo repository", func(t *testing.T) {
		tree := baseTree()
		tree["nimbus.toml"] = strings.ReplaceAll(tree["nimbus.toml"], "repositories.terra", "repositories.cargo")
		tree["profiles/common.toml"] = strings.ReplaceAll(tree["profiles/common.toml"], "terra:ghostty", "cargo:ghostty")
		if errs := loadAndValidate(t, writeTree(t, tree)); len(errs) == 0 {
			t.Fatal("reserved cargo repository accepted")
		}
	})
	t.Run("DNF newline", func(t *testing.T) {
		tree := baseTree()
		tree["nimbus.toml"] += "\n[dnf]\nproxy = \"direct\\ngpgcheck=False\"\n"
		if errs := loadAndValidate(t, writeTree(t, tree)); len(errs) == 0 {
			t.Fatal("DNF option injection accepted")
		}
	})
}

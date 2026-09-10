package definitions

import (
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestConstraintFamilies(t *testing.T) {
	for _, tc := range []struct {
		family, lower, upper string
		allowed, refused     []string
	}{
		{"0.56.*", "0:0.56", "0:0.57~", []string{"0.56-1.fc44", "0:0.56.1-9.fc44", "0.56.90-1"}, []string{"0.55.9-1", "0.57.0-1", "0.56.1~rc1-1", "0.56.2rc1-1", "0.56.1^git1-1", "1:0.56.1-1", "0.56..1-1"}},
		{"5.*", "0:5", "0:6~", []string{"5-1", "0:5.9.1-4.fc44"}, []string{"4.99-1", "6.0-1", "5.9~rc1-1", "50.0-1"}},
	} {
		t.Run(tc.family, func(t *testing.T) {
			c, err := parseConstraint("dnf:example", tc.family)
			if err != nil || c.Lower != tc.lower || c.Upper != tc.upper {
				t.Fatalf("constraint %+v: %v", c, err)
			}
			for _, evr := range tc.allowed {
				if !c.Matches(evr) {
					t.Errorf("refused %s", evr)
				}
			}
			for _, evr := range tc.refused {
				if c.Matches(evr) {
					t.Errorf("accepted %s", evr)
				}
			}
		})
	}
}

func TestConstraintsRequireSelectedCanonicalRPMAndSupportedFamily(t *testing.T) {
	for _, tc := range []struct{ key, family, want string }{
		{"git", "5.*", "canonical"}, {"flatpak:org.example.App", "5.*", "canonical"},
		{"dnf:git.x86_64", "5.*", "unqualified"},
		{"dnf:git", ">=5", "numeric family"}, {"dnf:git", "05.*", "numeric family"},
		{"dnf:git", "5.1.2.*", "numeric family"}, {"dnf:git", "9999999999.*", "numeric family"},
		{"dnf:absent", "5.*", "not a selected package"},
	} {
		t.Run(tc.key+tc.family, func(t *testing.T) {
			c, err := Load(writeTree(t, baseTree()))
			if err != nil {
				t.Fatal(err)
			}
			c.Machines["one"].PackageConstraints = map[string]string{tc.key: tc.family}
			requireError(t, Validate(c), tc.want)
		})
	}
}

func TestConstraintsGenerateOneNativeOwnedFile(t *testing.T) {
	tree := baseTree()
	tree["machines/one.toml"] += "\n[package_constraints]\n\"dnf:git\" = \"5.*\"\n\"terra:ghostty\" = \"0.56.*\"\n"
	c, err := Load(writeTree(t, tree))
	if err != nil {
		t.Fatal(err)
	}
	r, errs := Resolve(c, "one")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(r.Constraints) != 2 || r.Constraints[0].Package != "dnf:git" {
		t.Fatalf("constraints %+v", r.Constraints)
	}
	i := slices.IndexFunc(r.Files, func(f ResolvedFile) bool { return f.Target == VersionlockPath })
	if i < 0 {
		t.Fatal("no native versionlock file")
	}
	var native struct {
		Version  string
		Packages []struct {
			Name       string
			Conditions []map[string]string
		}
	}
	if err := toml.Unmarshal(r.Files[i].Content, &native); err != nil {
		t.Fatal(err)
	}
	if native.Version != "1.0" || len(native.Packages) != 2 || native.Packages[1].Name != "ghostty" || len(native.Packages[1].Conditions) != 2 {
		t.Fatalf("native file: %s", r.Files[i].Content)
	}
	c.Root_.DNF = map[string]any{"excludepkgs": true}
	requireError(t, Validate(c), "must be a string")
	c.Root_.DNF = nil
	tree["components/base.toml"] = strings.ReplaceAll(tree["components/base.toml"], "etc/example.conf", "etc/dnf/versionlock.toml")
	tree["system/root/etc/dnf/versionlock.toml"] = "version = \"1.0\"\n"
	c, err = Load(writeTree(t, tree))
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, Validate(c), "reserved for generated")
	c.Machines["one"].PackageConstraints = nil
	requireError(t, Validate(c), "reserved for generated")
}

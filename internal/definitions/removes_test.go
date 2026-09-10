package definitions

import (
	"testing"
)

func TestRPMRemovalConflictsUseNativeRequests(t *testing.T) {
	for _, tc := range []struct {
		name, selected, removed string
		conflict                bool
	}{
		{"bare requests", "demo", "demo", true},
		{"qualified selection", "demo.i686", "demo", true},
		{"qualified removal", "demo", "demo.i686", true},
		{"same architecture", "demo.i686", "demo.i686", true},
		{"different architectures", "demo.x86_64", "demo.i686", false},
		{"another RPM source", "terra:demo.i686", "demo", true},
		{"another source with different architecture", "terra:demo.x86_64", "demo.i686", false},
		{"dotted selection", "python3.14", "python3", false},
		{"dotted removal", "python3", "python3.14", false},
		{"qualified dotted name", "python3.14.i686", "python3.14", true},
		{"Flatpak is independent", "flatpak:org.example.App", "org.example.App", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeTree(t, baseTree()))
			if err != nil {
				t.Fatal(err)
			}
			c.Profiles["extra"].Packages = []string{tc.selected}
			c.Components["base"].Removes = []string{tc.removed}
			_, resolvedErrors := Resolve(c, "one")
			for _, check := range []struct {
				name string
				errs ErrorList
			}{
				{"Validate", Validate(c)},
				{"Resolve", resolvedErrors},
			} {
				t.Run(check.name, func(t *testing.T) {
					if tc.conflict {
						requireError(t, check.errs, "also selected as")
					} else if len(check.errs) > 0 {
						t.Fatalf("independent requests conflict: %s", check.errs.Error())
					}
				})
			}
		})
	}
}

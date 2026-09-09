package facts

import (
	"strings"
	"testing"
)

func TestRepositoryValuesFollowNativeContinuationRules(t *testing.T) {
	data := "\uFEFF[maker]\nname=\"first\n \t\n  # continued text\n  last\"\n# end value\n[other]\nenabled=0\n [maker] # repeated section\ngpgcheck='true'\n"
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(ending, func(t *testing.T) {
			repos, err := parseRepoFile("maker.repo", []byte(strings.ReplaceAll(data, "\n", ending)))
			if err != nil || len(repos) != 2 {
				t.Fatalf("repositories = %+v, %v", repos, err)
			}
			if repos[0].Name != "first\n\n# continued text\nlast" || repos[0].GPGCheck != "1" || repos[0].Options["gpgcheck"] != "true" {
				t.Fatalf("native continuation or repeated section differs: %+v", repos[0])
			}
			if repos[1].ID != "other" || repos[1].Enabled {
				t.Fatalf("indented section continued the previous value: %+v", repos[1])
			}
		})
	}
}

func TestRepositorySyntaxRejectsAmbiguousInput(t *testing.T) {
	for _, data := range []string{
		"gpgcheck=1\n",
		"[maker\ngpgcheck=1\n",
		"[]\ngpgcheck=1\n",
		"[maker] unexpected\ngpgcheck=1\n",
		"[*\r]\ngpgcheck=0\n",
		"[maker]\n  gpgcheck=1\n",
		"[maker]\ngpgkey=first\n# end value\n  second\n",
		"[maker]\ngpgkey=first\n\n  second\n",
		"[maker]\ngpgcheck\n",
		"[maker]\n=1\n",
		"[maker]\ngpgcheck=1\x00\n",
		"[maker]\ngpgcheck=\u00a01\n",
		"[maker]\nenabled=unknown\n",
	} {
		t.Run(data, func(t *testing.T) {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(ending, func(t *testing.T) {
					if repos, err := parseRepoFile("maker.repo", []byte(strings.ReplaceAll(data, "\n", ending))); err == nil || repos != nil || !strings.Contains(err.Error(), "maker.repo") {
						t.Fatalf("ambiguous repository input accepted: %+v, %v", repos, err)
					}
				})
			}
		})
	}
}

func TestRepositoryBooleansAcceptNativeOnAndOff(t *testing.T) {
	repos, err := parseRepoFile("maker.repo", []byte("[maker]\nenabled=On\ngpgcheck=oFf\n[other]\nenabled=OFF\ngpgcheck=ON\n"))
	if err != nil || len(repos) != 2 {
		t.Fatalf("native booleans rejected: %+v, %v", repos, err)
	}
	if !repos[0].Enabled || repos[0].GPGCheck != "0" || repos[1].Enabled || repos[1].GPGCheck != "1" {
		t.Fatalf("native booleans differ: %+v", repos)
	}
}

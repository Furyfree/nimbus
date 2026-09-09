package plan

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

func TestProvidesRequireEvidenceForTheReviewedPackage(t *testing.T) {
	a := TxPackage{Name: "native-a", Arch: "x86_64", EVR: "1-1.fc44", Repository: "fedora", Section: "installing"}
	z := TxPackage{Name: "native-z", Arch: "x86_64", EVR: "1-1.fc44", Repository: "fedora", Section: "installing"}
	i686 := a
	i686.Arch = "i686"
	const aEvidence = "native-a|x86_64|0:1-1.fc44|fedora\n"
	const zEvidence = "native-z|x86_64|1-1.fc44|fedora\n"
	tests := []struct {
		name     string
		requests []string
		rows     []TxPackage
		evidence map[string]string
		queryErr string
		want     map[string]string
		blocked  string
	}{
		{name: "single provide", requests: []string{"virtual-alpha"}, rows: []TxPackage{z}, evidence: map[string]string{"virtual-alpha": zEvidence}, want: map[string]string{"virtual-alpha": "native-z.x86_64"}},
		{name: "multiple provides", requests: []string{"virtual-alpha", "virtual-zulu"}, rows: []TxPackage{a, z}, evidence: map[string]string{"virtual-alpha": zEvidence, "virtual-zulu": aEvidence}, want: map[string]string{"virtual-alpha": "native-z.x86_64", "virtual-zulu": "native-a.x86_64"}},
		{name: "shared provider", requests: []string{"virtual-alpha", "virtual-zulu"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": aEvidence, "virtual-zulu": aEvidence}, want: map[string]string{"virtual-alpha": "native-a.x86_64", "virtual-zulu": "native-a.x86_64"}},
		{name: "direct names need no query", requests: []string{"native-a", "native-z.x86_64"}, rows: []TxPackage{a, z}, want: map[string]string{"native-a": "native-a.x86_64", "native-z.x86_64": "native-z.x86_64"}},
		{name: "bare multilib request", requests: []string{"native-a"}, rows: []TxPackage{a, i686}, want: map[string]string{"native-a": "native-a.x86_64"}},
		{name: "bare and qualified multilib requests", requests: []string{"native-a", "native-a.i686"}, rows: []TxPackage{a, i686}, want: map[string]string{"native-a": "native-a.x86_64", "native-a.i686": "native-a.i686"}},
		{name: "direct and provide", requests: []string{"native-a", "virtual-zulu"}, rows: []TxPackage{a, z}, evidence: map[string]string{"virtual-zulu": zEvidence}, want: map[string]string{"native-a": "native-a.x86_64", "virtual-zulu": "native-z.x86_64"}},
		{name: "only reviewed candidate counts", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": zEvidence + aEvidence + aEvidence}, want: map[string]string{"virtual-alpha": "native-a.x86_64"}},
		{name: "wrong version", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": "native-a|x86_64|2-1.fc44|fedora\n"}, blocked: "no reviewed package"},
		{name: "wrong epoch", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": "native-a|x86_64|1:1-1.fc44|fedora\n"}, blocked: "no reviewed package"},
		{name: "wrong repository", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": "native-a|x86_64|1-1.fc44|updates\n"}, blocked: "no reviewed package"},
		{name: "wrong architecture", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": "native-a|i686|1-1.fc44|fedora\n"}, blocked: "no reviewed package"},
		{name: "ambiguous providers", requests: []string{"virtual-alpha"}, rows: []TxPackage{a, z}, evidence: map[string]string{"virtual-alpha": aEvidence + zEvidence}, blocked: "multiple reviewed packages"},
		{name: "missing provider", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": ""}, blocked: "no reviewed package"},
		{name: "unavailable evidence", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, blocked: "not recorded"},
		{name: "query failure with output", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": aEvidence}, queryErr: "metadata unavailable", blocked: "metadata unavailable"},
		{name: "malformed evidence", requests: []string{"virtual-alpha"}, rows: []TxPackage{a}, evidence: map[string]string{"virtual-alpha": aEvidence + "incomplete|row\n"}, blocked: "invalid provider row"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				rows := slices.Clone(tt.rows)
				if reverse {
					slices.Reverse(rows)
				}
				desired := &definitions.Resolved{Machine: "vm"}
				for _, name := range tt.requests {
					desired.Packages = append(desired.Packages, definitions.ResolvedPackage{Canonical: "dnf:" + name, Prefix: "dnf", Name: name})
				}
				src := &facts.FakeSource{Commands: map[string][]byte{
					facts.Key("dnf5", append([]string{"--assumeno", "--cacheonly", "install"}, tt.requests...)...): previewText(rows),
					facts.Key("dnf5", "--cacheonly", "check-upgrade"):                                              nil,
				}, Failures: map[string]string{}}
				for request, evidence := range tt.evidence {
					key := facts.Key("dnf5", "--cacheonly", "repoquery", "--available", "--whatprovides", request, "--queryformat", "%{name}|%{arch}|%{evr}|%{repoid}\\n")
					src.Commands[key] = []byte(evidence)
					if tt.queryErr != "" {
						src.Failures[key] = tt.queryErr
					}
				}
				p, err := Build(Inputs{Resolved: desired, Facts: &facts.Facts{}, Source: src})
				if err != nil {
					t.Fatal(err)
				}
				op := find(p, "packages:install")
				if op == nil || len(op.Steps) != 1 {
					t.Fatalf("missing merged install: %+v", p)
				}
				if tt.blocked != "" {
					if p.Complete || !strings.Contains(op.Blocked, tt.blocked) || !strings.Contains(op.Blocked, "virtual-alpha") {
						t.Fatalf("reverse=%t: complete=%t blocked=%q, want %q with request context", reverse, p.Complete, op.Blocked, tt.blocked)
					}
				} else if !p.Complete || !maps.Equal(op.Resolved, tt.want) {
					t.Fatalf("reverse=%t: resolved=%v blocked=%q, want %v", reverse, op.Resolved, op.Blocked, tt.want)
				}
			}
		})
	}
}

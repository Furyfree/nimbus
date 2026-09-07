package doctor

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
)

func healthy() *facts.Facts {
	f := &facts.Facts{Commands: map[string]string{}}
	for _, name := range facts.RequiredCommands {
		f.Commands[name] = "/usr/bin/" + name
	}
	f.Platform.Value = facts.Platform{ID: "fedora", VersionID: "44", Arch: "x86_64"}
	f.Packages.Value = []facts.Package{{Name: "bash"}}
	f.Repositories.Value = []facts.Repository{{ID: "fedora", Enabled: true, GPGCheck: "1"}, {ID: "updates-testing", Enabled: false, GPGCheck: "0"}}
	f.SecureBoot.Value = facts.SecureBootEnabled
	f.SELinux.Value = facts.SELinuxEnforcing
	f.Firewalld.Value = "active"
	f.Checkout.Value = facts.Checkout{Origin: "github.com/furyfree-org/nimbus", Commit: "0123456789abcdef"}
	return f
}

func healthyConfig() Config {
	return Config{SupportedReleases: []string{"44"}, ApprovedOrigin: "github.com/furyfree-org/nimbus"}
}

func status(r Report, id string) Check {
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestHealthySystemPasses(t *testing.T) {
	r := Run(healthy(), healthyConfig())
	if r.Failed != 0 || r.Unknown != 0 || len(r.Checks) != 10 {
		t.Fatalf("report = %+v", r)
	}
	for _, c := range r.Checks {
		if c.Status != Pass || c.Observation == "" {
			t.Fatalf("check %s = %+v", c.ID, c)
		}
	}
}

func TestEveryFailureExplainsItself(t *testing.T) {
	cases := []struct {
		name  string
		facts func(*facts.Facts)
		cfg   func(*Config)
		id    string
		want  string
	}{
		{"unsupported release", func(f *facts.Facts) { f.Platform.Value.VersionID = "43" }, nil, "platform", "supports 44"},
		{"wrong distribution", func(f *facts.Facts) { f.Platform.Value.ID = "arch" }, nil, "platform", "Fedora only"},
		{"missing selector", nil, func(c *Config) { c.SelectorError = "read selector: no such file" }, "selector", "nimbus init"},
		{"origin mismatch", func(f *facts.Facts) { f.Checkout.Value.Origin = "github.com/someone/else" }, nil, "selector", "not the approved repository"},
		{"broken definitions", nil, func(c *Config) { c.DefinitionsError = "2 definition error(s)" }, "definitions", "nimbus validate"},
		{"missing command", func(f *facts.Facts) { f.Commands["flatpak"] = "" }, nil, "commands", "missing: flatpak"},
		{"unreadable packages", func(f *facts.Facts) { f.Packages.Error = "dnf5 failed" }, nil, "packages", "dnf5"},
		{"unsigned repository", func(f *facts.Facts) { f.Repositories.Value[0].GPGCheck = "0" }, nil, "repository-signatures", "gpgcheck=1"},
		{"secure boot off", func(f *facts.Facts) { f.SecureBoot.Value = facts.SecureBootDisabled }, nil, "secure-boot", "never changes it"},
		{"selinux permissive", func(f *facts.Facts) { f.SELinux.Value = facts.SELinuxPermissive }, nil, "selinux", "SELINUX=enforcing"},
		{"firewalld inactive", func(f *facts.Facts) { f.Firewalld.Value = "inactive" }, nil, "firewalld", "enable --now firewalld"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, cfg := healthy(), healthyConfig()
			if tc.facts != nil {
				tc.facts(f)
			}
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			r := Run(f, cfg)
			c := status(r, tc.id)
			if c.Status != Fail || r.Failed != 1 {
				t.Fatalf("%s = %+v (failed %d)", tc.id, c, r.Failed)
			}
			text := c.Observation + " " + c.Impact + " " + c.Remediation
			if c.Impact == "" || c.Remediation == "" || !strings.Contains(text, tc.want) {
				t.Fatalf("%s lacks impact, remediation, or %q: %+v", tc.id, tc.want, c)
			}
		})
	}
}

func TestUnknownIsNeverAFailure(t *testing.T) {
	f, cfg := healthy(), healthyConfig()
	f.SecureBoot.Value = facts.SecureBootUnavailable
	f.Repositories.Error = "permission denied"
	cfg.SupportedReleases = nil
	r := Run(f, cfg)
	if r.Failed != 0 || r.Unknown != 3 {
		t.Fatalf("report = failed %d unknown %d", r.Failed, r.Unknown)
	}
	if c := status(r, "platform"); c.Status != Unknown || !strings.Contains(c.Observation, "unavailable") {
		t.Fatalf("platform = %+v", c)
	}
}

func TestCheckoutOverrideSkipsSelector(t *testing.T) {
	f, cfg := healthy(), Config{SupportedReleases: []string{"44"}, CheckoutOverride: true}
	f.Checkout.Value.Dirty = true
	c := status(Run(f, cfg), "selector")
	if c.Status != Pass || !strings.Contains(c.Observation, "override") || !strings.Contains(c.Observation, "dirty") {
		t.Fatalf("selector = %+v", c)
	}
}

func TestUnsetGPGCheckIsUnknown(t *testing.T) {
	f, cfg := healthy(), healthyConfig()
	f.Repositories.Value = append(f.Repositories.Value, facts.Repository{ID: "vendor", Enabled: true})
	c := status(Run(f, cfg), "repository-signatures")
	if c.Status != Unknown || !strings.Contains(c.Observation, "vendor") || c.Remediation == "" {
		t.Fatalf("unset gpgcheck = %+v", c)
	}
}

func TestOverrideStillNeedsAReadableCheckout(t *testing.T) {
	f, cfg := healthy(), Config{SupportedReleases: []string{"44"}, CheckoutOverride: true}
	f.Checkout = facts.Section[facts.Checkout]{Error: "/tmp/x is not a Git checkout"}
	c := status(Run(f, cfg), "selector")
	if c.Status != Fail || !strings.Contains(c.Observation, "not a Git checkout") || c.Remediation == "" {
		t.Fatalf("override with unknown checkout = %+v", c)
	}
}

func TestUnrecognizedGPGCheckIsUnknown(t *testing.T) {
	f, cfg := healthy(), healthyConfig()
	f.Repositories.Value = append(f.Repositories.Value, facts.Repository{ID: "vendor", Enabled: true, GPGCheck: "maybe"})
	c := status(Run(f, cfg), "repository-signatures")
	if c.Status != Unknown || !strings.Contains(c.Observation, "vendor (gpgcheck=maybe)") {
		t.Fatalf("unrecognized gpgcheck = %+v", c)
	}
}

func TestMissingGitIsUnknownNotABrokenCheckout(t *testing.T) {
	f, cfg := healthy(), healthyConfig()
	f.Commands["git"] = ""
	f.Checkout = facts.Section[facts.Checkout]{Error: `git: executable file not found`}
	c := status(Run(f, cfg), "selector")
	if c.Status != Unknown || !strings.Contains(c.Remediation, "common profile") {
		t.Fatalf("missing git = %+v", c)
	}
	cfg.CheckoutOverride = true
	if c := status(Run(f, cfg), "selector"); c.Status != Unknown {
		t.Fatalf("missing git with override = %+v", c)
	}
}

func TestChezmoiSelectionIsComparedWithTheManifest(t *testing.T) {
	cfg := healthyConfig()
	cfg.Machine, cfg.Profiles, cfg.Dotfiles = "laptop", []string{"common", "development"}, true
	f := healthy()
	f.Commands["chezmoi"] = "/usr/bin/chezmoi"
	f.Chezmoi = facts.Section[facts.Chezmoi]{Value: facts.Chezmoi{Initialized: true, Machine: "laptop", ManagedByNimbus: true, Profiles: []string{"development", "common"}}}
	if c := status(Run(f, cfg), "chezmoi"); c.Status != Pass {
		t.Fatalf("matching selection = %+v", c)
	}
	f.Chezmoi.Value.Profiles = []string{"common"}
	if c := status(Run(f, cfg), "chezmoi"); c.Status != Fail || !strings.Contains(c.Remediation, "chezmoi init --prompt --promptString Machine=laptop") || !strings.Contains(c.Remediation, "Profiles=common/development") {
		t.Fatalf("stale profiles = %+v", c)
	}
	f.Chezmoi.Value = facts.Chezmoi{}
	if c := status(Run(f, cfg), "chezmoi"); c.Status != Fail || !strings.Contains(c.Remediation, "nimbus init") {
		t.Fatalf("not initialized = %+v", c)
	}
	f.Commands["chezmoi"] = ""
	if c := status(Run(f, cfg), "chezmoi"); c.Status != Unknown {
		t.Fatalf("without chezmoi = %+v", c)
	}
	cfg.Dotfiles = false
	if c := status(Run(f, cfg), "chezmoi"); c.Status != Pass {
		t.Fatalf("without dotfiles = %+v", c)
	}
}

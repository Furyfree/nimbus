package snapper

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

type recordingSource struct {
	*nativetest.FakeSource
	mutations []string
}

func (s *recordingSource) Run(name string, args ...string) ([]byte, error) {
	if name == "sudo" {
		s.mutations = append(s.mutations, nativetest.Key(name, args...))
	}
	return s.FakeSource.Run(name, args...)
}

func fixture(t *testing.T, configured bool) (*recordingSource, []byte) {
	t.Helper()
	template, err := os.ReadFile("../../system/root/etc/snapper/config-templates/nimbus")
	if err != nil {
		t.Fatal(err)
	}
	src := &nativetest.FakeSource{
		Commands: map[string][]byte{"findmnt --noheadings --output FSTYPE --target /": []byte("btrfs\n")},
		Failures: map[string]string{},
		Files:    map[string][]byte{Template: template},
		Dirs: map[string][]string{
			"/": {"etc"}, "/etc": {"snapper", "sysconfig"},
			"/etc/snapper": {"configs"}, "/etc/snapper/configs": {},
		},
	}
	for _, path := range []string{"/etc", "/etc/snapper", "/etc/snapper/configs"} {
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+path] = []byte("directory|root|root|755|1")
	}
	if configured {
		src.Dirs["/etc/snapper/configs"] = []string{"nimbus"}
		src.Files[Config] = template
		src.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=\"nimbus\"\n")
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+Config] = []byte("regular file|root|root|644|1")
	}
	return &recordingSource{FakeSource: src}, template
}

func TestInspect(t *testing.T) {
	for _, configured := range []bool{false, true} {
		src, template := fixture(t, configured)
		p, err := Inspect(src, template)
		if err != nil {
			t.Fatal(err)
		}
		if p.Setup == configured || configured && len(p.Changes()) != 0 {
			t.Fatalf("configured=%v: %#v", configured, p)
		}
		if len(src.mutations) != 0 {
			t.Fatalf("inspection mutated the system: %v", src.mutations)
		}
		for _, setting := range []string{"NUMBER_LIMIT=6", "NUMBER_MIN_AGE=0", "NUMBER_LIMIT_IMPORTANT=0", "TIMELINE_CREATE=no"} {
			if !slices.Contains(p.Settings, setting) {
				t.Errorf("missing retention setting %s", setting)
			}
		}
	}
	for _, tc := range []struct {
		name       string
		configured bool
		change     func(*nativetest.FakeSource)
		want       string
	}{
		{"foreign config", true, func(s *nativetest.FakeSource) { s.Files[Config] = []byte("SUBVOLUME=\"/\"\nFSTYPE=\"btrfs\"\n") }, "not Nimbus-owned"},
		{"writable config", true, func(s *nativetest.FakeSource) {
			s.Commands["stat --format=%F|%U|%G|%a|%h -- "+Config] = []byte("regular file|root|root|666|1")
		}, "not Nimbus-owned"},
		{"user-owned directory", true, func(s *nativetest.FakeSource) {
			s.Commands["stat --format=%F|%U|%G|%a|%h -- /etc/snapper/configs"] = []byte("directory|user|user|755|1")
		}, "not Nimbus-owned"},
		{"changed root", true, func(s *nativetest.FakeSource) {
			s.Files[Config] = []byte(strings.ReplaceAll(string(s.Files[Config]), "SUBVOLUME=\"/\"", "SUBVOLUME=\"/home\""))
		}, "no longer covers"},
		{"duplicate setting", true, func(s *nativetest.FakeSource) {
			s.Files[Config] = append(slices.Clone(s.Files[Config]), []byte("NUMBER_LIMIT=50\n")...)
		}, "duplicate Snapper setting"},
		{"unregistered config", true, func(s *nativetest.FakeSource) { delete(s.Files, "/etc/sysconfig/snapper") }, "registration and configuration disagree"},
		{"missing config", false, func(s *nativetest.FakeSource) { s.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=nimbus") }, "registration and configuration disagree"},
		{"foreign root config", false, func(s *nativetest.FakeSource) {
			s.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=root")
			s.Files["/etc/snapper/configs/root"] = []byte("SUBVOLUME=/")
		}, "already covers root"},
		{"existing snapshots", false, func(s *nativetest.FakeSource) {
			s.Dirs["/"] = append(s.Dirs["/"], ".snapshots")
			s.Commands["stat --format=%F|%U|%G|%a|%h -- /.snapshots"] = []byte("directory|root|root|750|1")
		}, "/.snapshots already exists"},
		{"other filesystem", false, func(s *nativetest.FakeSource) {
			s.Commands["findmnt --noheadings --output FSTYPE --target /"] = []byte("ext4\n")
		}, "requires a Btrfs root"},
		{"filesystem inspection failed", false, func(s *nativetest.FakeSource) {
			s.Failures["findmnt --noheadings --output FSTYPE --target /"] = "findmnt failed"
		}, "findmnt failed"},
		{"unsafe registration", false, func(s *nativetest.FakeSource) { s.Files["/etc/sysconfig/snapper"] = []byte("SNAPPER_CONFIGS=../root") }, "unsupported Snapper configuration name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, template := fixture(t, tc.configured)
			tc.change(src.FakeSource)
			if _, err := Inspect(src, template); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestConfigure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured bool
		change     func(*nativetest.FakeSource)
		command    string
		want       string
	}{
		{"create", false, nil, "create-config --fstype btrfs --template nimbus /", ""},
		{"unchanged", true, nil, "", ""},
		{"changed template", false, func(s *nativetest.FakeSource) {
			s.Files[Template] = []byte(strings.ReplaceAll(string(s.Files[Template]), "NUMBER_LIMIT=\"6\"", "NUMBER_LIMIT=\"8\""))
		}, "", "changed since approval"},
		{"native creation failure", false, nil, "create-config --fstype btrfs --template nimbus /", "create failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, template := fixture(t, tc.configured)
			p, err := Inspect(src, template)
			if err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(src.FakeSource)
			}
			if tc.command != "" {
				key := "sudo -- snapper --no-dbus --config nimbus " + tc.command
				src.Commands[key] = nil
				if tc.want != "" {
					src.Failures[key] = tc.want
				}
			}
			err = p.Configure(src)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Configure: %v, want %q", err, tc.want)
			}
			var wantCommands []string
			if tc.command != "" {
				wantCommands = []string{"sudo -- snapper --no-dbus --config nimbus " + tc.command}
			}
			if !slices.Equal(src.mutations, wantCommands) {
				t.Fatalf("mutations: %v, want %v", src.mutations, wantCommands)
			}
		})
	}
	t.Run("retention drift", func(t *testing.T) {
		src, template := fixture(t, true)
		src.Files[Config] = []byte(strings.ReplaceAll(string(template), "NUMBER_LIMIT=\"6\"", "NUMBER_LIMIT=\"50\""))
		p, err := Inspect(src, template)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(p.Changes(), []string{"NUMBER_LIMIT=6"}) {
			t.Fatalf("retention drift: %v", p.Changes())
		}
		src.Commands["sudo -- snapper --no-dbus --config nimbus set-config NUMBER_LIMIT=6"] = nil
		if err := p.Configure(src); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(src.mutations, []string{"sudo -- snapper --no-dbus --config nimbus set-config NUMBER_LIMIT=6"}) {
			t.Fatalf("retention update commands: %v", src.mutations)
		}
		src.Failures["sudo -- snapper --no-dbus --config nimbus set-config NUMBER_LIMIT=6"] = "set-config failed"
		if err := p.Configure(src); err == nil || !strings.Contains(err.Error(), "set-config failed") {
			t.Fatalf("lost native configuration failure: %v", err)
		}
	})
}

func TestSnapshotCommands(t *testing.T) {
	for _, tc := range []struct{ name, phase, pre, output, failure, want string }{
		{"pre", "pre", "", "7\n", "", "7"},
		{"post", "post", "7", "8\n", "", "8"},
		{"zero", "pre", "", "0", "", ""},
		{"empty", "pre", "", "", "", ""},
		{"malformed", "pre", "", "7\n8", "", ""},
		{"overflow", "pre", "", "18446744073709551616", "", ""},
		{"native failure", "pre", "", "7", "snapshot failed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := "sudo -- snapper --no-dbus --config nimbus create --type " + tc.phase + " --print-number --cleanup-algorithm number --description Nimbus system changes"
			if tc.pre != "" {
				key += " --pre-number " + tc.pre
			}
			src := &nativetest.FakeSource{Commands: map[string][]byte{key: []byte(tc.output)}, Failures: map[string]string{}}
			if tc.failure != "" {
				src.Failures[key] = tc.failure
			}
			id, err := Create(src, tc.phase, tc.pre)
			if id != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("Create: %q, %v; want %q", id, err, tc.want)
			}
			if tc.failure != "" && !strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("lost native failure: %v", err)
			}
		})
	}
	const cleanup = "sudo -- snapper --no-dbus --config nimbus cleanup number"
	src := &nativetest.FakeSource{Commands: map[string][]byte{cleanup: nil}, Failures: map[string]string{}}
	if err := Cleanup(src); err != nil {
		t.Fatal(err)
	}
	src.Failures[cleanup] = "cleanup failed"
	if err := Cleanup(src); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("lost cleanup failure: %v", err)
	}
}

package snapper

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

const mountQuery = "findmnt --raw --noheadings --output FSTYPE,UUID,FSROOT,OPTIONS,ID --mountpoint "

func storageFixture(t *testing.T) (*recordingSource, []byte) {
	t.Helper()
	src, template := fixture(t, false)
	src.Dirs["/"] = append(src.Dirs["/"], ".snapshots")
	src.Dirs["/.snapshots"] = nil
	src.Dirs["/etc/snapper"] = append(src.Dirs["/etc/snapper"], "config-templates")
	src.Dirs["/etc/snapper/config-templates"] = []string{"nimbus"}
	src.Dirs["/etc/sysconfig"] = []string{"snapper"}
	src.Files[registry] = []byte("# Keep distro settings\nSNAPPER_CONFIGS=\"\"\nOTHER=\"yes\"\n")
	for _, path := range []string{"/.snapshots", "/etc/sysconfig", "/etc/snapper/config-templates"} {
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+path] = []byte("directory|root|root|755|1")
	}
	for _, path := range []string{Config, Template, registry} {
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+path] = []byte("regular file|root|root|644|1")
	}
	src.Commands[mountQuery+"/"] = []byte("btrfs uuid /root rw,subvolid=256 35\n")
	src.Commands[mountQuery+"/.snapshots"] = []byte("btrfs uuid /snapshots rw,subvolid=258 36\n")
	src.Commands["stat --format=%i -- /.snapshots"] = []byte("256\n")
	src.Commands["snapper --no-dbus --config nimbus list"] = nil
	return src, template
}

func TestStorageAdoptionGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*nativetest.FakeSource)
	}{
		{"populated", func(s *nativetest.FakeSource) { s.Dirs["/.snapshots"] = []string{"1"} }},
		{"unreadable", func(s *nativetest.FakeSource) { delete(s.Dirs, "/.snapshots") }},
		{"symlink", func(s *nativetest.FakeSource) {
			s.Commands["stat --format=%F|%U|%G|%a|%h -- /.snapshots"] = []byte("symbolic link|root|root|777|1")
		}},
		{"writable", func(s *nativetest.FakeSource) {
			s.Commands["stat --format=%F|%U|%G|%a|%h -- /.snapshots"] = []byte("directory|root|root|777|1")
		}},
		{"foreign owner", func(s *nativetest.FakeSource) {
			s.Commands["stat --format=%F|%U|%G|%a|%h -- /.snapshots"] = []byte("directory|user|user|755|1")
		}},
		{"not mounted", func(s *nativetest.FakeSource) { s.Failures[mountQuery+"/.snapshots"] = "not a mountpoint" }},
		{"other filesystem", func(s *nativetest.FakeSource) {
			s.Commands[mountQuery+"/.snapshots"] = []byte("btrfs other /snapshots rw 36")
		}},
		{"not btrfs", func(s *nativetest.FakeSource) {
			s.Commands[mountQuery+"/.snapshots"] = []byte("ext4 uuid /snapshots rw 36")
		}},
		{"read only", func(s *nativetest.FakeSource) {
			s.Commands[mountQuery+"/.snapshots"] = []byte("btrfs uuid /snapshots ro 36")
		}},
		{"same subvolume", func(s *nativetest.FakeSource) {
			s.Commands[mountQuery+"/.snapshots"] = []byte("btrfs uuid /root rw 36")
		}},
		{"top level", func(s *nativetest.FakeSource) { s.Commands[mountQuery+"/.snapshots"] = []byte("btrfs uuid / rw 36") }},
		{"ordinary directory", func(s *nativetest.FakeSource) { s.Commands["stat --format=%i -- /.snapshots"] = []byte("300") }},
		{"changed unfinished config", func(s *nativetest.FakeSource) {
			s.Dirs["/etc/snapper/configs"] = []string{"nimbus"}
			s.Files[Config] = []byte(strings.ReplaceAll(string(s.Files[Template]), "NUMBER_LIMIT=\"6\"", "NUMBER_LIMIT=\"50\""))
			s.Files[Config] = append(s.Files[Config], pending...)
			s.Files[registry] = []byte("SNAPPER_CONFIGS=nimbus")
		}},
		{"unregistered foreign root config", func(s *nativetest.FakeSource) {
			s.Dirs["/etc/snapper/configs"] = []string{"root"}
			s.Files["/etc/snapper/configs/root"] = []byte("SUBVOLUME=/")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, template := storageFixture(t)
			tc.change(src.FakeSource)
			if _, err := Inspect(src, template); err == nil {
				t.Fatal("unsafe storage accepted")
			}
			if len(src.mutations) != 0 {
				t.Fatal("inspection mutated storage")
			}
		})
	}
}

func TestAdoptStorage(t *testing.T) {
	for _, failure := range []string{"", "config", "registration", "verification", "finish"} {
		t.Run("failure="+failure, func(t *testing.T) {
			src, template := storageFixture(t)
			p, err := Inspect(src, template)
			if err != nil || !p.Setup || p.Reuse == nil {
				t.Fatalf("preview: %v, %v", p, err)
			}
			if len(src.mutations) != 0 {
				t.Fatal("preview mutated storage")
			}
			var writes []string
			write := func(path string, before, after inspect.SystemFile) error {
				writes = append(writes, path)
				if failure == "config" && path == Config || failure == "registration" && path == registry || failure == "finish" && path == Config && string(after.Content) == string(template) {
					return errors.New("injected write failure")
				}
				if string(src.Files[path]) != string(before.Content) {
					t.Fatal("incorrect expected contents")
				}
				src.Files[path] = slices.Clone(after.Content)
				if path == Config {
					src.Dirs["/etc/snapper/configs"] = []string{"nimbus"}
				}
				return nil
			}
			if failure == "verification" {
				src.Failures["snapper --no-dbus --config nimbus list"] = "verification failed"
			}
			err = Adopt(src, p.adoptionDigest(), write)
			if (err != nil) != (failure != "") {
				t.Fatalf("Adopt: %v", err)
			}
			if failure != "" {
				p, err = Inspect(src, template)
				if err != nil || !p.Setup {
					t.Fatalf("interrupted adoption not retryable: %v, %v", p, err)
				}
				failure = ""
				delete(src.Failures, "snapper --no-dbus --config nimbus list")
				if err := Adopt(src, p.adoptionDigest(), write); err != nil {
					t.Fatal(err)
				}
			}
			p, err = Inspect(src, template)
			if err != nil || p.Setup || len(p.Changes()) != 0 {
				t.Fatalf("did not converge: %v, %v", p, err)
			}
			if got := string(src.Files[registry]); got != "# Keep distro settings\nSNAPPER_CONFIGS=\"nimbus\"\nOTHER=\"yes\"\n" {
				t.Fatalf("registration: %q", got)
			}
			for _, path := range writes {
				if path != Config && path != registry {
					t.Fatalf("unexpected write %s", path)
				}
			}
			if len(src.Dirs["/.snapshots"]) != 0 || len(src.mutations) != 0 {
				t.Fatal("adoption mutated snapshot storage")
			}
		})
	}
}

func TestAdoptionRechecksApproval(t *testing.T) {
	for _, change := range []string{"mount", "contents", "template", "registration ownership"} {
		t.Run(change, func(t *testing.T) {
			src, template := storageFixture(t)
			p, err := Inspect(src, template)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "mount":
				src.Commands[mountQuery+"/.snapshots"] = []byte("btrfs uuid /replacement rw,subvolid=260 40")
			case "contents":
				src.Dirs["/.snapshots"] = []string{"unexpected"}
			case "template":
				src.Files[Template] = []byte(strings.ReplaceAll(string(template), "NUMBER_LIMIT=\"6\"", "NUMBER_LIMIT=\"50\""))
			case "registration ownership":
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+registry] = []byte("regular file|user|user|644|1")
			}
			if err := Adopt(src, p.adoptionDigest(), func(string, inspect.SystemFile, inspect.SystemFile) error {
				t.Fatal("write after changed approval")
				return nil
			}); err == nil {
				t.Fatal("changed approval accepted")
			}
		})
	}
	// Configure must choose the fixed helper rather than native create-config.
	src, template := storageFixture(t)
	p, err := Inspect(src, template)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := "sudo -- " + exe + " internal snapper-adopt --expected " + p.adoptionDigest()
	src.Commands[command] = nil
	if err := p.Configure(src); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(src.mutations, []string{command}) {
		t.Fatalf("unexpected setup commands: %v", src.mutations)
	}
}

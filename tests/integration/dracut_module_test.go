package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The dracut helpers are stubbed: the test covers the module's own decisions,
// not dracut. inst_simple copies from the fake sysroot like dracut does.
const dracutStubs = `
inst_libdir_file() { :; }
inst_multiple() { :; }
inst_simple() { mkdir -p "$initdir${1%/*}" && cp "$dracutsysrootdir$1" "$initdir$1"; }
. "$MODULE"
install
`

func TestDracutModuleAddsSimpledrmOnlyWithTheBootDisplayDropIn(t *testing.T) {
	module := filepath.Join(repoRoot(t), "system/root/usr/lib/dracut/modules.d/40nimbus-plymouth/module-setup.sh")
	for _, test := range []struct {
		name, conf string
		dropIn     bool
		want       string
	}{
		{"desktop", "[Daemon]\nTheme=nimbus\n", true, "[Daemon]\nTheme=nimbus\nUseSimpledrm=2\n"},
		{"laptop keeps the native driver path", "[Daemon]\nTheme=nimbus\n", false, ""},
		{"an owner-set value wins", "[Daemon]\nTheme=nimbus\nUseSimpledrm=0\n", true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			sysroot, initdir := t.TempDir(), t.TempDir()
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(sysroot, path)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sysroot, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("etc/plymouth/plymouthd.conf", test.conf)
			if test.dropIn {
				write("etc/dracut.conf.d/90-nimbus-boot-display.conf", "omit_drivers+=\" i915 \"\n")
			}
			cmd := exec.Command("sh", "-c", dracutStubs)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "MODULE=" + module, "initdir=" + initdir, "dracutsysrootdir=" + sysroot}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			// Without a copy of its own the module leaves the file to 45plymouth.
			got, _ := os.ReadFile(filepath.Join(initdir, "etc/plymouth/plymouthd.conf"))
			if string(got) != test.want {
				t.Fatalf("initramfs plymouthd.conf = %q, want %q", got, test.want)
			}
			host, _ := os.ReadFile(filepath.Join(sysroot, "etc/plymouth/plymouthd.conf"))
			if string(host) != test.conf {
				t.Fatalf("host plymouthd.conf was modified: %q", host)
			}
		})
	}
}

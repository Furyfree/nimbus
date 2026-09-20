package bootmenu

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func mirrorFixture(t *testing.T, entries []string, saved string) (*nativetest.FakeSource, string, string, string) {
	t.Helper()
	root := t.TempDir()
	entriesDir := filepath.Join(root, "entries")
	mirrorDir := filepath.Join(root, "entries-nimbus")
	grubenv := filepath.Join(root, "grubenv")
	if err := os.MkdirAll(entriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		if err := os.WriteFile(filepath.Join(entriesDir, name+".conf"), []byte("title "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Dirs[entriesDir] = nil
	for _, name := range entries {
		src.Dirs[entriesDir] = append(src.Dirs[entriesDir], name+".conf")
	}
	if saved != "" {
		src.Files[grubenv] = []byte("# GRUB Environment Block\nsaved_entry=" + saved + "\n")
	}
	return src, entriesDir, grubenv, mirrorDir
}

func TestMirror(t *testing.T) {
	t.Run("mirrors the saved entry and drops stale copies", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-6.19.10-300.fc44.x86_64"}, "m-7.2.5-200.fc44.x86_64")
		if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mirrorDir, "m-6.19.10-300.fc44.x86_64.conf"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mirrorDir, "notes.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mirrorDir, ".nimbus-old.tmp"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q", id)
		}
		got, err := os.ReadDir(mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range got {
			names = append(names, entry.Name())
		}
		if !slices.Contains(names, id+".conf") || slices.Contains(names, "m-6.19.10-300.fc44.x86_64.conf") || !slices.Contains(names, "notes.txt") || slices.Contains(names, ".nimbus-old.tmp") {
			t.Fatalf("mirror contents: %v", names)
		}
		info, err := os.Stat(mirrorDir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("mirror mode = %v, err = %v", info.Mode().Perm(), err)
		}
	})
	t.Run("a debug entry never wins the newest fallback", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-7.2.6-200.fc44.x86_64-debug"}, "m-9.9.9-removed.fc44.x86_64")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("a debug entry led the fallback: got %q", id)
		}
	})
	t.Run("a +debug suffix never wins the newest fallback", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-7.2.6-200.fc44.x86_64+debug"}, "m-9.9.9-removed.fc44.x86_64")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("a +debug entry led the fallback: got %q", id)
		}
	})
	t.Run("a rescue entry never leads beside a lone debug kernel", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-0-rescue-abc", "m-7.2.6-200.fc44.x86_64-debug"}, "m-9.9.9-removed.fc44.x86_64")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.6-200.fc44.x86_64-debug" {
			t.Fatalf("a rescue entry led over the only real kernel: got %q", id)
		}
	})
	t.Run("a stale saved entry falls back to the newest", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-6.19.10-300.fc44.x86_64", "m-0-rescue-abc"}, "m-9.9.9-removed.fc44.x86_64")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q", id)
		}
	})
	t.Run("a rescue saved entry stays out of the top level", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-0-rescue-abc"}, "m-0-rescue-abc")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q", id)
		}
	})
	t.Run("without a grubenv the newest wins", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-6.19.10-300.fc44.x86_64", "m-7.2.5-200.fc44.x86_64"}, "")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil || id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q, %v", id, err)
		}
	})
	t.Run("no entries blocks", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, nil, "")
		if _, err := Mirror(src, entriesDir, grubenv, mirrorDir); err == nil {
			t.Fatal("expected an error without BLS entries")
		}
	})
	t.Run("a failed refresh keeps the previous mirror", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-6.19.10-300.fc44.x86_64"}, "m-7.2.5-200.fc44.x86_64")
		if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
			t.Fatal(err)
		}
		previous := filepath.Join(mirrorDir, "m-6.19.10-300.fc44.x86_64.conf")
		if err := os.WriteFile(previous, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(entriesDir, "m-7.2.5-200.fc44.x86_64.conf")); err != nil {
			t.Fatal(err)
		}
		if _, err := Mirror(src, entriesDir, grubenv, mirrorDir); err == nil {
			t.Fatal("expected a source read failure")
		}
		if data, err := os.ReadFile(previous); err != nil || string(data) != "old" {
			t.Fatalf("previous mirror was destroyed: %q %v", data, err)
		}
	})
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{"m-0-rescue-abc", "m-6.19.10-300.fc44.x86_64", "m-7.2.5-200.fc44.x86_64", "m-7.10.1-1.fc44.x86_64"}
	for i := range len(ordered) - 1 {
		if compareVersions(ordered[i], ordered[i+1]) >= 0 {
			t.Fatalf("%q should sort before %q", ordered[i], ordered[i+1])
		}
		if compareVersions(ordered[i+1], ordered[i]) <= 0 {
			t.Fatalf("%q should sort after %q", ordered[i+1], ordered[i])
		}
	}
	if compareVersions(ordered[1], ordered[1]) != 0 {
		t.Fatal("equal versions must compare equal")
	}
}

func TestMirrorWritesAPlainTitle(t *testing.T) {
	id := "m-7.2.5-200.fc44.x86_64"
	src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{id}, id)
	entry := "title Fedora Linux (7.2.5-200.fc44.x86_64) 44 (Forty Four)\nversion 7.2.5-200.fc44.x86_64\nlinux /vmlinuz-7.2.5-200.fc44.x86_64\noptions root=UUID=x title (kept)\n"
	if err := os.WriteFile(filepath.Join(entriesDir, id+".conf"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Mirror(src, entriesDir, grubenv, mirrorDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(mirrorDir, id+".conf"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(entry, "title Fedora Linux (7.2.5-200.fc44.x86_64) 44 (Forty Four)", "title Fedora Linux", 1)
	if string(got) != want {
		t.Fatalf("mirror = %q, want %q", got, want)
	}
	// Fedora's own entry, listed under Previous kernels, keeps its title.
	if live, _ := os.ReadFile(filepath.Join(entriesDir, id+".conf")); string(live) != entry {
		t.Fatalf("the live entry was modified: %q", live)
	}
	// A title without a parenthesis has nothing to shorten.
	if got := plainTitle([]byte("title Custom\n")); string(got) != "title Custom\n" {
		t.Fatalf("plain title changed: %q", got)
	}
}

// The engine payload is a contract: the marker gate, the internal call, the
// guard and the submenu label are what the plan and SPEC promise.
func TestPreviousKernelsPayloadContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "grub.d", "09_nimbus_previous_kernels"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		Marker,
		`rm -rf "$mirror"`,
		"/usr/bin/nimbus internal boot-menu",
		"blscfg $entry",
		"if [ -f $entries_path/$entry.conf -a -f $mirror_path/$entry.conf ]",
		"make_system_path_relative_to_its_root",
		"submenu 'Previous kernels'",
		"set blsdir=$entries_path",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("payload lacks %q", want)
		}
	}
	if strings.Count(script, "if [ -f $entries_path/$entry.conf -a -f $mirror_path/$entry.conf ]") != 2 {
		t.Fatal("both Nimbus menu blocks must be guarded on the live entry and its mirror")
	}
	// The submenu is only defined here and enabled under that guard;
	// 30_previous_kernels_nimbus places it after the other operating systems.
	for _, want := range []string{"function nimbus_previous_kernels {", "set nimbus_previous_kernels_ready=y"} {
		if !strings.Contains(script, want) {
			t.Fatalf("payload lacks %q", want)
		}
	}
	if guard := strings.LastIndex(script, "if [ -f $entries_path/$entry.conf"); guard < 0 || !strings.Contains(script[guard:], "set nimbus_previous_kernels_ready=y") {
		t.Fatal("the submenu must be enabled only under the entry and mirror guard")
	}
	placeData, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "grub.d", "30_previous_kernels_nimbus"))
	if err != nil {
		t.Fatal(err)
	}
	place := string(placeData)
	for _, want := range []string{Marker, `if [ "$nimbus_previous_kernels_ready" = "y" ]; then`, "    nimbus_previous_kernels\n"} {
		if !strings.Contains(place, want) {
			t.Fatalf("placement payload lacks %q", want)
		}
	}
	// The order Fedora, other systems, Previous kernels, firmware depends on
	// where the file sorts among Fedora's own scripts.
	if name := "30_previous_kernels_nimbus"; name <= "30_os-prober" || name >= "30_uefi-firmware" {
		t.Fatal("the placement script must sort between 30_os-prober and 30_uefi-firmware")
	}
	if !strings.Contains(script, "else\n    set blsdir=$entries_path\nfi") {
		t.Fatal("the fallback path must point blsdir at the live entries directory")
	}
	if !strings.Contains(script, "set blsdir=$mirror_path") {
		t.Fatal("the happy path must keep blsdir on the single-entry mirror")
	}
	if strings.Contains(script, "blscfg $entry\nfi\nset blsdir=$entries_path") {
		t.Fatal("the live entries directory must not replace the mirror before Fedora's blscfg runs")
	}
	submenu := script[strings.Index(script, "submenu 'Previous kernels'"):]
	if !strings.Contains(submenu, "set blsdir=$entries_path") {
		t.Fatal("the submenu must point blsdir at the live entries directory")
	}
	// The submenu must stay unfiltered: GRUB's default comes from
	// saved_entry, and the "non-default" filter would hide a rescue saved
	// entry.
	if strings.Contains(script, "blscfg non-default") {
		t.Fatal("payload filters the submenu by GRUB's default, which can hide rescue")
	}
}

// The Paper Dark drop-in must not attempt loadfont under Secure Boot: GRUB's
// shim-lock verifier refuses the font file type and prints an error for each
// call, so the faces load only when shim_lock is unset.
func TestPaperDarkFontGuard(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "grub.d", "36_paper_dark"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	guard := "if [ \"$shim_lock\" != \"y\" ]; then"
	start := strings.Index(script, guard)
	if start < 0 {
		t.Fatal("payload lacks the shim_lock font guard")
	}
	end := strings.Index(script[start:], "\nfi\n")
	if end < 0 {
		t.Fatal("payload lacks the guarded block end")
	}
	guarded := script[start : start+end]
	for _, font := range []string{"mono-16.pf2", "mono-20.pf2", "mono-24.pf2"} {
		if !strings.Contains(guarded, font) {
			t.Fatalf("font %s is loaded outside the shim_lock guard", font)
		}
	}
	if strings.Count(script, "loadfont $prefix") != 3 {
		t.Fatal("unexpected loadfont lines in the payload")
	}
}

// The menu hook regenerates grub.cfg on kernel installs and removals, because
// Fedora's own grub hook only runs when BLS is disabled. It must stay inert
// without the marker and warn instead of failing the kernel transaction; the
// mirror itself is refreshed by 09_nimbus_previous_kernels during mkconfig.
func TestMenuHookPayloadContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "kernel", "install.d", "96-nimbus-menu.install"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		"[ -f /etc/nimbus/boot-theme.enabled ] || exit 0",
		"/usr/sbin/grub2-mkconfig --no-grubenv-update -o /boot/grub2/grub.cfg",
		"warning: the Nimbus kernel menu was not regenerated; the previous grub.cfg is retained",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("hook payload lacks %q", want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(script), "exit 0") {
		t.Fatal("hook payload does not end in a successful exit, which would fail kernel updates")
	}
}

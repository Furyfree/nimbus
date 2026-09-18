package postinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestFDEBootEntries(t *testing.T) {
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	output := "BootCurrent: 0008\nBootOrder: 0008,0009,000A\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n" +
		"Boot000A  Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/EFI/Linux/old.efi\n"
	entries := fdeBootEntries(output)
	if len(entries) != 3 {
		t.Fatalf("got %+v", entries)
	}
	nimbus := fdeNimbusEntries(entries)
	if len(nimbus) != 2 || nimbus[0].ID != "0009" || nimbus[0].Loader != `EFI\Linux\nimbus.efi` {
		t.Fatalf("got %+v", nimbus)
	}
	if nimbus[1].ID != "000A" || nimbus[1].Loader != `EFI\Linux\old.efi` {
		t.Fatalf("forward slashes were not normalized: %+v", nimbus[1])
	}
	if !fdeEntryCorrect(entries, false) {
		t.Fatal("the current entry was not recognized")
	}
	if fdeEntryCorrect([]fdeBootEntry{nimbus[1]}, false) {
		t.Fatal("a stale entry was treated as correct")
	}
	if fdeLoaderPath("HD(1,GPT,g,0x800,0x200000)") != "" {
		t.Fatal("a device path without a loader was not rejected")
	}
	// The signed chain resolves the Fedora shim and matches the load option
	// data, which efibootmgr prints as UCS-2 hex when unterminated.
	options := fdeUCS2Hex(FDEBootLoader + " ")
	secure := "BootCurrent: 0008\nBootOrder: 0008,0009\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi" + options + "\n"
	secureEntries := fdeBootEntries(secure)
	if !fdeEntryCorrect(secureEntries, true) {
		t.Fatalf("the signed entry was not recognized: %+v", secureEntries)
	}
	if fdeEntryCorrect(secureEntries, false) {
		t.Fatal("a load-option entry was accepted as a direct entry")
	}
	if _, ok := fdeShimLoader(fdeBootEntries(output)); !ok {
		t.Fatal("the Fedora shim entry was not found")
	}
	// Some efibootmgr builds print the File(...) device-path form.
	fileForm := "BootCurrent: 0008\nBootOrder: 0008,0009\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/File(\\EFI\\fedora\\shimx64.efi)\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/File(\\EFI\\fedora\\shimx64.efi) File(.\\EFI\\Linux\\nimbus.efi )\n"
	if !fdeEntryCorrect(fdeBootEntries(fileForm), true) {
		t.Fatalf("the File() form was not recognized: %+v", fdeBootEntries(fileForm))
	}
}

func TestFDEReplacesStaleSameLabelEntries(t *testing.T) {
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	output := "BootCurrent: 0009\nBootOrder: 0009,000A\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n" +
		"Boot000A  Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\old.efi\n"
	create := nativetest.Key("sudo", "--", "efibootmgr", "-c", "-d", "/dev/vda", "-p", "1", "-L", FDEBootLabel, "-l", FDEBootLoader)
	newSource := func() *nativetest.FakeSource {
		return &nativetest.FakeSource{
			Commands: map[string][]byte{
				nativetest.Key("efibootmgr"):                                   []byte(output),
				nativetest.Key("sudo", "--", "efibootmgr", "-b", "0009", "-B"): nil,
				nativetest.Key("sudo", "--", "efibootmgr", "-b", "000A", "-B"): nil,
				create: nil,
			},
			Failures: map[string]string{},
			Files:    map[string][]byte{"/proc/mounts": []byte("/dev/vda1 /boot/efi vfat rw 0 0\n")},
			Dirs:     map[string][]string{"/sys/firmware/efi/efivars": {}},
			Paths:    map[string]string{},
		}
	}
	t.Run("a stale duplicate is removed and recreated", func(t *testing.T) {
		var out bytes.Buffer
		if err := fdeEnsureEntry(newSource(), &out, &out); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"efibootmgr -b 0009 -B", "efibootmgr -b 000A -B", "-c -d /dev/vda -p 1"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output lacks %q:\n%s", want, out.String())
			}
		}
	})
	t.Run("a single correct entry is left alone", func(t *testing.T) {
		src := newSource()
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		var out bytes.Buffer
		if err := fdeEnsureEntry(src, &out, &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "-B") {
			t.Fatalf("a single correct entry was replaced:\n%s", out.String())
		}
	})
	t.Run("the default boot order is promoted", func(t *testing.T) {
		src := newSource()
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" +
			"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		src.Commands[nativetest.Key("sudo", "--", "efibootmgr", "-o", "0009,0008")] = nil
		var out bytes.Buffer
		if err := fdeEnsureEntry(src, &out, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "-o 0009,0008") {
			t.Fatalf("the boot order was not promoted:\n%s", out.String())
		}
	})
}

func TestFDEESPDevice(t *testing.T) {
	cases := map[string][2]string{
		"/dev/vda1 /boot/efi vfat rw 0 0\n":                                  {"/dev/vda", "1"},
		"/dev/nvme0n1p2 /boot/efi vfat rw 0 0\n":                             {"/dev/nvme0n1", "2"},
		"/dev/mmcblk0p1 /boot/efi vfat rw 0 0\n":                             {"/dev/mmcblk0", "1"},
		"/dev/vda1 /boot/efi vfat rw 0 0\n/dev/vdb1 /boot/efi vfat rw 0 0\n": {"/dev/vdb", "1"},
	}
	for mounts, want := range cases {
		src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{"/proc/mounts": []byte(mounts)}, Dirs: map[string][]string{}, Paths: map[string]string{}}
		disk, part, err := fdeESPDevice(src)
		if err != nil || disk != want[0] || part != want[1] {
			t.Fatalf("%q: got %s %s, %v", mounts, disk, part, err)
		}
	}
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{"/proc/mounts": []byte("/dev/mapper/esp /boot/efi vfat rw 0 0\n")}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	if _, _, err := fdeESPDevice(src); err == nil {
		t.Fatal("a non-partition ESP was accepted")
	}
}

// fdeSetupSource replays the privileged setup and simulates its effects.
type fdeSetupSource struct {
	*nativetest.FakeSource
	t          *testing.T
	streams    []string
	staged     string
	digest     string
	removals   int
	built      string
	buildFails bool
	entryFails bool
	entry      bool
	stale      bool
	secure     bool
}

func (s *fdeSetupSource) Run(name string, args ...string) ([]byte, error) {
	if name == "efibootmgr" {
		const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
		fedora := "Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n"
		nimbus := "Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n"
		if s.secure {
			nimbus = "Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi" + fdeUCS2Hex(FDEBootLoader+" ") + "\n"
		}
		switch {
		case s.entry:
			return []byte("BootCurrent: 0009\nBootOrder: 0009,0008\n" + fedora + nimbus), nil
		case s.stale:
			return []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" + fedora + "Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\\\EFI\\\\Linux\\\\nimbus.efi\n"), nil
		default:
			return []byte("BootCurrent: 0008\nBootOrder: 0008\n" + fedora), nil
		}
	}
	return s.FakeSource.Run(name, args...)
}

func (s *fdeSetupSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	if name != "sudo" {
		s.t.Fatalf("unexpected native mutation: %s", name)
	}
	if len(args) == 1 && args[0] == "--validate" {
		return nil
	}
	if len(args) < 2 || args[0] != "--" {
		s.t.Fatalf("unexpected sudo invocation: %v", args)
	}
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "internal system-file"):
		s.staged = args[len(args)-1]
		data, err := os.ReadFile(s.staged)
		if err != nil {
			s.t.Fatal(err)
		}
		info, err := os.Stat(s.staged)
		if err != nil || info.Mode().Perm() != 0600 {
			s.t.Fatal("unsafe staging")
		}
		var payload apply.FilePayload
		if err := json.Unmarshal(data, &payload); err != nil {
			s.t.Fatal(err)
		}
		for i, arg := range args {
			if arg == "--plan" {
				s.digest = args[i+1]
			}
		}
		if payload.PlanDigest != s.digest || s.digest == "" {
			s.t.Fatal("payload is not bound to the inspected digest")
		}
		if payload.Change.Target != FDEUKIMarker {
			s.t.Fatal("wrong file payload target")
		}
		if !payload.Change.After.Exists {
			s.removals++
			delete(s.Files, FDEUKIMarker)
			break
		}
		if !bytes.Equal(payload.Change.After.Content, []byte(fdeMarkerText)) || payload.Change.After.Mode != "0644" || payload.Change.After.Owner != "root" || payload.Change.After.Group != "root" {
			s.t.Fatal("wrong file payload")
		}
		s.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		s.Dirs["/etc/nimbus"] = []string{"fde-uki.enabled"}
	case strings.Contains(joined, "internal fde-uki add"):
		if s.buildFails {
			return errors.New("fixture build failure")
		}
		s.built = args[len(args)-1]
	case strings.Contains(joined, "internal fde-uki genkey"):
	case args[1] == "mokutil" && strings.Contains(joined, "--import"):
	case args[1] == "efibootmgr" && strings.Contains(joined, " -c "):
		if s.entryFails {
			return errors.New("fixture entry failure")
		}
		s.entry = true
	case args[1] == "efibootmgr" && strings.Contains(joined, " -B"):
	case args[1] == "efibootmgr" && strings.Contains(joined, " -o "):
	default:
		s.t.Fatalf("unexpected native mutation: %s", joined)
	}
	return nil
}

func fdeSetupFixture(t *testing.T) *fdeSetupSource {
	t.Helper()
	const version = "6.19.10-300.fc44.x86_64"
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	base.Files["/proc/mounts"] = []byte("/dev/mapper/luks-1234[/root] / btrfs rw 0 0\n/dev/vda1 /boot/efi vfat rw 0 0\n")
	base.Dirs["/"] = []string{"etc"}
	base.Dirs["/etc"] = []string{"kernel", "nimbus"}
	base.Dirs["/etc/nimbus"] = []string{}
	base.Dirs["/sys/firmware/efi/efivars"] = []string{}
	base.Commands[nativetest.Key("uname", "-r")] = []byte(version + "\n")
	base.Commands[nativetest.Key("stat", "--format=%s", "--", fdeKernelDir+"/"+version+"/vmlinuz")] = []byte("18497536\n")
	base.Commands[nativetest.Key("stat", "--format=%s", "--", "/boot/initramfs-"+version+".img")] = []byte("47000000\n")
	base.Commands[nativetest.Key("df", "--output=avail", "-B1", "/boot/efi")] = []byte("Avail\n995000000\n")
	base.Commands[nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc")] = []byte("directory|root|root|755|6\n")
	base.Commands[nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/nimbus")] = []byte("directory|root|root|755|2\n")
	base.Commands[nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", FDEUKIMarker)] = []byte("regular file|root|root|644|1\n")
	src := &fdeSetupSource{FakeSource: base, t: t}
	fdeTestKeys(t)
	src.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	src.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
	return src
}

func fdeSetupTask() Task {
	return Task{ID: "fde", Owner: "component:fde", Title: "Set up TPM automatic disk unlock", Status: Pending, Action: &Action{Kind: SetupFDE}}
}

// The engine payload is a contract: the marker gate must stay inert, and a
// failed build must not fail the kernel update while telling the user.
func TestFDEHookPayloadContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "kernel", "install.d", "90-nimbus-uki.install"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		"[ -e /etc/nimbus/fde-uki.enabled ] || exit 0",
		"/usr/bin/nimbus internal fde-uki",
		"warning: the Nimbus kernel image was not rebuilt",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("hook payload lacks %q", want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(script), "exit 0") {
		t.Fatal("hook payload does not end in a successful exit, which would fail kernel updates")
	}
}

func TestFDESetupCommands(t *testing.T) {
	direct, err := FDESetupCommands(fdeSetupTask())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(slices.Concat(direct...), " ")
	if !strings.Contains(joined, "fde-uki genkey") || strings.Contains(joined, "mokutil --import") {
		t.Fatalf("direct preview wrong: %v", direct)
	}
	if !strings.Contains(joined, `-l \EFI\Linux\nimbus.efi`) || strings.Contains(joined, " -u ") {
		t.Fatalf("direct entry preview wrong: %v", direct)
	}
	secure := fdeSetupTask()
	secure.fdeSecure = true
	commands, err := FDESetupCommands(secure)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(slices.Concat(commands...), " ")
	if !strings.Contains(joined, "mokutil --import") || !strings.Contains(joined, " -u ") || !strings.Contains(joined, "<fedora-shim-loader>") {
		t.Fatalf("secure preview wrong: %v", commands)
	}
}

func TestVerifyFDESecureBoot(t *testing.T) {
	secureEntries := func() []byte {
		const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
		options := fdeUCS2Hex(FDEBootLoader + " ")
		return []byte("BootCurrent: 0009\nBootOrder: 0009,0008\n" +
			"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi" + options + "\n")
	}
	t.Run("enrolled MOK completes", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		fdeEnableSecureBoot(src.FakeSource, true)
		src.Commands[nativetest.Key("efibootmgr")] = secureEntries()
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		der := fdeKeys().mokDER
		key := nativetest.Key("sudo", "-n", "--", "mokutil", "--ignore-keyring", "--test-key", der)
		src.Commands[key] = []byte(der + " is already enrolled\n")
		src.ExitCodes[key] = 1
		fdeStubEnrollment(t, src.FakeSource, "1")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Complete || !got.FDESecure() {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("pending MOK stays pending", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		fdeEnableSecureBoot(src.FakeSource, true)
		src.Commands[nativetest.Key("efibootmgr")] = secureEntries()
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		der := fdeKeys().mokDER
		key := nativetest.Key("sudo", "-n", "--", "mokutil", "--ignore-keyring", "--test-key", der)
		src.Commands[key] = []byte(der + " is already in the enrollment request\n")
		src.ExitCodes[key] = 1
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || !strings.Contains(got.Detail, "pending") {
			t.Fatalf("got %+v", got)
		}
	})
}

// fdeEnrollRootSource builds the root-observed state used by the enrollment
// record writer.
func fdeEnrollRootSource(t *testing.T, slot, token string) *nativetest.FakeSource {
	t.Helper()
	fdeTestKeys(t)
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	src.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
	for _, pcr := range []string{"7", "12", "13", "14"} {
		src.Files["/sys/class/tpm/tpm0/pcr-sha256/"+pcr] = []byte("aa" + pcr + "\n")
	}
	src.Dirs["/sys/firmware/efi/efivars"] = []string{}
	src.Commands[nativetest.Key("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", "/dev/disk/by-uuid/1")] =
		[]byte(`{"tokens":{"` + token + `":{"type":"systemd-tpm2","keyslots":["` + slot + `"]}}}`)
	return src
}

func TestFDEEnrollmentRecord(t *testing.T) {
	previousPath, previousSource := fdeEnrollmentPath, fdeRootSource
	fdeEnrollmentPath = filepath.Join(t.TempDir(), "enrollment.json")
	fdeRootSource = fdeEnrollRootSource(t, "1", "0")
	t.Cleanup(func() { fdeEnrollmentPath = previousPath; fdeRootSource = previousSource })
	if err := WriteFDEEnrollment("1", "0"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fdeEnrollmentPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode=%v err=%v", info.Mode().Perm(), err)
	}
	data, err := ReadFDEEnrollment()
	if err != nil || !strings.Contains(string(data), `"enrolled_at"`) || !strings.Contains(string(data), `"keyslot":"1"`) {
		t.Fatalf("record roundtrip: %q, %v", data, err)
	}
	if err := WriteFDEEnrollment("1", "9"); err == nil {
		t.Fatal("an unobserved token was accepted")
	}
	if err := WriteFDEEnrollment("all", "0"); err == nil {
		t.Fatal("a non-numeric keyslot was accepted")
	}
	if err := os.Remove(fdeEnrollmentPath); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadFDEEnrollment(); err != nil || data != nil {
		t.Fatalf("a missing record was not reported as absent: %q, %v", data, err)
	}
}

func TestFDETokens(t *testing.T) {
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	key := nativetest.Key("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", "/dev/disk/by-uuid/1")
	src.Commands[key] = []byte(`{"tokens":{"1":{"type":"systemd-tpm2","keyslots":["2"]},"0":{"type":"systemd-tpm2","keyslots":["1"]},"9":{"type":"luks2-keyring"}}}`)
	tokens, err := fdeTokens(src)
	if err != nil || len(tokens) != 2 || tokens[0].Keyslot != "1" || tokens[0].ID != "0" || tokens[1].Keyslot != "2" {
		t.Fatalf("got %+v, %v", tokens, err)
	}
	src.Commands[key] = []byte(`{"tokens":{}}`)
	if tokens, err := fdeTokens(src); err != nil || len(tokens) != 0 {
		t.Fatalf("empty tokens: %+v, %v", tokens, err)
	}
	src.Commands[key] = []byte(`not json`)
	if _, err := fdeTokens(src); err == nil {
		t.Fatal("unparsable metadata was accepted")
	}
}

// fdeEnrollSource flips the token metadata on enrollment so the before/after
// observation can be exercised.
type fdeEnrollSource struct {
	*nativetest.FakeSource
	t         *testing.T
	enrolled  bool
	tokenJSON string
	// nextToken replaces tokenJSON after a successful cryptenroll wipe, so
	// renewal can be observed to leave a new single-slot token.
	nextToken string
	streams   []string
}

func (s *fdeEnrollSource) Run(name string, args ...string) ([]byte, error) {
	if name == "sudo" && len(args) == 6 && args[2] == "cryptsetup" {
		if s.tokenJSON != "" {
			return []byte(s.tokenJSON), nil
		}
		if s.enrolled {
			return []byte(`{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["1"]}},"keyslots":{"0":{"type":"luks2"},"1":{"type":"luks2"}}}`), nil
		}
		return []byte(`{"tokens":{},"keyslots":{"0":{"type":"luks2"}}}`), nil
	}
	return s.FakeSource.Run(name, args...)
}

func (s *fdeEnrollSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	if name == "sudo" && len(args) == 1 && args[0] == "--validate" {
		return nil
	}
	if name == "sudo" && len(args) > 1 && args[1] == "systemd-cryptenroll" {
		if strings.Contains(strings.Join(args, " "), "--wipe-slot") {
			if s.nextToken != "" {
				s.tokenJSON = s.nextToken
				s.enrolled = true
				return nil
			}
			s.tokenJSON = `{"tokens":{},"keyslots":{"0":{"type":"luks2"}}}`
			s.enrolled = false
			return nil
		}
		s.enrolled = true
		s.tokenJSON = ""
		return nil
	}
	if name == "sudo" && len(args) > 1 && (args[1] == "efibootmgr" || args[1] == "rm" || args[1] == "rmdir") {
		return nil
	}
	return s.FakeSource.Stream(nil, nil, name, args...)
}

func fdeEnrollFixture(t *testing.T) *fdeEnrollSource {
	t.Helper()
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	fdeTestKeys(t)
	previous := fdeExecutable
	fdeExecutable = func() (string, error) { return "/usr/bin/nimbus", nil }
	t.Cleanup(func() { fdeExecutable = previous })
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	base.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	base.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	base.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
	base.Dirs["/sys/firmware/efi/efivars"] = []string{}
	base.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009,0008\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
	base.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = []byte("")
	base.Commands[nativetest.Key("sudo", "--", "/usr/bin/nimbus", "internal", "fde-uki", "record", "1", "0")] = nil
	return &fdeEnrollSource{FakeSource: base, t: t}
}

// fdeRemoveFixture starts from an enrolled state whose token matches the
// record; the token metadata can be overridden per test.
func fdeRemoveFixture(t *testing.T, tokenJSON string) *fdeEnrollSource {
	t.Helper()
	src := fdeEnrollFixture(t)
	src.enrolled = true
	record := FDEEnrollment{Schema: 1, Device: "/dev/disk/by-uuid/1", UUID: "1", Keyslot: "1", Token: "0",
		PCRs: "7+14+12+13+11", Fingerprint: "sha256:x"}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	src.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = data
	src.Commands[nativetest.Key("sudo", "-n", "--", "test", "-d", fdeKeyDir)] = nil
	if tokenJSON != "" {
		src.tokenJSON = tokenJSON
	}
	return src
}

func TestFDERemoveCommands(t *testing.T) {
	task := Task{ID: "fde", Owner: "component:fde", Status: Pending, Action: &Action{Kind: RemoveFDE}}
	commands, err := FDERemoveCommands(task)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(slices.Concat(commands...), " ")
	for _, want := range []string{"--wipe-slot=<recorded-keyslot>", "efibootmgr", FDEUKIPath, FDEUKIMarker, "enrollment.json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("removal preview lacks %q: %v", want, commands)
		}
	}
}

func TestRunFDERemove(t *testing.T) {
	task := Task{ID: "fde", Owner: "component:fde", Status: Pending, Action: &Action{Kind: RemoveFDE}}
	t.Run("removes only the recorded ownership", func(t *testing.T) {
		src := fdeRemoveFixture(t, "")
		var out bytes.Buffer
		if err := RunFDERemove(t.Context(), src, &out, &out, task); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(src.streams, "\n")
		if !strings.Contains(joined, "--wipe-slot=1") {
			t.Fatalf("the recorded slot was not wiped: %v", src.streams)
		}
		for _, want := range []string{"efibootmgr -b 0009 -B", "rm -f " + FDEUKIPath, "rm -f " + FDEUKIMarker, "rmdir " + fdeKeyDir} {
			if !strings.Contains(joined, want) {
				t.Fatalf("removal lacks %q: %v", want, src.streams)
			}
		}
		if !strings.Contains(out.String(), "passphrase") {
			t.Fatalf("missing fallback guidance: %s", out.String())
		}
	})
	t.Run("an unowned token is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"5":{"type":"systemd-tpm2","keyslots":["3"]}},"keyslots":{"0":{"type":"luks2"},"3":{"type":"luks2"}}}`)
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err == nil || !strings.Contains(err.Error(), "not the one Nimbus recorded") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("the last unlock method is preserved", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["1"]}},"keyslots":{"1":{"type":"luks2"}}}`)
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err == nil || !strings.Contains(err.Error(), "no other unlock method") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("only an unreferenced keyslot counts as another unlock", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["1"]},"2":{"type":"systemd-fido2","keyslots":["2"]}},"keyslots":{"1":{"type":"luks2"},"2":{"type":"luks2"}}}`)
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err == nil || !strings.Contains(err.Error(), "no other unlock method") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a different volume is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, "")
		src.Files[fdeCmdlineFile] = []byte(strings.Replace(fdeTestBase, "luks-1", "luks-2", 1) + "\n")
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err == nil || !strings.Contains(err.Error(), "refusing to wipe") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a token bound to several keyslots is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["1","2"]}},"keyslots":{"0":{"type":"luks2"},"1":{"type":"luks2"},"2":{"type":"luks2"}}}`)
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err == nil || !strings.Contains(err.Error(), "refusing to wipe") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a pending BootNext entry is cleared with its entry", func(t *testing.T) {
		src := fdeRemoveFixture(t, "")
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootNext: 0009\nBootOrder: 0009,0008\n" +
			"Boot0008* Fedora\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		if err := RunFDERemove(t.Context(), src, io.Discard, io.Discard, task); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(src.streams, "\n"), "efibootmgr -N") {
			t.Fatalf("BootNext was not cleared: %v", src.streams)
		}
	})
	t.Run("setup without a token removes the boot path only", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{},"keyslots":{"0":{"type":"luks2"}}}`)
		var out bytes.Buffer
		if err := RunFDERemove(t.Context(), src, &out, &out, task); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(src.streams, "\n")
		if strings.Contains(joined, "--wipe-slot") {
			t.Fatalf("a token was wiped without an enrollment: %v", src.streams)
		}
		for _, want := range []string{"efibootmgr -b 0009 -B", "rm -f " + FDEUKIMarker, "rm -f " + FDEUKIPath} {
			if !strings.Contains(joined, want) {
				t.Fatalf("cleanup lacks %q: %v", want, src.streams)
			}
		}
	})
}

func TestFDERenewCommands(t *testing.T) {
	task := Task{ID: "fde", Owner: "component:fde", Status: Pending, Action: &Action{Kind: RenewFDE}}
	commands, err := FDERenewCommands(task)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(slices.Concat(commands...), " ")
	for _, want := range []string{"systemd-cryptenroll", "--wipe-slot=<recorded-keyslot>", "--tpm2-pcrs=7+14+12+13", "fde-uki record"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("renewal preview lacks %q: %v", want, commands)
		}
	}
}

func TestRunFDERenew(t *testing.T) {
	task := Task{ID: "fde", Owner: "component:fde", Status: Pending, Action: &Action{Kind: RenewFDE}}
	t.Run("replaces only the recorded enrollment", func(t *testing.T) {
		src := fdeRemoveFixture(t, "")
		src.nextToken = `{"tokens":{"1":{"type":"systemd-tpm2","keyslots":["2"]}},"keyslots":{"0":{"type":"luks2"},"2":{"type":"luks2"}}}`
		src.Commands[nativetest.Key("sudo", "--", "/usr/bin/nimbus", "internal", "fde-uki", "record", "2", "1")] = nil
		var out bytes.Buffer
		if err := RunFDERenew(t.Context(), src, &out, &out, task); err != nil {
			t.Fatal(err)
		}
		crypt := slices.IndexFunc(src.streams, func(stream string) bool {
			return strings.Contains(stream, "systemd-cryptenroll")
		})
		if crypt < 0 || !strings.Contains(src.streams[crypt], "--wipe-slot=1") {
			t.Fatalf("renewal argv wrong: %v", src.streams)
		}
		if !strings.Contains(out.String(), "TPM automatic unlock renewed") {
			t.Fatalf("missing renewal guidance: %s", out.String())
		}
	})
	t.Run("a foreign token is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"9":{"type":"systemd-tpm2","keyslots":["2"]}},"keyslots":{"0":{"type":"luks2"},"2":{"type":"luks2"}}}`)
		err := RunFDERenew(t.Context(), src, io.Discard, io.Discard, task)
		if err == nil || !strings.Contains(err.Error(), "not the one Nimbus recorded") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a multi-slot token is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, `{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["1","2"]}},"keyslots":{"0":{"type":"luks2"},"1":{"type":"luks2"},"2":{"type":"luks2"}}}`)
		err := RunFDERenew(t.Context(), src, io.Discard, io.Discard, task)
		if err == nil || !strings.Contains(err.Error(), "not the one Nimbus recorded") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a non-active boot entry is refused", func(t *testing.T) {
		src := fdeRemoveFixture(t, "")
		const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" +
			"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		err := RunFDERenew(t.Context(), src, io.Discard, io.Discard, task)
		if err == nil || !strings.Contains(err.Error(), "reboot into the Nimbus image") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestRunFDEEnroll(t *testing.T) {
	task := Task{ID: "fde", Owner: "component:fde", Title: "Set up TPM automatic disk unlock", Status: Pending, Action: &Action{Kind: EnrollFDE}}
	t.Run("fresh enrollment records the observed token", func(t *testing.T) {
		src := fdeEnrollFixture(t)
		var out bytes.Buffer
		if err := RunFDEEnroll(t.Context(), src, &out, &out, task); err != nil {
			t.Fatal(err)
		}
		crypt := slices.IndexFunc(src.streams, func(stream string) bool {
			return strings.Contains(stream, "systemd-cryptenroll")
		})
		if crypt < 0 || strings.Contains(src.streams[crypt], "--wipe-slot") {
			t.Fatalf("enrollment argv wrong: %v", src.streams)
		}
		if !strings.Contains(out.String(), "TPM enrollment recorded") {
			t.Fatalf("missing guidance: %s", out.String())
		}
	})
	t.Run("an unowned token is refused with a hint", func(t *testing.T) {
		src := fdeEnrollFixture(t)
		src.enrolled = true
		err := RunFDEEnroll(t.Context(), src, io.Discard, io.Discard, task)
		if err == nil || !strings.Contains(err.Error(), "does not own") || !strings.Contains(err.Error(), "--wipe-slot=1") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a non-active boot entry is refused", func(t *testing.T) {
		src := fdeEnrollFixture(t)
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" +
			"Boot0008* Fedora\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT,78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc,0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		err := RunFDEEnroll(t.Context(), src, io.Discard, io.Discard, task)
		if err == nil || !strings.Contains(err.Error(), "reboot into the Nimbus image") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestFDEEnrollEligibility(t *testing.T) {
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	base := func() *nativetest.FakeSource {
		src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
		src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		src.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
		src.Dirs["/sys/firmware/efi/efivars"] = []string{}
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009,0008\n" +
			"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
			"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
		return src
	}
	if err := fdeEnrollEligible(base()); err != nil {
		t.Fatalf("eligible setup was rejected: %v", err)
	}
	src := base()
	src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008,0009\n" +
		"Boot0008* Fedora\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n" +
		"Boot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
	if err := fdeEnrollEligible(src); err == nil || !strings.Contains(err.Error(), "reboot into the Nimbus image") {
		t.Fatalf("wrong boot entry was accepted: %v", err)
	}
	src = base()
	delete(src.Files, FDEUKIMarker)
	if err := fdeEnrollEligible(src); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("inactive setup was accepted: %v", err)
	}
}

func TestRunFDESetup(t *testing.T) {
	t.Run("fresh setup", func(t *testing.T) {
		src := fdeSetupFixture(t)
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err != nil {
			t.Fatal(err)
		}
		if src.staged == "" || src.built != "6.19.10-300.fc44.x86_64" || !src.entry {
			t.Fatalf("staged=%q built=%q entry=%v", src.staged, src.built, src.entry)
		}
		if !strings.Contains(out.String(), "Reboot when ready") {
			t.Fatalf("missing reboot guidance: %s", out.String())
		}
		if _, err := os.Stat(filepath.Dir(src.staged)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("staged payload remains")
		}
	})
	t.Run("repair keeps the marker", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Files[FDEUKIMarker] = []byte(fdeMarkerText)
		src.stale = true
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err != nil {
			t.Fatal(err)
		}
		if src.staged != "" {
			t.Fatal("an existing marker was rewritten")
		}
		if src.built == "" || !src.entry {
			t.Fatalf("repair did not rebuild: %q", src.streams)
		}
	})
	t.Run("foreign marker blocks", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Files[FDEUKIMarker] = []byte("foreign")
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err == nil || !strings.Contains(err.Error(), "unfamiliar content") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("insufficient space blocks before changes", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.Commands[nativetest.Key("df", "--output=avail", "-B1", "/boot/efi")] = []byte("Avail\n1000\n")
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err == nil || !strings.Contains(err.Error(), "bytes free") {
			t.Fatalf("got %v", err)
		}
		for _, stream := range src.streams {
			if strings.Contains(stream, "system-file") || strings.Contains(stream, "fde-uki") {
				t.Fatalf("mutated before the space check: %q", src.streams)
			}
		}
	})
	t.Run("secure setup requests MOK and creates the shim entry", func(t *testing.T) {
		src := fdeSetupFixture(t)
		fdeEnableSecureBoot(src.FakeSource, true)
		src.secure = true
		src.Commands[nativetest.Key("sudo", "-n", "--", "mokutil", "--ignore-keyring", "--test-key", fdeKeys().mokDER)] = []byte(fdeKeys().mokDER + " is not enrolled\n")
		src.Commands[nativetest.Key("mokutil", "--list-new")] = []byte("")
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Complete Enroll MOK") {
			t.Fatalf("missing MOK guidance:\n%s", out.String())
		}
		if !slices.ContainsFunc(src.streams, func(stream string) bool {
			return strings.Contains(stream, "mokutil --import")
		}) {
			t.Fatalf("no MOK import requested: %v", src.streams)
		}
		if src.built == "" || !src.entry {
			t.Fatalf("secure setup did not build and create the entry: %v", src.streams)
		}
	})
	t.Run("a failed build removes the new marker", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.buildFails = true
		var out bytes.Buffer
		err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask())
		if err == nil || !strings.Contains(err.Error(), "marker was removed") {
			t.Fatalf("got %v", err)
		}
		if src.removals != 1 {
			t.Fatalf("marker removal not recorded: %d", src.removals)
		}
		if _, exists := src.Files[FDEUKIMarker]; exists {
			t.Fatal("marker remains after the failed build")
		}
	})
	t.Run("an entry failure is reported", func(t *testing.T) {
		src := fdeSetupFixture(t)
		src.entryFails = true
		var out bytes.Buffer
		if err := RunFDESetup(t.Context(), src, &out, &out, fdeSetupTask()); err == nil || !strings.Contains(err.Error(), "create firmware entry") {
			t.Fatalf("got %v", err)
		}
	})
}

// fdeInspectSource replays the privileged ukify inspection.
type fdeInspectSource struct {
	*nativetest.FakeSource
	out []byte
	err error
}

func (s fdeInspectSource) Run(name string, args ...string) ([]byte, error) {
	if name == FDEUKITool {
		return s.out, s.err
	}
	return s.FakeSource.Run(name, args...)
}

func fdeVerifyFixture(t *testing.T) fdeInspectSource {
	t.Helper()
	const guid = "78f7ab0d-3bb7-4c71-ba1b-d8f8db372fdc"
	fdeTestKeys(t)
	base := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, ExitCodes: map[string]int{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	base.Files[FDEUKIMarker] = []byte(fdeMarkerText)
	base.Files[fdeCmdlineFile] = []byte(fdeTestBase + "\n")
	base.Files[fdeCrypttab] = []byte("luks-1 UUID=1 none discard,x-initrd.attach\n")
	base.Dirs["/sys/firmware/efi/efivars"] = []string{}
	base.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0009\nBootOrder: 0009,0008\nBoot0009* Nimbus UKI\tHD(1,GPT," + guid + ",0x800,0x200000)/\\EFI\\Linux\\nimbus.efi\n")
	base.Commands[nativetest.Key("stat", "--format=%s", "--", fdeKernelDir+"/"+fdeTestVersion+"/vmlinuz")] = []byte("18497536\n")
	return fdeInspectSource{FakeSource: base}
}

const fdeInspectOutput = `.sbat:
  size: 400 bytes
  sha256: aa
.pcrsig:
  size: 1 bytes
  sha256: aa
.pcrpkey:
  size: 1 bytes
  sha256: aa
.linux:
  size: 18497536 bytes
  sha256: bb
.osrel:
  size: 629 bytes
  sha256: cc
.cmdline:
  size: 137 bytes
  sha256: dd
  text:
    %s
.uname:
  size: 100 bytes
  sha256: ee
  text:
    6.19.10-300.fc44.x86_64
.initrd:
  size: 47000000 bytes
  sha256: ff
`

// fdeStubEnrollmentState registers an unenrolled root volume: no TPM token and
// no ownership record.
func fdeStubEnrollmentState(t *testing.T, src fdeInspectSource) {
	t.Helper()
	previous := fdeExecutable
	fdeExecutable = func() (string, error) { return "/usr/bin/nimbus", nil }
	t.Cleanup(func() { fdeExecutable = previous })
	src.Commands[nativetest.Key("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", "/dev/disk/by-uuid/1")] = []byte(`{"tokens":{}}`)
	src.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = []byte("")
}

// fdeStubEnrollment registers the native token metadata and the ownership
// record that a completed enrollment leaves behind.
func fdeStubEnrollment(t *testing.T, src *nativetest.FakeSource, slot string) {
	t.Helper()
	previous := fdeExecutable
	fdeExecutable = func() (string, error) { return "/usr/bin/nimbus", nil }
	t.Cleanup(func() { fdeExecutable = previous })
	src.Commands[nativetest.Key("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", "/dev/disk/by-uuid/1")] =
		[]byte(`{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["` + slot + `"]}}}`)
	record := FDEEnrollment{Schema: 1, Device: "/dev/disk/by-uuid/1", UUID: "1", Keyslot: slot, Token: "0",
		PCRs: "7+14+12+13+11", Fingerprint: "sha256:x"}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	src.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = data
}

func TestVerifyFDE(t *testing.T) {
	t.Run("verified image offers enrollment", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollmentState(t, src)
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || got.Action == nil || got.Action.Kind != EnrollFDE {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("changed PCR values offer renewal", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollment(t, src.FakeSource, "1")
		for _, pcr := range []string{"7", "12", "13", "14"} {
			src.Files["/sys/class/tpm/tpm0/pcr-sha256/"+pcr] = []byte("changed-" + pcr + "\n")
		}
		record := FDEEnrollment{Schema: 1, Device: "/dev/disk/by-uuid/1", UUID: "1", Keyslot: "1", Token: "0",
			PCRs: "7+14+12+13+11", Fingerprint: "sha256:x",
			PCRValues: map[string]string{"7": "recorded", "12": "recorded", "13": "recorded", "14": "recorded"}}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		src.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = data
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || got.Action == nil || got.Action.Kind != RenewFDE {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("matching PCR values complete", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollment(t, src.FakeSource, "1")
		values := map[string]string{}
		for _, pcr := range []string{"7", "12", "13", "14"} {
			values[pcr] = "same-" + pcr
			src.Files["/sys/class/tpm/tpm0/pcr-sha256/"+pcr] = []byte("same-" + pcr + "\n")
		}
		record := FDEEnrollment{Schema: 1, Device: "/dev/disk/by-uuid/1", UUID: "1", Keyslot: "1", Token: "0",
			PCRs: "7+14+12+13+11", Fingerprint: "sha256:x", PCRValues: values}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		src.Commands[nativetest.Key("sudo", "-n", "--", "/usr/bin/nimbus", "internal", "fde-uki", "state")] = data
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Complete {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("a stale embedded initramfs is rebuildable", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollment(t, src.FakeSource, "1")
		src.Commands[nativetest.Key("sudo", "-n", "--", "sha256sum", "/boot/initramfs-"+fdeTestVersion+".img")] =
			[]byte("001122  /boot/initramfs-" + fdeTestVersion + ".img\n")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || got.Action == nil || got.Action.Kind != SetupFDE || !strings.Contains(got.Detail, "older than /boot") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("recorded token completes", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollment(t, src.FakeSource, "1")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Complete {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("foreign token blocks", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		fdeStubEnrollment(t, src.FakeSource, "1")
		src.Commands[nativetest.Key("sudo", "-n", "--", "cryptsetup", "luksDump", "--dump-json-metadata", "/dev/disk/by-uuid/1")] =
			[]byte(`{"tokens":{"0":{"type":"systemd-tpm2","keyslots":["2"]}}}`)
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Blocked || !strings.Contains(got.Detail, "does not own") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("command line mismatch", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", "root=UUID=other ro", 1))
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !strings.Contains(got.Detail, "does not embed") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("an extended command line is not a match", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline+" init=/bin/sh", 1))
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !strings.Contains(got.Detail, "does not embed") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing section", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(".linux:\n  size: 1 bytes\n  sha256: aa\n")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !strings.Contains(got.Detail, "lacks the .initrd section") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("a damaged image offers repair", func(t *testing.T) {
		for name, output := range map[string]string{
			"mismatch":        strings.Replace(fdeInspectOutput, "%s", "root=UUID=other ro", 1),
			"missing section": ".linux:\n  size: 1 bytes\n  sha256: aa\n",
		} {
			t.Run(name, func(t *testing.T) {
				src := fdeVerifyFixture(t)
				src.out = []byte(output)
				got := VerifyFDE(src, fdeSetupTask())
				if got.Status != Pending || got.Action == nil || got.Action.Kind != SetupFDE {
					t.Fatalf("got %+v", got)
				}
			})
		}
	})
	t.Run("an uninstalled embedded kernel offers repair", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.out = []byte(strings.Replace(fdeInspectOutput, "%s", fdeTestCmdline, 1))
		delete(src.Commands, nativetest.Key("stat", "--format=%s", "--", fdeKernelDir+"/"+fdeTestVersion+"/vmlinuz"))
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || got.Action == nil || !strings.Contains(got.Detail, "no longer installed") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("administrator access unavailable", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.err = errors.New("sudo: a password is required")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status == Complete || !got.VerificationNeedsRoot {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("inactive setup", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		delete(src.Files, FDEUKIMarker)
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || !strings.Contains(got.Detail, "not active") {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("wrong entry", func(t *testing.T) {
		src := fdeVerifyFixture(t)
		src.Commands[nativetest.Key("efibootmgr")] = []byte("BootCurrent: 0008\nBootOrder: 0008\nBoot0008* Fedora\tHD(1,GPT,g,0x800,0x200000)/\\EFI\\fedora\\shimx64.efi\n")
		got := VerifyFDE(src, fdeSetupTask())
		if got.Status != Pending || !strings.Contains(got.Detail, "rerun the approved setup") {
			t.Fatalf("got %+v", got)
		}
	})
}

package postinstall

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func fixture(names ...string) (Inputs, *nativetest.FakeSource) {
	in := Inputs{
		Resolved: &definitions.Resolved{Machine: "test"},
		Applied:  state.Applied{Present: true, Receipts: map[string]state.Receipt{}},
		Facts:    inspect.Facts{User: inspect.Section[inspect.User]{Value: inspect.User{Name: "tester"}}},
	}
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Paths: map[string]string{}}
	for _, name := range names {
		prefix := "dnf"
		if name == "1password" {
			prefix = "onepassword"
		}
		pkg := definitions.ResolvedPackage{Canonical: prefix + ":" + name, Prefix: prefix, Name: name}
		in.Resolved.Packages = append(in.Resolved.Packages, pkg)
		in.Facts.Packages.Value = append(in.Facts.Packages.Value, inspect.Package{Name: name, Arch: "x86_64"})
		id := "package:" + pkg.Canonical
		in.Applied.Receipts[id] = receipt(id, "dnf")
		src.Paths[name] = "/usr/bin/" + name
	}
	return in, src
}

func receipt(id, provider string) state.Receipt {
	return state.Receipt{Resource: id, Provider: provider, Machine: "test", Schema: state.ReceiptSchema, Verified: true, Operation: "install", Timestamp: time.Unix(100, 0)}
}

func findTask(t *testing.T, tasks []Task, id string) Task {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("missing %s in %+v", id, tasks)
	return Task{}
}

func TestSelectionAndOnePasswordPrivacy(t *testing.T) {
	in, src := fixture("1password")
	got := findTask(t, Inspect(src, in), "onepassword")
	if got.Status != Unknown || got.Action == nil || !slices.Equal(got.Action.Argv, []string{"1password"}) {
		t.Fatalf("installed app is neither proven signed in nor unavailable: %+v", got)
	}
	// No command fixtures exist: sign-in inspection must not query accounts,
	// invoke op, read user configuration, or contact a service.
	guard := &readGuard{FakeSource: src}
	Inspect(guard, in)
	if len(guard.commands) != 0 || len(guard.files) != 0 {
		t.Fatalf("unexpected account inspection: %v, %v", guard.commands, guard.files)
	}
	in.Resolved.Packages = nil
	if got := Inspect(guard, in); len(got) != 0 {
		t.Fatalf("unselected installed app exposed a task: %+v", got)
	}
	in.Resolved = nil
	if got := Inspect(guard, in); got == nil || len(got) != 0 {
		t.Fatalf("expected a non-nil empty list: %+v", got)
	}
}

type readGuard struct {
	*nativetest.FakeSource
	commands []string
	files    []string
}

func (s *readGuard) Run(name string, args ...string) ([]byte, error) {
	s.commands = append(s.commands, nativetest.Key(name, args...))
	return s.FakeSource.Run(name, args...)
}

func (s *readGuard) ReadFile(path string) ([]byte, error) {
	s.files = append(s.files, path)
	return s.FakeSource.ReadFile(path)
}

func TestPackagePrerequisitesDoNotTrustOldReceipts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Inputs)
		want   Status
	}{
		{"absent", func(in *Inputs) { in.Facts.Packages.Value = nil }, Blocked},
		{"unknown", func(in *Inputs) { in.Facts.Packages.Error = "rpm unavailable" }, Unknown},
		{"unapplied", func(in *Inputs) { in.Applied.Present = false }, Blocked},
		{"unrecorded", func(in *Inputs) { clear(in.Applied.Receipts) }, Blocked},
		{"foreign", func(in *Inputs) {
			r := in.Applied.Receipts["package:onepassword:1password"]
			r.Machine = "other"
			in.Applied.Receipts[r.Resource] = r
		}, Blocked},
		{"native identity drift", func(in *Inputs) {
			r := in.Applied.Receipts["package:onepassword:1password"]
			r.Package = "other.x86_64"
			in.Applied.Receipts[r.Resource] = r
		}, Blocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture("1password")
			test.change(&in)
			got := findTask(t, Inspect(src, in), "onepassword")
			if got.Status != test.want || got.Action != nil {
				t.Fatalf("got %+v, want %s without action", got, test.want)
			}
		})
	}
}

func TestInstallerHelpersDoNotImplyApplicationCompletion(t *testing.T) {
	for _, name := range []string{"github-copilot-installer", "wowup-cf-installer"} {
		t.Run(name, func(t *testing.T) {
			in, src := fixture(name)
			helper := "/usr/bin/" + name
			src.Paths[helper] = helper
			guard := &readGuard{FakeSource: src}
			got := Inspect(guard, in)
			if len(got) != 1 || len(guard.commands) != 0 || len(guard.files) != 0 {
				t.Fatalf("unexpected helper inspection: tasks=%+v commands=%v files=%v", got, guard.commands, guard.files)
			}
			if name == "github-copilot-installer" {
				if got[0].Status != Unknown || got[0].Action == nil || !slices.Equal(got[0].Action.Argv, []string{"sudo", "--", helper, "install"}) {
					t.Fatalf("helper installation became application completion: %+v", got[0])
				}
			} else if got[0].Status != Blocked || got[0].Action != nil || !strings.Contains(got[0].Detail, "standalone install") {
				t.Fatalf("offered an unsupported WoWUp command: %+v", got[0])
			}
			delete(src.Paths, helper)
			got = Inspect(src, in)
			if got[0].Status != Blocked || got[0].Action != nil {
				t.Fatalf("missing helper can run: %+v", got[0])
			}
			in.Resolved.Packages = nil
			if got := Inspect(src, in); len(got) != 0 {
				t.Fatalf("unselected helper exposed a task: %+v", got)
			}
		})
	}
}

func TestMOKNativeEnrollmentStates(t *testing.T) {
	for _, test := range []struct {
		name, output, failure string
		want                  Status
	}{
		{"enrolled", mokCertificate + " is already enrolled", "", Complete},
		{"firmware trust", mokCertificate + " is already in db", "", Complete},
		{"request not completion", mokCertificate + " is already in the enrollment request", "", Pending},
		{"not enrolled", mokCertificate + " is not enrolled", "exit status 1", Pending},
		{"unexpected success", "", "", Unknown},
		{"native error", "", "cannot read EFI variables", Unknown},
		{"contradictory error", mokCertificate + " is already enrolled", "failed", Unknown},
		{"foreign certificate", "/tmp/other.der is already enrolled", "", Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture("akmod-nvidia", "akmods", "mokutil")
			in.Resolved.Components = []definitions.ResolvedComponent{{ID: "nvidia"}}
			in.Facts.SecureBoot.Value = inspect.SecureBootEnabled
			src.Files[mokCertificate] = []byte("certificate supplied to native validator")
			key := nativetest.Key("mokutil", "--test-key", mokCertificate)
			src.Commands[key] = []byte(test.output)
			if test.failure != "" {
				src.Failures[key] = test.failure
			}
			got := findTask(t, Inspect(src, in), "nvidia-mok")
			if got.Status != test.want || got.Action != nil {
				t.Fatalf("got %+v; want %s and instruction-only enrollment", got, test.want)
			}
		})
	}
}

func TestFingerprintReadOnlyObservation(t *testing.T) {
	for _, test := range []struct {
		name, devices, fingers string
		want                   Status
	}{
		{"inactive daemon", "", "", Unknown},
		{"no reader", `{"type":"ao","data":[[]]}`, "", NotApplicable},
		{"enrolled", `{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`, `{"type":"as","data":[["right-index-finger"]]}`, Complete},
		{"not enrolled", `{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`, `{"type":"as","data":[[]]}`, Pending},
		{"permission denied", `{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`, "", Unknown},
		{"malformed list", `{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`, `{"type":"as","data":[null]}`, Unknown},
		{"invalid finger", `{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`, `{"type":"as","data":[["not-a-finger"]]}`, Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture("fprintd")
			src.Paths["busctl"], src.Paths["fprintd-enroll"] = "/usr/bin/busctl", "/usr/bin/fprintd-enroll"
			base := []string{"--system", "--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call", fprintService}
			devices := nativetest.Key("busctl", append(slices.Clone(base), "/net/reactivated/Fprint/Manager", fprintService+".Manager", "GetDevices")...)
			fingers := nativetest.Key("busctl", append(slices.Clone(base), "/net/reactivated/Fprint/Device/0", fprintService+".Device", "ListEnrolledFingers", "s", "")...)
			if test.devices != "" {
				src.Commands[devices] = []byte(test.devices)
			}
			if test.fingers != "" {
				src.Commands[fingers] = []byte(test.fingers)
			}
			guard := &readGuard{FakeSource: src}
			got := findTask(t, Inspect(guard, in), "fingerprint")
			if got.Status != test.want || (got.Action != nil) != (test.want == Pending) {
				t.Fatalf("got %+v, want %s", got, test.want)
			}
			for _, command := range guard.commands {
				if !strings.HasPrefix(command, "busctl --system --auto-start=no --allow-interactive-authorization=no ") {
					t.Fatalf("unexpected mutating or interactive command %s", command)
				}
			}
		})
	}
}

func TestFingerprintNativeNoEnrolledPrintsError(t *testing.T) {
	for _, message := range []string{
		"Call failed: No fingerprints enrolled: exit status 1",
		"Call failed: Permission denied: exit status 1",
		"Call failed: No fingerprints enrolled: exit status 2",
		"Call failed: No fingerprints enrolled; authorization denied: exit status 1",
	} {
		t.Run(message, func(t *testing.T) {
			in, src := fixture("fprintd")
			src.Paths["busctl"], src.Paths["fprintd-enroll"] = "/usr/bin/busctl", "/usr/bin/fprintd-enroll"
			base := []string{"--system", "--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call", fprintService}
			devices := nativetest.Key("busctl", append(slices.Clone(base), "/net/reactivated/Fprint/Manager", fprintService+".Manager", "GetDevices")...)
			fingers := nativetest.Key("busctl", append(slices.Clone(base), "/net/reactivated/Fprint/Device/0", fprintService+".Device", "ListEnrolledFingers", "s", "")...)
			src.Commands[devices] = []byte(`{"type":"ao","data":[["/net/reactivated/Fprint/Device/0"]]}`)
			src.Failures[fingers] = fingers + ": " + message
			got := findTask(t, Inspect(src, in), "fingerprint")
			want := Unknown
			if message == "Call failed: No fingerprints enrolled: exit status 1" {
				want = Pending
			}
			if got.Status != want || (got.Action != nil) != (want == Pending) {
				t.Fatalf("got %+v; want %s", got, want)
			}
		})
	}
}

func TestRebootUsesCurrentBootAndOriginalChangeTime(t *testing.T) {
	for _, test := range []struct {
		name string
		boot string
		r    state.Receipt
		want Status
	}{
		{"before", "btime 50\n", state.Receipt{Operation: "install", Timestamp: time.Unix(100, 0)}, Pending},
		{"after", "btime 150\n", state.Receipt{Operation: "install", Timestamp: time.Unix(100, 0)}, Complete},
		{"original change", "btime 150\n", state.Receipt{Operation: "adopt", Timestamp: time.Unix(200, 0), ChangedAt: time.Unix(100, 0)}, Complete},
		{"legacy adoption unknown", "btime 150\n", state.Receipt{Operation: "adopt", Timestamp: time.Unix(100, 0)}, Unknown},
		{"missing boot", "", state.Receipt{Operation: "install", Timestamp: time.Unix(100, 0)}, Unknown},
		{"duplicate boot", "btime 150\nbtime 151\n", state.Receipt{Operation: "install", Timestamp: time.Unix(100, 0)}, Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture()
			r := receipt("target:default", "target")
			r.Reboot, r.Operation, r.Timestamp, r.ChangedAt = true, test.r.Operation, test.r.Timestamp, test.r.ChangedAt
			in.Applied.Receipts[r.Resource] = r
			src.Files["/proc/stat"] = []byte(test.boot)
			got := findTask(t, Inspect(src, in), "reboot")
			if got.Status != test.want || got.Action != nil {
				t.Fatalf("got %+v, want %s", got, test.want)
			}
		})
	}
}

func TestLogoutChecksCurrentGroupsInsteadOfReceiptAge(t *testing.T) {
	for _, test := range []struct {
		name, active, configured string
		want                     Status
	}{
		{"stale session", "tester wheel", "tester wheel docker", Pending},
		{"current session", "tester wheel docker", "tester wheel docker", Complete},
		{"configuration drift", "tester wheel docker", "tester wheel", Blocked},
		{"unknown", "", "tester wheel docker", Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture()
			r := receipt("group:docker:tester", "group")
			r.Logout, r.Intended = true, "true"
			in.Applied.Receipts[r.Resource] = r
			src.Commands["id -nG"] = []byte(test.active)
			src.Commands["id -nG -- tester"] = []byte(test.configured)
			got := findTask(t, Inspect(src, in), "logout")
			if got.Status != test.want || got.Action != nil {
				t.Fatalf("got %+v, want %s", got, test.want)
			}
		})
	}
}

func TestForeignOrFailedReceiptsDoNotCreateRequirements(t *testing.T) {
	in, src := fixture()
	for _, id := range []string{"target:foreign", "target:failed", "target:removed"} {
		r := receipt(id, "target")
		r.Reboot, r.Logout = true, true
		switch id {
		case "target:foreign":
			r.Machine = "other"
		case "target:failed":
			r.Verified = false
		case "target:removed":
			r.Operation = "remove"
		}
		in.Applied.Receipts[id] = r
	}
	if got := Inspect(src, in); len(got) != 0 {
		t.Fatalf("invalid receipts exposed tasks: %+v", got)
	}
}

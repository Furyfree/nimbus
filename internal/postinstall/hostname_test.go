package postinstall

import (
	"slices"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func hostnameFixture(t *testing.T, current string) (Inputs, *nativetest.FakeSource) {
	t.Helper()
	in, src := fixture()
	in.Resolved = &definitions.Resolved{Machine: "laptop"}
	src.Paths["hostnamectl"] = "/usr/bin/hostnamectl"
	src.Commands[nativetest.Key("/usr/bin/hostnamectl", "--static")] = []byte(current + "\n")
	return in, src
}

func TestHostnameTaskStates(t *testing.T) {
	if got := HostnameForMachine("desktop"); got != "nimbus-desktop" {
		t.Fatalf("policy derived %q", got)
	}
	t.Run("matching", func(t *testing.T) {
		in, src := hostnameFixture(t, "nimbus-laptop")
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Complete || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("differing", func(t *testing.T) {
		in, src := hostnameFixture(t, "fedora")
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Pending || got.Action == nil || got.Action.Kind != SetHostname {
			t.Fatalf("got %+v", got)
		}
		if got.Action.Hostname != "nimbus-laptop" {
			t.Fatalf("action carries %q", got.Action.Hostname)
		}
		commands, err := HostnameCommands(got)
		if err != nil || !slices.Equal(commands[0], []string{"sudo", "--", "/usr/bin/hostnamectl", "set-hostname", "nimbus-laptop"}) {
			t.Fatalf("commands=%v err=%v", commands, err)
		}
	})
	t.Run("invalid derived hostname", func(t *testing.T) {
		in, src := hostnameFixture(t, "fedora")
		in.Resolved.Machine = "laptop-"
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Blocked || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("hostnamectl missing", func(t *testing.T) {
		in, src := hostnameFixture(t, "fedora")
		delete(src.Paths, "hostnamectl")
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Blocked || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("readable only through hostnamectl", func(t *testing.T) {
		in, src := hostnameFixture(t, "fedora")
		guard := &readGuard{FakeSource: src}
		findTask(t, Inspect(guard, in), "hostname")
		if len(guard.commands) != 1 || guard.commands[0] != nativetest.Key("/usr/bin/hostnamectl", "--static") || len(guard.files) != 0 {
			t.Fatalf("unexpected reads: %v %v", guard.commands, guard.files)
		}
	})
	t.Run("read failure", func(t *testing.T) {
		in, src := hostnameFixture(t, "fedora")
		delete(src.Commands, nativetest.Key("/usr/bin/hostnamectl", "--static"))
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("unrecognized name", func(t *testing.T) {
		in, src := hostnameFixture(t, "NOT A HOST")
		got := findTask(t, Inspect(src, in), "hostname")
		if got.Status != Unknown || got.Action != nil {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestHostnameCommandsRejectForgedActions(t *testing.T) {
	valid := Task{ID: "hostname", Status: Pending, Action: HostnameAction("nimbus-laptop")}
	if _, err := HostnameCommands(valid); err != nil {
		t.Fatalf("valid action rejected: %v", err)
	}
	for _, task := range []Task{
		{ID: "hostname", Status: Pending, Action: &Action{Kind: SetHostname, Hostname: "nimbus-laptop", Argv: []string{"sudo", "--", "/usr/bin/hostnamectl", "set-hostname", "other"}}},
		{ID: "hostname", Status: Pending, Action: &Action{Kind: SetHostname, Hostname: "nimbus-laptop", Argv: []string{"sh", "-c", "hostnamectl set-hostname nimbus-laptop"}}},
		{ID: "hostname", Status: Pending, Action: &Action{Kind: SetHostname, Hostname: "bad_name", Argv: []string{"sudo", "--", "/usr/bin/hostnamectl", "set-hostname", "bad_name"}}},
		{ID: "hostname", Status: Complete, Action: HostnameAction("nimbus-laptop")},
		{ID: "other", Status: Pending, Action: HostnameAction("nimbus-laptop")},
	} {
		if _, err := HostnameCommands(task); err == nil {
			t.Fatalf("forged action accepted: %+v", task)
		}
	}
	if HostnameAction("localhost") != nil || HostnameAction("UPPER") != nil || HostnameAction("") != nil {
		t.Fatal("builder accepted an invalid hostname")
	}
}

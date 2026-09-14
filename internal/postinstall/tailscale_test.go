package postinstall

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestTailscaleOperatorReadinessAndPrivacy(t *testing.T) {
	for _, test := range []struct {
		name, prefs string
		status      Status
	}{
		{"unset", `{"WantRunning":false}`, Pending},
		{"empty", `{"WantRunning":true,"OperatorUser":""}`, Pending},
		{"other operator", `{"WantRunning":true,"OperatorUser":"other"}`, Pending},
		{"ready", `{"WantRunning":false,"OperatorUser":"tester","Config":{"private":"do-not-render"}}`, Complete},
		{"missing shape", `{}`, Unknown},
		{"null response", `null`, Unknown},
		{"bad type", `{"WantRunning":true,"OperatorUser":12}`, Unknown},
		{"null operator", `{"WantRunning":true,"OperatorUser":null}`, Unknown},
		{"malformed", `{"WantRunning":true`, Unknown},
		{"invalid name", `{"WantRunning":true,"OperatorUser":"other\nuser"}`, Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture("tailscale")
			src.Paths["/usr/bin/tailscale"] = "/usr/bin/tailscale"
			src.Commands["/usr/bin/tailscale debug prefs"] = []byte(test.prefs)
			src.Commands["/usr/bin/tailscale status --json --peers=false"] = []byte(`{"BackendState":"Stopped","Self":{"private":"do-not-render"}}`)
			guard := &readGuard{FakeSource: src}
			task := findTask(t, Inspect(guard, in), "tailscale-operator")
			if task.Status != test.status || (task.Action != nil) != (test.status == Pending) {
				t.Fatalf("unexpected readiness: %+v", task)
			}
			if task.Action != nil && (task.Action.User != "tester" || !slices.Equal(task.Action.Argv, []string{"sudo", "--", "/usr/bin/tailscale", "set", "--operator=tester"})) {
				t.Fatalf("wrong operator command: %+v", task.Action)
			}
			data, err := json.Marshal(task)
			if err != nil || strings.Contains(string(data), "do-not-render") || strings.Contains(string(data), "WantRunning") {
				t.Fatal("retained unrelated preferences")
			}
			wantReads := []string{"/usr/bin/tailscale debug prefs"}
			if test.status != Unknown {
				wantReads = append(wantReads, "/usr/bin/tailscale status --json --peers=false")
			}
			if !slices.Equal(guard.commands, wantReads) || len(guard.files) != 0 {
				t.Fatalf("unexpected reads: %v %v", guard.commands, guard.files)
			}
		})
	}
}

func TestTailscaleOperatorPrerequisites(t *testing.T) {
	for _, mode := range []string{"unselected", "missing package", "unknown packages", "missing receipt", "foreign receipt", "missing command", "root", "unknown user", "invalid user", "daemon unavailable"} {
		t.Run(mode, func(t *testing.T) {
			in, src := fixture("tailscale")
			src.Paths["/usr/bin/tailscale"] = "/usr/bin/tailscale"
			src.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":true}`)
			switch mode {
			case "unselected":
				in.Resolved.Packages = nil
			case "missing package":
				in.Facts.Packages.Value = nil
			case "unknown packages":
				in.Facts.Packages.Error = "unknown"
			case "missing receipt":
				clear(in.Applied.Receipts)
			case "foreign receipt":
				r := in.Applied.Receipts["package:dnf:tailscale"]
				r.Machine = "other"
				in.Applied.Receipts[r.Resource] = r
			case "missing command":
				delete(src.Paths, "/usr/bin/tailscale")
			case "root":
				in.Facts.User.Value.Name = "root"
			case "unknown user":
				in.Facts.User.Error = "unknown"
			case "invalid user":
				in.Facts.User.Value.Name = "--operator=root"
			case "daemon unavailable":
				src.Failures["/usr/bin/tailscale debug prefs"] = "local daemon unavailable"
			}
			guard := &readGuard{FakeSource: src}
			tasks := Inspect(guard, in)
			if mode == "unselected" {
				if len(tasks) != 0 || len(guard.commands) != 0 {
					t.Fatalf("inspected an unselected capability: %+v", tasks)
				}
				return
			}
			task := findTask(t, tasks, "tailscale-operator")
			if task.Action != nil || (task.Status != Unknown && task.Status != Blocked) {
				t.Fatalf("offered an unverified permission change: %+v", task)
			}
			if mode != "daemon unavailable" && len(guard.commands) != 0 {
				t.Fatalf("inspected preferences without prerequisites: %v", guard.commands)
			}
		})
	}
}

func TestTailscaleOperatorActionRejectsAmbiguousUser(t *testing.T) {
	for _, user := range []string{"", "root", "--help", "$(id)", "test user", "test\nuser"} {
		if action := TailscaleOperatorAction(user); action != nil {
			t.Fatalf("accepted %q: %+v", user, action)
		}
		if action := TailscaleLoginAction(user); action != nil {
			t.Fatalf("accepted login for %q: %+v", user, action)
		}
	}
}

func TestTailscaleSignInReadiness(t *testing.T) {
	for _, test := range []struct {
		name, response string
		status         Status
		login          bool
	}{
		{"login even with matching operator", `{"BackendState":"NeedsLogin"}`, Pending, true},
		{"connected", `{"BackendState":"Running"}`, Complete, false},
		{"intentionally stopped", `{"BackendState":"Stopped"}`, Complete, false},
		{"device approval", `{"BackendState":"NeedsMachineAuth"}`, Blocked, false},
		{"starting", `{"BackendState":"Starting"}`, Unknown, false},
		{"no state", `{"BackendState":"NoState"}`, Unknown, false},
		{"in use", `{"BackendState":"InUse"}`, Unknown, false},
		{"future state", `{"BackendState":"future-private-value"}`, Unknown, false},
		{"missing state", `{}`, Unknown, false},
		{"null", `null`, Unknown, false},
		{"bad type", `{"BackendState":42}`, Unknown, false},
		{"malformed", `{`, Unknown, false},
		{"daemon failure", ``, Unknown, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, src := fixture("tailscale")
			src.Paths["/usr/bin/tailscale"] = "/usr/bin/tailscale"
			src.Commands["/usr/bin/tailscale debug prefs"] = []byte(`{"WantRunning":false,"OperatorUser":"tester"}`)
			src.Commands["/usr/bin/tailscale status --json --peers=false"] = []byte(test.response)
			if test.response == "" {
				src.Failures["/usr/bin/tailscale status --json --peers=false"] = "private daemon failure"
			}
			task := findTask(t, Inspect(src, in), "tailscale-operator")
			if task.Status != test.status || (task.Action != nil) != test.login {
				t.Fatalf("unexpected task: %+v", task)
			}
			if test.login && (task.Action.Kind != LoginTailscale || !slices.Equal(task.Action.Argv, []string{"sudo", "--", "/usr/bin/tailscale", "up", "--operator=tester"})) {
				t.Fatalf("unexpected login action: %+v", task.Action)
			}
			data, _ := json.Marshal(task)
			if strings.Contains(string(data), "private") {
				t.Fatalf("leaked native data: %s", data)
			}
			if err := VerifyTailscaleConnection(src); (err == nil) != (test.name == "connected") {
				t.Fatalf("connection verification: %v", err)
			}
		})
	}
}

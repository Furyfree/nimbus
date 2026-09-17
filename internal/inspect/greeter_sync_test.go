package inspect

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func greeterSource() *nativetest.FakeSource {
	return &nativetest.FakeSource{Commands: map[string][]byte{
		"getent --service=files passwd owner":             []byte("owner:x:1000:1000::/home/owner:/bin/bash\n"),
		"rpm -q --queryformat %{VERSION} noctalia":        []byte("5.1.0"),
		GreeterHelper + " --supports secure-sync-v1":      []byte("secure-sync-v1\n"),
		GreeterBinary + " passwordless-sync status owner": []byte("owner: enabled by the noctalia-greeter managed rule\nOther administrator-authored Polkit rules are not included in this status.\n"),
	}, Files: map[string][]byte{GreeterPolicy: []byte(`<policyconfig><action id="org.noctalia.greeter.sync-appearance"><annotate key="org.freedesktop.policykit.exec.path">/usr/bin/noctalia-greeter-apply-appearance</annotate><annotate key="org.freedesktop.policykit.exec.argv1">--sync</annotate></action></policyconfig>`)}, Failures: map[string]string{}}
}

func TestGreeterCompatibilityRejectsLegacyOrUnconstrainedPolicy(t *testing.T) {
	for _, scenario := range []string{"supported", "old shell", "prerelease", "unknown shell", "old helper", "missing policy", "legacy action", "wrong argv", "wrong helper", "duplicate action", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			src := greeterSource()
			switch scenario {
			case "old shell":
				src.Commands["rpm -q --queryformat %{VERSION} noctalia"] = []byte("5.0.1")
			case "prerelease":
				src.Commands["rpm -q --queryformat %{VERSION} noctalia"] = []byte("5.1.0-rc1")
			case "unknown shell":
				delete(src.Commands, "rpm -q --queryformat %{VERSION} noctalia")
			case "old helper":
				src.Commands[GreeterHelper+" --supports secure-sync-v1"] = []byte("unsupported")
			case "missing policy":
				delete(src.Files, GreeterPolicy)
			case "legacy action":
				src.Files[GreeterPolicy] = []byte(strings.ReplaceAll(string(src.Files[GreeterPolicy]), "sync-appearance", "apply-appearance"))
			case "wrong argv":
				src.Files[GreeterPolicy] = []byte(strings.ReplaceAll(string(src.Files[GreeterPolicy]), "--sync", "--legacy"))
			case "wrong helper":
				src.Files[GreeterPolicy] = []byte(strings.ReplaceAll(string(src.Files[GreeterPolicy]), GreeterHelper, "/tmp/helper"))
			case "duplicate action":
				src.Files[GreeterPolicy] = []byte(strings.ReplaceAll(string(src.Files[GreeterPolicy]), "</policyconfig>", `<action id="org.noctalia.greeter.sync-appearance"/></policyconfig>`))
			case "malformed":
				src.Files[GreeterPolicy] = []byte("<")
			}
			if err := CheckGreeterSync(src); (err == nil) != (scenario == "supported") {
				t.Fatalf("compatibility: %v", err)
			}
		})
	}
}

func TestGreeterStatusNeverConfusesPermissionWithCompletion(t *testing.T) {
	for _, scenario := range []string{"enabled", "disabled", "permission", "foreign rule", "unexpected output", "wrong user", "root", "privileged failure"} {
		t.Run(scenario, func(t *testing.T) {
			src := greeterSource()
			key := GreeterBinary + " passwordless-sync status owner"
			user, privileged := "owner", false
			switch scenario {
			case "disabled":
				src.Commands[key] = []byte(strings.Replace(string(src.Commands[key]), ": enabled", ": not enabled", 1))
			case "permission":
				src.Failures[key] = "failed to open the Polkit rules directory: Permission denied"
			case "foreign rule":
				src.Failures[key] = "unrecognized managed rule; rerun status with sudo"
			case "unexpected output":
				src.Commands[key] = []byte("enabled")
			case "wrong user":
				src.Commands[key] = []byte(strings.ReplaceAll(string(src.Commands[key]), "owner:", "other:"))
			case "root":
				user = "root"
			case "privileged failure":
				privileged = true
			}
			have, err := ObserveGreeterSync(src, user, privileged)
			valid := scenario == "enabled" || scenario == "disabled" || scenario == "permission"
			if (err == nil) != valid {
				t.Fatalf("state=%+v err=%v", have, err)
			}
			if valid && (have.UID != 1000 || have.Known != (scenario != "permission") || have.Enabled != (scenario == "enabled")) {
				t.Fatalf("state=%+v", have)
			}
		})
	}
}

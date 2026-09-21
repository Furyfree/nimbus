package definitions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWiFiRetryOnlyWhenDisconnected(t *testing.T) {
	unit, err := os.ReadFile("../../system/root/etc/systemd/system/nimbus-wifi-retry.service")
	if err != nil {
		t.Fatal(err)
	}
	var script string
	for line := range strings.Lines(string(unit)) {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=/bin/bash -e -o pipefail -c '"); ok {
			script = strings.ReplaceAll(strings.TrimSuffix(s, "'"), "$$", "$")
		}
	}
	if script == "" {
		t.Fatal("missing retry command")
	}
	for _, tt := range []struct {
		name, devices, want    string
		queryFail, connectFail bool
	}{
		{name: "connected", devices: "wlan0:wifi:connected\neth0:ethernet:disconnected\n"},
		{name: "retry", devices: "wlan0:wifi:disconnected\n", want: "--wait 30 device connect wlan0\n"},
		{name: "handshake", devices: "wlan0:wifi:connecting (need authentication)\n", want: "--wait 30 device connect wlan0\n"},
		{name: "radio off", devices: "wlan0:wifi:unavailable\n"},
		{name: "query failed", queryFail: true},
		{name: "retry failed", devices: "wlan0:wifi:disconnected\n", want: "--wait 30 device connect wlan0\n", connectFail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			log := filepath.Join(bin, "calls")
			mock := "#!/bin/sh\nif [ \"$1\" = -t ]; then\n[ \"$QUERY_FAIL\" = 1 ] && exit 1\nprintf '%s' \"$DEVICES\"\nelse\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\n[ \"$CONNECT_FAIL\" = 1 ] && exit 1\nfi\nexit 0\n"
			if err := os.WriteFile(filepath.Join(bin, "nmcli"), []byte(mock), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "/bin/bash", "-e", "-o", "pipefail", "-c", script)
			cmd.Env = []string{"PATH=" + bin, "DEVICES=" + tt.devices, "CALLS=" + log}
			if tt.queryFail {
				cmd.Env = append(cmd.Env, "QUERY_FAIL=1")
			}
			if tt.connectFail {
				cmd.Env = append(cmd.Env, "CONNECT_FAIL=1")
			}
			out, err := cmd.CombinedOutput()
			if (err != nil) != (tt.queryFail || tt.connectFail) {
				t.Fatal(err, string(out))
			}
			calls, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(calls) != tt.want {
				t.Fatalf("calls %q, want %q", calls, tt.want)
			}
		})
	}
}

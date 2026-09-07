package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual bootstrap with native GPG and fake package tools. Every
// path and environment is isolated; sudo never invokes a privileged command.
func TestBootstrapCOPR(t *testing.T) {
	for _, tc := range []struct {
		name, failure, want string
		mutates             bool
	}{
		{"install", "", "init --checkout", true},
		{"existing engine", "existing", "init --checkout", false},
		{"GnuPG refusal", "gpg-refused", "GnuPG installation declined", false},
		{"GnuPG EOF", "gpg-eof", "GnuPG installation declined", false},
		{"missing key", "missing-key", "public key is not available", false},
		{"wrong key", "wrong-key", "fingerprint mismatch", false},
		{"malformed pin", "bad-pin", "invalid COPR signing-key fingerprint", false},
		{"foreign repo", "foreign-repo", "existing ", false},
		{"symlink repo", "symlink-repo", "refusing symlink", false},
		{"download failure", "download", "download failed", false},
		{"unsigned RPM", "signature", "signature verification failed", false},
		{"wrong package", "identity", "unexpected RPM identity", false},
		{"DNF failure", "install", "installation failed", true},
		{"shadowed engine", "shadow", "shadows", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			for _, path := range []string{bin, filepath.Join(root, "system/keys"), filepath.Join(root, "etc"), filepath.Join(root, "tmp")} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"bash", "dirname", "cat", "cp", "rm", "mkdir", "mktemp", "awk", "gpg", "cmp", "chmod", "install"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Skipf("bootstrap test requires %s", name)
				}
				if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			key, err := os.ReadFile(filepath.Join(repoRoot(t), "system/keys/brave.asc"))
			if err != nil {
				t.Fatal(err)
			}
			write("system/keys/nimbus.asc", string(key), 0600)
			write("system/keys/nimbus.fingerprint", "DBF1A116C220B8C7164F98230686B78420038257\n", 0600)
			write("nimbus.toml", "fixture", 0600)
			write("os-release", "ID=fedora\nVERSION_ID=44\n", 0600)
			write("engine", "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> \"$TRACE\"\nprintf '%s\\n' \"$*\"\n", 0700)
			write("preserve", "unrelated file", 0600)
			write("bin/id", "#!/usr/bin/env bash\necho 1000\n", 0700)
			write("bin/stat", "#!/usr/bin/env bash\necho 0\n", 0700)
			write("bin/uname", "#!/usr/bin/env bash\necho x86_64\n", 0700)
			write("bin/dnf5", `#!/usr/bin/env bash
printf 'download %s\n' "$*" >> "$TRACE"
if [ "$FAILURE" = download ]; then echo 'download failed' >&2; exit 1; fi
for arg in "$@"; do
  case "$arg" in --destdir=*) printf fixture > "${arg#--destdir=}/nimbus.rpm";; esac
done
`, 0700)
			write("bin/rpmkeys", `#!/usr/bin/env bash
printf 'rpmkeys %s\n' "$*" >> "$TRACE"
if [[ "$*" == *--checksig* && "$FAILURE" == signature ]]; then exit 1; fi
`, 0700)
			write("bin/rpm", `#!/usr/bin/env bash
if [ "$1" = -qf ]; then echo nimbus
elif [ "$FAILURE" = identity ]; then echo 'other x86_64'
else echo 'nimbus x86_64'; fi
`, 0700)
			write("bin/sudo", `#!/usr/bin/env bash
printf 'sudo %s\n' "$*" >> "$TRACE"
case "$1" in
  install) shift; exec install "$@" ;;
  rpmkeys) exit 0 ;;
  dnf5)
    if [ "$FAILURE" = install ]; then echo 'installation failed' >&2; exit 1; fi
    cp "$ROOT/engine" "$ROOT/bin/nimbus"
    chmod 700 "$ROOT/bin/nimbus" ;;
  *) exit 99 ;;
esac
`, 0700)
			data, err := os.ReadFile(filepath.Join(repoRoot(t), "bootstrap"))
			if err != nil {
				t.Fatal(err)
			}
			text := strings.NewReplacer(
				"/etc/os-release", filepath.Join(root, "os-release"),
				"ENGINE=/usr/bin/nimbus", "ENGINE="+filepath.Join(bin, "nimbus"),
				"/etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus", filepath.Join(root, "etc/key"),
				"/etc/yum.repos.d/nimbus-engine.repo", filepath.Join(root, "etc/repo"),
			).Replace(string(data))
			write("bootstrap", text, 0700)
			switch tc.failure {
			case "gpg-refused", "gpg-eof":
				if err := os.Remove(filepath.Join(bin, "gpg")); err != nil {
					t.Fatal(err)
				}
			case "existing":
				engine, err := os.ReadFile(filepath.Join(root, "engine"))
				if err != nil {
					t.Fatal(err)
				}
				write("bin/nimbus", string(engine), 0700)
			case "missing-key":
				if err := os.Remove(filepath.Join(root, "system/keys/nimbus.asc")); err != nil {
					t.Fatal(err)
				}
			case "wrong-key":
				write("system/keys/nimbus.fingerprint", strings.Repeat("A", 40), 0600)
			case "bad-pin":
				write("system/keys/nimbus.fingerprint", "not a fingerprint", 0600)
			case "foreign-repo":
				write("etc/repo", "foreign repository", 0600)
			case "symlink-repo":
				if err := os.Symlink(filepath.Join(root, "engine"), filepath.Join(root, "etc/repo")); err != nil {
					t.Fatal(err)
				}
			case "shadow":
				write("bin/nimbus", "#!/usr/bin/env bash\nexit 99\n", 0700)
				write("bootstrap", strings.ReplaceAll(text, "ENGINE="+filepath.Join(bin, "nimbus"), "ENGINE="+filepath.Join(root, "other-nimbus")), 0700)
			}
			cmd := exec.Command(filepath.Join(bin, "bash"), filepath.Join(root, "bootstrap"), "--machine", "vm")
			cmd.Env = []string{"PATH=" + bin, "HOME=" + root, "TMPDIR=" + filepath.Join(root, "tmp"), "ROOT=" + root, "TRACE=" + filepath.Join(root, "trace"), "FAILURE=" + tc.failure, "LC_ALL=C", "work=" + filepath.Join(root, "preserve")}
			if tc.failure == "gpg-refused" {
				cmd.Stdin = strings.NewReader("n\n")
			}
			output, err := cmd.CombinedOutput()
			success := tc.failure == "" || tc.failure == "existing"
			if (err == nil) != success || !strings.Contains(string(output), tc.want) {
				t.Fatalf("bootstrap: %v\n%s", err, output)
			}
			trace, _ := os.ReadFile(filepath.Join(root, "trace"))
			if strings.Contains(string(trace), "sudo ") != tc.mutates {
				t.Fatalf("unexpected mutations: %s", trace)
			}
			if tc.mutates {
				verification := strings.Index(string(trace), "--checksig")
				mutation := strings.Index(string(trace), "sudo ")
				if verification < 0 || verification > mutation || !strings.Contains(string(trace), "_pkgverify_level all") {
					t.Fatalf("mutation preceded required signature verification: %s", trace)
				}
			}
			if !success && strings.Contains(string(trace), "init --checkout") {
				t.Fatal("init ran after bootstrap failed")
			}
			entries, err := os.ReadDir(filepath.Join(root, "tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary download/keyring not cleaned: %v %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(root, "preserve")); err != nil {
				t.Fatalf("cleanup used an inherited environment path: %v", err)
			}
		})
	}
}

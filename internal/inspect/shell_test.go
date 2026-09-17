package inspect

import (
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestObserveLoginShellRequiresOneLocalNonRootAccount(t *testing.T) {
	for _, tc := range []struct {
		name, user, record string
		valid              bool
	}{
		{"local", "owner", "owner:x:1000:1000::/home/owner:/bin/bash\n", true},
		{"different user", "owner", "other:x:1000:1000::/home/owner:/bin/bash\n", false},
		{"root alias", "owner", "owner:x:0:1000::/home/owner:/bin/bash\n", false},
		{"missing", "owner", "", false},
		{"multiple", "owner", "owner:x:1000:1000::/home/owner:/bin/bash\nother:x:1001:1001::/home/other:/bin/bash\n", false},
		{"relative shell", "owner", "owner:x:1000:1000::/home/owner:bash\n", false},
		{"invalid UID", "owner", "owner:x:abc:1000::/home/owner:/bin/bash\n", false},
		{"root", "root", "", false},
		{"option", "--help", "", false},
		{"unnamed", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &nativetest.FakeSource{Commands: map[string][]byte{
				nativetest.Key("getent", "--service=files", "passwd", tc.user): []byte(tc.record),
			}}
			have, err := ObserveLoginShell(src, tc.user)
			if (err == nil) != tc.valid {
				t.Fatalf("state=%+v err=%v", have, err)
			}
			if tc.valid && have != (LoginShell{UID: 1000, Shell: "/bin/bash"}) {
				t.Fatalf("wrong local account state: %+v", have)
			}
		})
	}
}

func TestLoginShellMustBeAllowedAndExecutable(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/usr/bin/bash", "/bin/zsh", "/usr/bin/zsh"} {
		src := &nativetest.FakeSource{Files: map[string][]byte{"/etc/shells": []byte("# Login shells\n" + shell + "\n")},
			Commands: map[string][]byte{nativetest.Key("test", "-x", shell): nil}}
		if err := CheckLoginShell(src, shell); err != nil {
			t.Fatal(err)
		}
		delete(src.Commands, nativetest.Key("test", "-x", shell))
		if err := CheckLoginShell(src, shell); err == nil {
			t.Fatal("non-executable shell was accepted")
		}
		src.Files["/etc/shells"] = []byte("# " + shell + "\n")
		if err := CheckLoginShell(src, shell); err == nil {
			t.Fatal("commented-out shell was accepted")
		}
	}
	if err := CheckLoginShell(&nativetest.FakeSource{}, "/tmp/zsh"); err == nil {
		t.Fatal("arbitrary executable was accepted")
	}
}

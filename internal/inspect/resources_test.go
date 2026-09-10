package inspect

import (
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func TestObserveFileRefusesSymlinkAndUnknownParent(t *testing.T) {
	src := &nativetest.FakeSource{Dirs: map[string][]string{"/": {"etc"}, "/etc": {"nimbus.conf"}}, Commands: map[string][]byte{nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc"): []byte("symbolic link|root|root|777|1")}}
	if _, err := ObserveFile(src, "/etc/nimbus.conf"); err == nil {
		t.Fatal("ancestor symlink accepted")
	}
	clear(src.Dirs)
	if _, err := ObserveFile(src, "/etc/nimbus.conf"); err == nil {
		t.Fatal("unknown parent treated as missing target")
	}
}
func TestObserveFileDistinguishesVerifiedAbsence(t *testing.T) {
	src := &nativetest.FakeSource{Dirs: map[string][]string{"/": {"etc"}, "/etc": {}}, Commands: map[string][]byte{nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc"): []byte("directory|root|root|755|1")}}
	result, err := ObserveFile(src, "/etc/nimbus.conf")
	if err != nil || result.Exists {
		t.Fatalf("absence: %+v %v", result, err)
	}
}

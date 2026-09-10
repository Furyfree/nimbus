package inspect

import (
	"testing"
)

func TestNativePackageRequestsPreserveDotsAndArchitectures(t *testing.T) {
	pkgs := []Package{{Name: "driver-libs", Arch: "i686"}, {Name: "driver-libs", Arch: "x86_64"}, {Name: "python3.14", Arch: "x86_64"}}
	for _, tt := range []struct{ request, want string }{{"driver-libs", "driver-libs.x86_64"}, {"driver-libs.i686", "driver-libs.i686"}, {"python3.14", "python3.14.x86_64"}} {
		got, ok := FindPackage(pkgs, tt.request)
		if !ok || got.ID() != tt.want {
			t.Fatalf("%s: %+v, %v", tt.request, got, ok)
		}
	}
	if _, ok := FindPackage(pkgs, "driver-libs.noarch"); ok {
		t.Fatal("another architecture satisfied the request")
	}
}

package apply

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
)

func TestKeysAreReadFromRPM2ArchiveOutput(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"./etc/pki/rpm-gpg/RPM-GPG-KEY-rpmfusion-free-fedora-44": "KEY 44",
		"./etc/pki/rpm-gpg/RPM-GPG-KEY-rpmfusion-free-fedora-45": "KEY 45",
		"./etc/yum.repos.d/rpmfusion-free.repo":                  "[rpmfusion-free]",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	// rpm2archive writes the archive to standard output when that is a
	// pipe, which is how Nimbus runs it; nothing appears beside the RPM.
	src := &facts.FakeSource{Commands: map[string][]byte{
		facts.Key("rpm2archive", "/stage/rpmfusion-free-release.rpm"): buf.Bytes(),
	}}
	keys, err := ExtractKeysWithRPM2Archive(src)("/stage/rpmfusion-free-release.rpm")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || string(keys["etc/pki/rpm-gpg/RPM-GPG-KEY-rpmfusion-free-fedora-44"]) != "KEY 44" {
		t.Fatalf("keys = %v", keys)
	}
	src.Commands[facts.Key("rpm2archive", "/stage/broken.rpm")] = []byte("not an archive")
	if _, err := ExtractKeysWithRPM2Archive(src)("/stage/broken.rpm"); err == nil {
		t.Fatal("garbage output was accepted as an archive")
	}
}

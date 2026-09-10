package apply

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// ExtractKeysWithRPM2Archive is the real key extractor. rpm2archive writes
// the gzip tar to standard output when that is a pipe, which Source.Run
// captures; the key files live below ./etc/pki/rpm-gpg/ inside it.
func ExtractKeysWithRPM2Archive(src native.Source) func(string) (map[string][]byte, error) {
	return func(rpmPath string) (map[string][]byte, error) {
		archive, err := src.Run("rpm2archive", rpmPath)
		if err != nil {
			return nil, err
		}
		return readKeysFromArchive(archive)
	}
}

// readKeysFromArchive returns the key files below etc/pki/rpm-gpg/ inside a
// gzip tar written by rpm2archive.
func readKeysFromArchive(archive []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	keys := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(name, "etc/pki/rpm-gpg/") {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 1<<20+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("%s is larger than a key can be", name)
		}
		keys[name] = data
	}
	return keys, nil
}

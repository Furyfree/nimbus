package apply

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"strings"
)

// readKeysFromArchive returns the key files below etc/pki/rpm-gpg/ inside a
// gzip tar written by rpm2archive.
func readKeysFromArchive(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	keys := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(name, "etc/pki/rpm-gpg/") {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, err
		}
		keys[name] = data
	}
	return keys, nil
}

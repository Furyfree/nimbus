package definitions

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
)

// DigestVersion frames the digest input so a later format cannot collide.
const DigestVersion = "nimbus-definitions-v1"

// Entry is one regular file inside the definition boundary.
type Entry struct {
	Path    string // slash-separated, checkout-relative
	Mode    uint32 // 0100644 or 0100755
	Content []byte
}

// Digest returns the canonical SHA-256 of the definition entries. Entries are
// sorted bytewise by path; each is framed as u64 path length, path, u32 mode,
// u64 content length, content.
func Digest(entries []Entry) string {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return cmp.Compare(a.Path, b.Path) })

	h := sha256.New()
	h.Write([]byte(DigestVersion))
	h.Write([]byte{0})
	var buf [8]byte
	for _, e := range sorted {
		binary.BigEndian.PutUint64(buf[:], uint64(len(e.Path)))
		h.Write(buf[:])
		h.Write([]byte(e.Path))
		binary.BigEndian.PutUint32(buf[:4], e.Mode)
		h.Write(buf[:4])
		binary.BigEndian.PutUint64(buf[:], uint64(len(e.Content)))
		h.Write(buf[:])
		h.Write(e.Content)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

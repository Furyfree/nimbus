package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/state"
)

// Tests supply an isolated system tree and receipt owner.
var filesSystemRoot = "/"
var filesReceiptUID uint32

type fileAcceptance struct {
	Target      string `json:"target"`
	Source      string `json:"source"`
	Diff        string `json:"diff"`
	Definitions string `json:"definitions"`
	Changed     bool   `json:"changed"`
}

type acceptInput struct {
	selected               *selected
	decl                   definitions.ResolvedFile
	source, target         []byte
	sourceInfo, targetInfo os.FileInfo
	receipt                state.Receipt
	checkoutIdentity       string
	result                 fileAcceptance
}

func newFiles(opts *options) *cobra.Command {
	group := &cobra.Command{Use: "files", Short: "Inspect and accept managed system-file content", Args: noArgs}
	var flags machineFlags
	var yes, preview bool
	cmd := &cobra.Command{
		Use: "accept /etc/PATH", Short: "Review a live file change and capture it into its checkout source",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			before, err := prepareAcceptance(flags, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !opts.json {
				if _, err := fmt.Fprintf(out, "Capture %s into %s\nSystem-file sources must not contain secrets.\n%s", before.result.Target, before.result.Source, before.result.Diff); err != nil {
					return fmt.Errorf("write file acceptance preview: %w", err)
				}
			}
			if !bytes.Equal(before.source, before.target) && !preview {
				if opts.json && !yes {
					return usageError{errors.New("files accept --json requires --plan or explicit --yes")}
				}
				if !yes && !approver(cmd.InOrStdin(), out, "file acceptance") {
					return errors.New("file acceptance not approved; source unchanged")
				}
				lockPath, err := apply.LockPath()
				if err != nil {
					return err
				}
				lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "files accept", Operation: before.result.Definitions, PID: os.Getpid(), Started: time.Now().UTC()})
				if err != nil {
					return err
				}
				defer lock.Release()
				fresh, err := prepareAcceptance(flags, args[0])
				if err != nil {
					return err
				}
				if !sameAcceptance(before, fresh) {
					return errors.New("file acceptance inputs changed after approval; review again")
				}
				if err := replaceAcceptedSource(before); err != nil {
					return err
				}
				before.result.Changed = true
			}
			if opts.json {
				return writeJSON(out, before.result, nil)
			}
			if before.result.Changed {
				_, err = fmt.Fprintln(out, "Source updated. Run validate, then sync --plan and sync to verify the new definition.")
			} else if preview {
				_, err = fmt.Fprintln(out, "Preview only; source unchanged.")
			} else {
				_, err = fmt.Fprintln(out, "Source already matches the live file.")
			}
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the displayed reverse capture")
	cmd.Flags().BoolVarP(&preview, "plan", "p", false, "show the reverse diff without changing the source")
	group.AddCommand(cmd)
	return group
}

func prepareAcceptance(flags machineFlags, target string) (*acceptInput, error) {
	if !strings.HasPrefix(target, "/etc/") || filepath.Clean(target) != target {
		return nil, errors.New("accept requires one normalized absolute /etc file target")
	}
	s, err := loadSelected(flags)
	if err != nil {
		return nil, err
	}
	identity, err := acceptanceCheckoutIdentity(s.Root)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(s.Resolved.Files, func(file definitions.ResolvedFile) bool {
		return file.Target == target
	})
	if i < 0 || !strings.HasPrefix(s.Resolved.Files[i].Source, "system/root/etc/") {
		return nil, errors.New("target is not a selected generic system file")
	}
	decl := &s.Resolved.Files[i]
	receiptData, ri, err := readAcceptFile(stateRoot, filepath.Join(state.ReceiptsDir, state.FileName("file:"+target)))
	if err != nil {
		return nil, fmt.Errorf("ownership receipt: %w", err)
	}
	rstat, ok := ri.Sys().(*syscall.Stat_t)
	if !ok || rstat.Uid != filesReceiptUID || ri.Mode().Perm()&0022 != 0 {
		return nil, errors.New("ownership receipt must be trusted and not writable by group or others")
	}
	var receipt state.Receipt
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		return nil, fmt.Errorf("ownership receipt: %w", err)
	}
	if (receipt.Schema != state.ReceiptSchema && receipt.Schema != 1) || !receipt.Verified || receipt.Provider != "system-file" || receipt.Resource != "file:"+target || receipt.Machine != s.Resolved.Machine || receipt.PlanDigest == "" {
		return nil, errors.New("no successful matching Nimbus ownership receipt for this file")
	}
	source, si, err := readAcceptFile(s.Root, decl.Source)
	if err != nil {
		return nil, err
	}
	live, ti, err := readAcceptFile(filesSystemRoot, strings.TrimPrefix(target, "/"))
	if err != nil {
		return nil, fmt.Errorf("live target: %w", err)
	}
	owner, err := user.Lookup(decl.Owner)
	if err != nil {
		return nil, fmt.Errorf("declared owner: %w", err)
	}
	group, err := user.LookupGroup(decl.Group)
	if err != nil {
		return nil, fmt.Errorf("declared group: %w", err)
	}
	st := ti.Sys().(*syscall.Stat_t)
	mode, err := strconv.ParseUint(decl.Mode, 8, 32)
	if err != nil || owner.Uid != fmt.Sprint(st.Uid) || group.Gid != fmt.Sprint(st.Gid) || uint64(ti.Mode().Perm()) != mode {
		return nil, errors.New("live owner, group, or mode differs from the declared metadata; content acceptance cannot capture metadata drift")
	}
	if !utf8.Valid(source) || !utf8.Valid(live) || bytes.IndexByte(source, 0) >= 0 || bytes.IndexByte(live, 0) >= 0 {
		return nil, errors.New("file acceptance requires text content without NUL bytes")
	}
	entries := slices.Clone(s.Checkout.Entries)
	for i := range entries {
		if entries[i].Path == decl.Source {
			entries[i].Content = live
		}
	}
	result := fileAcceptance{Target: target, Source: filepath.Join(s.Root, decl.Source), Definitions: definitions.Digest(entries)}
	if !bytes.Equal(source, live) {
		result.Diff = acceptanceDiff(decl.Source, source, live)
	}
	return &acceptInput{selected: s, decl: *decl, source: source, target: live, sourceInfo: si, targetInfo: ti, receipt: receipt, checkoutIdentity: identity, result: result}, nil
}

func sameAcceptance(a, b *acceptInput) bool {
	ar, _ := json.Marshal(a.receipt)
	br, _ := json.Marshal(b.receipt)
	return a.selected.Root == b.selected.Root && a.checkoutIdentity == b.checkoutIdentity && a.selected.Checkout.Digest() == b.selected.Checkout.Digest() && a.result.Definitions == b.result.Definitions && bytes.Equal(ar, br) && bytes.Equal(a.source, b.source) && bytes.Equal(a.target, b.target) && os.SameFile(a.sourceInfo, b.sourceInfo) && os.SameFile(a.targetInfo, b.targetInfo) && a.sourceInfo.Mode() == b.sourceInfo.Mode() && a.targetInfo.Mode() == b.targetInfo.Mode()
}

// Read identity inputs directly: reverse capture never runs Git commands.
// A linked worktree needs separate gitdir ownership handling and is refused.
func acceptanceCheckoutIdentity(root string) (string, error) {
	h := sha256.New()
	var head string
	for _, name := range []string{".git/HEAD", ".git/config", ".git/packed-refs"} {
		data, _, err := readAcceptFile(root, name)
		if errors.Is(err, os.ErrNotExist) && name == ".git/packed-refs" {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("checkout identity %s: %w", name, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)
		if name == ".git/HEAD" {
			head = strings.TrimSpace(string(data))
		}
	}
	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		if !strings.HasPrefix(ref, "refs/") || filepath.Clean(ref) != ref {
			return "", errors.New("invalid checkout HEAD reference")
		}
		data, _, err := readAcceptFile(root, ".git/"+ref)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Open every ancestor relative to an already opened directory, never following
// symlinks. The retained parent descriptor also anchors the atomic replacement.
func acceptParent(root, rel string) (*os.File, string, error) {
	if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel {
		return nil, "", errors.New("invalid relative file path")
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if e != nil {
			return nil, "", e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), filepath.Dir(rel)), parts[len(parts)-1], nil
}

func readAcceptAt(parent *os.File, name string) ([]byte, os.FileInfo, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return nil, nil, errors.New("file must be regular with one link and no symlinks")
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > limit {
		return nil, nil, errors.New("file exceeds the 4 MiB acceptance limit")
	}
	return data, info, nil
}

func readAcceptFile(root, rel string) ([]byte, os.FileInfo, error) {
	parent, name, err := acceptParent(root, rel)
	if err != nil {
		return nil, nil, err
	}
	defer parent.Close()
	return readAcceptAt(parent, name)
}

func replaceAcceptedSource(in *acceptInput) error {
	parent, name, err := acceptParent(in.selected.Root, in.decl.Source)
	if err != nil {
		return err
	}
	defer parent.Close()
	data, info, err := readAcceptAt(parent, name)
	if err != nil {
		return err
	}
	if !os.SameFile(in.sourceInfo, info) || !bytes.Equal(data, in.source) || info.Mode() != in.sourceInfo.Mode() {
		return errors.New("source changed before replacement")
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	tmp := ".nimbus-accept-" + hex.EncodeToString(nonce[:])
	fd, err := syscall.Openat(int(parent.Fd()), tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), tmp)
	defer syscall.Unlinkat(int(parent.Fd()), tmp)
	if _, err = f.Write(in.target); err == nil {
		err = f.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = syscall.Renameat(int(parent.Fd()), tmp, int(parent.Fd()), name); err != nil {
		return err
	}
	return parent.Sync()
}

// A complete replacement diff stays linear for large configuration files and
// represents final-newline changes explicitly.
func acceptanceDiff(path string, before, after []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s (live content)\n", path, path)
	for _, side := range []struct {
		prefix string
		data   []byte
	}{{"-", before}, {"+", after}} {
		if len(side.data) == 0 {
			continue
		}
		for line := range strings.SplitSeq(strings.TrimSuffix(string(side.data), "\n"), "\n") {
			fmt.Fprintf(&b, "%s%s\n", side.prefix, line)
		}
		if side.data[len(side.data)-1] != '\n' {
			b.WriteString("\\ No newline at end of file\n")
		}
	}
	return b.String()
}

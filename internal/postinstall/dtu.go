package postinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/plan"
)

const (
	DTUCertificatePath   = "/etc/NetworkManager/certs/dtu-eduroam.pem"
	DTUCertificateSHA256 = "4044ec3c69ea71dade30be85294f22d6a3659cfb9c2b90986cebe3623775a7c1"
	dtuPreviousSHA256    = "936b4f18224d20594983341d08c6dd8cebc69e384c2d22e65a55f86689b76a73"
	dtuCertificateDir    = "/etc/NetworkManager/certs"
)

// DTUCertificate binds approval to file metadata and native SELinux observations.
// Neither existing file content nor network credentials are exposed in a task.
type DTUCertificate struct {
	Observed string `json:"observed"`
	SELinux  bool   `json:"selinux,omitzero"`
}

//go:embed dtu/ca.pem
var dtuCA []byte

var dtuNow = time.Now
var dtuLoad = loadDTUCertificate

func dtuValidity(now time.Time) error {
	if now.Before(time.Date(2022, 10, 3, 12, 1, 13, 0, time.UTC)) || !now.Before(time.Date(2027, 12, 2, 11, 29, 11, 0, time.UTC)) {
		return errors.New("the pinned DTU certificate bundle is outside its validity period; check the clock or install a reviewed Nimbus release with a renewed bundle")
	}
	return nil
}

func validateDTUCertificate(data []byte, now time.Time) error {
	if fmt.Sprintf("%x", sha256.Sum256(data)) != DTUCertificateSHA256 {
		return errors.New("DTU certificate checksum differs from the reviewed bundle; no certificate was installed")
	}
	if err := dtuValidity(now); err != nil {
		return err
	}
	var certs []*x509.Certificate
	for rest := bytes.TrimSpace(data); len(rest) != 0; {
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return errors.New("invalid DTU certificate bundle")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return errors.New("DTU bundle contains an invalid or expired CA certificate")
		}
		certs = append(certs, cert)
		rest = bytes.TrimSpace(tail)
	}
	if len(certs) != 3 {
		return errors.New("DTU bundle does not contain the reviewed three CA certificates")
	}
	for _, cert := range certs {
		if err := cert.CheckSignatureFrom(certs[0]); err != nil {
			return errors.New("DTU certificate chain verification failed")
		}
	}
	return nil
}

func dtuCertificateTask(src native.Source, in Inputs) Task {
	t := Task{ID: "dtu-network", Owner: "component:dtu-network", Title: "Install the DTU eduroam CA certificate", Status: Unknown,
		Prerequisites: []string{"Selected prerequisites must be installed with verified Nimbus receipts or recorded baseline identities; use nimbus sync for missing prerequisites. Certificate installation needs approved sudo."},
		Instructions: []string{
			"Verify the bundled DTU CAT certificate, SHA-256 " + DTUCertificateSHA256 + ".",
			"Install only " + DTUCertificatePath + " as root:root, mode 0644, and restore its native SELinux label when enabled. No global CA trust changes or network restart.",
		},
		Verification: "Check pinned content, all three CA certificates, validity, ownership, permissions and SELinux labels. Certificate installation alone does not verify eduroam access. Renew the bundle before 2027-12-02.",
		Recovery:     "Retry after installation or labeling failure. An unfamiliar destination is never replaced. Removing component selection or resetting evidence does not delete the certificate; review any NetworkManager profile using it before manually removing it.",
	}
	if !in.Facts.User.Known() || !operatorName.MatchString(in.Facts.User.Value.Name) || in.Facts.User.Value.Name == "root" {
		t.Status, t.Detail = Blocked, "Run DTU setup as your named, non-root desktop user."
		return t
	}
	for _, name := range []string{"NetworkManager", "NetworkManager-wifi", "policycoreutils", "libselinux-utils"} {
		i := slices.IndexFunc(in.Resolved.Packages, func(p definitions.ResolvedPackage) bool { return p.Canonical == "dnf:"+name })
		if i < 0 {
			t.Status, t.Detail = Blocked, "The dtu-network package selection is incomplete; sync its component first."
			return t
		}
		if status, detail := dtuPackageReady(in, in.Resolved.Packages[i]); status != Complete {
			t.Status, t.Detail = status, detail
			return t
		}
	}
	for _, command := range []string{"stat", "/usr/sbin/getenforce", "/usr/sbin/matchpathcon", "/usr/sbin/restorecon"} {
		if _, err := src.LookPath(command); err != nil {
			t.Status, t.Detail = Blocked, "Required certificate inspection tools are missing; run nimbus sync."
			return t
		}
	}
	if err := dtuValidity(dtuNow()); err != nil {
		t.Status, t.Detail = Blocked, err.Error()
		return t
	}
	have, evidence, labelProblem, err := observeDTU(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if have.Exists && !knownDTUCertificate(have.Content) {
		t.Status, t.Detail = Blocked, "An unfamiliar file exists at "+DTUCertificatePath+"; review it manually before retrying. Nimbus will not replace it."
		return t
	}
	current := have.Exists && fmt.Sprintf("%x", sha256.Sum256(have.Content)) == DTUCertificateSHA256
	if current {
		if err := validateDTUCertificate(have.Content, dtuNow()); err != nil {
			t.Status, t.Detail = Blocked, err.Error()
			return t
		}
	}
	if current && dtuMetadataProblem(have) == "" && labelProblem == "" {
		t.Status, t.Detail = Complete, "DTU eduroam CA bundle verified; Wi-Fi sign-in is not tested."
		return t
	}
	t.Status, t.Detail = Pending, "Install the reviewed DTU eduroam CA bundle for NetworkManager."
	if current {
		t.Detail = strings.Join(slices.DeleteFunc([]string{dtuMetadataProblem(have), labelProblem}, func(s string) bool { return s == "" }), "; ")
	}
	t.Action = &Action{Kind: InstallDTUCertificate, DTU: &evidence}
	return t
}

func dtuMetadataProblem(have inspect.SystemFile) string {
	var problems []string
	if have.Owner != "root" || have.Group != "root" {
		problems = append(problems, fmt.Sprintf("DTU certificate ownership is %s:%s; expected root:root", have.Owner, have.Group))
	}
	if have.Mode != "0644" {
		problems = append(problems, fmt.Sprintf("DTU certificate permissions are %s; expected 0644", have.Mode))
	}
	return strings.Join(problems, "; ")
}

func observeDTU(src native.Source) (inspect.SystemFile, DTUCertificate, string, error) {
	have, err := inspect.ObserveFile(src, DTUCertificatePath)
	if err != nil {
		return have, DTUCertificate{}, "", errors.New("DTU certificate path is unreadable or unsafe; inspect its parents, links and permissions")
	}
	for _, dir := range []string{"/etc", "/etc/NetworkManager", dtuCertificateDir} {
		names, err := src.ReadDir(filepath.Dir(dir))
		if err != nil {
			return have, DTUCertificate{}, "", errors.New("cannot inspect DTU certificate parent directory")
		}
		if !slices.Contains(names, filepath.Base(dir)) {
			break
		}
		out, err := src.Run("stat", "--format=%U|%G|%a", "--", dir)
		fields := strings.Split(strings.TrimSpace(string(out)), "|")
		if err != nil || len(fields) != 3 {
			return have, DTUCertificate{}, "", errors.New("cannot inspect DTU certificate directory ownership")
		}
		mode, err := strconv.ParseUint(fields[2], 8, 32)
		if err != nil || fields[0] != "root" || fields[1] != "root" || mode&0022 != 0 {
			return have, DTUCertificate{}, "", errors.New("DTU certificate directory must be root-owned and not writable by other users")
		}
	}
	mode, err := src.Run("/usr/sbin/getenforce")
	if err != nil {
		return have, DTUCertificate{}, "", errors.New("cannot inspect SELinux mode")
	}
	enforcing := strings.TrimSpace(string(mode))
	if !slices.Contains([]string{"Enforcing", "Permissive", "Disabled"}, enforcing) {
		return have, DTUCertificate{}, "", errors.New("unrecognized SELinux mode")
	}
	evidence := DTUCertificate{SELinux: enforcing != "Disabled"}
	var labelProblems []string
	var labels []string
	if evidence.SELinux && have.Exists {
		for _, path := range []string{dtuCertificateDir, DTUCertificatePath} {
			want, err := src.Run("/usr/sbin/matchpathcon", "-n", "--", path)
			if err != nil || !strings.Contains(string(want), ":object_r:") {
				return have, evidence, "", errors.New("cannot determine the native DTU certificate SELinux label")
			}
			got, err := src.Run("stat", "--format=%C", "--", path)
			if err != nil {
				return have, evidence, "", errors.New("cannot inspect the current DTU certificate SELinux label")
			}
			wantLabel, gotLabel := strings.TrimSpace(string(want)), strings.TrimSpace(string(got))
			// Native verification follows restorecon semantics, including preserved
			// SELinux user fields. Full context equality rejects valid labels.
			verified, verifyErr := src.Run("/usr/sbin/matchpathcon", "-V", "--", path)
			result := strings.TrimSpace(string(verified))
			labels = append(labels, wantLabel, gotLabel, result)
			if verifyErr == nil && result == path+" verified." {
				continue
			}
			exit, exited := errors.AsType[interface {
				error
				ExitCode() int
			}](verifyErr)
			if exited && exit.ExitCode() == 1 && result == fmt.Sprintf("%s has context %s, should be %s", path, gotLabel, wantLabel) {
				labelProblems = append(labelProblems, "SELinux label needs repair on "+path)
				continue
			}
			return have, evidence, "", fmt.Errorf("cannot verify the SELinux label on %s with matchpathcon -V", path)
		}
	}
	data, err := json.Marshal(struct {
		File   inspect.SystemFile
		Mode   string
		Labels []string
	}{have, enforcing, labels})
	if err != nil {
		return have, evidence, "", err
	}
	evidence.Observed = fmt.Sprintf("%x", sha256.Sum256(data))
	return have, evidence, strings.Join(labelProblems, "; "), nil
}

// DTUCommands describes the fixed file-only operations in the approval preview.
func DTUCommands(task Task) ([][]string, error) {
	if task.ID != "dtu-network" || task.Status != Pending || task.Action == nil || task.Action.Kind != InstallDTUCertificate || task.Action.DTU == nil || !pictureHash.MatchString(task.Action.DTU.Observed) || len(task.Action.Argv) != 0 || len(task.Action.Commands) != 0 {
		return nil, errors.New("invalid DTU certificate action")
	}
	commands := [][]string{{"sudo", "nimbus", "internal", "system-file", "--plan", "<approved-digest>", "--payload", "<verified-certificate-change>"}}
	if task.Action.DTU.SELinux {
		commands = append(commands, []string{"sudo", "--", "/usr/sbin/restorecon", "--", dtuCertificateDir, DTUCertificatePath})
	}
	return commands, nil
}

func knownDTUCertificate(data []byte) bool {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	return hash == DTUCertificateSHA256 || hash == dtuPreviousSHA256
}

func loadDTUCertificate(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bytes.Clone(dtuCA), nil
}

// RunDTUCertificate runs only after approval; it never connects to eduroam.
func RunDTUCertificate(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if _, err := DTUCommands(task); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Verifying the bundled DTU CA certificates..."); err != nil {
		return err
	}
	data, err := dtuLoad(ctx)
	if err != nil {
		return err
	}
	if err := validateDTUCertificate(data, dtuNow()); err != nil {
		return err
	}
	have, observed, _, err := observeDTU(src)
	if err != nil {
		return err
	}
	if observed != *task.Action.DTU {
		return errors.New("DTU certificate state changed after approval; inspect and retry")
	}
	if have.Exists && !knownDTUCertificate(have.Content) {
		return errors.New("refusing to replace an unfamiliar certificate")
	}
	payload := apply.FilePayload{PlanDigest: observed.Observed, Change: plan.FileChange{Target: DTUCertificatePath, Before: have, After: inspect.SystemFile{Exists: true, Content: data, Owner: "root", Group: "root", Mode: "0644"}}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "nimbus-dtu-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	staged := filepath.Join(dir, "certificate.json")
	if err := os.WriteFile(staged, encoded, 0600); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := src.Stream(out, errOut, "sudo", "--", exe, "internal", "system-file", "--plan", observed.Observed, "--payload", staged); err != nil {
		return err
	}
	if observed.SELinux {
		if err := src.Stream(out, errOut, "sudo", "--", "/usr/sbin/restorecon", "--", dtuCertificateDir, DTUCertificatePath); err != nil {
			return fmt.Errorf("certificate written but SELinux labeling failed; rerun the task: %w", err)
		}
	}
	actual, _, labelProblem, err := observeDTU(src)
	if err != nil {
		return err
	}
	if !actual.Exists {
		return errors.New("DTU certificate is missing after installation")
	}
	if !bytes.Equal(actual.Content, data) {
		return errors.New("DTU certificate checksum changed after installation")
	}
	if problem := dtuMetadataProblem(actual); problem != "" {
		return errors.New(problem)
	}
	if labelProblem != "" {
		return errors.New(labelProblem)
	}
	return validateDTUCertificate(actual.Content, dtuNow())
}

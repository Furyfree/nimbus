package postinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	DTUCertificateURL    = "https://itswiki.compute.dtu.dk/images/0/07/Eduroam_aug2020.pem"
	DTUCertificatePath   = "/etc/NetworkManager/certs/dtu-eduroam.pem"
	DTUCertificateSHA256 = "936b4f18224d20594983341d08c6dd8cebc69e384c2d22e65a55f86689b76a73"
	dtuCertificateDir    = "/etc/NetworkManager/certs"
	dtuMaxDownload       = 64 << 10
)

// DTUCertificate binds approval to file metadata and native SELinux observations.
// Neither existing file content nor network credentials are exposed in a task.
type DTUCertificate struct {
	Observed string `json:"observed"`
	SELinux  bool   `json:"selinux,omitzero"`
}

var dtuNow = time.Now
var dtuFetch = fetchDTUCertificate

func dtuValidity(now time.Time) error {
	if now.Before(time.Date(2015, 12, 2, 11, 19, 11, 0, time.UTC)) || !now.Before(time.Date(2027, 12, 2, 11, 25, 30, 0, time.UTC)) {
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

func dtuNetwork(src native.Source, in Inputs) Task {
	t := Task{ID: "dtu-network", Owner: "component:dtu-network", Title: "Install the DTU eduroam CA certificate", Status: Unknown,
		Prerequisites: []string{"Apply the selected dtu-network component with nimbus sync. Installation needs internet access and approved sudo."},
		Instructions: []string{
			"Download " + DTUCertificateURL + " and verify SHA-256 " + DTUCertificateSHA256 + ".",
			"Install only " + DTUCertificatePath + " as root:root, mode 0644, and restore its native SELinux label when enabled. No global CA trust changes or network restart.",
			"When configuring eduroam in NetworkManager, select this file as the CA certificate and use DTU's current authentication and server-name settings. Enter credentials in the native network dialog; Nimbus does not configure or test Wi-Fi sign-in.",
		},
		Verification: "Check pinned content, all three CA certificates, validity, ownership, permissions and SELinux labels. Certificate installation alone does not verify eduroam access. Renew the bundle before 2027-12-02.",
		Recovery:     "Retry after a download or labeling failure. An unfamiliar destination is never replaced. Removing component selection or resetting evidence does not delete the certificate; review any NetworkManager profile using it before manually removing it.",
	}
	if !in.Facts.User.Known() || !operatorName.MatchString(in.Facts.User.Value.Name) || in.Facts.User.Value.Name == "root" {
		t.Status, t.Detail = Blocked, "Run DTU setup as your named, non-root desktop user."
		return t
	}
	for _, name := range []string{"NetworkManager", "policycoreutils", "libselinux-utils"} {
		i := slices.IndexFunc(in.Resolved.Packages, func(p definitions.ResolvedPackage) bool { return p.Canonical == "dnf:"+name })
		if i < 0 {
			t.Status, t.Detail = Blocked, "The dtu-network package selection is incomplete; sync its component first."
			return t
		}
		if status, detail := packageReady(in, in.Resolved.Packages[i]); status != Complete {
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
	have, evidence, labelsMatch, err := observeDTU(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	if have.Exists && fmt.Sprintf("%x", sha256.Sum256(have.Content)) != DTUCertificateSHA256 {
		t.Status, t.Detail = Blocked, "An unfamiliar file exists at "+DTUCertificatePath+"; review it manually before retrying. Nimbus will not replace it."
		return t
	}
	if have.Exists {
		if err := validateDTUCertificate(have.Content, dtuNow()); err != nil {
			t.Status, t.Detail = Blocked, err.Error()
			return t
		}
	}
	if have.Exists && have.Owner == "root" && have.Group == "root" && have.Mode == "0644" && labelsMatch {
		t.Status, t.Detail = Complete, "DTU eduroam CA bundle verified; Wi-Fi sign-in is not tested."
		return t
	}
	t.Status, t.Detail = Pending, "Install the reviewed DTU eduroam CA bundle for NetworkManager."
	if have.Exists {
		t.Detail = "Repair ownership, permissions or SELinux labels on the matching DTU CA bundle."
	}
	t.Action = &Action{Kind: InstallDTUCertificate, DTU: &evidence}
	return t
}

func observeDTU(src native.Source) (inspect.SystemFile, DTUCertificate, bool, error) {
	have, err := inspect.ObserveFile(src, DTUCertificatePath)
	if err != nil {
		return have, DTUCertificate{}, false, errors.New("DTU certificate path is unreadable or unsafe; inspect its parents, links and permissions")
	}
	for _, dir := range []string{"/etc", "/etc/NetworkManager", dtuCertificateDir} {
		names, err := src.ReadDir(filepath.Dir(dir))
		if err != nil {
			return have, DTUCertificate{}, false, errors.New("cannot inspect DTU certificate parent directory")
		}
		if !slices.Contains(names, filepath.Base(dir)) {
			break
		}
		out, err := src.Run("stat", "--format=%U|%G|%a", "--", dir)
		fields := strings.Split(strings.TrimSpace(string(out)), "|")
		if err != nil || len(fields) != 3 {
			return have, DTUCertificate{}, false, errors.New("cannot inspect DTU certificate directory ownership")
		}
		mode, err := strconv.ParseUint(fields[2], 8, 32)
		if err != nil || fields[0] != "root" || fields[1] != "root" || mode&0022 != 0 {
			return have, DTUCertificate{}, false, errors.New("DTU certificate directory must be root-owned and not writable by other users")
		}
	}
	mode, err := src.Run("/usr/sbin/getenforce")
	if err != nil {
		return have, DTUCertificate{}, false, errors.New("cannot inspect SELinux mode")
	}
	enforcing := strings.TrimSpace(string(mode))
	if !slices.Contains([]string{"Enforcing", "Permissive", "Disabled"}, enforcing) {
		return have, DTUCertificate{}, false, errors.New("unrecognized SELinux mode")
	}
	evidence := DTUCertificate{SELinux: enforcing != "Disabled"}
	labelsMatch := true
	var labels []string
	if evidence.SELinux && have.Exists {
		for _, path := range []string{dtuCertificateDir, DTUCertificatePath} {
			want, err := src.Run("/usr/sbin/matchpathcon", "-n", "--", path)
			if err != nil || !strings.Contains(string(want), ":object_r:") {
				return have, evidence, false, errors.New("cannot determine the native DTU certificate SELinux label")
			}
			got, err := src.Run("stat", "--format=%C", "--", path)
			if err != nil {
				return have, evidence, false, errors.New("cannot inspect the current DTU certificate SELinux label")
			}
			labels = append(labels, strings.TrimSpace(string(want)), strings.TrimSpace(string(got)))
			labelsMatch = labelsMatch && strings.TrimSpace(string(want)) == strings.TrimSpace(string(got))
		}
	}
	data, err := json.Marshal(struct {
		File   inspect.SystemFile
		Mode   string
		Labels []string
	}{have, enforcing, labels})
	if err != nil {
		return have, evidence, false, err
	}
	evidence.Observed = fmt.Sprintf("%x", sha256.Sum256(data))
	return have, evidence, labelsMatch, nil
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

func fetchDTUCertificate(ctx context.Context) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("DTU certificate redirects require review")
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DTUCertificateURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download DTU certificate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DTU certificate download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, dtuMaxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > dtuMaxDownload {
		return nil, errors.New("DTU certificate download exceeds 64 KiB")
	}
	return data, nil
}

// RunDTUNetwork runs only after approval; it never connects to eduroam.
func RunDTUNetwork(ctx context.Context, src native.Source, out, errOut io.Writer, task Task) error {
	if _, err := DTUCommands(task); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Downloading and verifying the pinned DTU CA bundle..."); err != nil {
		return err
	}
	data, err := dtuFetch(ctx)
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
	if have.Exists && fmt.Sprintf("%x", sha256.Sum256(have.Content)) != DTUCertificateSHA256 {
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
	actual, _, labelsMatch, err := observeDTU(src)
	if err != nil {
		return err
	}
	if !plan.SameFile(actual, payload.Change.After) || !labelsMatch {
		return errors.New("DTU certificate content, metadata or SELinux labels failed verification; rerun the task")
	}
	return validateDTUCertificate(actual.Content, dtuNow())
}

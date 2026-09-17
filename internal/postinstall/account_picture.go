package postinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

const accountsBus = "org.freedesktop.Accounts"

// AccountPicture binds approval to the source image and observed account icon.
// Image bytes and unrelated account properties are never retained in the task.
type AccountPicture struct {
	UID           string `json:"uid"`
	Home          string `json:"home"`
	SourceSHA256  string `json:"source_sha256"`
	CurrentPath   string `json:"current_path"`
	CurrentSHA256 string `json:"current_sha256,omitempty"`
}

var pictureHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

// AccountPictureAction constructs the sole supported AccountsService mutation.
// It runs as the caller; the service applies its normal Polkit authorization.
func AccountPictureAction(user string, picture AccountPicture) *Action {
	uid, err := strconv.ParseUint(picture.UID, 10, 32)
	if err != nil || uid == 0 || strconv.FormatUint(uid, 10) != picture.UID ||
		!operatorName.MatchString(user) || user == "root" ||
		!filepath.IsAbs(picture.Home) || filepath.Clean(picture.Home) != picture.Home || picture.Home == "/" ||
		strings.ContainsAny(picture.Home, "\r\n\x00") || !pictureHash.MatchString(picture.SourceSHA256) ||
		(picture.CurrentSHA256 != "" && !pictureHash.MatchString(picture.CurrentSHA256)) {
		return nil
	}
	return &Action{Kind: SetAccountPicture, User: user, Picture: &picture, Argv: []string{
		"/usr/bin/busctl", "--system", "--auto-start=no", "--allow-interactive-authorization=yes",
		"call", accountsBus, "/org/freedesktop/Accounts/User" + picture.UID,
		accountsBus + ".User", "SetIconFile", "s", accountPicturePath(picture.Home),
	}}
}

func accountPicturePath(home string) string {
	return filepath.Join(home, ".config", "noctalia", "assets", "profile-picture.jpg")
}

func accountPicture(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "account-picture", Owner: "package:" + pkg.Canonical,
		Title: "Set the login account picture", Status: Unknown,
		Prerequisites: []string{"Apply the Chezmoi profile picture and run this task as your normal desktop user with AccountsService running."},
		Instructions:  []string{"Register ~/.config/noctalia/assets/profile-picture.jpg through AccountsService. Its native permission prompt remains enabled; the service owns its stored copy. The greeter reads it next time it starts."},
		Verification:  "Read this user's IconFile property and compare its image bytes with the source. A successful command alone does not prove the picture was saved.",
		Recovery:      "Retry after fixing the image or AccountsService. Use your desktop's account settings to replace or clear the picture; package deselection does not remove it. Nimbus stores no completion receipt.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	home, err := os.UserHomeDir()
	if err != nil || !in.Facts.User.Known() || !operatorName.MatchString(in.Facts.User.Value.Name) || in.Facts.User.Value.Name == "root" || !filepath.IsAbs(home) {
		t.Status, t.Detail = Blocked, "A named, non-root invoking user with an absolute home directory is required."
		return t
	}
	image, err := src.ReadFile(accountPicturePath(home))
	if err != nil {
		t.Status, t.Detail = Blocked, "The profile picture is missing or unreadable; apply the Chezmoi profile picture before retrying."
		return t
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(image))
	if err != nil || len(image) > 1024*1024 || config.Width > 4096 || config.Height > 4096 {
		t.Status, t.Detail = Blocked, "The profile picture must be a JPEG no larger than 1 MiB and 4096 pixels per side."
		return t
	}
	if _, err := jpeg.Decode(bytes.NewReader(image)); err != nil {
		t.Status, t.Detail = Blocked, "The profile picture is damaged or incomplete; replace the source image before retrying."
		return t
	}
	if _, err := src.LookPath("/usr/bin/busctl"); err != nil {
		t.Status, t.Detail = Blocked, "busctl is unavailable; repair the systemd tools before retrying."
		return t
	}
	uid, err := src.Run("id", "-u")
	if err != nil {
		t.Detail = "The invoking account UID could not be read."
		return t
	}
	picture := AccountPicture{UID: strings.TrimSpace(string(uid)), Home: home, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(image))}
	if AccountPictureAction(in.Facts.User.Value.Name, picture) == nil {
		t.Status, t.Detail = Blocked, "The invoking account or image path is not valid for account-picture setup."
		return t
	}
	user, err := accountProperty(src, picture.UID, "UserName")
	if err != nil || user != in.Facts.User.Value.Name {
		t.Detail = "AccountsService could not confirm the invoking account. Check accounts-daemon and log into the desktop before retrying."
		return t
	}
	path, err := accountProperty(src, picture.UID, "IconFile")
	if err != nil || (path != "" && (!filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00"))) {
		t.Detail = "AccountsService's current icon path could not be read. Check accounts-daemon before retrying."
		return t
	}
	picture.CurrentPath = path
	if path != "" {
		current, err := src.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Detail = "The current account picture could not be read; check its permissions before retrying."
			return t
		}
		if err == nil {
			picture.CurrentSHA256 = fmt.Sprintf("%x", sha256.Sum256(current))
		}
		if picture.CurrentSHA256 == picture.SourceSHA256 {
			t.Status, t.Detail = Complete, "The account picture matches the Chezmoi source."
			return t
		}
	}
	t.Status, t.Detail = Pending, "Set the invoking user's account picture from the Chezmoi image, replacing any previous account picture."
	t.Action = AccountPictureAction(user, picture)
	return t
}

func accountProperty(src native.Source, uid, property string) (string, error) {
	data, err := src.Run("/usr/bin/busctl", "--system", "--auto-start=no", "--allow-interactive-authorization=no", "--json=short",
		"get-property", accountsBus, "/org/freedesktop/Accounts/User"+uid, accountsBus+".User", property)
	if err != nil {
		return "", err
	}
	var response struct {
		Type string  `json:"type"`
		Data *string `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Type != "s" || response.Data == nil {
		return "", fmt.Errorf("unrecognized AccountsService %s response", property)
	}
	return *response.Data, nil
}

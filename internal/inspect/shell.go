package inspect

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

// LoginShell binds the shell to the local account's numeric identity.
type LoginShell struct {
	UID   uint64 `json:"uid"`
	Shell string `json:"shell"`
}

var loginName = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

// ObserveLoginShell deliberately uses the files NSS service, never a network
// account directory. It retains no password, GECOS, home or group fields.
func ObserveLoginShell(src native.Source, user string) (LoginShell, error) {
	if !loginName.MatchString(user) || user == "root" {
		return LoginShell{}, fmt.Errorf("login shell requires a named, non-root local user")
	}
	out, err := src.Run("getent", "--service=files", "passwd", user)
	if err != nil {
		return LoginShell{}, fmt.Errorf("inspect local login shell for %s: %w", user, err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), ":")
	if len(fields) != 7 || fields[0] != user || strings.ContainsAny(fields[6], "\r\n\x00") {
		return LoginShell{}, fmt.Errorf("invalid local account record for %s", user)
	}
	uid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil || uid == 0 || !path.IsAbs(fields[6]) || path.Clean(fields[6]) != fields[6] {
		return LoginShell{}, fmt.Errorf("invalid local account identity or login shell for %s", user)
	}
	return LoginShell{UID: uid, Shell: fields[6]}, nil
}

// CheckLoginShell checks the supported executable and native login-shell list.
func CheckLoginShell(src native.Source, shell string) error {
	switch shell {
	case "/bin/bash", "/usr/bin/bash", "/bin/zsh", "/usr/bin/zsh":
	default:
		return fmt.Errorf("unsupported login shell %q", shell)
	}
	data, err := src.ReadFile("/etc/shells")
	if err != nil {
		return fmt.Errorf("read allowed login shells: %w", err)
	}
	allowed := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		allowed = allowed || strings.TrimSpace(line) == shell
	}
	if !allowed {
		return fmt.Errorf("%s is not listed in /etc/shells", shell)
	}
	if _, err := src.Run("test", "-x", shell); err != nil {
		return fmt.Errorf("login shell %s is not executable: %w", shell, err)
	}
	return nil
}

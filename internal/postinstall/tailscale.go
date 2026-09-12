package postinstall

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

var operatorName = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

// TailscaleOperatorAction constructs the one supported permission change.
// The invoking account is supplied by native inspection, never a shell expansion.
func TailscaleOperatorAction(user string) *Action {
	if !operatorName.MatchString(user) || user == "root" {
		return nil
	}
	return &Action{Kind: SetTailscaleOperator, User: user,
		Argv: []string{"sudo", "--", "/usr/bin/tailscale", "set", "--operator=" + user}}
}

func tailscaleOperator(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "tailscale-operator", Owner: "package:" + pkg.Canonical,
		Title: "Allow this user to manage Tailscale", Status: Unknown,
		Prerequisites: []string{"The selected Tailscale package is applied and tailscaled is running."},
		Instructions:  []string{"Set the invoking user as the local Tailscale operator so the CLI and Noctalia Tailnet controls work without sudo. This permits changing local Tailscale settings."},
		Verification:  "Read the operator from the running daemon after the native command; package presence or a successful command alone does not prove permission was granted.",
		Recovery:      "Revoke with sudo tailscale set --operator=, or explicitly set a different operator. Tailscale owns this preference; removing its package selection does not reset it.",
	}
	if status, detail := packageReady(in, pkg); status != Complete {
		t.Status, t.Detail = status, detail
		return t
	}
	action := TailscaleOperatorAction(in.Facts.User.Value.Name)
	if !in.Facts.User.Known() || action == nil {
		t.Status, t.Detail = Blocked, "A named, non-root invoking user is required for Tailscale operator setup."
		return t
	}
	if _, err := src.LookPath("/usr/bin/tailscale"); err != nil {
		t.Status, t.Detail = Blocked, "Tailscale is unavailable; repair the selected package with nimbus sync."
		return t
	}
	// This is a read-only LocalAPI request. Never retain or render the other
	// preferences, which can contain private network or account information.
	data, err := src.Run("/usr/bin/tailscale", "debug", "prefs")
	if err != nil {
		t.Detail = "The Tailscale operator could not be read. Check that tailscaled is running and its local API is accessible, then retry."
		return t
	}
	operator, ok := readOperator(data)
	if !ok {
		t.Detail = "Tailscale returned an unrecognized preferences response; operator permission is unknown."
		return t
	}
	if operator == action.User {
		t.Status, t.Detail = Complete, action.User+" is already the local Tailscale operator."
		return t
	}
	if operator == "" {
		operator = "(none)"
	}
	t.Status = Pending
	t.Detail = fmt.Sprintf("Change the local Tailscale operator from %s to %s. Sign-in, connection state and other preferences are unchanged.", operator, action.User)
	t.Action = action
	return t
}

func readOperator(data []byte) (string, bool) {
	var prefs struct {
		OperatorUser json.RawMessage
		WantRunning  *bool
	}
	if json.Unmarshal(data, &prefs) != nil || prefs.WantRunning == nil {
		return "", false
	}
	// OperatorUser is omitted when unset. WantRunning is always present in a
	// native preferences response; an empty object must not imply "unset".
	if len(prefs.OperatorUser) == 0 {
		return "", true
	}
	var operator *string
	if json.Unmarshal(prefs.OperatorUser, &operator) != nil || operator == nil {
		return "", false
	}
	return *operator, *operator == "" || operatorName.MatchString(*operator)
}

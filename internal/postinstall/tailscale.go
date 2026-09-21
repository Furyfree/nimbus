package postinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
)

var operatorName = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

// TailscaleOperatorAction constructs the permission-only change.
// The invoking account is supplied by native inspection, never a shell expansion.
func TailscaleOperatorAction(user string) *Action {
	if !operatorName.MatchString(user) || user == "root" {
		return nil
	}
	return &Action{Kind: SetTailscaleOperator, User: user,
		Argv: []string{"sudo", "--", "/usr/bin/tailscale", "set", "--operator=" + user}}
}

// TailscaleLoginAction starts native sign-in and connects with the operator set.
// Do not use tailscale login: switching profiles can clear operator permission.
func TailscaleLoginAction(user string) *Action {
	action := TailscaleOperatorAction(user)
	if action != nil {
		action.Kind = LoginTailscale
		action.Argv[3] = "up"
	}
	return action
}

func tailscaleOperator(src native.Source, in Inputs, pkg definitions.ResolvedPackage) Task {
	t := Task{
		ID: "tailscale-operator", Owner: "package:" + pkg.Canonical,
		Title: "Allow this user to manage Tailscale", Status: Unknown,
		Prerequisites: []string{"The selected Tailscale package is applied and tailscaled is running."},
		Instructions:  []string{"Set the invoking user as the local Tailscale operator so the CLI and Noctalia Tailnet controls work without sudo. This permits changing local Tailscale settings."},
		Verification:  "Read the operator and sign-in state from the daemon after the native command. Initial sign-in must also reach a running connection; a successful command alone is insufficient.",
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
	if operator == "" {
		operator = "(none)"
	}
	backend, err := tailscaleBackend(src)
	if err != nil {
		t.Detail = err.Error()
		return t
	}
	switch backend {
	case "NeedsLogin":
		t.Status = Pending
		t.Detail = fmt.Sprintf("Tailscale needs sign-in. Current operator: %s. After approval, sign in through the native link and connect this computer with %s as operator.", operator, action.User)
		t.Action = TailscaleLoginAction(action.User)
		t.Instructions = []string{
			"This starts interactive sign-in and brings Tailscale online. Complete the browser sign-in and let the command finish; --yes approves the action but cannot sign in for you.",
			"Use this task for initial setup: tailscale login can switch profiles and clear the operator setting. Existing non-default preferences remain subject to Tailscale's native checks; Nimbus never adds --reset.",
		}
		t.Recovery = "If interrupted, rerun this task to inspect the current state and retry. If device approval is required, approve it in the Tailscale admin console. Disconnect with tailscale down; revoke operator access with sudo tailscale set --operator=."
		return t
	case "NeedsMachineAuth":
		t.Status, t.Detail = Blocked, "Tailscale needs device approval in the admin console. Complete that approval, then rerun this task; no new login is started."
		return t
	case "Running", "Stopped":
		// A signed-in but intentionally stopped machine needs no connection change.
		t.CurrentState = backend
	default:
		t.Detail = "Tailscale is not ready (" + backend + "); wait for the daemon, then retry. No changes are offered."
		return t
	}
	if operator == action.User {
		t.Summary = "Signed in; operator configured"
		t.Status, t.Detail = Complete, action.User+" is already the local Tailscale operator."
		if backend == "Stopped" {
			t.Detail += " Signed in; connection is stopped and left unchanged."
		} else {
			t.Detail += " Signed in and connected."
		}
		return t
	}
	t.Status = Pending
	t.Summary = "Signed in; operator needs setup"
	t.Detail = fmt.Sprintf("Signed in (%s). Change the local Tailscale operator from %s to %s. Connection state and other preferences are unchanged.", backend, operator, action.User)
	t.Action = action
	return t
}

// tailscaleBackend retains only the public state label, never account or peer data.
func tailscaleBackend(src native.Source) (string, error) {
	data, err := src.Run("/usr/bin/tailscale", "status", "--json", "--peers=false")
	if err != nil {
		return "", errors.New("Tailscale sign-in state could not be read; check that tailscaled is running, then retry")
	}
	var status struct{ BackendState string }
	if json.Unmarshal(data, &status) == nil {
		switch status.BackendState {
		case "NeedsLogin", "NeedsMachineAuth", "Running", "Stopped", "Starting", "NoState", "InUse":
			return status.BackendState, nil
		}
	}
	return "", errors.New("Tailscale returned an unrecognized sign-in state; no changes are offered")
}

// VerifyTailscaleConnection is required after the approved initial up action.
// Routine status deliberately permits a subsequently stopped connection.
func VerifyTailscaleConnection(src native.Source) error {
	backend, err := tailscaleBackend(src)
	if err != nil {
		return err
	}
	if backend != "Running" {
		return errors.New("Tailscale sign-in did not establish a running connection; inspect tailscale status and rerun this task")
	}
	return nil
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

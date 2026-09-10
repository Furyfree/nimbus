package postinstall

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

func sessionTasks(src native.Source, in Inputs) []Task {
	var reboots, logouts []state.Receipt
	for id, receipt := range in.Applied.Receipts {
		if !validReceipt(in, id, receipt) {
			continue
		}
		if receipt.Reboot {
			reboots = append(reboots, receipt)
		}
		if receipt.Logout {
			logouts = append(logouts, receipt)
		}
	}
	var result []Task
	if len(reboots) > 0 {
		result = append(result, rebootTask(src, reboots))
	}
	if len(logouts) > 0 {
		result = append(result, logoutTask(src, in, logouts))
	}
	return result
}

func rebootTask(src native.Source, receipts []state.Receipt) Task {
	t := Task{
		ID: "reboot", Owner: "nimbus", Title: "Restart after system changes",
		Status: Unknown, Detail: "Receipts request a reboot, but its completion could not be established.", Reboot: true,
		Prerequisites: []string{"Nimbus has a verified receipt for a change requiring a restart."},
		Instructions:  []string{"Save your work and restart through the desktop's power menu when ready."},
		Verification:  "The current boot started after every recorded change requiring a reboot.",
		Recovery:      "If the next boot fails, select a working kernel or use the documented live-media recovery procedure.",
	}
	data, err := src.ReadFile("/proc/stat")
	if err != nil {
		return t
	}
	var boot time.Time
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || n <= 0 || !boot.IsZero() {
				return t
			}
			boot = time.Unix(n, 0)
		}
	}
	if boot.IsZero() {
		return t
	}
	unknown := false
	for _, receipt := range receipts {
		changed := receipt.ChangeTime()
		if changed.IsZero() {
			unknown = true
		} else if !boot.After(changed) {
			t.Status, t.Detail = Pending, "The current boot predates a recorded system change requiring a restart."
			return t
		}
	}
	if !unknown {
		t.Status, t.Detail, t.Reboot = Complete, "The current boot started after the recorded changes requiring a restart.", false
	}
	return t
}

func logoutTask(src native.Source, in Inputs, receipts []state.Receipt) Task {
	t := Task{
		ID: "logout", Owner: "nimbus", Title: "Refresh session group membership",
		Status: Unknown, Detail: "Receipts request a fresh login, but current group membership could not be established.", Logout: true,
		Prerequisites: []string{"Nimbus has a verified group change receipt for the invoking user."},
		Instructions:  []string{"Save your work, log out of the desktop, and log back in.", "Other already-running sessions may also need to be restarted."},
		Verification:  "The invoking process has each selected group recorded as requiring a new login.",
		Recovery:      "Use password login on a TTY if the graphical session cannot start; inspect groups before changing configuration.",
	}
	if !in.Facts.User.Known() || in.Facts.User.Value.Name == "" {
		return t
	}
	var groups []string
	for _, receipt := range receipts {
		parts := strings.Split(receipt.Resource, ":")
		if len(parts) != 3 || parts[0] != "group" || parts[1] == "" || parts[2] != in.Facts.User.Value.Name || receipt.Provider != "group" || receipt.Intended != "true" {
			return t
		}
		groups = append(groups, parts[1])
	}
	out, err := src.Run("id", "-nG")
	if err != nil || len(strings.Fields(string(out))) == 0 {
		return t
	}
	active := strings.Fields(string(out))
	out, err = src.Run("id", "-nG", "--", in.Facts.User.Value.Name)
	if err != nil || len(strings.Fields(string(out))) == 0 {
		return t
	}
	configured := strings.Fields(string(out))
	for _, group := range groups {
		if !slices.Contains(configured, group) {
			t.Status, t.Detail = Blocked, "A recorded group membership is no longer configured; inspect nimbus status before logging out."
			return t
		}
	}
	for _, group := range groups {
		if !slices.Contains(active, group) {
			t.Status, t.Detail = Pending, "The invoking process lacks a configured group; log out and back in to refresh the desktop session."
			return t
		}
	}
	t.Status, t.Detail, t.Logout = Complete, "The invoking process has the recorded groups. Other already-running sessions were not inspected.", false
	return t
}

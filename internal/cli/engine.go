package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/output"
	"github.com/Furyfree/nimbus/internal/selector"
	"github.com/spf13/cobra"
)

const engineRestart = "NIMBUS_ENGINE_RESTART"

var engineNEVRA = regexp.MustCompile(`^nimbus-[0-9]+:[A-Za-z0-9._+~^]+-[A-Za-z0-9._+~^]+\.x86_64$`)
var engineInstalledArgs = []string{"-q", "--qf", "%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\\n", "nimbus"}
var engineConfigArgs = []string{"dnf5", "--dump-repo-config=nimbus-engine"}
var engineRefreshArgs = []string{"dnf5", "--refresh", "--repo=nimbus-engine", "--setopt=nimbus-engine.skip_if_unavailable=0", "makecache"}
var engineQueryArgs = []string{"dnf5", "-q", "--cacheonly", "--repo=nimbus-engine", "--setopt=nimbus-engine.skip_if_unavailable=0", "repoquery", "--upgrades", "--latest-limit=1", "--arch=x86_64", "--qf", "%{full_nevra}", "nimbus"}

// Resolve only selector identity, without opening version-dependent definitions.
func maintenanceSelection(flags machineFlags) (machineFlags, error) {
	if flags.checkout == "" || flags.machine == "" {
		path, err := selector.DefaultPath()
		if err != nil {
			return flags, err
		}
		sel, err := selector.Load(path)
		if err != nil {
			return flags, err
		}
		if flags.checkout == "" {
			flags.checkout = sel.Checkout
			if err := selector.Verify(sel, flags.checkout); err != nil {
				return flags, err
			}
		}
		if flags.machine == "" {
			flags.machine = sel.Machine
		}
	}
	if err := definitions.ValidateID(flags.machine); err != nil {
		return flags, err
	}
	root, err := canonical(flags.checkout)
	if err != nil {
		return flags, err
	}
	flags.checkout = root
	return flags, nil
}

// enginePreflight never reads manifests or fetches Git. DNF owns RPM ordering,
// enabled sources, exclusions and signatures. A restarted process checks again.
func enginePreflight(cmd *cobra.Command, src native.Source, flags machineFlags, sf syncFlags, upgrade bool) (string, bool, error) {
	out := cmd.OutOrStdout()
	if cmd.Flags().Lookup("json") != nil {
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			out = cmd.ErrOrStderr()
		}
	}
	installed, err := src.Run("rpm", engineInstalledArgs...)
	if err != nil {
		return "", false, fmt.Errorf("inspect installed Nimbus RPM: %w", err)
	}
	current := strings.TrimSpace(string(installed))
	if !engineNEVRA.MatchString(current) {
		return "", false, errors.New("installed Nimbus RPM identity is missing or ambiguous")
	}
	if expected := os.Getenv(engineRestart); expected != "" && expected != current {
		return "", false, errors.New("Nimbus RPM changed during restart; rerun nimbus sync --upgrade")
	}
	if err := checkEngineRepository(src); err != nil {
		return "", false, err
	}
	if _, err := fmt.Fprintln(out, "Checking Nimbus updates..."); err != nil {
		return "", false, err
	}
	if err := src.Stream(out, cmd.ErrOrStderr(), "sudo", engineRefreshArgs...); err != nil {
		return "", false, fmt.Errorf("Nimbus metadata refresh failed; repositories were not fetched: %w", err)
	}
	available, err := src.Run("sudo", engineQueryArgs...)
	if err != nil {
		return "", false, fmt.Errorf("Nimbus update check failed; repositories were not fetched: %w", err)
	}
	candidate := strings.TrimSpace(string(available))
	if candidate == "" {
		return current, false, nil
	}
	if !engineNEVRA.MatchString(candidate) {
		return "", false, errors.New("Nimbus update check returned an ambiguous RPM identity")
	}
	if !upgrade {
		return "", false, fmt.Errorf("Nimbus update available: %s -> %s; run nimbus sync --upgrade. Neither Git repository was fetched", current, candidate)
	}
	if os.Getenv(engineRestart) != "" {
		return "", false, errors.New("another Nimbus update appeared during restart; stopped to prevent a restart loop; rerun nimbus sync --upgrade")
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return "", false, err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "Nimbus engine upgrade", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return "", false, err
	}
	defer func() {
		if lock != nil {
			_ = lock.Release()
		}
	}()
	executable, err := syncExecutable()
	if err != nil {
		return "", false, err
	}
	owner, err := src.Run("rpm", "-qf", "--qf", "%{NAME}", executable)
	if err != nil || strings.TrimSpace(string(owner)) != "nimbus" {
		return "", false, errors.New("this executable is not owned by the installed Nimbus RPM; use /usr/bin/nimbus sync --upgrade")
	}
	// Restrict the requested engine to its source while allowing native
	// dependencies from the already configured repositories. Never allow erasing.
	args := []string{"dnf5", "--setopt=cacheonly=metadata", "--setopt=nimbus-engine.gpgcheck=1", "--setopt=nimbus-engine.skip_if_unavailable=0", "upgrade", "--from-repo=nimbus-engine", candidate}
	if _, err := fmt.Fprintf(out, "Nimbus: %s -> %s\nDNF will review Nimbus and required dependencies. After success, restart Nimbus, sync configuration, then run Topgrade.\n$ sudo %s\n", current, candidate, strings.Join(args, " ")); err != nil {
		return "", false, err
	}
	if sf.yes {
		args = append(args, "--assumeyes")
	}
	if err := src.Stream(out, cmd.ErrOrStderr(), "sudo", args...); err != nil {
		return "", false, fmt.Errorf("Nimbus update failed; configuration sync and Topgrade skipped: %w", err)
	}
	verified, err := src.Run("rpm", engineInstalledArgs...)
	if err != nil || strings.TrimSpace(string(verified)) != candidate {
		return "", false, errors.New("Nimbus update could not be verified; configuration sync and Topgrade skipped")
	}
	versionData, err := src.Run(executable, "version", "--json")
	var build struct {
		Engine string `json:"engine"`
	}
	if err != nil || json.Unmarshal(versionData, &build) != nil || !strings.Contains(candidate, ":"+strings.TrimPrefix(build.Engine, "v")+"-") || build.Engine == "" {
		return "", false, errors.New("updated executable version could not be verified; configuration sync and Topgrade skipped")
	}
	if _, err := fmt.Fprintf(out, "Nimbus updated to %s; restarting before configuration sync.\n", candidate); err != nil {
		return "", false, err
	}
	args = []string{"sync", "--upgrade", "--checkout", flags.checkout, "--machine", flags.machine}
	if sf.yes {
		args = append(args, "--yes")
	}
	if sf.prune {
		args = append(args, "--prune")
	}
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		args = append(args, "--verbose")
	}
	if err := lock.Release(); err != nil {
		return "", false, err
	}
	lock = nil
	child := exec.CommandContext(cmd.Context(), executable, args...)
	child.Env = append(os.Environ(), engineRestart+"="+candidate)
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), output.Native(out), output.Native(cmd.ErrOrStderr())
	return candidate, true, runChild(child)
}

// DNF resolves global defaults and repository overrides itself. Its dump may
// contain proxy credentials: never print, persist, or include it in an error.
func checkEngineRepository(src native.Source) error {
	data, err := src.Run("sudo", engineConfigArgs...)
	if err != nil {
		return errors.New("cannot inspect the configured Nimbus package repository; no repositories were fetched")
	}
	values := map[string]string{}
	headers := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "======== ") {
			headers++
		}
		key, value, ok := strings.Cut(line, " = ")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if headers != 1 || values["enabled"] != "1" {
		return errors.New("the configured nimbus-engine repository is missing or disabled; restore the intended package source before syncing")
	}
	if values["pkg_gpgcheck"] != "1" || values["sslverify"] != "1" {
		return errors.New("the configured nimbus-engine repository must enable package signature and TLS verification before syncing")
	}
	return nil
}

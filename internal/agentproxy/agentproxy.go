// Package agentproxy coordinates the user-owned proxy through its native APIs.
package agentproxy

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/output"
)

// The Node adapter is embedded in the engine, not a mutable installed helper.
// Node already belongs to the native Mise tool selection.
//
//go:embed resources/*
var resources embed.FS

type ProviderResult struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
	Reason   string `json:"reason,omitempty"`
}

type Result struct {
	Providers []ProviderResult `json:"providers,omitempty"`
	Status    string           `json:"status"`
	Detail    string           `json:"detail"`
	Changes   []string         `json:"changes,omitempty"`
	Notices   []string         `json:"notices,omitempty"`
}

func StatePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_STATE_HOME must be absolute")
	}
	return filepath.Join(base, "nimbus", "agent-proxy.json"), nil
}

// Inspect reads only local registration evidence. It never starts a CLI,
// contacts a provider, writes files, or opens the credential store.
func Inspect(src native.Source, machine string) Result {
	p, err := StatePath()
	if err != nil {
		return Result{Status: "blocked", Detail: err.Error()}
	}
	data, err := src.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Result{Status: "not-configured", Detail: "Run nimbus postinstall agent-proxy to connect Copilot to local agents."}
	}
	if err != nil {
		return Result{Status: "blocked", Detail: "Cannot read private agent-proxy registration state."}
	}
	var state struct {
		Version    int    `json:"version"`
		Machine    string `json:"machine"`
		ProviderID string `json:"providerId"`
		Configured bool   `json:"configured"`
	}
	if len(data) > 4<<20 || json.Unmarshal(data, &state) != nil || state.Version != 1 || state.ProviderID == "" {
		return Result{Status: "blocked", Detail: "Invalid agent-proxy registration state; preserve it for inspection."}
	}
	if state.Machine != machine || !state.Configured {
		return Result{Status: "pending", Detail: "Agent-proxy setup has not completed for this machine."}
	}
	for _, cmd := range []string{"mise", "herdr"} {
		if _, err := src.LookPath(cmd); err != nil {
			return Result{Status: "pending", Detail: "Missing " + cmd + "; apply the Mise tool selection."}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Status: "blocked", Detail: "Cannot resolve the user home."}
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	if _, err := src.ReadFile(filepath.Join(config, "agent-proxy", "config.yaml")); err != nil {
		return Result{Status: "pending", Detail: "Managed proxy configuration is missing or unreadable; apply Chezmoi."}
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	version, err := src.ReadFile(filepath.Join(base, "agent-proxy", "current", "VERSION"))
	if err != nil || strings.TrimSpace(string(version)) != "1.3.0-nimbus.2-source" {
		return Result{Status: "pending", Detail: "Proxy installation needs setup; run nimbus postinstall agent-proxy."}
	}
	return Result{Status: "configured", Detail: "Local integration registered; run the task to recheck services and refresh models."}
}

// Run is called only after approval, or for an already opted-in upgrade refresh.
// All discovery, authenticated IPC and lifecycle changes stay inside this call.
func Run(ctx context.Context, mode, machine string, out io.Writer) (Result, error) {
	if mode != "setup" && mode != "refresh" && mode != "disable-refresh" {
		return Result{}, errors.New("invalid agent-proxy operation")
	}
	if os.Getuid() == 0 {
		return Result{}, errors.New("agent-proxy setup must run as the desktop user, not root")
	}
	p, err := StatePath()
	if err != nil {
		return Result{}, err
	}
	// This process takes the native lock; the adapter also checks private files.
	// Refuse symlinks before creating the owner-only lock directory.
	dir := filepath.Dir(p)
	for parent := dir; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		st, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Result{}, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return Result{}, fmt.Errorf("unsafe state ancestor: %s", parent)
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Result{}, err
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return Result{}, err
	}
	dirStat, ok := dirInfo.Sys().(*syscall.Stat_t)
	if !ok || dirStat.Uid != uint32(os.Getuid()) || dirInfo.Mode().Perm()&0077 != 0 {
		return Result{}, errors.New("Nimbus state directory must be private and owned by this user")
	}
	lock, err := os.OpenFile(filepath.Join(dir, "agent-proxy.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return Result{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return Result{}, errors.New("unsafe agent-proxy lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return Result{}, errors.New("another agent-proxy operation is running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	temp, err := os.MkdirTemp("", "nimbus-agent-proxy-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(temp)
	entries, err := resources.ReadDir("resources")
	if err != nil {
		return Result{}, err
	}
	for _, entry := range entries {
		b, err := resources.ReadFile("resources/" + entry.Name())
		if err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(filepath.Join(temp, entry.Name()), b, 0600); err != nil {
			return Result{}, err
		}
	}
	runtimeQuery := exec.CommandContext(ctx, "mise", "which", "node")
	runtimePath, err := runtimeQuery.Output()
	if err != nil {
		return Result{}, errors.New("Mise could not resolve Node; apply the tool selection first")
	}
	node := strings.TrimSpace(string(runtimePath))
	if !filepath.IsAbs(node) {
		return Result{}, errors.New("Mise returned an invalid Node path")
	}
	command := exec.CommandContext(ctx, node, filepath.Join(temp, "sync.mjs"), "--"+mode, "--machine", machine)
	command.Env = append(os.Environ(), "PATH="+filepath.Dir(node)+":"+os.Getenv("PATH"))
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	command.WaitDelay = 5 * time.Second
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = output.Native(out)
	// No stdin: routine updates cannot turn into account or GUI authorization.
	err = command.Run()
	var result Result
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		if err != nil {
			return Result{}, fmt.Errorf("agent-proxy operation failed; inspect the reported stage: %w", err)
		}
		return Result{}, errors.New("invalid agent-proxy operation report")
	}
	if err != nil {
		return result, errors.New(result.Detail)
	}
	return result, nil
}

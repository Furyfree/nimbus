// Package checkout updates approved Git repositories without discarding local work.
package checkout

import (
	"fmt"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/selector"
)

// Repository records the checkout that passed inspection and its fetched target.
type Repository struct {
	Name, Path, Origin                                 string
	head, branch, remoteBranch, target, approvedOrigin string
}

// Inspect is read-only. The caller must inspect every repository before fetching.
func Inspect(src native.Source, name, path, approvedOrigin string) (*Repository, error) {
	r := &Repository{Name: name, Path: path, approvedOrigin: approvedOrigin}
	root, err := r.git(src, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil, r.problem("is not a readable Git working tree")
	}
	r.Path = root
	origin, err := r.git(src, "remote", "get-url", "--all", "origin")
	if err != nil || strings.Contains(origin, "\n") {
		return nil, r.problem("cannot identify a single origin; check the repository's remote configuration")
	}
	r.Origin, err = selector.NormalizeOrigin(origin)
	approved, approvedErr := selector.NormalizeOrigin(approvedOrigin)
	if err != nil || approvedErr != nil || r.Origin != approved {
		return nil, r.problem("origin does not match the approved repository; restore its origin before syncing")
	}
	status, err := r.git(src, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, r.problem("cannot inspect local changes; check the repository with git status")
	}
	if status != "" {
		return nil, r.problem("has local changes:\n%s\nCommit or move your changes, then run nimbus sync again", status)
	}
	r.branch, err = r.git(src, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || !strings.HasPrefix(r.branch, "refs/heads/") {
		return nil, r.problem("has no checked-out branch; switch to the branch you want to sync")
	}
	upstream, err := r.git(src, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", r.branch)
	remote, remoteBranch, ok := strings.Cut(upstream, "\x00")
	if err != nil || !ok || remote != "origin" || !strings.HasPrefix(remoteBranch, "refs/heads/") {
		return nil, r.problem("branch must track a branch on origin; set its upstream before syncing")
	}
	r.remoteBranch = remoteBranch
	r.head, err = r.git(src, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, r.problem("cannot read its current commit")
	}
	if _, err := r.git(src, "rev-parse", "--verify", "@{upstream}"); err != nil {
		return nil, r.problem("upstream branch is missing locally; check it with git fetch origin before syncing")
	}
	if _, err := r.git(src, "merge-base", "--is-ancestor", r.head, "@{upstream}"); err != nil {
		return nil, r.problem("has local commits or divergent history; reconcile the branch with origin before syncing")
	}
	return r, nil
}

// Prepare fetches the selected branch and verifies that it can fast-forward.
// Prepare every repository before updating either working tree.
func (r *Repository) Prepare(src native.Source) error {
	r.target = ""
	if err := r.Check(src); err != nil {
		return err
	}
	if _, err := r.git(src, "fetch", "--no-tags", "--no-recurse-submodules", "origin", r.remoteBranch); err != nil {
		return r.problem("could not fetch origin; check your connection and Git authentication, then retry")
	}
	target, err := r.git(src, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return r.problem("could not read the fetched commit; retry the sync")
	}
	if _, err := r.git(src, "merge-base", "--is-ancestor", r.head, target); err != nil {
		return r.problem("cannot fast-forward to origin; reconcile local commits or divergent history before syncing")
	}
	r.target = target
	return nil
}

// Update applies only the prepared commit, after checking for intervening edits.
func (r *Repository) Update(src native.Source) error {
	if r.target == "" {
		return r.problem("has no prepared update")
	}
	if err := r.Check(src); err != nil {
		return err
	}
	if _, err := r.git(src, "-c", "merge.autoStash=false", "merge", "--ff-only", "--no-overwrite-ignore", "--no-edit", "--no-stat", r.target); err != nil {
		return r.problem("could not fast-forward; inspect git status and resolve the reported repository state before retrying")
	}
	current, err := Inspect(src, r.Name, r.Path, r.approvedOrigin)
	if err != nil {
		return err
	}
	if current.head != r.target || current.branch != r.branch || current.remoteBranch != r.remoteBranch || current.Path != r.Path {
		return r.problem("changed during the update; inspect git status before retrying")
	}
	return nil
}

// Check rejects changes since inspection without fetching or updating files.
func (r *Repository) Check(src native.Source) error {
	current, err := Inspect(src, r.Name, r.Path, r.approvedOrigin)
	if err != nil {
		return err
	}
	if current.head != r.head || current.branch != r.branch || current.remoteBranch != r.remoteBranch || current.Path != r.Path {
		return r.problem("changed while preparing updates; run nimbus sync again")
	}
	return nil
}

func (r *Repository) git(src native.Source, args ...string) (string, error) {
	out, err := src.Run("git", inspect.GitArgs(r.Path, args...)...)
	return strings.TrimSpace(string(out)), err
}

func (r *Repository) problem(format string, args ...any) error {
	return fmt.Errorf("%s repository (%s): %s", r.Name, r.Path, fmt.Sprintf(format, args...))
}

package git

import (
	"fmt"
	"os/exec"
	"strings"

	xerrors "github.com/thanhhaudev/llmreviewkit/errors"
)

func EnsureRepo(cwd string) error {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return xerrors.User(
			"not_a_git_repo",
			"not inside a git repository",
			"run from a git working tree, or 'git init' first",
		)
	}
	if strings.TrimSpace(string(out)) != "true" {
		return xerrors.User("not_a_git_repo", "not inside a git work tree", "")
	}
	return nil
}

func RootOf(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", xerrors.User("git_root", "cannot determine git root", "")
	}
	return strings.TrimSpace(string(out)), nil
}

// Diff produces the unified diff text for the given target.
// For TargetWorkingTree, untracked files are reported separately via UntrackedFiles().
func Diff(cwd string, t Target) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}

	var args []string
	switch t.Kind {
	case TargetWorkingTree:
		args = []string{"diff", "HEAD"}
	case TargetBranchDiff:
		args = []string{"diff", t.Base + "...HEAD"}
	case TargetCommit:
		// --format= suppresses commit message header so output is pure diff.
		args = []string{"show", "--format=", "--patch", t.Commit}
	case TargetCommitRange:
		args = []string{"diff", t.FromSHA + ".." + t.ToSHA}
	}

	if len(t.Paths) > 0 {
		args = append(args, "--")
		args = append(args, t.Paths...)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		// Working tree fallback: HEAD may not exist on a brand new repo.
		if t.Kind == TargetWorkingTree {
			c2 := exec.Command("git", "diff")
			c2.Dir = cwd
			staged, _ := c2.Output()
			return string(staged), nil
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return "", xerrors.User("git_diff_failed",
				fmt.Sprintf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr))),
				"check ref/commit exists")
		}
		return "", xerrors.Internal("git_diff", "git command error", err)
	}
	return string(out), nil
}

// UntrackedFiles returns untracked files honoring .gitignore.
// Only relevant for TargetWorkingTree.
func UntrackedFiles(cwd string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

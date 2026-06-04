package git

import (
	"fmt"

	xerrors "github.com/thanhhaudev/llmreviewkit/errors"
)

type TargetKind int

const (
	TargetWorkingTree TargetKind = iota + 1
	TargetBranchDiff
	TargetCommit
	TargetCommitRange
)

type Target struct {
	Kind    TargetKind
	Base    string
	Commit  string
	FromSHA string
	ToSHA   string
	Paths   []string
}

func (t Target) Label() string {
	switch t.Kind {
	case TargetWorkingTree:
		return "working tree"
	case TargetBranchDiff:
		return fmt.Sprintf("branch diff vs %s", t.Base)
	case TargetCommit:
		return fmt.Sprintf("commit %s", short(t.Commit))
	case TargetCommitRange:
		return fmt.Sprintf("commits %s..%s", short(t.FromSHA), short(t.ToSHA))
	}
	return "unknown"
}

func (t Target) Validate() error {
	switch t.Kind {
	case TargetWorkingTree:
		return nil
	case TargetBranchDiff:
		if t.Base == "" {
			return xerrors.User("missing_base", "--base is required for branch diff", "")
		}
	case TargetCommit:
		if t.Commit == "" {
			return xerrors.User("missing_commit", "--commit is required", "")
		}
	case TargetCommitRange:
		if t.FromSHA == "" || t.ToSHA == "" {
			return xerrors.User("missing_range", "--from and --to are both required", "")
		}
	default:
		return xerrors.User("bad_target", "unknown target kind", "")
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

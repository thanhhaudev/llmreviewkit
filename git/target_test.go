package git

import (
	"strings"
	"testing"
)

func TestTarget_Validate_WorkingTree(t *testing.T) {
	tgt := Target{Kind: TargetWorkingTree}
	if err := tgt.Validate(); err != nil {
		t.Errorf("working tree should validate, got: %v", err)
	}
}

func TestTarget_Validate_BranchDiff_RequiresBase(t *testing.T) {
	if err := (Target{Kind: TargetBranchDiff}).Validate(); err == nil {
		t.Error("branch diff without base should fail")
	}
	if err := (Target{Kind: TargetBranchDiff, Base: "main"}).Validate(); err != nil {
		t.Errorf("branch diff with base should pass, got: %v", err)
	}
}

func TestTarget_Validate_Commit_RequiresSHA(t *testing.T) {
	if err := (Target{Kind: TargetCommit}).Validate(); err == nil {
		t.Error("commit without SHA should fail")
	}
	if err := (Target{Kind: TargetCommit, Commit: "abc123"}).Validate(); err != nil {
		t.Errorf("commit with SHA should pass, got: %v", err)
	}
}

func TestTarget_Validate_CommitRange_RequiresBoth(t *testing.T) {
	if err := (Target{Kind: TargetCommitRange, FromSHA: "a"}).Validate(); err == nil {
		t.Error("range with only from should fail")
	}
	if err := (Target{Kind: TargetCommitRange, ToSHA: "b"}).Validate(); err == nil {
		t.Error("range with only to should fail")
	}
	if err := (Target{Kind: TargetCommitRange, FromSHA: "a", ToSHA: "b"}).Validate(); err != nil {
		t.Errorf("range with both should pass, got: %v", err)
	}
}

func TestTarget_Label(t *testing.T) {
	cases := []struct {
		t    Target
		want string
	}{
		{Target{Kind: TargetWorkingTree}, "working tree"},
		{Target{Kind: TargetBranchDiff, Base: "main"}, "branch diff vs main"},
		{Target{Kind: TargetCommit, Commit: "abcd1234ef"}, "commit abcd1234"},
		{Target{Kind: TargetCommitRange, FromSHA: "abcdef1234", ToSHA: "1234567890"}, "commits abcdef12..12345678"},
	}
	for _, tc := range cases {
		got := tc.t.Label()
		if !strings.HasPrefix(got, strings.SplitN(tc.want, " ", 2)[0]) {
			t.Errorf("Label() = %q, want prefix %q", got, tc.want)
		}
		if got != tc.want {
			t.Errorf("Label() = %q, want %q", got, tc.want)
		}
	}
}

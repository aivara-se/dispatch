package condition_test

import (
	"context"
	"testing"

	"github.com/aivara-se/dispatch/internal/condition"
	"github.com/aivara-se/dispatch/internal/github"
)

type source struct {
	pull    github.PullRequest
	reviews []github.Review
}

func (s source) PullRequest(context.Context, string, string, int) (github.PullRequest, error) {
	return s.pull, nil
}

func (s source) Reviews(context.Context, string, string, int) ([]github.Review, error) {
	return s.reviews, nil
}

func TestConditions(t *testing.T) {
	reader := condition.New(source{
		pull: github.PullRequest{Head: "s1", Merged: true},
		reviews: []github.Review{
			{Login: "momo", State: "APPROVED", CommitID: "s0"},
			{Login: "mama", State: "CHANGES_REQUESTED", CommitID: "s1"},
		},
	})
	cases := []struct {
		key   string
		start string
		want  bool
	}{
		{key: "pr.created", want: false},
		{key: "pr.approved", want: false},
		{key: "pr.rejected", want: true},
		{key: "pr.updated", start: "s0", want: true},
		{key: "pr.updated", start: "s1", want: false},
		{key: "pr.merged", want: true},
	}
	for _, tc := range cases {
		got, read, err := reader.Holds(context.Background(), tc.key, "aivara-se", "dispatch", 212, tc.start)
		if err != nil {
			t.Fatalf("%s: %v", tc.key, err)
		}
		if got != tc.want {
			t.Fatalf("%s start=%q = %v, want %v", tc.key, tc.start, got, tc.want)
		}
		if read == nil {
			t.Fatalf("%s: no evidence", tc.key)
		}
	}
}

func TestTheApprovalHoldsEachReviewersVerdict(t *testing.T) {
	cases := []struct {
		name     string
		reviews  []github.Review
		approved bool
		rejected bool
	}{
		{
			name: "a comment leaves an approval standing",
			reviews: []github.Review{
				{Login: "momo", State: "APPROVED", CommitID: "s1"},
				{Login: "momo", State: "COMMENTED", CommitID: "s1"},
			},
			approved: true,
		},
		{
			name: "a pending review leaves an approval standing",
			reviews: []github.Review{
				{Login: "momo", State: "APPROVED", CommitID: "s1"},
				{Login: "momo", State: "PENDING", CommitID: "s1"},
			},
			approved: true,
		},
		{
			name: "a change request holds against another reviewer's approval",
			reviews: []github.Review{
				{Login: "mama", State: "CHANGES_REQUESTED", CommitID: "s1"},
				{Login: "momo", State: "APPROVED", CommitID: "s1"},
			},
			rejected: true,
		},
		{
			name: "a reviewer's later approval replaces the change request",
			reviews: []github.Review{
				{Login: "mama", State: "CHANGES_REQUESTED", CommitID: "s1"},
				{Login: "mama", State: "APPROVED", CommitID: "s1"},
			},
			approved: true,
		},
		{
			name: "a dismissed review is no verdict",
			reviews: []github.Review{
				{Login: "momo", State: "APPROVED", CommitID: "s1"},
				{Login: "momo", State: "DISMISSED", CommitID: "s1"},
			},
		},
		{
			name: "a verdict on another head is not read",
			reviews: []github.Review{
				{Login: "momo", State: "APPROVED", CommitID: "s0"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := condition.New(source{pull: github.PullRequest{Head: "s1"}, reviews: tc.reviews})
			for key, want := range map[string]bool{"pr.approved": tc.approved, "pr.rejected": tc.rejected} {
				got, read, err := reader.Holds(context.Background(), key, "aivara-se", "dispatch", 212, "s0")
				if err != nil {
					t.Fatalf("%s: %v", key, err)
				}
				if got != want {
					t.Fatalf("%s = %v, want %v", key, got, want)
				}
				if read == nil {
					t.Fatalf("%s: no evidence", key)
				}
			}
		})
	}
}

func TestUnknownCondition(t *testing.T) {
	reader := condition.New(source{})
	if _, _, err := reader.Holds(context.Background(), "pr.draft", "aivara-se", "dispatch", 1, ""); err == nil {
		t.Fatal("unknown condition accepted")
	}
}

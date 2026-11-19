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
			{State: "APPROVED", CommitID: "s0"},
			{State: "CHANGES_REQUESTED", CommitID: "s1"},
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

func TestApprovedIsTheLastReviewOnTheHead(t *testing.T) {
	reader := condition.New(source{
		pull: github.PullRequest{Head: "s1"},
		reviews: []github.Review{
			{State: "CHANGES_REQUESTED", CommitID: "s1"},
			{State: "APPROVED", CommitID: "s1"},
			{State: "COMMENTED", CommitID: "s0"},
		},
	})
	got, _, err := reader.Holds(context.Background(), "pr.approved", "aivara-se", "dispatch", 212, "s0")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("the last review on the head is an approval")
	}
}

func TestUnknownCondition(t *testing.T) {
	reader := condition.New(source{})
	if _, _, err := reader.Holds(context.Background(), "pr.draft", "aivara-se", "dispatch", 1, ""); err == nil {
		t.Fatal("unknown condition accepted")
	}
}

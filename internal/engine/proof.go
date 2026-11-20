package engine

import (
	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/webhook"
)

// proof is what a delivery proves: the conditions that hold, and the logins
// each login condition names.
type proof struct {
	holds  map[string]bool
	logins map[string][]string
}

// triggers maps a pull request action to the condition it proves.
var triggers = map[string]string{
	"opened":           config.ConditionCreated,
	"review_requested": config.ConditionReviewRequested,
	"assigned":         config.ConditionAssigned,
}

// verdicts maps a submitted review's state to the condition it proves. A
// comment or a pending review proves neither.
var verdicts = map[string]string{
	"approved":          config.ConditionApproved,
	"changes_requested": config.ConditionRejected,
}

// proved is the set of conditions a delivery proves. pr.updated and pr.merged
// are read after a task, never from a delivery, so neither is here. An opened
// pull request that names reviewers proves a review request as well.
func proved(event webhook.Event) proof {
	out := proof{holds: map[string]bool{}, logins: map[string][]string{}}
	if event.Kind != webhook.KindPullRequest && event.Kind != webhook.KindPullRequestReview {
		return out
	}
	key := triggers[event.Action]
	if event.Kind == webhook.KindPullRequestReview {
		key = ""
		if event.Action == "submitted" {
			key = verdicts[event.Review]
		}
	}
	if key != "" {
		out.holds[key] = true
		if config.PerLogin(key) {
			out.logins[key] = event.Logins
		}
	}
	if event.Action == "opened" && len(event.Logins) > 0 {
		out.holds[config.ConditionReviewRequested] = true
		out.logins[config.ConditionReviewRequested] = event.Logins
	}
	return out
}

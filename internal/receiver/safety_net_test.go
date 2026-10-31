package receiver

// The safety net for splitting this package into a receiver, a dispatcher and a
// scheduler. Three things the rest of the suite leaves open:
//
//  1. the caps are not pinned in literals — every other test loops over the
//     constant, so a changed constant moves the test with it;
//  2. the ten dispatch types are enumerated in one table and counted in
//     nothing, so a deleted row leaves a shorter table that still passes;
//  3. every test is one or two deliveries, so nothing fixes the order of the
//     audit lines across a burst.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/audit"
)

// TestTheDocumentedCapsAreTheShippedOnes pins what the documents promise, in
// literals. PRODUCT.md: ten rounds, five wakes per pull request, five per
// branch. SYSTEMS.md: a 1 MiB body. ADR 024: an hour of dedup.
func TestTheDocumentedCapsAreTheShippedOnes(t *testing.T) {
	if MaxRounds != 10 {
		t.Errorf("MaxRounds = %d, want 10: the review cap PRODUCT.md states", MaxRounds)
	}
	if MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5: the failed-check cap PRODUCT.md states", MaxFailures)
	}
	if MaxBody != 1<<20 {
		t.Errorf("MaxBody = %d, want 1 MiB: the bound SYSTEMS.md states", MaxBody)
	}
	if DedupTTL != time.Hour {
		t.Errorf("DedupTTL = %v, want 1 h: the window ADR 024 states", DedupTTL)
	}

	// The boundary in literals: ten changes-requested reviews wake, the
	// eleventh wakes nobody. A constant that drifts from ten fails here even
	// though TestCapStopsWakingAfterTenRounds still passes.
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	for i := 1; i <= 10; i++ {
		post(rc, "pull_request_review", fmt.Sprintf("r%d", i),
			changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama"))
	}
	if len(runner.calls) != 10 {
		t.Fatalf("runs started = %d after ten reviews, want 10", len(runner.calls))
	}
	post(rc, "pull_request_review", "r11",
		changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama"))
	if len(runner.calls) != 10 {
		t.Fatalf("runs started = %d after the eleventh, want 10", len(runner.calls))
	}
	last := recorder.records[len(recorder.records)-1]
	if last.Decision != audit.Ignored || !strings.Contains(last.Why, "10-round cap") {
		t.Fatalf("record = %+v, want ignored by the 10-round cap", last)
	}

	// The same boundary for wakes: five failed checks wake, the sixth does not.
	runner = &stubRunner{}
	rc, recorder = newReceiver(runner)
	for i := 1; i <= 5; i++ {
		post(rc, "workflow_run", fmt.Sprintf("f%d", i),
			workflowRunBody("completed", "failure", "thani-sh-meme", 105))
	}
	if len(runner.calls) != 5 {
		t.Fatalf("runs started = %d after five failures, want 5", len(runner.calls))
	}
	post(rc, "workflow_run", "f6", workflowRunBody("completed", "failure", "thani-sh-meme", 105))
	if len(runner.calls) != 5 {
		t.Fatalf("runs started = %d after the sixth, want 5", len(runner.calls))
	}
	last = recorder.records[len(recorder.records)-1]
	if last.Decision != audit.Ignored || !strings.Contains(last.Why, "5-wake cap") {
		t.Fatalf("record = %+v, want ignored by the 5-wake cap", last)
	}
}

// TestEveryDispatchTypeIsReachableFromASignedDelivery enumerates the ten
// sentences SYSTEMS.md ("The reason") lists, one row each, and checks that a
// signed delivery still starts a run with that reason and no other. What it adds
// to TestEndToEndSignedPostStartsRun, which posts the same ten and pins the
// argv, is the count — a deleted row fails here instead of leaving a shorter
// table — and the two-wake ready_for_review delivery. It runs on the stub
// runner, so the count is checked on every run without a process.
func TestEveryDispatchTypeIsReachableFromASignedDelivery(t *testing.T) {
	cases := []struct {
		name     string
		event    string
		body     string
		profiles []string
		reason   string
	}{
		{
			name:     "review_requested",
			event:    "pull_request",
			body:     reviewBody("review_requested", "thani-sh-mama", "thani-sh-meme"),
			profiles: []string{"meme"},
			reason:   "Dispatch: review requested on aivara-se/dispatch#105",
		},
		{
			name:     "changes_requested",
			event:    "pull_request_review",
			body:     changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama"),
			profiles: []string{"mama"},
			reason:   "Dispatch: changes requested on aivara-se/dispatch#105",
		},
		{
			name:     "approved_unarmed",
			event:    "pull_request_review",
			body:     reviewSurfaceBody("submitted", "approved", "MEMBER", "thani-sh-meme", "thani-sh-mama", "ok"),
			profiles: []string{"mama"},
			reason:   "Dispatch: approved but not armed on aivara-se/dispatch#105",
		},
		{
			name:     "issue_assigned",
			event:    "issues",
			body:     issueBody("assigned", "thani-sh-meme", "thani-sh-mama"),
			profiles: []string{"mama"},
			reason:   "Dispatch: issue assigned on aivara-se/dispatch#124",
		},
		{
			name:     "pull_request_assigned",
			event:    "pull_request",
			body:     prAssignBody("assigned", "thani-sh-meme", "thani-sh-mama"),
			profiles: []string{"mama"},
			reason:   "Dispatch: pull request assigned on aivara-se/dispatch#105",
		},
		{
			name:     "checks_failed",
			event:    "workflow_run",
			body:     workflowRunBody("completed", "failure", "thani-sh-meme", 105),
			profiles: []string{"meme"},
			reason:   "Dispatch: checks failed on aivara-se/dispatch#105",
		},
		{
			name:     "red_main",
			event:    "workflow_run",
			body:     pushRunBody(testRepo, "push", "completed", "failure", "main", "main", "thani-sh-meme"),
			profiles: []string{"meme"},
			reason:   "Dispatch: checks failed on aivara-se/dispatch@main",
		},
		{
			name:     "mentioned",
			event:    "issue_comment",
			body:     commentBody(124, "created", "MEMBER", "thani-sh-meme", "@thani-sh-mama please look"),
			profiles: []string{"mama"},
			reason:   "Dispatch: mentioned on aivara-se/dispatch#124",
		},
		{
			name:     "ready_for_review",
			event:    "pull_request",
			body:     readyBody("thani-sh", "thani-sh-meme", "thani-sh-mama"),
			profiles: []string{"mama", "meme"},
			reason:   "Dispatch: ready for review on aivara-se/dispatch#105",
		},
		{
			name:     "approval_dismissed",
			event:    "pull_request_review",
			body:     dismissedBody(false, "open", "null", "thani-sh-meme", "thani-sh-mama"),
			profiles: []string{"mama"},
			reason:   "Dispatch: approval dismissed on aivara-se/dispatch#105",
		},
	}

	if len(cases) != 10 {
		t.Fatalf("dispatch types = %d, want the ten sentences SYSTEMS.md lists", len(cases))
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			if w := post(rc, c.event, "d1", c.body); w.Code != 202 {
				t.Fatalf("status = %d, want 202", w.Code)
			}

			if len(runner.calls) != len(c.profiles) {
				t.Fatalf("runs started = %+v, want %d", runner.calls, len(c.profiles))
			}
			for i, profile := range c.profiles {
				if runner.calls[i].profile != profile {
					t.Errorf("calls[%d].profile = %q, want %q", i, runner.calls[i].profile, profile)
				}
				if runner.calls[i].reason != c.reason {
					t.Errorf("calls[%d].reason = %q, want %q", i, runner.calls[i].reason, c.reason)
				}
			}
			rec := recorder.records[len(recorder.records)-1]
			if rec.Decision != audit.Started {
				t.Fatalf("decision = %q (%s), want started", rec.Decision, rec.Why)
			}
			if rec.Profile != strings.Join(c.profiles, ",") {
				t.Errorf("profiles = %q, want %q", rec.Profile, strings.Join(c.profiles, ","))
			}
		})
	}
}

// TestTheAuditLinesOfAMixedBurst pins the record itself: the order, the fields
// each kind of line carries, and the exact JSON an operator reads. A burst is
// what a scheduler rewrite perturbs — a line that moves, loses its answer or
// gains an event field fails here, byte for byte.
func TestTheAuditLinesOfAMixedBurst(t *testing.T) {
	runner := &liveRunner{}
	rc, recorder := newReceiver(runner)

	// 1. A review requested from meme, started.
	first := stamp(reviewBody("review_requested", "thani-sh-mama", "thani-sh-meme"), "d1")
	postSigned(rc, "pull_request", "d1", first, sign(first))

	// 2. The same bytes under a fresh delivery id: the dedup sees them.
	postSigned(rc, "pull_request", "d2", first, sign(first))

	// 3. A changes-requested review from meme, waking mama.
	post(rc, "pull_request_review", "d3",
		changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama"))

	// 4. A second one while mama's run is live: held, not started.
	post(rc, "pull_request_review", "d4",
		changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama"))

	// 5. Mama's run exits zero: the held delivery's follow-up starts, and a
	// zero exit writes no line of its own.
	runner.exit(1, 0)

	// 6. Meme's run exits non-zero: a line of its own, carrying d1.
	runner.exit(0, 1)

	// 7. A failed check for meme, now that its key is free: started.
	post(rc, "workflow_run", "d7", workflowRunBody("completed", "failure", "thani-sh-meme", 105))

	var got strings.Builder
	for _, r := range recorder.records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got.Write(line)
		got.WriteByte('\n')
	}

	want := `{"delivery":"d1","event":"pull_request","repository":"aivara-se/dispatch","number":105,"decision":"started","profile":"meme","reason":"Dispatch: review requested on aivara-se/dispatch#105","answer":202}
{"delivery":"d2","event":"pull_request","repository":"aivara-se/dispatch","number":105,"decision":"duplicate","why":"the body was accepted before","answer":202}
{"delivery":"d3","event":"pull_request_review","repository":"aivara-se/dispatch","number":105,"decision":"started","profile":"mama","reason":"Dispatch: changes requested on aivara-se/dispatch#105","answer":202}
{"delivery":"d4","event":"pull_request_review","repository":"aivara-se/dispatch","number":105,"decision":"held","profile":"mama","reason":"Dispatch: changes requested on aivara-se/dispatch#105","why":"held for aivara-se/dispatch#105","answer":202}
{"delivery":"d4","decision":"started","profile":"mama","reason":"Dispatch: changes requested on aivara-se/dispatch#105"}
{"delivery":"d1","profile":"meme","exit":1}
{"delivery":"d7","event":"workflow_run","repository":"aivara-se/dispatch","number":105,"decision":"started","profile":"meme","reason":"Dispatch: checks failed on aivara-se/dispatch#105","answer":202}
`
	if got.String() != want {
		t.Fatalf("audit lines changed:\n--- got ---\n%s--- want ---\n%s", got.String(), want)
	}
}

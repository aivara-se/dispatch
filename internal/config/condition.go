package config

// The closed condition set a flow may name.
const (
	ConditionCreated         = "pr.created"
	ConditionApproved        = "pr.approved"
	ConditionRejected        = "pr.rejected"
	ConditionUpdated         = "pr.updated"
	ConditionMerged          = "pr.merged"
	ConditionReviewRequested = "pr.review_requested"
	ConditionAssigned        = "pr.assigned"
)

// The action types a task may run, and the name the one named action takes.
const (
	ActionPrompt = "prompt"
	ActionScript = "script"
	ActionNamed  = "action"
	ActionLabel  = "label"
	LabelBlocked = "blocked"
)

// conditionFacts is what the loader and the engine must know about a
// condition: whether a delivery proves it, and whether it names a login
// rather than a pull request.
var conditionFacts = map[string]struct {
	proven   bool
	perLogin bool
}{
	ConditionCreated:         {proven: true},
	ConditionApproved:        {proven: true},
	ConditionRejected:        {proven: true},
	ConditionUpdated:         {},
	ConditionMerged:          {},
	ConditionReviewRequested: {proven: true, perLogin: true},
	ConditionAssigned:        {proven: true, perLogin: true},
}

// ValidCondition reports whether name is a condition a flow may name.
func ValidCondition(name string) bool {
	_, ok := conditionFacts[name]
	return ok
}

// Proven reports whether a delivery proves key, so _start_.next may name it.
// pr.updated and pr.merged are read after a task, never from a delivery.
func Proven(key string) bool {
	return conditionFacts[key].proven
}

// PerLogin reports whether key starts one job per login an agent holds,
// rather than one job per pull request.
func PerLogin(key string) bool {
	return conditionFacts[key].perLogin
}

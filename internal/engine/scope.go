package engine

import "strings"

// scope is the set of repositories a delivery may start work in, written as
// owner/name, or owner/* for every repository of an owner.
type scope []pattern

type pattern struct {
	owner string
	name  string
}

func serve(repos []string) scope {
	out := make(scope, 0, len(repos))
	for _, repo := range repos {
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || owner == "" || name == "" {
			continue
		}
		out = append(out, pattern{owner: owner, name: name})
	}
	return out
}

func (s scope) serves(owner, repo string) bool {
	for _, pattern := range s {
		if !strings.EqualFold(pattern.owner, owner) {
			continue
		}
		if pattern.name == "*" || strings.EqualFold(pattern.name, repo) {
			return true
		}
	}
	return false
}

package hermes

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// sessionID reads the last session_id line Hermes writes to stderr.
func sessionID(stderr string) string {
	id := ""
	for _, line := range strings.Split(stderr, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "session_id:"); ok {
			id = strings.TrimSpace(value)
		}
	}
	return id
}

func (r *Runner) stateDB(profile string) string {
	if profile == "" || profile == "default" {
		return filepath.Join(r.home, "state.db")
	}
	return filepath.Join(r.home, "profiles", profile, "state.db")
}

// tools reads the session's tool calls. A missing session or store leaves
// them empty; the action's output stands on its own.
func (r *Runner) tools(profile, session string) (string, error) {
	if session == "" || r.home == "" {
		return "", nil
	}
	path := r.stateDB(profile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT tool_calls FROM messages WHERE session_id = ? AND tool_calls IS NOT NULL ORDER BY id`, session)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var calls []json.RawMessage
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			continue
		}
		calls = append(calls, items...)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(calls) == 0 {
		return "", nil
	}
	body, err := json.Marshal(calls)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Package audit appends one JSON Lines record per verified delivery.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

// MaxSize is the size at which the audit file rotates.
const MaxSize = 10 << 20

// Decision is what the service did with a delivery.
const (
	Started     = "started"
	Held        = "held"
	Superseded  = "superseded"
	Ignored     = "ignored"
	Duplicate   = "duplicate"
	StartFailed = "start_failed"
)

// Record is one line of the audit. A delivery's line carries the delivery and
// what the service did with it — `held` when a run already holds its key, whose
// follow-up appends a line of its own carrying the same delivery id, and
// `superseded` when a later delivery for that key replaces it
// ([`021`](docs/adrs/021-one-run-per-pull-request-per-agent.md),
// [`024`](docs/adrs/024-a-superseded-delivery-names-its-replacement.md)); a
// run's line carries the delivery it was started for, the profile and the exit
// status; a boot's line carries the service's own start
// ([`015`](docs/adrs/015-the-audit-records-a-runs-exit.md),
// [`018`](docs/adrs/018-the-services-boot-reaches-the-audit.md)). It never
// carries a secret value or a payload body.
type Record struct {
	Delivery   string `json:"delivery,omitempty"`
	Event      string `json:"event,omitempty"`
	Repository string `json:"repository,omitempty"`
	Number     int    `json:"number,omitempty"`
	Decision   string `json:"decision,omitempty"`
	Profile    string `json:"profile,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Why        string `json:"why,omitempty"`
	Answer     int    `json:"answer,omitempty"`
	Exit       int    `json:"exit,omitempty"`
	Boot       *Boot  `json:"boot,omitempty"`
}

// Boot is the one line the service appends for itself when it starts: when,
// the build it runs, where it listens, and how many agents it knows.
type Boot struct {
	At       string `json:"at"`
	Revision string `json:"revision"`
	Dirty    string `json:"dirty"`
	Listen   string `json:"listen"`
	Agents   int    `json:"agents"`
}

// Unknown is what a binary with no VCS metadata says about itself.
const Unknown = "unknown"

// BootRecord returns the service's line at startup, read from the binary's own
// build info.
func BootRecord(listen string, agents int, now time.Time) Record {
	revision, dirty := buildInfo()
	return Record{Boot: &Boot{
		At:       now.UTC().Format(time.RFC3339),
		Revision: revision,
		Dirty:    dirty,
		Listen:   listen,
		Agents:   agents,
	}}
}

// buildInfo returns the revision the binary was built from and whether its
// tree was modified. A build with no VCS metadata says unknown for both.
func buildInfo() (revision, dirty string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Unknown, Unknown
	}
	revision, dirty = Unknown, Unknown
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value
		}
	}
	return revision, dirty
}

// Log is the audit file, rotated once past its size bound.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	path string
	size int64
	max  int64
}

// Open opens the audit file at path, appending to it if it exists.
func Open(path string) (*Log, error) {
	return openLog(path, MaxSize)
}

func openLog(path string, max int64) (*Log, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat audit file: %w", err)
	}
	return &Log{f: f, path: path, size: info.Size(), max: max}, nil
}

// Append writes one line for r, rotating first when the line would pass the
// size bound.
func (l *Log) Append(r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.size > 0 && l.size+int64(len(line)) > l.max {
		if err := l.rotate(); err != nil {
			return err
		}
	}
	n, err := l.f.Write(line)
	l.size += int64(n)
	if err != nil {
		return fmt.Errorf("write audit record: %w", err)
	}
	return nil
}

// Close closes the audit file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

func (l *Log) rotate() error {
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("close audit file for rotation: %w", err)
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return fmt.Errorf("rotate audit file: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("reopen audit file: %w", err)
	}
	l.f = f
	l.size = 0
	return nil
}

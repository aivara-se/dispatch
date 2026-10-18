// Package audit writes the file the service records itself in: the audit log,
// one JSON line per look.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Line is one look's record: the trigger that caused it, the deliveries it
// carried, the decision made, the card and the assignee it concerns, the run
// it started with its tag, and the answer the sender got. A zero field
// means the thing was absent, and is written as absent. The tag is the run's
// own handle: the value the service started the run with, the one
// DISPATCHER_RUN carries in that run's own process.
type Line struct {
	Trigger    string   `json:"trigger,omitempty"`
	Deliveries []string `json:"deliveries,omitempty"`
	Decision   string   `json:"decision,omitempty"`
	Card       int      `json:"card,omitempty"`
	Assignee   string   `json:"assignee,omitempty"`
	Run        string   `json:"run,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	Answer     int      `json:"answer,omitempty"`
}

// Log appends to the audit file. The file rotates when a line would take it
// past maxBytes: the file is renamed with `.1` inserted before its extension,
// replacing the generation before it, so one full generation is kept and the
// file stays bounded.
type Log struct {
	mu       sync.Mutex
	maxBytes int64
	audit    file
}

// file is one append-only file the Log writes, with the size of what it holds.
type file struct {
	path string
	f    *os.File
	size int64
}

// Open opens the audit file at path, creating it. A maxBytes that is not
// positive is refused; a line larger than maxBytes is still written, so no
// record is dropped by rotation.
func Open(path string, maxBytes int64) (*Log, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes %d is not positive", maxBytes)
	}
	auditFile, err := openFile(path)
	if err != nil {
		return nil, err
	}
	return &Log{maxBytes: maxBytes, audit: *auditFile}, nil
}

// Append writes one line to the audit file.
func (l *Log) Append(line Line) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.audit.write(line, l.maxBytes)
}

// Close closes the file. A write after Close is an error.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.audit.f.Close(); err != nil {
		return fmt.Errorf("close audit: %w", err)
	}
	return nil
}

// openFile opens one output file in append mode, creating it if absent, and
// records the size of what it already holds.
func openFile(path string) (*file, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return &file{path: path, f: f, size: info.Size()}, nil
}

// write appends one record as a JSON line, rotating the file first when the
// line would take it past maxBytes.
func (f *file) write(record any, maxBytes int64) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	if f.size > 0 && f.size+int64(len(data)) > maxBytes {
		if err := f.rotate(); err != nil {
			return err
		}
	}
	n, err := f.f.Write(data)
	if err != nil {
		return fmt.Errorf("append %s: %w", f.path, err)
	}
	f.size += int64(n)
	return nil
}

// rotate renames the file to its `.1` name and opens a fresh file in its
// place, so what was written stays on disk untouched.
func (f *file) rotate() error {
	if err := os.Rename(f.path, insertBeforeExt(f.path, ".1")); err != nil {
		return fmt.Errorf("rotate %s: %w", f.path, err)
	}
	if err := f.f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.path, err)
	}
	next, err := openFile(f.path)
	if err != nil {
		return err
	}
	f.f = next.f
	f.size = next.size
	return nil
}

// insertBeforeExt returns path with insert placed before its extension, so
// `log.jsonl` becomes `log<insert>.jsonl` and stays the kind of file it was.
func insertBeforeExt(path, insert string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + insert + ext
}

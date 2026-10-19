package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendWritesOneLinePerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	records := []Record{
		{Delivery: "d1", Event: "ping", Decision: Ignored, Why: "not a review request", Answer: 202},
		{Delivery: "d2", Event: "pull_request", Repository: "aivara-se/dispatch", Number: 105, Decision: Started, Profile: "meme", Reason: "Dispatch: review requested on aivara-se/dispatch#105", Answer: 202},
	}
	for _, r := range records {
		if err := log.Append(r); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(records) {
		t.Fatalf("lines = %d, want %d", len(lines), len(records))
	}

	var got Record
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatal(err)
	}
	if got != records[1] {
		t.Fatalf("second record = %+v, want %+v", got, records[1])
	}
}

func TestAppendRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log, err := openLog(path, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	record := Record{Delivery: "d1", Event: "ping", Decision: Ignored, Answer: 202}
	if err := log.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(record); err != nil {
		t.Fatal(err)
	}

	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("rotated file: %v", err)
	}
	current, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Size() == 0 {
		t.Fatal("rotated file is empty")
	}
	if rotated.Size() != current.Size() {
		t.Fatalf("rotated %d bytes, current %d: want one record each", rotated.Size(), current.Size())
	}
	for _, name := range []string{path, path + ".1"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if lines := strings.Count(string(data), "\n"); lines != 1 {
			t.Fatalf("%s holds %d lines, want 1", name, lines)
		}
	}
}

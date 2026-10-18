package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// open opens a Log on a fresh audit path and returns it with that path.
func open(t *testing.T, maxBytes int64) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log, err := Open(path, maxBytes)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return log, path
}

// lines reads one file's JSON lines back as strings, without the newline each
// ends with.
func lines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func TestAppendWritesOneJSONLinePerLook(t *testing.T) {
	log, path := open(t, 1<<20)
	records := []Line{
		{Trigger: "boot", Decision: "woken meme for card 69"},
		{Trigger: "delivery", Deliveries: []string{"d-1"}, Decision: "assigned", Card: 69, Assignee: "thani-sh-meme", Run: "run-1", Tag: "tag-1", Answer: 202},
	}
	for _, record := range records {
		if err := log.Append(record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got := lines(t, path)
	if len(got) != len(records) {
		t.Fatalf("audit file holds %d lines, want %d", len(got), len(records))
	}
	for i, want := range records {
		var have Line
		if err := json.Unmarshal([]byte(got[i]), &have); err != nil {
			t.Fatalf("line %d does not decode: %v", i, err)
		}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("line %d gave %+v, want %+v", i, have, want)
		}
	}
}

func TestZeroFieldsAreWrittenAsAbsent(t *testing.T) {
	log, path := open(t, 1<<20)
	if err := log.Append(Line{Trigger: "boot"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"trigger\":\"boot\"}\n" {
		t.Errorf("audit file holds %q, want only the trigger", data)
	}
}

func TestDeadWritesToTheDeadLetterFileNotTheAuditFile(t *testing.T) {
	log, path := open(t, 1<<20)
	dir := filepath.Dir(path)
	miss := Miss{Reason: "board read failed", Line: Line{Trigger: "boot", Card: 69}}
	if err := log.Dead(miss); err != nil {
		t.Fatalf("dead: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadLetter := filepath.Join(dir, "audit.dead.jsonl")
	auditLines := lines(t, path)
	if len(auditLines) != 0 {
		t.Errorf("audit file holds %q, want nothing", auditLines)
	}
	deadLines := lines(t, deadLetter)
	if len(deadLines) != 1 {
		t.Fatalf("dead-letter file holds %d lines, want 1", len(deadLines))
	}
	var have Miss
	if err := json.Unmarshal([]byte(deadLines[0]), &have); err != nil {
		t.Fatalf("the miss does not decode: %v", err)
	}
	if !reflect.DeepEqual(have, miss) {
		t.Errorf("the miss gave %+v, want %+v", have, miss)
	}
}

func TestFilesAppendAcrossReopens(t *testing.T) {
	log, path := open(t, 1<<20)
	if err := log.Append(Line{Trigger: "boot"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	log, err := Open(path, 1<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := log.Append(Line{Trigger: "delivery"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got := lines(t, path)
	want := []string{"{\"trigger\":\"boot\"}", "{\"trigger\":\"delivery\"}"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("audit file holds %q, want %q in arrival order", got, want)
	}
}

// TestRotationKeepsOneGeneration appends five 16-byte lines with maxBytes 40:
// the first two fill the file, the third rotates it away, the fourth and fifth
// repeat that, so the `.1` file holds the third and fourth lines and the
// current file holds the fifth.
func TestRotationKeepsOneGeneration(t *testing.T) {
	log, path := open(t, 40)
	for _, trigger := range []string{"a", "b", "c", "d", "e"} {
		if err := log.Append(Line{Trigger: trigger}); err != nil {
			t.Fatalf("append %s: %v", trigger, err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	rotated := lines(t, filepath.Join(filepath.Dir(path), "audit.1.jsonl"))
	current := lines(t, path)
	if len(rotated) != 2 || !strings.Contains(rotated[0], "\"c\"") || !strings.Contains(rotated[1], "\"d\"") {
		t.Errorf("rotated file holds %q, want the c and d lines", rotated)
	}
	if len(current) != 1 || !strings.Contains(current[0], "\"e\"") {
		t.Errorf("current file holds %q, want the e line", current)
	}
}

func TestTheDeadLetterFileRotatesToo(t *testing.T) {
	log, path := open(t, 100)
	for i := 0; i < 3; i++ {
		if err := log.Dead(Miss{Reason: "reason", Line: Line{Trigger: "boot"}}); err != nil {
			t.Fatalf("dead: %v", err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	dir := filepath.Dir(path)
	if got := lines(t, filepath.Join(dir, "audit.dead.1.jsonl")); len(got) != 2 {
		t.Errorf("rotated dead-letter file holds %q, want two lines", got)
	}
	if got := lines(t, filepath.Join(dir, "audit.dead.jsonl")); len(got) != 1 {
		t.Errorf("dead-letter file holds %q, want one line", got)
	}
}

func TestALineLargerThanMaxBytesIsStillWritten(t *testing.T) {
	log, path := open(t, 8)
	if err := log.Append(Line{Trigger: "boot", Decision: "woken meme for card 69"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := lines(t, path); len(got) != 1 {
		t.Errorf("audit file holds %q, want the one oversized line", got)
	}
}

func TestOpenRefusesAMaxBytesThatIsNotPositive(t *testing.T) {
	for _, maxBytes := range []int64{0, -1} {
		if _, err := Open(filepath.Join(t.TempDir(), "audit.jsonl"), maxBytes); err == nil {
			t.Errorf("open accepted maxBytes %d", maxBytes)
		}
	}
}

func TestWritingAfterCloseIsAnError(t *testing.T) {
	log, path := open(t, 1<<20)
	if err := log.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := log.Append(Line{Trigger: "boot"}); err == nil {
		t.Error("append after close succeeded")
	}
	if err := log.Dead(Miss{Reason: "reason"}); err == nil {
		t.Error("dead after close succeeded")
	}
	if got := lines(t, path); len(got) != 0 {
		t.Errorf("audit file holds %q, want nothing", got)
	}
}

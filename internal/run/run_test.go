package run

import (
	"testing"
	"time"
)

// TestStartReportsTheExitStatus pins the one fact the audit's run line needs:
// a child that runs is waited on, and its status reaches the caller once.
func TestStartReportsTheExitStatus(t *testing.T) {
	cases := []struct {
		name string
		path string
		want int
	}{
		{"zero", "/bin/true", 0},
		{"non-zero", "/bin/false", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exits := make(chan int, 1)
			err := New(tc.path).Start("meme", "Dispatch: a reason", func(exit int) { exits <- exit })
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			select {
			case got := <-exits:
				if got != tc.want {
					t.Fatalf("exit = %d, want %d", got, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the child's exit status was never reported")
			}
		})
	}
}

func TestStartThatCannotStart(t *testing.T) {
	err := New("/nonexistent/hermes").Start("meme", "Dispatch: a reason", func(int) {})
	if err == nil {
		t.Fatal("Start of a missing binary returned no error")
	}
}

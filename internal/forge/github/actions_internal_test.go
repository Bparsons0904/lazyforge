package github

import (
	"testing"
	"time"
)

func TestRunDuration(t *testing.T) {
	start := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rn   run
		want time.Duration
	}{
		{"completed", run{Status: "completed", RunStartedAt: start, UpdatedAt: start.Add(time.Minute)}, time.Minute},
		{"in progress", run{Status: "in_progress", RunStartedAt: start, UpdatedAt: start.Add(time.Minute)}, 0},
		// A run GitHub never started (e.g. skipped at queue time) can arrive with no run_started_at.
		{"completed without a start", run{Status: "completed", UpdatedAt: start}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runToDomain(tt.rn).Duration; got != tt.want {
				t.Fatalf("Duration %v, want %v", got, tt.want)
			}
		})
	}
}

package core

import (
	"strings"
	"testing"
	"time"
)

func TestStatsReportUsesWallClockForShare(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(100 * time.Millisecond)
	s := NewStats()
	s.now = func() time.Time {
		if s.wallStart.IsZero() {
			return start
		}
		return end
	}
	s.begin()
	s.add("one", &chatResponse{TotalDuration: int64(80 * time.Millisecond)})
	s.add("two", &chatResponse{TotalDuration: int64(80 * time.Millisecond)})
	s.end()

	report := s.Report()
	if !strings.Contains(report, "model wall clock 100ms") {
		t.Fatalf("report does not show measured wall clock:\n%s", report)
	}
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "one ") || strings.HasPrefix(line, "two ") {
			if !strings.Contains(line, "80%") {
				t.Fatalf("call share was not divided by wall clock: %q", line)
			}
		}
	}
}

func TestStatsReportIncludesElapsedCalls(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(100 * time.Millisecond)
	s := NewStats()
	s.now = func() time.Time {
		if s.wallStart.IsZero() {
			return start
		}
		return end
	}
	s.begin()
	s.addElapsed("embed", 30*time.Millisecond)
	s.end()

	report := s.Report()
	if !strings.Contains(report, "embed") || !strings.Contains(report, "30%") {
		t.Fatalf("report omitted elapsed embedding call:\n%s", report)
	}
}

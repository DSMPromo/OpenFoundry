package handlers

import (
	"context"
	"testing"
	"time"
)

func schedulerTestDef(id string, sched ReportSchedule) ReportDefinition {
	return ReportDefinition{
		ID:            id,
		Name:          "Scheduled " + id,
		Owner:         "ops",
		GeneratorKind: "csv",
		DatasetName:   "ds",
		Active:        true,
		Template:      ReportTemplate{Title: "Scheduled " + id},
		Schedule:      sched,
	}
}

func TestNextRunAfter(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	five := 5
	cases := []struct {
		name  string
		sched ReportSchedule
		want  bool
	}{
		{"manual", ReportSchedule{Cadence: "manual"}, false},
		{"empty", ReportSchedule{}, false},
		{"daily", ReportSchedule{Cadence: "daily"}, true},
		{"weekly", ReportSchedule{Cadence: "weekly"}, true},
		{"monthly", ReportSchedule{Cadence: "monthly"}, true},
		{"interval", ReportSchedule{Cadence: "daily", IntervalMinutes: &five}, true},
		{"cron-missing-expr", ReportSchedule{Cadence: "cron"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, ok := nextRunAfter(tc.sched, now)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok && !next.After(now) {
				t.Fatalf("next %v is not strictly after now %v", next, now)
			}
		})
	}
}

func TestNextRunAfterCron(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 30, 0, 0, time.UTC)
	expr := "0 * * * *" // the top of every hour
	next, ok := nextRunAfter(ReportSchedule{Cadence: "cron", Expression: &expr, Timezone: "UTC"}, now)
	if !ok {
		t.Fatal("expected a next cron fire time")
	}
	if next.Minute() != 0 || !next.After(now) {
		t.Fatalf("unexpected next cron fire: %v", next)
	}
}

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour).Format(time.RFC3339Nano)
	future := now.Add(time.Hour).Format(time.RFC3339Nano)

	if due, _ := scheduleDue(schedulerTestDef("a", ReportSchedule{Cadence: "daily", Enabled: true, NextRunAt: &past}), now); !due {
		t.Fatal("a past, enabled daily schedule must be due")
	}
	if due, _ := scheduleDue(schedulerTestDef("b", ReportSchedule{Cadence: "daily", Enabled: true, NextRunAt: &future}), now); due {
		t.Fatal("a future schedule must not be due")
	}
	if due, _ := scheduleDue(schedulerTestDef("c", ReportSchedule{Cadence: "manual", Enabled: true, NextRunAt: &past}), now); due {
		t.Fatal("a manual cadence must never be due")
	}
}

func TestSchedulerRunOnceGeneratesAndAdvances(t *testing.T) {
	store := NewMemoryReportStore()
	past := time.Date(2026, 5, 22, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	def := schedulerTestDef("r1", ReportSchedule{Cadence: "daily", Enabled: true, NextRunAt: &past})
	if _, err := store.CreateDefinition(context.Background(), def); err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(store, time.Minute, nil, nil)
	sched.now = func() time.Time { return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC) }
	sched.RunOnce(context.Background())

	execs, err := store.ListExecutions(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 1 {
		t.Fatalf("want 1 execution after the sweep, got %d", len(execs))
	}
	if execs[0].TriggeredBy != "schedule" {
		t.Fatalf("execution triggered_by = %q, want schedule", execs[0].TriggeredBy)
	}

	got, _ := store.GetDefinition(context.Background(), "r1")
	if got.Schedule.NextRunAt == nil || *got.Schedule.NextRunAt == past {
		t.Fatalf("next_run_at was not advanced: %v", got.Schedule.NextRunAt)
	}

	// A second sweep at the same instant must not double-run: the
	// definition is no longer due.
	sched.RunOnce(context.Background())
	execs, _ = store.ListExecutions(context.Background(), "r1")
	if len(execs) != 1 {
		t.Fatalf("second sweep double-ran the report: %d executions", len(execs))
	}
}

package handlers

import (
	"context"
	"log/slog"
	"time"

	cron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

// Scheduler runs report definitions whose schedule has come due. It is
// safe to run on every replica: each due definition is claimed with a
// compare-and-swap on its next_run_at, so exactly one replica generates
// a given run.
type Scheduler struct {
	store       ReportStore
	interval    time.Duration
	log         *slog.Logger
	now         func() time.Time
	distributor *Distributor
}

// NewScheduler builds a Scheduler. A non-positive interval defaults to
// one minute.
func NewScheduler(store ReportStore, interval time.Duration, log *slog.Logger, dist *Distributor) *Scheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		store:       store,
		interval:    interval,
		log:         log,
		now:         func() time.Time { return time.Now().UTC() },
		distributor: dist,
	}
}

// Run scans for due reports every interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

// RunOnce performs a single scheduling sweep. It is exported so tests
// can drive the scheduler deterministically.
func (s *Scheduler) RunOnce(ctx context.Context) {
	defs, err := s.store.ListDefinitions(ctx)
	if err != nil {
		s.log.Warn("scheduler: list definitions failed", slog.String("error", err.Error()))
		return
	}
	now := s.now()
	for _, d := range defs {
		due, at := scheduleDue(d, now)
		if !due {
			continue
		}
		if err := s.runDue(ctx, d, at, now); err != nil {
			s.log.Warn("scheduler: report run failed",
				slog.String("report_id", d.ID), slog.String("error", err.Error()))
		}
	}
}

// runDue claims one due definition and, if the claim wins the race
// against other replicas, generates a single execution.
func (s *Scheduler) runDue(ctx context.Context, d ReportDefinition, dueNextRunAt string, now time.Time) error {
	next, ok := nextRunAfter(d.Schedule, now)
	if !ok {
		return nil // no further occurrence — leave the definition as is
	}
	advanced := d.Schedule
	nextStr := next.UTC().Format(time.RFC3339Nano)
	advanced.NextRunAt = &nextStr

	claimed, err := s.store.ClaimDue(ctx, d.ID, dueNextRunAt, advanced)
	if err != nil {
		return err
	}
	if !claimed {
		return nil // another replica already advanced this definition
	}

	d.Schedule = advanced
	e, err := buildExecution(d)
	if err != nil {
		return err
	}
	e.TriggeredBy = "schedule"
	AttachDistribution(ctx, s.distributor, &e, d.Recipients)
	if err := s.store.SaveExecution(ctx, e); err != nil {
		return err
	}
	s.log.Info("scheduler: report generated",
		slog.String("report_id", d.ID), slog.String("execution_id", e.ID))
	return nil
}

// scheduleDue reports whether d is due to run at now, and the
// next_run_at value a claim must compare-and-swap against.
func scheduleDue(d ReportDefinition, now time.Time) (bool, string) {
	if !d.Active || !d.Schedule.Enabled {
		return false, ""
	}
	if d.Schedule.Cadence == "" || d.Schedule.Cadence == "manual" {
		return false, ""
	}
	if d.Schedule.NextRunAt == nil {
		return false, ""
	}
	at, err := time.Parse(time.RFC3339Nano, *d.Schedule.NextRunAt)
	if err != nil || at.After(now) {
		return false, ""
	}
	return true, *d.Schedule.NextRunAt
}

// nextRunAfter computes the next fire time strictly after now for the
// given schedule. The bool is false when the schedule has no further
// occurrence: a manual cadence, or an absent / unsatisfiable cron
// expression.
func nextRunAfter(sched ReportSchedule, now time.Time) (time.Time, bool) {
	if sched.IntervalMinutes != nil && *sched.IntervalMinutes > 0 {
		return now.Add(time.Duration(*sched.IntervalMinutes) * time.Minute), true
	}
	switch sched.Cadence {
	case "daily":
		return now.Add(24 * time.Hour), true
	case "weekly":
		return now.Add(7 * 24 * time.Hour), true
	case "monthly":
		return now.AddDate(0, 1, 0), true
	case "cron":
		if sched.Expression == nil || *sched.Expression == "" {
			return time.Time{}, false
		}
		loc := time.UTC
		if sched.Timezone != "" {
			if l, err := time.LoadLocation(sched.Timezone); err == nil {
				loc = l
			}
		}
		cs, err := cron.ParseCron(*sched.Expression, cron.Unix5, loc)
		if err != nil {
			return time.Time{}, false
		}
		return cron.NextFireAfter(&cs, now)
	default:
		return time.Time{}, false
	}
}

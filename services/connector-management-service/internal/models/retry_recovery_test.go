package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClassifyRunFailure(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want RetryFailureCategory
	}{
		{"empty", "", RetryFailureCategoryUnknown},
		{"credential 401", "request failed: 401 unauthorized", RetryFailureCategoryCredential},
		{"credential expired token", "token expired while refreshing", RetryFailureCategoryCredential},
		{"network reset", "connection reset by peer", RetryFailureCategoryNetwork},
		{"network 504", "upstream gave 504 gateway timeout", RetryFailureCategoryNetwork},
		{"destination conflict", "dataset write conflict on transaction", RetryFailureCategoryDestination},
		{"destination throttled", "destination throttled, rate limit exceeded", RetryFailureCategoryDestination},
		{"source schema", "source table not found", RetryFailureCategorySource},
		{"source query", "source query failed mid-execution", RetryFailureCategorySource},
		{"unknown fallback", "something else entirely broke", RetryFailureCategorySource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyRunFailure(tc.err); got != tc.want {
				t.Fatalf("ClassifyRunFailure(%q) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}

func TestComputeRetryBackoffSeconds_ExponentialWithCap(t *testing.T) {
	policy := RetryBackoffPolicy{
		InitialBackoffSeconds: 4,
		MaxBackoffSeconds:     60,
		BackoffMultiplier:     2.0,
	}
	cases := []struct {
		attempt int
		want    int
	}{
		{0, 4}, // clamped to attempt=1
		{1, 4},
		{2, 8},
		{3, 16},
		{4, 32},
		{5, 60}, // capped
		{10, 60},
	}
	for _, tc := range cases {
		got := ComputeRetryBackoffSeconds(policy, tc.attempt)
		if got != tc.want {
			t.Fatalf("attempt=%d: got %d, want %d", tc.attempt, got, tc.want)
		}
	}
}

func TestEvaluateRetryDecision_ScheduleAndEscalate(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	policy := RetryBackoffPolicy{
		MaxAttempts:           5,
		InitialBackoffSeconds: 10,
		MaxBackoffSeconds:     600,
		BackoffMultiplier:     2.0,
		PreserveCheckpoint:    true,
		EscalateAfterAttempts: 3,
		RetryableSubstrings:   []string{"timeout"},
	}
	failure := RunFailureContext{
		Category:      RetryFailureCategoryNetwork,
		ErrorMessage:  "tls handshake timeout",
		Attempt:       2,
		HasCheckpoint: true,
	}
	decision := EvaluateRetryDecision(policy, failure, now)
	if decision.Action != RetryDecisionRetry {
		t.Fatalf("expected retry, got %s", decision.Action)
	}
	if decision.NextAttempt != 3 || decision.BackoffSeconds != 40 {
		t.Fatalf("unexpected schedule: attempt=%d backoff=%d", decision.NextAttempt, decision.BackoffSeconds)
	}
	if decision.EscalateToDataHealth {
		t.Fatalf("not yet at escalate threshold (attempt 2 < %d)", policy.EscalateAfterAttempts)
	}
	if !decision.PreserveCheckpoint {
		t.Fatalf("expected checkpoint preservation")
	}

	// Force escalation by raising attempt to 3 (matches EscalateAfterAttempts).
	failure.Attempt = 3
	decision = EvaluateRetryDecision(policy, failure, now)
	if decision.Action != RetryDecisionEscalate {
		t.Fatalf("expected escalate at attempt 3, got %s", decision.Action)
	}
	if !decision.EscalateToDataHealth {
		t.Fatalf("expected escalation flag when at threshold")
	}

	// Exhaust: attempt equals MaxAttempts.
	failure.Attempt = 5
	decision = EvaluateRetryDecision(policy, failure, now)
	if decision.Action != RetryDecisionExhausted {
		t.Fatalf("expected exhausted at max attempts, got %s", decision.Action)
	}
}

func TestEvaluateRetryDecision_NonRetryablePattern(t *testing.T) {
	policy := RetryBackoffPolicy{
		MaxAttempts:            5,
		InitialBackoffSeconds:  10,
		MaxBackoffSeconds:      600,
		BackoffMultiplier:      2.0,
		EscalateAfterAttempts:  3,
		NonRetryableSubstrings: []string{"invalid credentials"},
	}
	decision := EvaluateRetryDecision(policy, RunFailureContext{
		Category:     RetryFailureCategoryCredential,
		ErrorMessage: "Invalid credentials: token rejected",
		Attempt:      1,
	}, time.Now())
	if decision.Action != RetryDecisionNoRetry {
		t.Fatalf("expected no_retry, got %s", decision.Action)
	}
	if !decision.EscalateToDataHealth {
		t.Fatalf("non-retryable failures must escalate so Data Health surfaces them")
	}
}

func TestEvaluateRetryDecision_RetryableAllowlistGate(t *testing.T) {
	policy := RetryBackoffPolicy{
		MaxAttempts:           5,
		InitialBackoffSeconds: 5,
		MaxBackoffSeconds:     60,
		BackoffMultiplier:     2.0,
		EscalateAfterAttempts: 3,
		RetryableSubstrings:   []string{"timeout"},
	}
	decision := EvaluateRetryDecision(policy, RunFailureContext{
		Category:     RetryFailureCategoryNetwork,
		ErrorMessage: "schema mismatch on destination", // does not match the allowlist
		Attempt:      1,
	}, time.Now())
	if decision.Action != RetryDecisionNoRetry {
		t.Fatalf("expected no_retry outside the allowlist, got %s", decision.Action)
	}
}

func TestBuildRetryRecoverySummary_AggregatesCounts(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	policy := DefaultSourceRetryPolicy(sourceID, now)

	exhaustedAt := now.Add(-time.Minute)
	exhaustedRun := RetryRecoveryRunSummary{
		RunID:       uuid.New(),
		SyncDefID:   uuid.New(),
		Attempt:     6, // exceeds default network MaxAttempts(6) → exhausted
		MaxAttempts: 6,
		Category:    RetryFailureCategoryNetwork,
		Error:       "connection reset by peer",
		Status:      "failed",
		StartedAt:   exhaustedAt,
	}
	retryingRun := RetryRecoveryRunSummary{
		RunID:         uuid.New(),
		SyncDefID:     uuid.New(),
		Attempt:       1, // first attempt → schedule retry
		MaxAttempts:   6,
		Category:      RetryFailureCategoryNetwork,
		Error:         "tls handshake timeout",
		Status:        "retrying",
		StartedAt:     now,
		HasCheckpoint: true,
	}

	summary := BuildRetryRecoverySummary(RetryRecoveryInput{
		SourceID:  sourceID,
		Policy:    policy,
		Failures:  []RetryRecoveryRunSummary{exhaustedRun, retryingRun},
		CheckedAt: now,
	})

	if summary.ExhaustedCount != 1 {
		t.Fatalf("ExhaustedCount=%d, want 1", summary.ExhaustedCount)
	}
	if summary.BackoffInProgressCount != 1 {
		t.Fatalf("BackoffInProgressCount=%d, want 1", summary.BackoffInProgressCount)
	}
	if summary.CheckpointPreservedRuns != 1 {
		t.Fatalf("CheckpointPreservedRuns=%d, want 1", summary.CheckpointPreservedRuns)
	}
	if len(summary.RecentRuns) != 2 {
		t.Fatalf("RecentRuns=%d, want 2", len(summary.RecentRuns))
	}
	for _, run := range summary.RecentRuns {
		if run.Decision == nil {
			t.Fatalf("expected decision attached to every run, missing for %s", run.RunID)
		}
	}
}

func TestRetryRecoveryHealthChecks_EscalationFeedsHealthSummary(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	policy := DefaultSourceRetryPolicy(sourceID, now)

	summary := BuildRetryRecoverySummary(RetryRecoveryInput{
		SourceID: sourceID,
		Policy:   policy,
		Failures: []RetryRecoveryRunSummary{{
			RunID:       uuid.New(),
			SyncDefID:   uuid.New(),
			Attempt:     6, // exhausted under default network policy
			MaxAttempts: 6,
			Category:    RetryFailureCategoryNetwork,
			Error:       "i/o timeout",
			Status:      "failed",
			StartedAt:   now,
		}},
		CheckedAt: now,
	})

	conn := Connection{ID: sourceID, Name: "warehouse-source"}
	healthSummary := BuildDataConnectionHealthSummary(DataConnectionHealthInput{
		Source:        conn,
		RetryRecovery: &summary,
		CheckedAt:     now,
	})

	if healthSummary.State != DataConnectionHealthCritical {
		t.Fatalf("expected critical state, got %s", healthSummary.State)
	}
	found := false
	for _, check := range healthSummary.Checks {
		if check.Code == "retry_exhausted" && check.Surface == DataConnectionHealthSurfaceRetry {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected retry_exhausted check on the retry surface, got %+v", healthSummary.Checks)
	}
}

func TestNormalizeSourceRetryPolicy_FillsDefaults(t *testing.T) {
	now := time.Now().UTC()
	sourceID := uuid.New()
	policy := SourceRetryPolicy{
		SourceID: sourceID,
		// only override one category; the rest should fall back to defaults.
		Categories: map[RetryFailureCategory]RetryBackoffPolicy{
			RetryFailureCategoryDestination: {
				MaxAttempts:           99,           // exceeds clamp
				InitialBackoffSeconds: 0,            // invalid
				BackoffMultiplier:     20,           // exceeds clamp
				JitterRatio:           5,            // exceeds clamp
				EscalateAfterAttempts: 200,          // exceeds MaxAttempts after clamp
				RetryableSubstrings:   []string{""}, // empties stripped
			},
		},
	}
	normalized := NormalizeSourceRetryPolicy(policy, sourceID, now)
	dest := normalized.Categories[RetryFailureCategoryDestination]
	if dest.MaxAttempts != 50 {
		t.Fatalf("MaxAttempts clamp failed: %d", dest.MaxAttempts)
	}
	if dest.BackoffMultiplier != 10 {
		t.Fatalf("BackoffMultiplier clamp failed: %v", dest.BackoffMultiplier)
	}
	if dest.JitterRatio != 1 {
		t.Fatalf("JitterRatio clamp failed: %v", dest.JitterRatio)
	}
	if dest.EscalateAfterAttempts != dest.MaxAttempts {
		t.Fatalf("EscalateAfterAttempts must be <= MaxAttempts, got %d/%d", dest.EscalateAfterAttempts, dest.MaxAttempts)
	}
	if len(dest.RetryableSubstrings) != 0 {
		t.Fatalf("empty retryable substrings should be stripped, got %v", dest.RetryableSubstrings)
	}
	for _, category := range RetryFailureCategories() {
		if _, ok := normalized.Categories[category]; !ok {
			t.Fatalf("missing default category %s after normalize", category)
		}
	}
}

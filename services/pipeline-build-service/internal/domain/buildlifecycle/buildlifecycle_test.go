package buildlifecycle

import (
	"errors"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// legalEdges is an independent restatement of the lifecycle diagram. It
// is deliberately not the production validTransitions map, so a typo in
// production code is caught by the test rather than mirrored by it.
var legalEdges = map[[2]models.BuildState]bool{
	{models.BuildResolution, models.BuildQueued}:   true,
	{models.BuildResolution, models.BuildFailed}:   true,
	{models.BuildResolution, models.BuildAborting}: true,
	{models.BuildQueued, models.BuildRunning}:      true,
	{models.BuildQueued, models.BuildFailed}:       true,
	{models.BuildQueued, models.BuildAborting}:     true,
	{models.BuildRunning, models.BuildCompleted}:   true,
	{models.BuildRunning, models.BuildFailed}:      true,
	{models.BuildRunning, models.BuildAborting}:    true,
	{models.BuildAborting, models.BuildAborted}:    true,
}

// TestIsValidTransitionCoversEveryStatePair checks all 49 ordered state
// pairs — every legal edge accepted, every forbidden edge rejected.
func TestIsValidTransitionCoversEveryStatePair(t *testing.T) {
	for _, from := range models.AllBuildStates {
		for _, to := range models.AllBuildStates {
			want := legalEdges[[2]models.BuildState{from, to}]
			if got := IsValidTransition(from, to); got != want {
				t.Errorf("IsValidTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalStatesHaveNoOutgoingTransition(t *testing.T) {
	for _, from := range models.AllBuildStates {
		if !from.IsTerminal() {
			continue
		}
		for _, to := range models.AllBuildStates {
			if IsValidTransition(from, to) {
				t.Errorf("terminal state %s must not transition to %s", from, to)
			}
		}
	}
}

func TestSelfTransitionIsNotAnEdge(t *testing.T) {
	for _, s := range models.AllBuildStates {
		if IsValidTransition(s, s) {
			t.Errorf("self-transition %s → %s must not be a valid edge", s, s)
		}
	}
}

func TestCheckTransition(t *testing.T) {
	if err := CheckTransition(models.BuildResolution, models.BuildQueued); err != nil {
		t.Fatalf("legal transition rejected: %v", err)
	}
	err := CheckTransition(models.BuildCompleted, models.BuildRunning)
	if err == nil {
		t.Fatal("expected an error for an illegal transition")
	}
	var ite *ErrInvalidTransition
	if !errors.As(err, &ite) {
		t.Fatalf("error is not *ErrInvalidTransition: %v", err)
	}
	if ite.From != models.BuildCompleted || ite.To != models.BuildRunning {
		t.Fatalf("unexpected error fields: %+v", ite)
	}
}

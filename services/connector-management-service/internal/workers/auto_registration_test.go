package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time                 { return c.now }
func (c fakeClock) NewTicker(time.Duration) Ticker { return nil }

type fakeAutoStore struct {
	connections []models.Connection
	signatures  map[string]*string
	upserts     []models.DiscoveredSource
}

func (s *fakeAutoStore) ListConnections(context.Context, *uuid.UUID) ([]models.Connection, error) {
	return s.connections, nil
}

func (s *fakeAutoStore) UpsertRegistration(_ context.Context, _ uuid.UUID, source models.DiscoveredSource, _ string, _ bool, _ bool, _ *uuid.UUID, _ json.RawMessage) (*models.ConnectionRegistration, error) {
	s.upserts = append(s.upserts, source)
	return &models.ConnectionRegistration{ID: uuid.New(), Selector: source.Selector}, nil
}

func (s *fakeAutoStore) GetRegistrationSignature(_ context.Context, _ uuid.UUID, selector string) (*string, error) {
	return s.signatures[selector], nil
}

func (s *fakeAutoStore) RecordRegistrationSignature(_ context.Context, _ uuid.UUID, selector string, signature *string) error {
	s.signatures[selector] = signature
	return nil
}

func TestAutoRegistrationRunOnceSkipsUnchangedWhenUpdateDetectionEnabled(t *testing.T) {
	connID := uuid.New()
	sig := "v1"
	cfg := json.RawMessage(`{"auto_registration":{"enabled":true,"selectors":["public.orders"],"update_detection":true},"tables":[{"selector":"public.orders","source_signature":"v1"},{"selector":"public.customers","source_signature":"v2"}]}`)
	store := &fakeAutoStore{connections: []models.Connection{{ID: connID, Name: "warehouse", ConnectorType: "postgresql", Config: cfg}}, signatures: map[string]*string{"public.orders": &sig}}
	recorder := NewAutoRegistrationRecorder()
	worker := &AutoRegistrationWorker{Store: store, Clock: fakeClock{now: time.Unix(10, 0).UTC()}, Recorder: recorder}
	summary, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Registered != 0 || summary.Errors != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(store.upserts) != 0 {
		t.Fatalf("expected no upserts, got %d", len(store.upserts))
	}
	run := recorder.LastRun(connID)
	if run == nil || run.UpdateBreakdown["unchanged"] != 1 {
		t.Fatalf("unexpected last run: %+v", run)
	}
}

func TestAutoRegistrationRunOnceRegistersChangedSelector(t *testing.T) {
	connID := uuid.New()
	prev := "v1"
	cfg := json.RawMessage(`{"auto_registration":{"enabled":true,"registration_mode":"zero_copy","update_detection":true},"tables":[{"selector":"public.orders","source_signature":"v2"}]}`)
	store := &fakeAutoStore{connections: []models.Connection{{ID: connID, Name: "warehouse", ConnectorType: "postgresql", Config: cfg}}, signatures: map[string]*string{"public.orders": &prev}}
	worker := &AutoRegistrationWorker{Store: store, Clock: fakeClock{now: time.Unix(10, 0).UTC()}, Recorder: NewAutoRegistrationRecorder()}
	summary, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Registered != 1 {
		t.Fatalf("expected registered=1 got %+v", summary)
	}
	if len(store.upserts) != 1 || store.upserts[0].Selector != "public.orders" {
		t.Fatalf("unexpected upserts: %+v", store.upserts)
	}
	if got := store.signatures["public.orders"]; got == nil || *got != "v2" {
		t.Fatalf("signature not recorded: %v", got)
	}
}

func TestAutoRegistrationRunOnceRecordsDiscoveryError(t *testing.T) {
	connID := uuid.New()
	cfg := json.RawMessage(`{"auto_registration":{"enabled":true}}`)
	store := &fakeAutoStore{connections: []models.Connection{{ID: connID, Name: "bad", ConnectorType: "postgresql", Config: cfg}}, signatures: map[string]*string{}}
	recorder := NewAutoRegistrationRecorder()
	worker := &AutoRegistrationWorker{Store: store, Clock: fakeClock{now: time.Unix(10, 0).UTC()}, Recorder: recorder, Discover: func(context.Context, *models.Connection) ([]models.DiscoveredSource, error) {
		return nil, errors.New("boom")
	}}
	summary, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Errors != 1 {
		t.Fatalf("expected errors=1 got %+v", summary)
	}
	run := recorder.LastRun(connID)
	if run == nil || run.LastError == nil || *run.LastError != "boom" {
		t.Fatalf("unexpected run: %+v", run)
	}
}

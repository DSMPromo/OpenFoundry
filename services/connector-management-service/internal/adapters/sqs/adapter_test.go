package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	sqsdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/sqs"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeDriver struct {
	connectErr error
	msgs       []sqsdriver.Message
	receiveErr error
}

func (f *fakeDriver) Connect(_ context.Context) error { return f.connectErr }
func (f *fakeDriver) Receive(_ context.Context, _ int32, _ int32) ([]sqsdriver.Message, error) {
	return f.msgs, f.receiveErr
}

func newTestAdapter(d *fakeDriver) *Adapter {
	a := New()
	a.SetDriverFactory(func(_ context.Context, _ sqsdriver.Config) (driver, error) { return d, nil })
	return a
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(nil); err == nil {
		t.Fatal("want error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error for missing identity")
	}
	if err := ValidateConfig(json.RawMessage(`{"queue_url":"http://x/q"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverSources(t *testing.T) {
	c := &models.Connection{
		ID:     uuid.New(),
		Config: json.RawMessage(`{"queue_url":"http://x/q","queue_name":"q"}`),
	}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Selector != "http://x/q" || sources[0].SourceKind != defaultSourceKind {
		t.Fatalf("source projection wrong: %+v", sources)
	}
	if !sources[0].SupportsSync {
		t.Error("SQS source should advertise SupportsSync for future ingestion")
	}
}

func TestQueryVirtualTableProjectsMessages(t *testing.T) {
	fake := &fakeDriver{msgs: []sqsdriver.Message{
		{MessageID: "m1", Body: "hi", Attributes: map[string]string{"k": "v"}},
		{MessageID: "m2", Body: "ho"},
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"queue_url":"http://x/q"}`)}
	got, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.RowCount != 2 || got.Mode != "sqs_receive" {
		t.Fatalf("result wrong: rc=%d mode=%q", got.RowCount, got.Mode)
	}
	if !strings.Contains(string(got.Rows[0]), `"message_id":"m1"`) {
		t.Errorf("row 0 missing message_id: %s", string(got.Rows[0]))
	}
}

func TestTestConnectionSuccessAndFailure(t *testing.T) {
	ok := newTestAdapter(&fakeDriver{})
	res, _ := ok.TestConnection(context.Background(), json.RawMessage(`{"queue_url":"http://x/q"}`))
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}

	fail := newTestAdapter(&fakeDriver{connectErr: errors.New("AccessDenied")})
	res, _ = fail.TestConnection(context.Background(), json.RawMessage(`{"queue_url":"http://x/q"}`))
	if res.Success || !strings.Contains(res.Message, "AccessDenied") {
		t.Fatalf("expected failure with SDK message, got %+v", res)
	}
}

func TestBuildIngestSpecRoundTripsURL(t *testing.T) {
	c := &models.Connection{Name: "events", Config: json.RawMessage(`{"queue_url":"http://x/q"}`)}
	src := &models.DiscoveredSource{Selector: "http://x/q", SourceKind: defaultSourceKind}
	spec, err := New().BuildIngestSpec(context.Background(), c, src)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Source != ConnectorType || !strings.Contains(string(spec.Config), `"queue_url":"http://x/q"`) {
		t.Fatalf("spec wrong: %+v", spec)
	}
}

func TestStreamArrowIsNotImplemented(t *testing.T) {
	if _, err := New().StreamArrow(context.Background(), nil, nil, ""); err == nil {
		t.Error("StreamArrow should return ErrNotImplemented")
	}
}

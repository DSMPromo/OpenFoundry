package sns

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	snsdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/sns"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeDriver struct{ connectErr error }

func (f *fakeDriver) Connect(_ context.Context) error { return f.connectErr }

func newTestAdapter(d *fakeDriver) *Adapter {
	a := New()
	a.SetDriverFactory(func(_ context.Context, _ snsdriver.Config) (driver, error) { return d, nil })
	return a
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(nil); err == nil {
		t.Fatal("want error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"topic_arn":"arn:aws:sns:us-east-1:0:t"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverSources(t *testing.T) {
	c := &models.Connection{
		ID:     uuid.New(),
		Config: json.RawMessage(`{"topic_arn":"arn:aws:sns:us-east-1:0:t"}`),
	}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Selector == "" || sources[0].SourceKind != defaultSourceKind {
		t.Fatalf("source projection wrong: %+v", sources)
	}
}

func TestQueryAndIngestNotImplemented(t *testing.T) {
	a := New()
	if _, err := a.QueryVirtualTable(context.Background(), nil, nil, ""); err == nil {
		t.Error("QueryVirtualTable should be NotImplemented for SNS")
	}
	if _, err := a.BuildIngestSpec(context.Background(), nil, nil); err == nil {
		t.Error("BuildIngestSpec should be NotImplemented for SNS")
	}
	if _, err := a.StreamArrow(context.Background(), nil, nil, ""); err == nil {
		t.Error("StreamArrow should be NotImplemented for SNS")
	}
}

func TestTestConnectionSuccessAndFailure(t *testing.T) {
	ok := newTestAdapter(&fakeDriver{})
	res, _ := ok.TestConnection(context.Background(), json.RawMessage(`{"topic_arn":"arn:aws:sns:us-east-1:0:t"}`))
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}

	fail := newTestAdapter(&fakeDriver{connectErr: errors.New("Forbidden")})
	res, _ = fail.TestConnection(context.Background(), json.RawMessage(`{"topic_arn":"arn:aws:sns:us-east-1:0:t"}`))
	if res.Success || !strings.Contains(res.Message, "Forbidden") {
		t.Fatalf("expected failure: %+v", res)
	}
}

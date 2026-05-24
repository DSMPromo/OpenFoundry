package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	ddbdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/dynamodb"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeDriver struct {
	connectErr error
	items      []map[string]any
	scanErr    error
}

func (f *fakeDriver) Connect(_ context.Context) error { return f.connectErr }
func (f *fakeDriver) Scan(_ context.Context, _ int32) ([]map[string]any, error) {
	return f.items, f.scanErr
}

func newTestAdapter(d *fakeDriver) *Adapter {
	a := New()
	a.SetDriverFactory(func(_ context.Context, _ ddbdriver.Config) (driver, error) { return d, nil })
	return a
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(nil); err == nil {
		t.Fatal("want error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"table_name":"kv"}`)); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestDiscoverSources(t *testing.T) {
	c := &models.Connection{
		ID:     uuid.New(),
		Config: json.RawMessage(`{"table_name":"kv"}`),
	}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Selector != "kv" || sources[0].SourceKind != defaultSourceKind {
		t.Fatalf("projection: %+v", sources)
	}
}

func TestQueryVirtualTableUnionsColumnsAcrossItems(t *testing.T) {
	fake := &fakeDriver{items: []map[string]any{
		{"id": "a", "score": 1},
		{"id": "b", "score": 2, "tag": "x"},
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"table_name":"kv"}`)}
	got, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.RowCount != 2 || got.Mode != "dynamodb_scan" {
		t.Fatalf("result: rc=%d mode=%q", got.RowCount, got.Mode)
	}
	// id + score from item 0, tag added from item 1.
	colSet := map[string]bool{}
	for _, c := range got.Columns {
		colSet[c] = true
	}
	for _, want := range []string{"id", "score", "tag"} {
		if !colSet[want] {
			t.Errorf("missing column %q in %+v", want, got.Columns)
		}
	}
}

func TestTestConnectionSuccessAndFailure(t *testing.T) {
	ok := newTestAdapter(&fakeDriver{})
	res, _ := ok.TestConnection(context.Background(), json.RawMessage(`{"table_name":"kv"}`))
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}

	fail := newTestAdapter(&fakeDriver{connectErr: errors.New("ResourceNotFoundException")})
	res, _ = fail.TestConnection(context.Background(), json.RawMessage(`{"table_name":"missing"}`))
	if res.Success || !strings.Contains(res.Message, "ResourceNotFoundException") {
		t.Fatalf("expected failure: %+v", res)
	}
}

func TestStreamArrowAndBuildIngestSpec(t *testing.T) {
	a := New()
	if _, err := a.StreamArrow(context.Background(), nil, nil, ""); err == nil {
		t.Error("StreamArrow should return ErrNotImplemented")
	}
	c := &models.Connection{Name: "kv", Config: json.RawMessage(`{"table_name":"kv"}`)}
	spec, err := a.BuildIngestSpec(context.Background(), c, &models.DiscoveredSource{Selector: "kv"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Source != ConnectorType {
		t.Errorf("source = %q", spec.Source)
	}
}

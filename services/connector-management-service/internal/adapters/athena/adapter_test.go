package athena

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	athenadriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/athena"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type fakeDriver struct {
	connectErr error
	runResp    *athenadriver.QueryResult
	runErr     error
	gotSQL     string
}

func (f *fakeDriver) Connect(_ context.Context) error { return f.connectErr }
func (f *fakeDriver) Run(_ context.Context, sql string) (*athenadriver.QueryResult, error) {
	f.gotSQL = sql
	if f.runErr != nil {
		return nil, f.runErr
	}
	if f.runResp != nil {
		return f.runResp, nil
	}
	return &athenadriver.QueryResult{}, nil
}

func newTestAdapter(d *fakeDriver) *Adapter {
	a := New()
	a.SetDriverFactory(func(_ context.Context, _ athenadriver.Config) (driver, error) { return d, nil })
	return a
}

func TestDiscoverSourcesIncludesDatabaseInDisplay(t *testing.T) {
	c := &models.Connection{
		ID:     uuid.New(),
		Config: json.RawMessage(`{"workgroup":"ops","database":"analytics","output_location":"s3://ofy/q/"}`),
	}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sources[0].DisplayName, "ops/analytics") {
		t.Fatalf("display = %q", sources[0].DisplayName)
	}
}

func TestQueryVirtualTableExecutesSQLFromSelector(t *testing.T) {
	fake := &fakeDriver{runResp: &athenadriver.QueryResult{
		Columns:          []string{"a"},
		Rows:             []map[string]string{{"a": "1"}, {"a": "2"}},
		QueryExecutionID: "q-99",
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"workgroup":"primary"}`)}
	got, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{Selector: "SELECT a FROM t"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "athena_query" || got.RowCount != 2 {
		t.Fatalf("result: %+v", got)
	}
	if !strings.Contains(string(got.Metadata), "q-99") {
		t.Errorf("metadata missing execution id: %s", string(got.Metadata))
	}
	if fake.gotSQL != "SELECT a FROM t" {
		t.Errorf("SQL routed wrong: %q", fake.gotSQL)
	}
}

func TestQueryVirtualTableAppendsLimitWhenAbsent(t *testing.T) {
	fake := &fakeDriver{}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"workgroup":"primary"}`)}
	limit := 7
	_, _ = a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{
		Selector: "SELECT * FROM t",
		Limit:    &limit,
	}, "")
	if !strings.Contains(fake.gotSQL, "LIMIT 7") {
		t.Errorf("LIMIT not appended: %q", fake.gotSQL)
	}
}

func TestQueryVirtualTableLeavesExistingLimitAlone(t *testing.T) {
	fake := &fakeDriver{}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"workgroup":"primary"}`)}
	limit := 7
	_, _ = a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{
		Selector: "SELECT * FROM t LIMIT 3",
		Limit:    &limit,
	}, "")
	if strings.Count(fake.gotSQL, "LIMIT") != 1 {
		t.Errorf("LIMIT duplicated: %q", fake.gotSQL)
	}
}

func TestQueryVirtualTableErrorsWithoutSQL(t *testing.T) {
	a := newTestAdapter(&fakeDriver{})
	c := &models.Connection{Config: json.RawMessage(`{"workgroup":"primary"}`)}
	_, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{}, "")
	if err == nil || !strings.Contains(err.Error(), "SQL") {
		t.Fatalf("expected SQL error: %v", err)
	}
}

func TestTestConnectionSuccessAndFailure(t *testing.T) {
	ok := newTestAdapter(&fakeDriver{})
	res, _ := ok.TestConnection(context.Background(), json.RawMessage(`{}`))
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}

	fail := newTestAdapter(&fakeDriver{connectErr: errors.New("AccessDeniedException")})
	res, _ = fail.TestConnection(context.Background(), json.RawMessage(`{}`))
	if res.Success || !strings.Contains(res.Message, "AccessDeniedException") {
		t.Fatalf("expected failure: %+v", res)
	}
}

func TestBuildIngestSpecIsNotImplemented(t *testing.T) {
	if _, err := New().BuildIngestSpec(context.Background(), nil, nil); err == nil {
		t.Error("BuildIngestSpec should be NotImplemented for Athena")
	}
}

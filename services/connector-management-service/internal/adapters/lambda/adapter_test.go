package lambda

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	lambdadriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/lambda"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// fakeDriver fulfills the unexported driver interface so the adapter
// can be unit-tested without an AWS account.
type fakeDriver struct {
	connectErr error
	gotPayload []byte
	invokeOut  *lambdadriver.InvokeResult
	invokeErr  error
}

func (f *fakeDriver) Connect(_ context.Context) error { return f.connectErr }
func (f *fakeDriver) Invoke(_ context.Context, payload []byte) (*lambdadriver.InvokeResult, error) {
	f.gotPayload = append([]byte(nil), payload...)
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeOut != nil {
		return f.invokeOut, nil
	}
	return &lambdadriver.InvokeResult{StatusCode: 200, Payload: []byte(`{"ok":true}`)}, nil
}

func newTestAdapter(d *fakeDriver) *Adapter {
	a := New()
	a.SetDriverFactory(func(_ context.Context, _ lambdadriver.Config) (driver, error) { return d, nil })
	return a
}

func TestValidateConfig(t *testing.T) {
	if err := ValidateConfig(nil); err == nil {
		t.Fatal("want error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"function_name":""}`)); err == nil {
		t.Fatal("want error for empty function_name")
	}
	if err := ValidateConfig(json.RawMessage(`{"function_name":"x"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverSourcesReturnsConfiguredFunction(t *testing.T) {
	c := &models.Connection{
		ID:     uuid.New(),
		Config: json.RawMessage(`{"function_name":"openfoundry-echo","qualifier":"PROD"}`),
	}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(sources))
	}
	if sources[0].Selector != "openfoundry-echo" {
		t.Errorf("selector = %q", sources[0].Selector)
	}
	if !strings.Contains(sources[0].DisplayName, "PROD") {
		t.Errorf("display = %q, want qualifier suffix", sources[0].DisplayName)
	}
	if sources[0].SourceKind != defaultSourceKind {
		t.Errorf("source_kind = %q", sources[0].SourceKind)
	}
}

func TestQueryVirtualTableWrapsInvokeOutput(t *testing.T) {
	fake := &fakeDriver{invokeOut: &lambdadriver.InvokeResult{
		StatusCode: 200,
		Payload:    []byte(`{"echo":"hi"}`),
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"function_name":"echo"}`)}
	q := &models.VirtualTableQueryRequest{Selector: "echo"}

	got, err := a.QueryVirtualTable(context.Background(), c, q, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if got.RowCount != 1 || string(got.Rows[0]) != `{"echo":"hi"}` {
		t.Errorf("result row wrong: %+v", got.Rows)
	}
	if got.Selector != "echo" || got.Mode != "lambda_invoke" {
		t.Errorf("meta wrong: selector=%q mode=%q", got.Selector, got.Mode)
	}
	// Sanity: the query was serialized into the payload.
	if !strings.Contains(string(fake.gotPayload), `"selector":"echo"`) {
		t.Errorf("payload missing selector: %s", string(fake.gotPayload))
	}
}

func TestQueryVirtualTableWrapsNonJSONPayload(t *testing.T) {
	fake := &fakeDriver{invokeOut: &lambdadriver.InvokeResult{
		StatusCode: 200,
		Payload:    []byte(`hello`), // not valid JSON
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"function_name":"echo"}`)}
	got, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	row := string(got.Rows[0])
	if !strings.Contains(row, `"raw":"hello"`) {
		t.Errorf("expected {raw:hello} wrap, got %q", row)
	}
}

func TestQueryVirtualTableSurfacesFunctionError(t *testing.T) {
	fake := &fakeDriver{invokeOut: &lambdadriver.InvokeResult{
		StatusCode:    200,
		Payload:       []byte(`{"errorMessage":"boom"}`),
		FunctionError: "Handled",
	}}
	a := newTestAdapter(fake)
	c := &models.Connection{Config: json.RawMessage(`{"function_name":"echo"}`)}
	_, err := a.QueryVirtualTable(context.Background(), c, &models.VirtualTableQueryRequest{}, "")
	if err == nil || !strings.Contains(err.Error(), "Handled") {
		t.Fatalf("want function-error wrap, got %v", err)
	}
}

func TestTestConnectionSucceedsWhenDriverConnectSucceeds(t *testing.T) {
	a := newTestAdapter(&fakeDriver{})
	res, err := a.TestConnection(context.Background(), json.RawMessage(`{"function_name":"echo"}`))
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if !res.Success {
		t.Fatalf("success = false: %s", res.Message)
	}
}

func TestTestConnectionReportsFailureOnConnectError(t *testing.T) {
	a := newTestAdapter(&fakeDriver{connectErr: errors.New("ResourceNotFoundException")})
	res, err := a.TestConnection(context.Background(), json.RawMessage(`{"function_name":"missing"}`))
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Message, "ResourceNotFoundException") {
		t.Errorf("message = %q", res.Message)
	}
}

func TestStreamArrowAndBuildIngestSpecAreNotImplemented(t *testing.T) {
	a := New()
	if _, err := a.StreamArrow(context.Background(), nil, nil, ""); err == nil {
		t.Error("StreamArrow should return ErrNotImplemented")
	}
	if _, err := a.BuildIngestSpec(context.Background(), nil, nil); err == nil {
		t.Error("BuildIngestSpec should return ErrNotImplemented")
	}
}

// Package lambda is the connector-management adapter for AWS Lambda.
// It exposes a registered Lambda function as a single OpenFoundry
// "connection" — operators can test the connection (verifies the
// function exists), discover it as a virtual source, and invoke it
// through the query path (payload in, JSON out).
//
// Lambda is compute, not a data lake — DiscoverSources surfaces only
// the configured function (selector = the function name) and
// StreamArrow / BuildIngestSpec return ErrNotImplemented so the
// dispatcher routes through the invoke path instead of the
// ingestion-replication bridge.
package lambda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	lambdadriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/lambda"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// ConnectorType is the `connections.connector_type` value the adapter
// is registered under in cmd/connector-management-service/main.go.
const ConnectorType = "lambda"

const defaultSourceKind = "lambda_function"

// Adapter implements [adapters.ConnectorAdapter] for AWS Lambda.
type Adapter struct {
	driverFactory driverFactory
}

// driverFactory is a constructor seam the test suite swaps to inject a
// fake driver without standing up LocalStack from unit tests.
type driverFactory func(ctx context.Context, cfg lambdadriver.Config) (driver, error)

// driver is the subset of *lambdadriver.Driver the adapter consumes.
// Tests fake this directly.
type driver interface {
	Connect(ctx context.Context) error
	Invoke(ctx context.Context, payload []byte) (*lambdadriver.InvokeResult, error)
}

// New returns a ready-to-use Adapter backed by the default driver
// constructor.
func New() *Adapter {
	return &Adapter{driverFactory: defaultDriverFactory}
}

// Factory returns an [adapters.Factory] yielding the singleton Adapter.
// Like the s3 adapter, Lambda's adapter is stateless per call.
func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

// SetDriverFactory overrides the driver constructor used by
// TestConnection / QueryVirtualTable. Production keeps the default.
func (a *Adapter) SetDriverFactory(f driverFactory) {
	if f != nil {
		a.driverFactory = f
	}
}

func defaultDriverFactory(ctx context.Context, cfg lambdadriver.Config) (driver, error) {
	return lambdadriver.New(ctx, cfg)
}

// ValidateConfig mirrors the s3 adapter's contract: catch the bad
// shape early so the connector-management API surface returns a
// useful error before the driver attempts an AWS call.
func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("lambda connector requires 'function_name'")
	}
	_, err := lambdadriver.ConfigFromJSON(raw)
	return err
}

// DiscoverSources surfaces the configured function as a single
// virtual source. Multi-function discovery (via ListFunctions) is a
// follow-up — operators currently register one Lambda per connection.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("lambda: connection is nil")
	}
	cfg, err := lambdadriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	selector := strings.TrimSpace(cfg.FunctionName)
	display := selector
	if cfg.Qualifier != "" {
		display = fmt.Sprintf("%s:%s", selector, cfg.Qualifier)
	}
	metadata, err := json.Marshal(map[string]any{
		"function_name": cfg.FunctionName,
		"qualifier":     cfg.Qualifier,
	})
	if err != nil {
		return nil, fmt.Errorf("lambda: marshal source metadata: %w", err)
	}
	return []adapters.Source{{
		Selector:         selector,
		DisplayName:      display,
		SourceKind:       defaultSourceKind,
		SupportsSync:     false, // Lambda invocation is on-demand, not a stream.
		SupportsZeroCopy: false,
		Metadata:         metadata,
	}}, nil
}

// QueryVirtualTable invokes the Lambda with the caller's query
// serialized as the request payload, then returns the function's
// response as a single-row Result. This intentionally keeps the wire
// shape compatible with the connector dispatcher even though Lambda
// is not a tabular source — the operator gets back the function's
// JSON output verbatim in `rows[0]`.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if c == nil {
		return nil, errors.New("lambda: connection is nil")
	}
	if q == nil {
		return nil, errors.New("lambda: query is nil")
	}
	cfg, err := lambdadriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	d, err := a.driverFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("lambda: marshal query: %w", err)
	}
	out, err := d.Invoke(ctx, payload)
	if err != nil {
		return nil, err
	}
	if out.FunctionError != "" {
		return nil, fmt.Errorf("lambda: function raised %q: %s", out.FunctionError, string(out.Payload))
	}
	row := out.Payload
	if len(row) == 0 || !json.Valid(row) {
		// Lambda returned non-JSON output; wrap it so the result row
		// stays JSON-shaped per VirtualTableQueryResponse contract.
		row, _ = json.Marshal(map[string]any{"raw": string(out.Payload)})
	}
	return &adapters.Result{
		Selector: cfg.FunctionName,
		Mode:     "lambda_invoke",
		Columns:  []string{"result"},
		RowCount: 1,
		Rows:     []json.RawMessage{row},
	}, nil
}

// StreamArrow is not supported — Lambda invocation does not stream
// row batches.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: lambda arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec is not supported — Lambda is compute, not an
// ingestion source. Pipelines that need Lambda as a transform step
// will call this driver directly from the pipeline runtime in a
// later phase.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: lambda ingestion bridge", adapters.ErrNotImplemented)
}

// TestConnection verifies the function exists via GetFunction.
// Matches the s3 adapter's contract so handlers.TestConnectorDriver
// picks it up automatically.
func (a *Adapter) TestConnection(ctx context.Context, raw json.RawMessage) (adapters.ConnectionTestResult, error) {
	cfg, err := lambdadriver.ConfigFromJSON(raw)
	if err != nil {
		return adapters.ConnectionTestResult{Success: false, Message: err.Error()}, nil
	}
	d, err := a.driverFactory(ctx, cfg)
	if err != nil {
		return adapters.ConnectionTestResult{Success: false, Message: err.Error()}, nil
	}
	if err := d.Connect(ctx); err != nil {
		return adapters.ConnectionTestResult{Success: false, Message: err.Error()}, nil
	}
	return adapters.ConnectionTestResult{
		Success: true,
		Message: fmt.Sprintf("lambda function %q reachable", cfg.FunctionName),
	}, nil
}

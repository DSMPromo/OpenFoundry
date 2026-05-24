// Package athena is the connector-management adapter for AWS Athena.
// Operators register an Athena workgroup as a connection; the
// adapter exposes it as a virtual source whose QueryVirtualTable
// runs the supplied SQL synchronously and returns the result rows.
package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	athenadriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/athena"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	ConnectorType     = "athena"
	defaultSourceKind = "athena_workgroup"

	// defaultQueryTimeout caps how long the adapter polls for a
	// synchronous query to settle before returning a timeout error.
	// Athena queries can run for minutes — adapter callers that need
	// long-running queries should switch to an async pattern.
	defaultQueryTimeout = 60 * time.Second
)

type Adapter struct {
	driverFactory driverFactory
}

type driverFactory func(ctx context.Context, cfg athenadriver.Config) (driver, error)

type driver interface {
	Connect(ctx context.Context) error
	Run(ctx context.Context, sql string) (*athenadriver.QueryResult, error)
}

func New() *Adapter { return &Adapter{driverFactory: defaultDriverFactory} }

func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

func (a *Adapter) SetDriverFactory(f driverFactory) {
	if f != nil {
		a.driverFactory = f
	}
}

func defaultDriverFactory(ctx context.Context, cfg athenadriver.Config) (driver, error) {
	return athenadriver.New(ctx, cfg)
}

func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("athena connector requires at least an empty config (defaults workgroup=primary)")
	}
	_, err := athenadriver.ConfigFromJSON(raw)
	return err
}

func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("athena: connection is nil")
	}
	cfg, err := athenadriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	display := cfg.WorkGroup
	if cfg.Database != "" {
		display = fmt.Sprintf("%s/%s", cfg.WorkGroup, cfg.Database)
	}
	metadata, _ := json.Marshal(map[string]any{
		"workgroup":       cfg.WorkGroup,
		"database":        cfg.Database,
		"output_location": cfg.OutputLocation,
	})
	return []adapters.Source{{
		Selector:         cfg.WorkGroup,
		DisplayName:      display,
		SourceKind:       defaultSourceKind,
		SupportsSync:     false, // Athena is query-on-demand, not a stream.
		SupportsZeroCopy: false,
		Metadata:         metadata,
	}}, nil
}

// QueryVirtualTable runs the SQL carried in Filters[0] (until a
// dedicated SQL field lands on VirtualTableQueryRequest) and
// returns the result rows. Limit gets appended as a SQL LIMIT
// clause when the request set one and the SQL didn't.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if c == nil || q == nil {
		return nil, errors.New("athena: connection or query is nil")
	}
	cfg, err := athenadriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	sql := strings.TrimSpace(q.Selector)
	if sql == "" && len(q.Filters) > 0 {
		sql = strings.TrimSpace(q.Filters[0])
	}
	if sql == "" {
		return nil, errors.New("athena: query selector or filters[0] must contain SQL")
	}
	if q.Limit != nil && *q.Limit > 0 && !strings.Contains(strings.ToLower(sql), " limit ") {
		sql = fmt.Sprintf("%s LIMIT %d", sql, *q.Limit)
	}

	timeout := defaultQueryTimeout
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > timeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	d, err := a.driverFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	res, err := d.Run(ctx, sql)
	if err != nil {
		return nil, err
	}
	rows := make([]json.RawMessage, 0, len(res.Rows))
	for _, row := range res.Rows {
		raw, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("athena: marshal row: %w", err)
		}
		rows = append(rows, raw)
	}
	return &adapters.Result{
		Selector: cfg.WorkGroup,
		Mode:     "athena_query",
		Columns:  res.Columns,
		RowCount: len(rows),
		Rows:     rows,
		Metadata: mustJSON(map[string]string{"query_execution_id": res.QueryExecutionID}),
	}, nil
}

func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: athena arrow streaming", adapters.ErrNotImplemented)
}

func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: athena ingestion bridge (use the s3 / Iceberg path for table replication)", adapters.ErrNotImplemented)
}

func (a *Adapter) TestConnection(ctx context.Context, raw json.RawMessage) (adapters.ConnectionTestResult, error) {
	cfg, err := athenadriver.ConfigFromJSON(raw)
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
		Message: fmt.Sprintf("athena workgroup %q reachable", cfg.WorkGroup),
	}, nil
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

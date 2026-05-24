// Package dynamodb is the connector-management adapter for DynamoDB.
// Operators register a table as a connection; the adapter exposes
// it as a virtual source with QueryVirtualTable backed by Scan
// (items → result rows).
package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	ddbdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/dynamodb"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	ConnectorType     = "dynamodb"
	defaultSourceKind = "dynamodb_table"
)

type Adapter struct {
	driverFactory driverFactory
}

type driverFactory func(ctx context.Context, cfg ddbdriver.Config) (driver, error)

type driver interface {
	Connect(ctx context.Context) error
	Scan(ctx context.Context, limit int32) ([]map[string]any, error)
}

func New() *Adapter { return &Adapter{driverFactory: defaultDriverFactory} }

func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

func (a *Adapter) SetDriverFactory(f driverFactory) {
	if f != nil {
		a.driverFactory = f
	}
}

func defaultDriverFactory(ctx context.Context, cfg ddbdriver.Config) (driver, error) {
	return ddbdriver.New(ctx, cfg)
}

func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("dynamodb connector requires 'table_name'")
	}
	_, err := ddbdriver.ConfigFromJSON(raw)
	return err
}

func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("dynamodb: connection is nil")
	}
	cfg, err := ddbdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(map[string]any{"table_name": cfg.TableName})
	return []adapters.Source{{
		Selector:         cfg.TableName,
		DisplayName:      cfg.TableName,
		SourceKind:       defaultSourceKind,
		SupportsSync:     true,
		SupportsZeroCopy: false,
		Metadata:         metadata,
	}}, nil
}

// QueryVirtualTable scans the table (Limit-capped) and returns each
// item as a result row. Column list is the union of keys observed
// across the scanned items so callers see all attributes that exist
// in this page.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if c == nil || q == nil {
		return nil, errors.New("dynamodb: connection or query is nil")
	}
	cfg, err := ddbdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	d, err := a.driverFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	limit := int32(50)
	if q.Limit != nil && *q.Limit > 0 {
		limit = int32(*q.Limit)
	}
	items, err := d.Scan(ctx, limit)
	if err != nil {
		return nil, err
	}
	columns := collectColumns(items)
	rows := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		row, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("dynamodb: marshal row: %w", err)
		}
		rows = append(rows, row)
	}
	return &adapters.Result{
		Selector: cfg.TableName,
		Mode:     "dynamodb_scan",
		Columns:  columns,
		RowCount: len(rows),
		Rows:     rows,
	}, nil
}

func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: dynamodb arrow streaming", adapters.ErrNotImplemented)
}

func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil || src == nil {
		return nil, errors.New("dynamodb: connection or source is nil")
	}
	cfg, err := ddbdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"selector": src.Selector, "table_name": cfg.TableName})
	if err != nil {
		return nil, err
	}
	return &adapters.IngestSpec{Name: c.Name, Namespace: "default", Source: ConnectorType, Config: raw}, nil
}

func (a *Adapter) TestConnection(ctx context.Context, raw json.RawMessage) (adapters.ConnectionTestResult, error) {
	cfg, err := ddbdriver.ConfigFromJSON(raw)
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
		Message: fmt.Sprintf("dynamodb table %q reachable", cfg.TableName),
	}, nil
}

// collectColumns returns the union of keys across items, preserving
// stable insertion order (first item's keys first, then any new keys
// from subsequent items).
func collectColumns(items []map[string]any) []string {
	seen := map[string]bool{}
	var cols []string
	for _, item := range items {
		for k := range item {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	return cols
}

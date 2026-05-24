// Package sqs is the connector-management adapter for AWS SQS.
// Operators register a queue as a connection; the adapter exposes
// it as a virtual source with QueryVirtualTable backed by
// ReceiveMessage (messages → result rows).
package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	sqsdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/sqs"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	ConnectorType     = "sqs"
	defaultSourceKind = "sqs_queue"
)

// Adapter is the SQS connector adapter. Stateless apart from the
// optional driverFactory test seam.
type Adapter struct {
	driverFactory driverFactory
}

type driverFactory func(ctx context.Context, cfg sqsdriver.Config) (driver, error)

// driver is the subset of *sqsdriver.Driver the adapter uses.
type driver interface {
	Connect(ctx context.Context) error
	Receive(ctx context.Context, max int32, waitSeconds int32) ([]sqsdriver.Message, error)
}

// New returns a fresh Adapter wired to the production driver factory.
func New() *Adapter { return &Adapter{driverFactory: defaultDriverFactory} }

// Factory yields a singleton Adapter.
func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

// SetDriverFactory swaps the driver constructor (tests only).
func (a *Adapter) SetDriverFactory(f driverFactory) {
	if f != nil {
		a.driverFactory = f
	}
}

func defaultDriverFactory(ctx context.Context, cfg sqsdriver.Config) (driver, error) {
	return sqsdriver.New(ctx, cfg)
}

// ValidateConfig surfaces required-field violations early.
func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("sqs connector requires 'queue_url' or 'queue_name'")
	}
	_, err := sqsdriver.ConfigFromJSON(raw)
	return err
}

// DiscoverSources surfaces the configured queue as a single source.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("sqs: connection is nil")
	}
	cfg, err := sqsdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	selector := cfg.QueueURL
	if selector == "" {
		selector = cfg.QueueName
	}
	display := selector
	metadata, err := json.Marshal(map[string]any{
		"queue_url":  cfg.QueueURL,
		"queue_name": cfg.QueueName,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: marshal source metadata: %w", err)
	}
	return []adapters.Source{{
		Selector:         selector,
		DisplayName:      display,
		SourceKind:       defaultSourceKind,
		SupportsSync:     true, // a future ingestion-replication-service hook can drain into a dataset.
		SupportsZeroCopy: false,
		Metadata:         metadata,
	}}, nil
}

// QueryVirtualTable polls the queue and projects each message as a
// row {message_id, body, attributes}. The query Limit caps the
// ReceiveMessage MaxNumberOfMessages (1-10 per SQS contract).
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if c == nil || q == nil {
		return nil, errors.New("sqs: connection or query is nil")
	}
	cfg, err := sqsdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	d, err := a.driverFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	max := int32(10)
	if q.Limit != nil && *q.Limit > 0 && *q.Limit < 10 {
		max = int32(*q.Limit)
	}
	msgs, err := d.Receive(ctx, max, 0)
	if err != nil {
		return nil, err
	}
	rows := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		row, err := json.Marshal(map[string]any{
			"message_id": m.MessageID,
			"body":       m.Body,
			"attributes": m.Attributes,
		})
		if err != nil {
			return nil, fmt.Errorf("sqs: marshal row: %w", err)
		}
		rows = append(rows, row)
	}
	selector := cfg.QueueURL
	if selector == "" {
		selector = cfg.QueueName
	}
	return &adapters.Result{
		Selector: selector,
		Mode:     "sqs_receive",
		Columns:  []string{"message_id", "body", "attributes"},
		RowCount: len(rows),
		Rows:     rows,
	}, nil
}

// StreamArrow is unsupported — SQS messages are streamed via the
// ReceiveMessage loop, not Arrow IPC.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: sqs arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec emits a placeholder spec sufficient for the
// ingestion-replication-service to discover the source by URL —
// the actual stream-replay hookup lands in its own PR.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil || src == nil {
		return nil, errors.New("sqs: connection or source is nil")
	}
	cfg, err := sqsdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{
		"selector":   src.Selector,
		"queue_url":  cfg.QueueURL,
		"queue_name": cfg.QueueName,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{Name: c.Name, Namespace: "default", Source: ConnectorType, Config: raw}, nil
}

// TestConnection wraps Connect.
func (a *Adapter) TestConnection(ctx context.Context, raw json.RawMessage) (adapters.ConnectionTestResult, error) {
	cfg, err := sqsdriver.ConfigFromJSON(raw)
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
	id := cfg.QueueURL
	if id == "" {
		id = cfg.QueueName
	}
	return adapters.ConnectionTestResult{
		Success: true,
		Message: fmt.Sprintf("sqs queue %q reachable", strings.TrimSpace(id)),
	}, nil
}

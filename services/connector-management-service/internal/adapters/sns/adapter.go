// Package sns is the connector-management adapter for AWS SNS.
// SNS is publish-only from this service's perspective: the adapter
// supports TestConnection (GetTopicAttributes) and DiscoverSources
// (returns the topic as a single source) but not QueryVirtualTable
// or StreamArrow since SNS doesn't expose a read surface — fan-out
// is delivered through subscriptions to other services (SQS,
// Lambda, HTTP). Publishing through OpenFoundry pipelines lands in
// a follow-up PR that wires a notification-style egress.
package sns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	snsdriver "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/drivers/sns"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	ConnectorType     = "sns"
	defaultSourceKind = "sns_topic"
)

type Adapter struct {
	driverFactory driverFactory
}

type driverFactory func(ctx context.Context, cfg snsdriver.Config) (driver, error)

type driver interface {
	Connect(ctx context.Context) error
}

func New() *Adapter { return &Adapter{driverFactory: defaultDriverFactory} }

func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

func (a *Adapter) SetDriverFactory(f driverFactory) {
	if f != nil {
		a.driverFactory = f
	}
}

func defaultDriverFactory(ctx context.Context, cfg snsdriver.Config) (driver, error) {
	return snsdriver.New(ctx, cfg)
}

func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("sns connector requires 'topic_arn'")
	}
	_, err := snsdriver.ConfigFromJSON(raw)
	return err
}

func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("sns: connection is nil")
	}
	cfg, err := snsdriver.ConfigFromJSON(c.Config)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{"topic_arn": cfg.TopicARN})
	if err != nil {
		return nil, fmt.Errorf("sns: marshal source metadata: %w", err)
	}
	return []adapters.Source{{
		Selector:         cfg.TopicARN,
		DisplayName:      cfg.TopicARN,
		SourceKind:       defaultSourceKind,
		SupportsSync:     false,
		SupportsZeroCopy: false,
		Metadata:         metadata,
	}}, nil
}

// QueryVirtualTable is unsupported — SNS has no read surface.
func (a *Adapter) QueryVirtualTable(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (*adapters.Result, error) {
	return nil, fmt.Errorf("%w: sns query (subscribe via SQS / Lambda instead)", adapters.ErrNotImplemented)
}

func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: sns arrow streaming", adapters.ErrNotImplemented)
}

func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: sns ingest spec (sns is egress-only)", adapters.ErrNotImplemented)
}

func (a *Adapter) TestConnection(ctx context.Context, raw json.RawMessage) (adapters.ConnectionTestResult, error) {
	cfg, err := snsdriver.ConfigFromJSON(raw)
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
		Message: fmt.Sprintf("sns topic %q reachable", cfg.TopicARN),
	}, nil
}

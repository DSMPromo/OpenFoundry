// Package iot is the Go port of the Rust IoT / IIoT connector that
// lives in `services/connector-management-service/src/connectors/iot.rs`.
//
// The Rust connector is a productive MQTT 3.1.1 client backed by
// `rumqttc` that subscribes to topic filters and drains messages into
// an Arrow IPC payload for downstream version-creation. Go's
// ecosystem has equivalent libraries (eclipse/paho.mqtt.golang,
// at-wat/mqtt) but go.mod has not yet adopted one — pulling in an
// MQTT client is a separate dependency decision. This package
// therefore ports every pure-function helper 1:1 (validate_config,
// topic_filters, resolved_port, subscription_qos, max_messages, …)
// and leaves the live broker capabilities returning
// [adapters.ErrNotImplemented] behind a single `mqttRunner` seam so
// the production driver can be wired in without rewriting the
// adapter shape.
//
// JDBC follows the same pattern (see internal/adapters/jdbc); this
// keeps the pipeline-build / connector-management slices that depend
// on adapter registration unblocked while the broader connector-
// runtime parity work continues.
package iot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under.
	ConnectorType = "iot"

	sourceKindMQTTTopic = "mqtt_topic"

	defaultPort              = 1883
	defaultTLSPort           = 8883
	defaultKeepAliveSecs     = int64(30)
	defaultConnectTimeoutMs  = int64(5_000)
	defaultDiscoveryWindowMs = int64(2_000)
	defaultMaxMessages       = int64(1_000)
	defaultFetchWindowMs     = int64(5_000)
	defaultPreviewLimit      = 50
	defaultPreviewWindowMs   = int64(2_000)

	keepAliveClampLow      = int64(5)
	keepAliveClampHigh     = int64(600)
	connectTimeoutClampLow = int64(500)
	connectTimeoutClampHi  = int64(60_000)
	discoveryWindowLow     = int64(100)
	discoveryWindowHigh    = int64(30_000)
	fetchWindowLow         = int64(100)
	fetchWindowHigh        = int64(600_000)
	maxMessagesLow         = int64(1)
	maxMessagesHigh        = int64(1_000_000)
)

// QoS mirrors the MQTT QoS levels the Rust connector accepts (0 / 1 /
// 2 → AtMostOnce / AtLeastOnce / ExactlyOnce). Exposed as a typed
// value so future MQTT integrations don't have to reinvent the
// translation.
type QoS int

const (
	QoSAtMostOnce  QoS = 0
	QoSAtLeastOnce QoS = 1
	QoSExactlyOnce QoS = 2
)

// Adapter implements [adapters.ConnectorAdapter] for MQTT-based IoT
// sources. It is safe for concurrent use; the embedded runner is
// stateless until an MQTT driver is wired in.
type Adapter struct {
	runner mqttRunner
}

// New returns a ready-to-use [Adapter] that returns
// [adapters.ErrNotImplemented] from any capability that requires a
// live MQTT broker. Tests / future production wiring can replace the
// runner via [Adapter.SetRunner].
func New() *Adapter {
	return &Adapter{runner: stubRunner{}}
}

// Factory returns an [adapters.Factory] that constructs fresh IoT
// adapters; the registry stores the factory and asks for an instance
// per request so per-connection state stays scoped.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetRunner overrides the [mqttRunner] backing live broker
// operations. Intended for tests and the eventual MQTT-driver wiring.
func (a *Adapter) SetRunner(runner mqttRunner) {
	if runner != nil {
		a.runner = runner
	}
}

type iotConfig struct {
	BrokerHost        string   `json:"broker_host"`
	BrokerPort        *int64   `json:"broker_port"`
	TLS               bool     `json:"tls"`
	Username          string   `json:"username"`
	Password          string   `json:"password"`
	ClientID          string   `json:"client_id"`
	KeepAliveSecs     *int64   `json:"keep_alive_secs"`
	ConnectTimeoutMs  *int64   `json:"connect_timeout_ms"`
	DiscoveryWindowMs *int64   `json:"discovery_window_ms"`
	MaxMessages       *int64   `json:"max_messages"`
	MaxDurationMs     *int64   `json:"max_duration_ms"`
	QoS               *int64   `json:"qos"`
	Topic             string   `json:"topic"`
	Topics            []string `json:"topics"`
}

func parseConfig(raw json.RawMessage) (*iotConfig, error) {
	cfg := &iotConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("iot: invalid config: %w", err)
	}
	return cfg, nil
}

// ValidateConfig mirrors Rust's `validate_config`: a non-empty
// `broker_host` plus at least one topic via `topic` or `topics[]`.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.BrokerHost) == "" {
		return errors.New("iot connector requires 'broker_host'")
	}
	if len(topicFilters(cfg)) == 0 {
		return errors.New("iot connector requires 'topic' or non-empty 'topics'")
	}
	return nil
}

// DiscoverSources mirrors Rust's `discover_sources`. With the live
// runner replaced (test / production wiring), observed topics are
// returned; otherwise the configured filters are returned as-is so
// callers still get a stable catalog.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	filters := topicFilters(cfg)

	observed, runnerErr := a.runner.Drain(ctx, runnerOpts{
		Config:      cfg,
		Filters:     filters,
		MaxMessages: -1, // unbounded — discovery uses window only
		WindowMs:    discoveryWindowMs(cfg),
		Purpose:     "discover",
	})

	if runnerErr == nil && len(observed) > 0 {
		seen := map[string]struct{}{}
		ordered := make([]string, 0, len(observed))
		for _, msg := range observed {
			topic := msg.Topic
			if topic == "" {
				continue
			}
			if _, ok := seen[topic]; ok {
				continue
			}
			seen[topic] = struct{}{}
			ordered = append(ordered, topic)
		}
		out := make([]adapters.Source, 0, len(ordered))
		for _, topic := range ordered {
			meta, _ := json.Marshal(map[string]any{"topic": topic, "observed": true})
			out = append(out, adapters.Source{
				Selector:         topic,
				DisplayName:      topic,
				SourceKind:       sourceKindMQTTTopic,
				SupportsSync:     true,
				SupportsZeroCopy: true,
				Metadata:         meta,
			})
		}
		return out, nil
	}

	out := make([]adapters.Source, 0, len(filters))
	for _, filter := range filters {
		meta, _ := json.Marshal(map[string]any{"filter": filter, "observed": false})
		out = append(out, adapters.Source{
			Selector:         filter,
			DisplayName:      filter,
			SourceKind:       sourceKindMQTTTopic,
			SupportsSync:     true,
			SupportsZeroCopy: true,
			Metadata:         meta,
		})
	}
	return out, nil
}

// QueryVirtualTable mirrors Rust's `query_virtual_table`: drains a
// short window of messages from the broker (or returns
// [adapters.ErrNotImplemented] when no live runner is wired in) and
// surfaces the rows in the standard virtual-table envelope.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("iot: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	limit := defaultPreviewLimit
	if q.Limit != nil {
		limit = *q.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	filters := selectorFilters(cfg, q.Selector)
	messages, err := a.runner.Drain(ctx, runnerOpts{
		Config:      cfg,
		Filters:     filters,
		MaxMessages: int64(limit),
		WindowMs:    defaultPreviewWindowMs,
		Purpose:     "preview",
	})
	if err != nil {
		return nil, err
	}
	rawRows := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		buf, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("iot: marshal preview row: %w", err)
		}
		rawRows = append(rawRows, buf)
	}
	meta, _ := json.Marshal(map[string]any{
		"selector":    q.Selector,
		"filters":     filters,
		"broker_host": cfg.BrokerHost,
		"broker_port": resolvedPort(cfg),
		"tls":         cfg.TLS,
		"messages":    int64(len(messages)),
		"window_ms":   defaultPreviewWindowMs,
	})
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "zero_copy",
		Columns: []string{
			"topic",
			"payload",
			"qos",
			"retained",
			"received_at",
		},
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow mirrors Rust's `fetch_dataset` ingest path. With the
// stub runner this returns [adapters.ErrNotImplemented]; with a live
// runner it materialises the drained messages as a single Arrow IPC
// frame for dataset-versioning-service to ingest as a new version.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: iot arrow streaming requires an MQTT driver", adapters.ErrNotImplemented)
}

// BuildIngestSpec emits the `iot` source variant the bridge forwards
// to ingestion-replication-service. The selector becomes the topic
// filter; broker host / port / tls flow through verbatim so the
// bridge can re-establish the MQTT subscription at sync time.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("iot: connection is nil")
	}
	if src == nil {
		return nil, errors.New("iot: source is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BrokerHost) == "" {
		return nil, errors.New("iot: connection config missing 'broker_host'")
	}
	specCfg := map[string]any{
		"broker_host": cfg.BrokerHost,
		"broker_port": resolvedPort(cfg),
		"tls":         cfg.TLS,
		"topic":       src.Selector,
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("iot: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*iotConfig, error) {
	if c == nil {
		return nil, errors.New("iot: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BrokerHost) == "" {
		return nil, errors.New("iot connector requires 'broker_host'")
	}
	if len(topicFilters(cfg)) == 0 {
		return nil, errors.New("iot connector requires 'topic' or non-empty 'topics'")
	}
	return cfg, nil
}

// topicFilters mirrors Rust's `topic_filters`: prefer the
// `topics[]` array when non-empty, fall back to `topic`.
func topicFilters(cfg *iotConfig) []string {
	if len(cfg.Topics) > 0 {
		out := make([]string, 0, len(cfg.Topics))
		for _, t := range cfg.Topics {
			if strings.TrimSpace(t) != "" {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if strings.TrimSpace(cfg.Topic) != "" {
		return []string{cfg.Topic}
	}
	return nil
}

func selectorFilters(cfg *iotConfig, selector string) []string {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return topicFilters(cfg)
	}
	return []string{trimmed}
}

// resolvedPort mirrors Rust's `resolved_port`: explicit `broker_port`
// wins; otherwise default 1883 (or 8883 when `tls=true`).
func resolvedPort(cfg *iotConfig) int {
	if cfg.BrokerPort != nil {
		return int(*cfg.BrokerPort)
	}
	if cfg.TLS {
		return defaultTLSPort
	}
	return defaultPort
}

func subscriptionQoS(cfg *iotConfig) QoS {
	v := int64(0)
	if cfg.QoS != nil {
		v = *cfg.QoS
	}
	switch v {
	case 0:
		return QoSAtMostOnce
	case 1:
		return QoSAtLeastOnce
	default:
		return QoSExactlyOnce
	}
}

func fetchWindowMs(cfg *iotConfig) int64 {
	v := defaultFetchWindowMs
	if cfg.MaxDurationMs != nil {
		v = *cfg.MaxDurationMs
	}
	return clamp64(v, fetchWindowLow, fetchWindowHigh)
}

func discoveryWindowMs(cfg *iotConfig) int64 {
	v := defaultDiscoveryWindowMs
	if cfg.DiscoveryWindowMs != nil {
		v = *cfg.DiscoveryWindowMs
	}
	return clamp64(v, discoveryWindowLow, discoveryWindowHigh)
}

func keepAliveSecs(cfg *iotConfig) int64 {
	v := defaultKeepAliveSecs
	if cfg.KeepAliveSecs != nil {
		v = *cfg.KeepAliveSecs
	}
	return clamp64(v, keepAliveClampLow, keepAliveClampHigh)
}

func connectTimeoutMs(cfg *iotConfig) int64 {
	v := defaultConnectTimeoutMs
	if cfg.ConnectTimeoutMs != nil {
		v = *cfg.ConnectTimeoutMs
	}
	return clamp64(v, connectTimeoutClampLow, connectTimeoutClampHi)
}

func maxMessages(cfg *iotConfig) int64 {
	v := defaultMaxMessages
	if cfg.MaxMessages != nil {
		v = *cfg.MaxMessages
	}
	return clamp64(v, maxMessagesLow, maxMessagesHigh)
}

func sanitizeFileName(selector string) string {
	out := make([]rune, 0, len(selector))
	for _, ch := range selector {
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') {
			out = append(out, ch)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

func clamp64(v, low, high int64) int64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// Message is the row shape produced by the runner — one MQTT publish
// captured as JSON-serialisable fields. Mirrors the Rust Arrow row
// schema (`topic`, `payload`, `qos`, `retained`, `received_at`).
type Message struct {
	Topic      string          `json:"topic"`
	Payload    json.RawMessage `json:"payload"`
	QoS        string          `json:"qos"`
	Retained   bool            `json:"retained"`
	ReceivedAt string          `json:"received_at"`
}

// runnerOpts is the parameter bundle the runner accepts. Kept private
// so the live MQTT driver can grow new knobs without breaking
// callers.
type runnerOpts struct {
	Config      *iotConfig
	Filters     []string
	MaxMessages int64
	WindowMs    int64
	Purpose     string
}

// mqttRunner is the seam an MQTT driver satisfies. The default
// [stubRunner] returns [adapters.ErrNotImplemented]; tests / future
// production wiring replace it via [Adapter.SetRunner].
type mqttRunner interface {
	Drain(ctx context.Context, opts runnerOpts) ([]Message, error)
}

type stubRunner struct{}

// Drain is the no-MQTT-driver path. It returns
// [adapters.ErrNotImplemented] so the dispatcher can translate the
// failure into the standard envelope without the adapter needing to
// know about HTTP status codes.
func (stubRunner) Drain(_ context.Context, _ runnerOpts) ([]Message, error) {
	return nil, fmt.Errorf("%w: iot connector requires an MQTT driver", adapters.ErrNotImplemented)
}

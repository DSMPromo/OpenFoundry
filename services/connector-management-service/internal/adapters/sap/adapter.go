// Package sap is the Go port of the Rust SAP OData connector that
// lives in `services/connector-management-service/src/connectors/sap.rs`.
//
// Capabilities mirrored from the Rust module:
//
//   - DiscoverSources    — inline `entities[]` short-circuit, otherwise
//     a GET against the OData service root and the standard
//     `d.EntitySets[]` / `value[]` shapes are normalised.
//   - QueryVirtualTable  — bounded fetch of one entity set, JSON rows
//     clamped to [1, 500].
//   - StreamArrow        — not exposed by the Rust connector; returns
//     [adapters.ErrNotImplemented].
//   - BuildIngestSpec    — placeholder spec the bridge forwards to
//     ingestion-replication-service; selector is the entity set name.
//
// HTTP egress: stdlib http.Client, Bearer + custom-header auth. The
// Rust `http_runtime` module that supports connector-agent proxying
// and EgressPolicy validation is not yet ported (same gap as the
// other HTTP-based adapters); per-instance overrides via
// [Adapter.SetHTTPClient] keep the test surface symmetric with the
// salesforce / bigquery adapters.
package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under. Mirrors the Rust dispatch arm.
	ConnectorType = "sap"

	defaultSourceKind   = "sap_entity_set"
	defaultPreviewLimit = 50
)

// Adapter implements [adapters.ConnectorAdapter] for SAP OData. It is
// safe for concurrent use; the embedded HTTP client is reused across
// requests.
type Adapter struct {
	httpClient *http.Client
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{httpClient: http.DefaultClient}
}

// Factory returns an [adapters.Factory] that constructs fresh SAP
// adapters; the registry stores the factory and asks for an instance
// per request so per-connection state stays scoped.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded [http.Client]. Intended for
// tests pointing at an httptest fake-OData server.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

type sapConfig struct {
	BaseURL     string            `json:"base_url"`
	ServicePath string            `json:"service_path"`
	BearerToken string            `json:"bearer_token"`
	Headers     map[string]string `json:"headers"`
	Entities    []json.RawMessage `json:"entities"`
}

func parseConfig(raw json.RawMessage) (*sapConfig, error) {
	cfg := &sapConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("sap: invalid config: %w", err)
	}
	return cfg, nil
}

// ValidateConfig mirrors Rust's `validate_config`: a non-empty
// `base_url` is the only required identity field.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New("sap connector requires 'base_url'")
	}
	return nil
}

// DiscoverSources mirrors Rust's `discover_sources`. Inline
// `entities[]` short-circuits the HTTP path; otherwise the OData
// service root is fetched and `d.EntitySets[]` / `value[]` shapes are
// normalised.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	if len(cfg.Entities) > 0 {
		return entitiesFromConfig(cfg.Entities), nil
	}

	u, err := serviceRoot(cfg)
	if err != nil {
		return nil, err
	}
	body, err := a.fetch(ctx, cfg, u)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("sap: decode service root: %w", err)
	}

	if names := odataEntitySetNames(payload); len(names) > 0 {
		out := make([]adapters.Source, 0, len(names))
		for _, name := range names {
			meta, _ := json.Marshal(map[string]any{})
			out = append(out, adapters.Source{
				Selector:         name,
				DisplayName:      name,
				SourceKind:       defaultSourceKind,
				SupportsSync:     true,
				SupportsZeroCopy: true,
				Metadata:         meta,
			})
		}
		return out, nil
	}

	out := make([]adapters.Source, 0)
	if obj, ok := payload.(map[string]any); ok {
		if list, ok := obj["value"].([]any); ok {
			for _, item := range list {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				selector := stringOrEmpty(entry, "url", "name")
				if selector == "" {
					continue
				}
				display := stringOrEmpty(entry, "title", "name")
				if display == "" {
					display = selector
				}
				meta, _ := json.Marshal(entry)
				out = append(out, adapters.Source{
					Selector:         selector,
					DisplayName:      display,
					SourceKind:       defaultSourceKind,
					SupportsSync:     true,
					SupportsZeroCopy: true,
					Metadata:         meta,
				})
			}
		}
	}
	return out, nil
}

// QueryVirtualTable mirrors Rust's `query_virtual_table`: fetches the
// entity set, normalises (`d.results[]` / `value[]` / array root),
// and returns rows clamped to [1, 500].
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("sap: query request is nil")
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

	entityURL, err := buildEntityURL(cfg, q.Selector)
	if err != nil {
		return nil, err
	}
	body, err := a.fetch(ctx, cfg, entityURL)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("sap: decode entity payload: %w", err)
	}
	rows := normalizeEntityRows(payload)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("sap: marshal preview row: %w", err)
		}
		rawRows = append(rawRows, buf)
	}
	meta, _ := json.Marshal(map[string]any{
		"selector": q.Selector,
		"url":      entityURL.String(),
	})
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "zero_copy",
		Columns:  columnsFromRows(rawRows),
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow returns [adapters.ErrNotImplemented]: the Rust SAP
// connector does not expose `stream_arrow_ipc`; sync rows go through
// the JSON `fetch_dataset` path.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: sap arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec emits the `sap` source variant the bridge forwards
// to ingestion-replication-service. The selector becomes the entity
// set name; base URL + service path are passed through verbatim so
// the bridge can re-fetch the entity at sync time.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("sap: connection is nil")
	}
	if src == nil {
		return nil, errors.New("sap: source is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("sap: connection config missing 'base_url'")
	}
	specCfg := map[string]any{
		"base_url": cfg.BaseURL,
		"entity":   src.Selector,
	}
	if cfg.ServicePath != "" {
		specCfg["service_path"] = cfg.ServicePath
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("sap: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*sapConfig, error) {
	if c == nil {
		return nil, errors.New("sap: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("sap connector requires 'base_url'")
	}
	return cfg, nil
}

func (a *Adapter) fetch(ctx context.Context, cfg *sapConfig, u *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("sap: build %s: %w", u.String(), err)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sap: transport error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("SAP service returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func entitiesFromConfig(entities []json.RawMessage) []adapters.Source {
	out := make([]adapters.Source, 0, len(entities))
	for _, raw := range entities {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		selector := stringOrEmpty(obj, "selector", "name")
		if selector == "" {
			continue
		}
		display := stringOrEmpty(obj, "display_name", "label")
		if display == "" {
			display = selector
		}
		meta, _ := json.Marshal(obj)
		out = append(out, adapters.Source{
			Selector:         selector,
			DisplayName:      display,
			SourceKind:       defaultSourceKind,
			SupportsSync:     true,
			SupportsZeroCopy: true,
			Metadata:         meta,
		})
	}
	return out
}

// odataEntitySetNames extracts `d.EntitySets[]` from an OData $metadata
// response. Mirrors the Rust shape exactly.
func odataEntitySetNames(payload any) []string {
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	d, ok := obj["d"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := d["EntitySets"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// normalizeEntityRows mirrors Rust's `normalize_entity_rows`: peels off
// the OData `d.results` envelope, falls back to `value`, and finally
// unwraps a top-level array or wraps a scalar.
func normalizeEntityRows(payload any) []any {
	if obj, ok := payload.(map[string]any); ok {
		if d, ok := obj["d"].(map[string]any); ok {
			if rows, ok := d["results"].([]any); ok {
				return rows
			}
		}
		if rows, ok := obj["value"].([]any); ok {
			return rows
		}
	}
	if list, ok := payload.([]any); ok {
		return list
	}
	return []any{payload}
}

func serviceRoot(cfg *sapConfig) (*url.URL, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("sap: parse base_url: %w", err)
	}
	servicePath := cfg.ServicePath
	if servicePath == "" {
		servicePath = "/"
	}
	rel, err := url.Parse(servicePath)
	if err != nil {
		return nil, fmt.Errorf("sap: parse service_path: %w", err)
	}
	return base.ResolveReference(rel), nil
}

func buildEntityURL(cfg *sapConfig, selector string) (*url.URL, error) {
	root, err := serviceRoot(cfg)
	if err != nil {
		return nil, err
	}
	rel, err := url.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("sap: parse selector: %w", err)
	}
	return root.ResolveReference(rel), nil
}

func stringOrEmpty(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func columnsFromRows(rows []json.RawMessage) []string {
	for _, row := range rows {
		dec := json.NewDecoder(strings.NewReader(string(row)))
		tok, err := dec.Token()
		if err != nil {
			continue
		}
		delim, ok := tok.(json.Delim)
		if !ok || delim != '{' {
			continue
		}
		out := []string{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			key, ok := tok.(string)
			if !ok {
				break
			}
			out = append(out, key)
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				break
			}
		}
		return out
	}
	return []string{}
}

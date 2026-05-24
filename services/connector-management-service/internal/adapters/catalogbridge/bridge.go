// Package catalogbridge is the Go port of the Rust
// `services/connector-management-service/src/connectors/catalog_bridge.rs`
// helper module that backs the "thin" tabular adapters (Tableau, Power BI,
// ODBC, JDBC, …). Each per-connector adapter is a one-screen wrapper that
// supplies its connector_name, default_source_kind, and identity_fields and
// delegates capability methods here.
//
// Capability surface ported from the Rust helper:
//
//   - ValidateConfig       — rejects configs that lack both an inline
//     catalog and the bridge fields (`base_url` + `catalog_path` /
//     `*_path_template`).
//   - DiscoverSources      — returns inline `tables/views/datasets/streams/
//     reports/entities` entries OR fetches a remote catalog and normalises
//     the wrapped envelopes (`data`/`items`/`records`/`value`).
//   - QueryVirtualTable    — returns inline `sample_rows`/`preview_rows`
//     OR fetches the resource template URL and clamps to [1, 500] rows.
//
// Capabilities NOT exposed by the Rust helper (and therefore returned by
// callers as [adapters.ErrNotImplemented]): Arrow IPC streaming and
// IngestSpec construction. The Rust connectors that delegate to this
// helper expose only `validate_config`, `test_connection`, `fetch_dataset`,
// `discover_sources`, and `query_virtual_table`.
//
// HTTP egress note: the Rust helper routes through `http_runtime`, which
// in turn supports the connector-agent proxy + `EgressPolicy::validate_url`
// gate. The Go port keeps the same JSON envelope shape but uses the
// stdlib http client directly — agent proxying / egress validation are
// follow-up work tracked under the broader connector-runtime parity plan.
// Adapters can override the client via [Bridge.HTTPClient].
package catalogbridge

import (
	"bytes"
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

// Bridge is the per-adapter helper. Each per-connector adapter constructs
// one with [New] and forwards its capability methods to the bridge methods
// defined here.
type Bridge struct {
	// ConnectorName matches the Rust `CONNECTOR_NAME` constant — used in
	// validation errors and metadata envelopes ("tableau", "power_bi",
	// "odbc", "jdbc", …).
	ConnectorName string
	// DefaultSourceKind is emitted on discovered sources when the inline
	// catalog entry / remote payload does not override it. Mirrors the
	// Rust `DEFAULT_SOURCE_KIND` constant ("tableau_view", "power_bi_dataset",
	// …).
	DefaultSourceKind string
	// IdentityFields are the per-connector configuration keys the Rust
	// helper requires when the config uses a `*_path_template` (e.g.
	// `site_id` for Tableau). [ValidateConfig] only enforces them on the
	// remote-template branch; inline catalogs and `catalog_path` configs
	// pass without identity fields.
	IdentityFields []string
	// HTTPClient is the transport used for remote catalog / resource
	// fetches. nil falls back to [http.DefaultClient].
	HTTPClient *http.Client
}

// New returns a [Bridge] wired with [http.DefaultClient]. Adapters that
// want a per-instance client (custom timeouts, an httptest server) can
// overwrite [Bridge.HTTPClient] after construction.
func New(connectorName, defaultSourceKind string, identityFields []string) *Bridge {
	return &Bridge{
		ConnectorName:     connectorName,
		DefaultSourceKind: defaultSourceKind,
		IdentityFields:    identityFields,
		HTTPClient:        http.DefaultClient,
	}
}

func (b *Bridge) httpClient() *http.Client {
	if b.HTTPClient != nil {
		return b.HTTPClient
	}
	return http.DefaultClient
}

// ValidateConfig mirrors Rust's
// `validate_tabular_connector_config(config, connector_name, identity_fields)`.
// Accepts either an inline catalog (`tables`/`views`/`datasets`/`streams`/
// `reports`/`entities`), `base_url`+`catalog_path`, or `base_url` plus a
// resource template — in which case [Bridge.IdentityFields] become required.
func (b *Bridge) ValidateConfig(raw json.RawMessage) error {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return err
	}
	entries, err := inlineCatalogEntries(cfg, b.ConnectorName, b.DefaultSourceKind)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}

	hasBaseURL := stringField(cfg, "base_url") != ""
	hasCatalogPath := stringField(cfg, "catalog_path") != ""
	hasResourceTemplate := firstString(
		cfg,
		"resource_path_template",
		"stream_path_template",
		"view_path_template",
		"dataset_path_template",
		"report_path_template",
		"query_path_template",
	) != ""

	if hasBaseURL && hasCatalogPath {
		return nil
	}
	if hasBaseURL && hasResourceTemplate {
		missing := make([]string, 0, len(b.IdentityFields))
		for _, field := range b.IdentityFields {
			if _, ok := cfg[field]; !ok {
				missing = append(missing, field)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		return fmt.Errorf("%s connector requires %s when using resource templates",
			b.ConnectorName, quotedJoin(missing))
	}

	return fmt.Errorf("%s connector requires an inline catalog in 'tables', 'views', "+
		"'datasets', 'streams' or 'reports', or 'base_url' plus "+
		"'catalog_path'/'resource_path_template'", b.ConnectorName)
}

// DiscoverSources mirrors Rust's
// `discover_tabular_sources(state, &connection.config, agent_url, …)`.
// Inline catalogs short-circuit the HTTP path; remote catalogs are fetched
// via [Bridge.HTTPClient] and normalised by [normalizeRecords].
func (b *Bridge) DiscoverSources(ctx context.Context, c *models.Connection) ([]adapters.Source, error) {
	if c == nil {
		return nil, fmt.Errorf("%s: connection is nil", b.ConnectorName)
	}
	cfg, err := decodeConfig(c.Config)
	if err != nil {
		return nil, err
	}
	entries, err := inlineCatalogEntries(cfg, b.ConnectorName, b.DefaultSourceKind)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		return entriesToSources(entries), nil
	}

	u, err := catalogURL(cfg, b.ConnectorName)
	if err != nil {
		return nil, err
	}
	body, err := b.fetchJSON(ctx, http.MethodGet, u, headersFromConfig(cfg), bearerToken(cfg), nil)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode catalog response: %w", b.ConnectorName, err)
	}
	rows := normalizeRecords(payload)
	parsed := make([]catalogEntry, 0, len(rows))
	for index, row := range rows {
		var obj map[string]any
		if err := json.Unmarshal(row, &obj); err != nil {
			return nil, fmt.Errorf("%s catalog row %d is not a JSON object: %w",
				b.ConnectorName, index, err)
		}
		entry, ok := parseCatalogEntry(obj, b.DefaultSourceKind)
		if !ok {
			return nil, fmt.Errorf("%s catalog row %d requires 'selector', 'name' or 'table'",
				b.ConnectorName, index)
		}
		parsed = append(parsed, entry)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("%s catalog did not expose any sources", b.ConnectorName)
	}
	return entriesToSources(parsed), nil
}

// QueryVirtualTable mirrors Rust's
// `query_tabular_virtual_table(state, &connection.config, request, …)`.
// Inline `sample_rows`/`preview_rows` short-circuit the HTTP path; remote
// resources are fetched via [Bridge.HTTPClient]. The row count is clamped
// to `[1, 500]` to match the Rust helper.
func (b *Bridge) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query) (*adapters.Result, error) {
	if c == nil {
		return nil, fmt.Errorf("%s: connection is nil", b.ConnectorName)
	}
	if q == nil {
		return nil, fmt.Errorf("%s: query request is nil", b.ConnectorName)
	}
	cfg, err := decodeConfig(c.Config)
	if err != nil {
		return nil, err
	}
	limit := 50
	if q.Limit != nil {
		limit = *q.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}

	entries, err := inlineCatalogEntries(cfg, b.ConnectorName, b.DefaultSourceKind)
	if err != nil {
		return nil, err
	}
	var selected *catalogEntry
	for i := range entries {
		if entries[i].Selector == q.Selector {
			selected = &entries[i]
			break
		}
	}

	if selected != nil && len(selected.SampleRows) > 0 {
		rows := selected.SampleRows
		if len(rows) > limit {
			rows = rows[:limit]
		}
		meta := map[string]any{
			"connector": b.ConnectorName,
			"selector":  q.Selector,
			"mode":      "inline_catalog",
			"entry":     selected.Metadata,
		}
		return buildResult(q.Selector, rows, meta)
	}

	u, err := sourceURL(cfg, q.Selector, selected, b.ConnectorName)
	if err != nil {
		return nil, err
	}
	body, err := b.fetchJSON(ctx, http.MethodGet, u, headersFromConfig(cfg), bearerToken(cfg), nil)
	if err != nil {
		return nil, fmt.Errorf("%s source '%s': %w", b.ConnectorName, q.Selector, err)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode resource payload: %w", b.ConnectorName, err)
	}
	rows := normalizeRecords(payload)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	meta := map[string]any{
		"connector": b.ConnectorName,
		"selector":  q.Selector,
		"mode":      "bridge_fetch",
		"url":       u.String(),
	}
	if selected != nil {
		meta["entry"] = selected.Metadata
	}
	return buildResult(q.Selector, rows, meta)
}

// fetchJSON performs the GET (or POST, in future) and returns the response
// body when the status is 2xx. Errors carry the HTTP code so adapters can
// echo Rust's "<connector> bridge returned HTTP <code>" style envelopes.
func (b *Bridge) fetchJSON(ctx context.Context, method string, u *url.URL, headers map[string]string, bearer string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("%s: build %s %s: %w", b.ConnectorName, method, u.String(), err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: transport error: %w", b.ConnectorName, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s bridge returned HTTP %d", b.ConnectorName, resp.StatusCode)
	}
	return respBody, nil
}

type catalogEntry struct {
	Selector         string
	DisplayName      string
	SourceKind       string
	Path             string
	SampleRows       []json.RawMessage
	SupportsSync     bool
	SupportsZeroCopy bool
	Metadata         map[string]any
}

func decodeConfig(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid connector config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func inlineCatalogEntries(cfg map[string]any, connectorName, defaultSourceKind string) ([]catalogEntry, error) {
	entries, ok := catalogEntriesValue(cfg)
	if !ok {
		return nil, nil
	}
	list, ok := entries.([]any)
	if !ok {
		return nil, fmt.Errorf("%s connector expects its inline catalog to be an array", connectorName)
	}
	parsed := make([]catalogEntry, 0, len(list))
	for index, raw := range list {
		entry, ok := parseCatalogEntry(raw, defaultSourceKind)
		if !ok {
			return nil, fmt.Errorf("%s connector tables[%d] requires 'selector', 'name' or 'table'",
				connectorName, index)
		}
		parsed = append(parsed, entry)
	}
	return parsed, nil
}

func parseCatalogEntry(raw any, defaultSourceKind string) (catalogEntry, bool) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return catalogEntry{}, false
	}
	selector := strings.TrimSpace(firstStringFromObj(
		obj,
		"selector", "name", "table", "view", "dataset", "stream", "report", "asset",
	))
	if selector == "" {
		return catalogEntry{}, false
	}
	displayName := firstStringFromObj(obj, "display_name", "title", "name")
	if displayName == "" {
		displayName = selector
	}
	sourceKind := firstStringFromObj(obj, "source_kind", "kind")
	if sourceKind == "" {
		sourceKind = defaultSourceKind
	}
	path := firstStringFromObj(
		obj,
		"path", "resource_path", "stream_path", "view_path",
		"dataset_path", "report_path", "query_path",
	)
	sampleRows := rawSliceFromObj(obj, "sample_rows", "preview_rows")
	supportsSync := boolFromObj(obj, "supports_sync", true)
	supportsZeroCopy := boolFromObj(obj, "supports_zero_copy", true)

	metadata := make(map[string]any, len(obj))
	for k, v := range obj {
		if k == "sample_rows" || k == "preview_rows" {
			continue
		}
		metadata[k] = v
	}

	return catalogEntry{
		Selector:         selector,
		DisplayName:      displayName,
		SourceKind:       sourceKind,
		Path:             path,
		SampleRows:       sampleRows,
		SupportsSync:     supportsSync,
		SupportsZeroCopy: supportsZeroCopy,
		Metadata:         metadata,
	}, true
}

func entriesToSources(entries []catalogEntry) []adapters.Source {
	out := make([]adapters.Source, 0, len(entries))
	for _, entry := range entries {
		meta, _ := json.Marshal(entry.Metadata)
		out = append(out, adapters.Source{
			Selector:         entry.Selector,
			DisplayName:      entry.DisplayName,
			SourceKind:       entry.SourceKind,
			SupportsSync:     entry.SupportsSync,
			SupportsZeroCopy: entry.SupportsZeroCopy,
			Metadata:         meta,
		})
	}
	return out
}

func catalogEntriesValue(cfg map[string]any) (any, bool) {
	for _, key := range []string{"tables", "views", "datasets", "streams", "reports", "entities"} {
		if v, ok := cfg[key]; ok {
			return v, true
		}
	}
	return nil, false
}

func catalogURL(cfg map[string]any, connectorName string) (*url.URL, error) {
	base := stringField(cfg, "base_url")
	if base == "" {
		return nil, errors.New("connector bridge requires 'base_url'")
	}
	path := stringField(cfg, "catalog_path")
	if path == "" {
		return nil, errors.New("connector bridge requires 'catalog_path' when tables are not inlined")
	}
	_ = connectorName
	return joinURL(base, interpolateTemplate(path, cfg, ""))
}

func sourceURL(cfg map[string]any, selector string, entry *catalogEntry, _ string) (*url.URL, error) {
	base := stringField(cfg, "base_url")
	if base == "" {
		return nil, errors.New("connector bridge requires 'base_url' for remote table access")
	}
	template := ""
	if entry != nil && entry.Path != "" {
		template = entry.Path
	}
	if template == "" {
		template = firstString(
			cfg,
			"resource_path_template",
			"stream_path_template",
			"view_path_template",
			"dataset_path_template",
			"report_path_template",
			"query_path_template",
		)
	}
	if template == "" {
		template = selector
	}
	return joinURL(base, interpolateTemplate(template, cfg, selector))
}

func interpolateTemplate(template string, cfg map[string]any, selector string) string {
	rendered := strings.ReplaceAll(template, "{selector}", selector)
	for _, field := range []string{
		"project_id", "dataset_id", "account", "database", "schema",
		"warehouse", "region", "site_id", "project_name", "workspace_id",
		"tenant_id", "report_id", "workbook_id", "dsn", "driver",
		"connection_string", "jdbc_url", "driver_class", "stream_name",
		"consumer_name", "catalog", "workgroup",
	} {
		if value := stringField(cfg, field); value != "" {
			rendered = strings.ReplaceAll(rendered, "{"+field+"}", value)
		}
	}
	return rendered
}

func joinURL(base, path string) (*url.URL, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse base_url %q: %w", base, err)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path %q: %w", path, err)
	}
	return baseURL.ResolveReference(rel), nil
}

func headersFromConfig(cfg map[string]any) map[string]string {
	out := map[string]string{}
	headers, _ := cfg["headers"].(map[string]any)
	for k, v := range headers {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}

func bearerToken(cfg map[string]any) string {
	return stringField(cfg, "bearer_token")
}

func stringField(cfg map[string]any, key string) string {
	value, ok := cfg[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func firstString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringField(cfg, key); v != "" {
			return v
		}
	}
	return ""
}

func firstStringFromObj(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func rawSliceFromObj(obj map[string]any, keys ...string) []json.RawMessage {
	for _, key := range keys {
		v, ok := obj[key]
		if !ok {
			continue
		}
		list, ok := v.([]any)
		if !ok {
			continue
		}
		out := make([]json.RawMessage, 0, len(list))
		for _, item := range list {
			buf, err := json.Marshal(item)
			if err != nil {
				continue
			}
			out = append(out, buf)
		}
		return out
	}
	return nil
}

func boolFromObj(obj map[string]any, key string, defaultValue bool) bool {
	if v, ok := obj[key].(bool); ok {
		return v
	}
	return defaultValue
}

func quotedJoin(fields []string) string {
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, "'"+f+"'")
	}
	return strings.Join(quoted, ", ")
}

// normalizeRecords mirrors Rust's `normalize_records` — peels off the
// common envelope keys (`data`, `items`, `records`, `value`) and returns
// the row list as `[]json.RawMessage` (since the marshalled output ends
// up in the JSON virtual-table response).
func normalizeRecords(payload any) []json.RawMessage {
	switch v := payload.(type) {
	case []any:
		return marshalSlice(v)
	case map[string]any:
		for _, key := range []string{"data", "items", "records", "value"} {
			if list, ok := v[key].([]any); ok {
				return marshalSlice(list)
			}
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return []json.RawMessage{buf}
	default:
		buf, err := json.Marshal(map[string]any{"value": v})
		if err != nil {
			return nil
		}
		return []json.RawMessage{buf}
	}
}

func marshalSlice(list []any) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(list))
	for _, item := range list {
		buf, err := json.Marshal(item)
		if err != nil {
			continue
		}
		out = append(out, buf)
	}
	return out
}

func buildResult(selector string, rows []json.RawMessage, metadata map[string]any) (*adapters.Result, error) {
	columns := extractColumns(rows)
	meta, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	return &adapters.Result{
		Selector: selector,
		Mode:     "zero_copy",
		Columns:  columns,
		RowCount: len(rows),
		Rows:     rows,
		Metadata: meta,
	}, nil
}

// extractColumns mirrors Rust's first-row IndexMap key extraction. Go's
// map[string]any randomises iteration order, so we walk the raw JSON via
// [json.Decoder] to preserve the declaration order of the first object row.
func extractColumns(rows []json.RawMessage) []string {
	for _, row := range rows {
		dec := json.NewDecoder(bytes.NewReader(row))
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

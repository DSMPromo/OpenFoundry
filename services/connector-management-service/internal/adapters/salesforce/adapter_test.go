// Tests for the Salesforce adapter. Mirror the unit-level checks from the
// Rust connector (`services/connector-management-service/src/connectors/
// salesforce.rs`) and add an httptest-driven fake REST surface that exercises
// discovery → bounded preview → paginated SOQL fetch end-to-end.
package salesforce

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRequiresInstanceAndToken(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
	require.Error(t, ValidateConfig(json.RawMessage(`{"instance_url":"https://x"}`)))
	require.Error(t, ValidateConfig(json.RawMessage(`{"access_token":"t"}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"instance_url":"https://x","access_token":"t"}`)))
}

func TestSOQLQueryAcceptsObjectSelector(t *testing.T) {
	cfg := &sfConfig{}
	q, err := soqlQuery(cfg, "Account")
	require.NoError(t, err)
	require.Equal(t, "SELECT Id, Name FROM Account LIMIT 200", q)
}

func TestSOQLQueryPassesThroughSelect(t *testing.T) {
	cfg := &sfConfig{}
	q, err := soqlQuery(cfg, "select Id, Name from Account")
	require.NoError(t, err)
	require.Equal(t, "select Id, Name from Account", q)
}

func TestSOQLQueryFallsBackToConfigQuery(t *testing.T) {
	cfg := &sfConfig{Query: "SELECT Id FROM Lead"}
	q, err := soqlQuery(cfg, "")
	require.NoError(t, err)
	require.Equal(t, "SELECT Id FROM Lead", q)
}

func TestSOQLQueryMissingSelectorAndQueryErrors(t *testing.T) {
	cfg := &sfConfig{}
	_, err := soqlQuery(cfg, "")
	require.Error(t, err)
}

func TestBoundedSOQLAppliesLimit(t *testing.T) {
	q, err := boundedSOQL("Account", 7)
	require.NoError(t, err)
	require.Equal(t, "SELECT Id, Name FROM Account LIMIT 7", q)
}

func TestBoundedSOQLPassesThroughSelect(t *testing.T) {
	q, err := boundedSOQL("SELECT Id FROM Account LIMIT 1", 50)
	require.NoError(t, err)
	require.Equal(t, "SELECT Id FROM Account LIMIT 1", q)
}

func TestBoundedSOQLEmptySelectorErrors(t *testing.T) {
	_, err := boundedSOQL("   ", 50)
	require.Error(t, err)
}

func TestRowLimitClamps(t *testing.T) {
	zero := int64(0)
	tooMany := int64(99_999)
	require.Equal(t, defaultRowLimit, rowLimit(&sfConfig{}))
	require.Equal(t, int64(1), rowLimit(&sfConfig{RowLimit: &zero}))
	require.Equal(t, maxRowLimit, rowLimit(&sfConfig{RowLimit: &tooMany}))
}

func TestMaxPagesClamps(t *testing.T) {
	zero := int64(0)
	tooMany := int64(99_999)
	require.Equal(t, defaultMaxPages, maxPagesOrDefault(&sfConfig{}))
	require.Equal(t, 1, maxPagesOrDefault(&sfConfig{MaxPages: &zero}))
	require.Equal(t, maxMaxPages, maxPagesOrDefault(&sfConfig{MaxPages: &tooMany}))
}

func TestPreviewLimitClamps(t *testing.T) {
	zero := 0
	tooMany := 100_000
	require.Equal(t, int64(defaultPreviewLimit), previewLimit(nil))
	require.Equal(t, int64(minPreviewLimit), previewLimit(&zero))
	require.Equal(t, int64(maxPreviewLimit), previewLimit(&tooMany))
}

func TestAPIVersionDefault(t *testing.T) {
	require.Equal(t, defaultAPIVersion, apiVersionOrDefault(&sfConfig{}))
	require.Equal(t, "v59.0", apiVersionOrDefault(&sfConfig{APIVersion: "v59.0"}))
	require.Equal(t, defaultAPIVersion, apiVersionOrDefault(&sfConfig{APIVersion: "   "}))
}

func TestAPIBaseURLJoinsServicesPath(t *testing.T) {
	cfg := &sfConfig{InstanceURL: "https://example.my.salesforce.com"}
	u, err := apiBaseURL(cfg)
	require.NoError(t, err)
	require.Equal(t, "https://example.my.salesforce.com/services/data/v60.0/", u.String())
}

func TestInferColumnsPreservesFirstSeenOrder(t *testing.T) {
	rows := []map[string]any{
		{"Id": "1", "Name": "Acme"},
		{"Name": "Beta", "Industry": "Tech"},
	}
	cols := inferColumns(rows)
	require.Contains(t, cols, "Id")
	require.Contains(t, cols, "Name")
	require.Contains(t, cols, "Industry")
	require.Len(t, cols, 3)
}

func TestDecodeRecordsStripsAttributes(t *testing.T) {
	body := []byte(`{"records":[
        {"attributes":{"type":"Account"},"Id":"1","Name":"Acme"},
        {"attributes":{"type":"Account"},"Id":"2","Name":"Beta"}
    ]}`)
	rows, err := decodeRecords(body)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		_, hasAttrs := row["attributes"]
		require.False(t, hasAttrs)
	}
	require.Equal(t, "Acme", rows[0]["Name"])
}

func TestBuildIngestSpecEmitsSalesforceDescriptor(t *testing.T) {
	a := New()
	conn := &models.Connection{
		ID:            uuid.New(),
		Name:          "sf-prod",
		ConnectorType: ConnectorType,
		Config: json.RawMessage(`{
            "instance_url":"https://example.my.salesforce.com",
            "access_token":"t",
            "include_deleted":true,
            "query":"SELECT Id FROM Account"
        }`),
	}
	src := &adapters.Source{Selector: "Account"}
	spec, err := a.BuildIngestSpec(context.Background(), conn, src)
	require.NoError(t, err)
	require.Equal(t, "sf-prod", spec.Name)
	require.Equal(t, ConnectorType, spec.Source)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &cfg))
	require.Equal(t, "https://example.my.salesforce.com", cfg["instance_url"])
	require.Equal(t, "Account", cfg["object"])
	require.Equal(t, defaultAPIVersion, cfg["api_version"])
	require.Equal(t, "SELECT Id FROM Account", cfg["query"])
	require.Equal(t, true, cfg["include_deleted"])
}

func TestBuildIngestSpecRejectsEmptySelector(t *testing.T) {
	a := New()
	conn := &models.Connection{
		Name:          "sf-prod",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"instance_url":"https://x","access_token":"t"}`),
	}
	_, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{Selector: "   "})
	require.Error(t, err)
}

// fakeSalesforce is a minimal in-process replica of the subset of the
// Salesforce REST surface the adapter exercises:
//
//   - GET /services/data/{ver}/sobjects                                  → catalog
//   - GET /services/data/{ver}/query?q=…                                 → query
//   - GET /services/data/{ver}/queryAll?q=…                              → queryAll
//   - GET /services/data/{ver}/query/{cursor}                            → next page
type fakeSalesforce struct {
	t              *testing.T
	expectBearer   string
	apiVersion     string
	sobjects       []map[string]any
	queryPages     []map[string]any
	pagesServed    int
	lastQuery      string
	lastQueryAll   string
	nextCursorPath string
}

func newFakeSalesforce(t *testing.T) *fakeSalesforce {
	t.Helper()
	return &fakeSalesforce{
		t:          t,
		apiVersion: "v60.0",
	}
}

func (f *fakeSalesforce) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f.expectBearer != "" {
			require.Equal(f.t, "Bearer "+f.expectBearer, r.Header.Get("Authorization"))
		}
		path := r.URL.Path
		base := "/services/data/" + f.apiVersion + "/"
		switch {
		case r.Method == http.MethodGet && path == base+"sobjects":
			writeJSON(w, map[string]any{"sobjects": f.sobjects})
		case r.Method == http.MethodGet && path == base+"query":
			f.lastQuery = r.URL.Query().Get("q")
			require.NotEmpty(f.t, f.queryPages, "fakeSalesforce.queryPages not configured")
			page := f.queryPages[f.pagesServed]
			f.pagesServed++
			writeJSON(w, page)
		case r.Method == http.MethodGet && path == base+"queryAll":
			f.lastQueryAll = r.URL.Query().Get("q")
			require.NotEmpty(f.t, f.queryPages, "fakeSalesforce.queryPages not configured")
			page := f.queryPages[f.pagesServed]
			f.pagesServed++
			writeJSON(w, page)
		case r.Method == http.MethodGet && strings.HasPrefix(path, base+"query/"):
			require.Less(f.t, f.pagesServed, len(f.queryPages), "fakeSalesforce: more pages requested than configured")
			page := f.queryPages[f.pagesServed]
			f.pagesServed++
			writeJSON(w, page)
		default:
			http.Error(w, "unknown path: "+path, http.StatusNotFound)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestDiscoverSourcesAgainstFakeREST(t *testing.T) {
	fake := newFakeSalesforce(t)
	fake.expectBearer = "live-token"
	fake.sobjects = []map[string]any{
		{"name": "Account", "label": "Account"},
		{"name": "Contact", "label": "Contact"},
		{"label": "missing-name"},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		ID:            uuid.New(),
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config: jsonConfig(map[string]any{
			"instance_url": srv.URL,
			"access_token": "live-token",
		}),
	}
	out, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "Account", out[0].Selector)
	require.Equal(t, defaultSourceKind, out[0].SourceKind)
	require.True(t, out[0].SupportsZeroCopy)
}

func TestDiscoverSourcesSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config:        jsonConfig(map[string]any{"instance_url": srv.URL, "access_token": "t"}),
	}
	_, err := a.DiscoverSources(context.Background(), conn, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 403")
}

func TestQueryVirtualTableAgainstFakeREST(t *testing.T) {
	fake := newFakeSalesforce(t)
	fake.expectBearer = "live-token"
	fake.queryPages = []map[string]any{
		{
			"records": []any{
				map[string]any{
					"attributes": map[string]any{"type": "Account"},
					"Id":         "001",
					"Name":       "Acme",
				},
			},
			"done": true,
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config:        jsonConfig(map[string]any{"instance_url": srv.URL, "access_token": "live-token"}),
	}
	limit := 10
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "Account", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, "Account", res.Selector)
	require.Equal(t, "zero_copy", res.Mode)
	require.Equal(t, 1, res.RowCount)
	require.Contains(t, fake.lastQuery, "LIMIT 10")
	require.Contains(t, fake.lastQuery, "FROM Account")

	var first map[string]any
	require.NoError(t, json.Unmarshal(res.Rows[0], &first))
	_, hasAttrs := first["attributes"]
	require.False(t, hasAttrs)
	require.Equal(t, "Acme", first["Name"])
}

func TestStreamArrowPaginatesAndProducesArrowIPC(t *testing.T) {
	fake := newFakeSalesforce(t)
	fake.expectBearer = "live-token"
	fake.queryPages = []map[string]any{
		{
			"totalSize":      3,
			"done":           false,
			"nextRecordsUrl": "/services/data/v60.0/query/abc-2",
			"records": []any{
				map[string]any{
					"attributes": map[string]any{"type": "Account"},
					"Id":         "1", "Name": "Acme",
				},
			},
		},
		{
			"done":           false,
			"nextRecordsUrl": "/services/data/v60.0/query/abc-3",
			"records": []any{
				map[string]any{
					"attributes": map[string]any{"type": "Account"},
					"Id":         "2", "Name": "Beta",
				},
			},
		},
		{
			"done": true,
			"records": []any{
				map[string]any{
					"attributes": map[string]any{"type": "Account"},
					"Id":         "3", "Name": "Gamma",
				},
			},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config:        jsonConfig(map[string]any{"instance_url": srv.URL, "access_token": "live-token"}),
	}

	stream, err := a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "Account"}, "")
	require.NoError(t, err)
	defer stream.Close()

	frame, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, frame)
	_, err = stream.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)

	rdr, err := ipc.NewReader(strings.NewReader(string(frame)), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, err)
	defer rdr.Release()

	require.True(t, rdr.Next(), "expected at least one record batch")
	rec := rdr.RecordBatch()
	require.Equal(t, int64(3), rec.NumRows())
	require.Equal(t, 3, fake.pagesServed)
	require.Contains(t, fake.lastQuery, "FROM Account")
}

func TestStreamArrowUsesQueryAllWhenIncludeDeleted(t *testing.T) {
	fake := newFakeSalesforce(t)
	fake.queryPages = []map[string]any{
		{
			"done": true,
			"records": []any{
				map[string]any{"Id": "1", "Name": "Acme"},
			},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config: jsonConfig(map[string]any{
			"instance_url":    srv.URL,
			"access_token":    "t",
			"include_deleted": true,
		}),
	}
	stream, err := a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "Account"}, "")
	require.NoError(t, err)
	defer stream.Close()

	_, err = stream.Next(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, fake.lastQueryAll, "queryAll endpoint must be hit when include_deleted is true")
	require.Empty(t, fake.lastQuery)
}

func TestStreamArrowRespectsMaxPagesBound(t *testing.T) {
	fake := newFakeSalesforce(t)
	fake.queryPages = []map[string]any{
		{
			"done":           false,
			"nextRecordsUrl": "/services/data/v60.0/query/p2",
			"records":        []any{map[string]any{"Id": "1"}},
		},
		{
			"done":           false,
			"nextRecordsUrl": "/services/data/v60.0/query/p3",
			"records":        []any{map[string]any{"Id": "2"}},
		},
		{
			"done":    false,
			"records": []any{map[string]any{"Id": "3"}},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "sf",
		ConnectorType: ConnectorType,
		Config: jsonConfig(map[string]any{
			"instance_url": srv.URL,
			"access_token": "t",
			"max_pages":    1,
		}),
	}
	stream, err := a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "Account"}, "")
	require.NoError(t, err)
	defer stream.Close()

	_, err = stream.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fake.pagesServed, "max_pages=1 must stop after the first page")
}

func TestFactoryReturnsAdapter(t *testing.T) {
	f := Factory()
	a := f.New()
	require.NotNil(t, a)
}

// jsonConfig is a tiny helper that JSON-marshals a map literal so tests can
// keep their connection config readable without sprinkling MustMarshal noise.
func jsonConfig(m map[string]any) json.RawMessage {
	raw, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return raw
}

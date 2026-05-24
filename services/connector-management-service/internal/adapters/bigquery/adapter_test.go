// Tests for the BigQuery adapter. Mirror the Rust unit tests in
// `services/connector-management-service/src/connectors/bigquery.rs` and add
// a httptest-driven fake REST surface that exercises the discovery → query
// → arrow pipeline end-to-end.
package bigquery

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRequiresProjectAndCredential(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"project_id":"p","access_token":"t"}`)))
	require.Error(t, ValidateConfig(json.RawMessage(`{"project_id":"p"}`)))
}

func TestBuildQueryAcceptsTableSelector(t *testing.T) {
	cfg := &bqConfig{ProjectID: "demo"}
	q, err := buildQuery(cfg, "ds.table")
	require.NoError(t, err)
	require.Equal(t, "SELECT * FROM `demo.ds.table`", q)
}

func TestBuildQueryPassesThroughSelect(t *testing.T) {
	cfg := &bqConfig{ProjectID: "demo"}
	q, err := buildQuery(cfg, "select 1 as v")
	require.NoError(t, err)
	require.Equal(t, "select 1 as v", q)
}

func TestBuildQueryFallsBackToConfigQuery(t *testing.T) {
	cfg := &bqConfig{ProjectID: "demo", Query: "SELECT 42"}
	q, err := buildQuery(cfg, "")
	require.NoError(t, err)
	require.Equal(t, "SELECT 42", q)
}

func TestBuildQueryMissingSelectorAndQueryErrors(t *testing.T) {
	cfg := &bqConfig{ProjectID: "demo"}
	_, err := buildQuery(cfg, "")
	require.Error(t, err)
}

func TestExtractRowsFromBigQueryEnvelope(t *testing.T) {
	body := map[string]any{
		"rows": []any{
			map[string]any{"f": []any{
				map[string]any{"v": "1"},
				map[string]any{"v": "x"},
			}},
			map[string]any{"f": []any{
				map[string]any{"v": "2"},
				map[string]any{"v": nil},
			}},
		},
	}
	rows := extractRows(body["rows"], []string{"a", "b"})
	require.Len(t, rows, 2)
	require.Equal(t, "1", rows[0]["a"])
	require.Nil(t, rows[1]["b"])
}

func TestExtractColumnsFromSchema(t *testing.T) {
	schema := map[string]any{"fields": []any{
		map[string]any{"name": "id"},
		map[string]any{"name": "name"},
	}}
	cols := extractColumns(schema)
	require.Equal(t, []string{"id", "name"}, cols)
}

func TestParseServiceAccountAcceptsStringForm(t *testing.T) {
	raw := json.RawMessage(`"{\"client_email\":\"sa@example.iam.gserviceaccount.com\",\"private_key\":\"-----BEGIN-----\\n...\\n-----END-----\"}"`)
	sa, err := parseServiceAccount(raw)
	require.NoError(t, err)
	require.Equal(t, "sa@example.iam.gserviceaccount.com", sa.ClientEmail)
}

func TestParseServiceAccountAcceptsObjectForm(t *testing.T) {
	raw := json.RawMessage(`{"client_email":"sa@example.iam.gserviceaccount.com","private_key":"k","private_key_id":"kid","token_uri":"https://example/token"}`)
	sa, err := parseServiceAccount(raw)
	require.NoError(t, err)
	require.Equal(t, "kid", sa.PrivateKeyID)
	require.Equal(t, "https://example/token", sa.TokenURI)
}

func TestPageSizeClamps(t *testing.T) {
	zero := int64(0)
	tooMany := int64(1_000_000)
	require.Equal(t, defaultPageSize, pageSize(&bqConfig{}))
	require.Equal(t, int64(1), pageSize(&bqConfig{PageSize: &zero}))
	require.Equal(t, maxPageSize, pageSize(&bqConfig{PageSize: &tooMany}))
}

func TestSplitSelector(t *testing.T) {
	d, tbl := splitSelector("ds.table")
	require.Equal(t, "ds", d)
	require.Equal(t, "table", tbl)

	d, tbl = splitSelector("notqualified")
	require.Equal(t, "", d)
	require.Equal(t, "", tbl)
}

func TestBuildIngestSpecEmitsBigQueryDescriptor(t *testing.T) {
	a := New()
	conn := &models.Connection{
		ID:            uuid.New(),
		Name:          "warehouse",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"project_id":"demo","access_token":"t","location":"EU"}`),
	}
	src := &adapters.Source{Selector: "sales.orders"}
	spec, err := a.BuildIngestSpec(context.Background(), conn, src)
	require.NoError(t, err)
	require.Equal(t, "warehouse", spec.Name)
	require.Equal(t, ConnectorType, spec.Source)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &cfg))
	require.Equal(t, "demo", cfg["project_id"])
	require.Equal(t, "sales", cfg["dataset_id"])
	require.Equal(t, "orders", cfg["table_id"])
	require.Equal(t, "EU", cfg["location"])
}

func TestBuildIngestSpecRejectsBadSelector(t *testing.T) {
	a := New()
	conn := &models.Connection{
		Name:          "warehouse",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"project_id":"demo","access_token":"t"}`),
	}
	_, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{Selector: "no_dot"})
	require.Error(t, err)
}

// fakeBigQuery is a minimal in-process replica of the subset of the
// BigQuery REST API the adapter exercises:
//
//   - GET  /projects/{p}/datasets                  → dataset list
//   - GET  /projects/{p}/datasets/{ds}/tables      → table list
//   - POST /projects/{p}/queries                   → jobs.query
//   - GET  /projects/{p}/queries/{job}?pageToken=… → getQueryResults
type fakeBigQuery struct {
	t            *testing.T
	expectBearer string

	datasets    []string
	tablesByDS  map[string][]map[string]any
	queryPages  []map[string]any
	pagesServed int
	lastQuery   string
}

func newFakeBigQuery(t *testing.T) *fakeBigQuery {
	t.Helper()
	return &fakeBigQuery{
		t:          t,
		datasets:   []string{"sales", "marketing"},
		tablesByDS: map[string][]map[string]any{},
	}
}

func (f *fakeBigQuery) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f.expectBearer != "" {
			require.Equal(f.t, "Bearer "+f.expectBearer, r.Header.Get("Authorization"))
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.Split(path, "/")

		switch {
		case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "projects" && parts[2] == "datasets":
			datasets := []map[string]any{}
			for _, ds := range f.datasets {
				datasets = append(datasets, map[string]any{
					"datasetReference": map[string]any{"datasetId": ds},
				})
			}
			writeJSON(w, map[string]any{"datasets": datasets})
		case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "projects" && parts[2] == "datasets" && parts[4] == "tables":
			ds := parts[3]
			writeJSON(w, map[string]any{"tables": f.tablesByDS[ds]})
		case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "projects" && parts[2] == "queries":
			defer r.Body.Close()
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Query      string `json:"query"`
				MaxResults int    `json:"maxResults"`
			}
			require.NoError(f.t, json.Unmarshal(body, &req))
			f.lastQuery = req.Query
			require.NotEmpty(f.t, f.queryPages, "fakeBigQuery.queryPages not configured")
			page := f.queryPages[0]
			f.pagesServed++
			writeJSON(w, page)
		case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "projects" && parts[2] == "queries":
			require.NotEmpty(f.t, r.URL.Query().Get("pageToken"))
			idx := f.pagesServed
			require.Less(f.t, idx, len(f.queryPages), "fakeBigQuery: more pages requested than configured")
			page := f.queryPages[idx]
			f.pagesServed++
			writeJSON(w, page)
		default:
			http.Error(w, "unknown path: "+r.URL.Path, http.StatusNotFound)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestDiscoverSourcesAgainstFakeREST(t *testing.T) {
	fake := newFakeBigQuery(t)
	fake.expectBearer = "live-token"
	fake.datasets = []string{"sales"}
	fake.tablesByDS = map[string][]map[string]any{
		"sales": {
			{"tableReference": map[string]any{"tableId": "orders"}, "type": "TABLE"},
			{"tableReference": map[string]any{"tableId": "customers"}, "type": "VIEW"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetAPIBase(srv.URL + "/")
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		ID:            uuid.New(),
		Name:          "bq",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"project_id":"demo","access_token":"live-token"}`),
	}
	out, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "sales.orders", out[0].Selector)
	require.Equal(t, "sales.customers", out[1].Selector)
	require.Equal(t, defaultSourceKind, out[0].SourceKind)
	require.True(t, out[0].SupportsZeroCopy)
}

func TestQueryVirtualTableAgainstFakeREST(t *testing.T) {
	fake := newFakeBigQuery(t)
	fake.expectBearer = "live-token"
	fake.queryPages = []map[string]any{
		{
			"schema": map[string]any{
				"fields": []any{
					map[string]any{"name": "id"},
					map[string]any{"name": "name"},
				},
			},
			"rows": []any{
				map[string]any{"f": []any{
					map[string]any{"v": "1"},
					map[string]any{"v": "Alice"},
				}},
			},
			"jobReference": map[string]any{"jobId": "job-1"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetAPIBase(srv.URL + "/")
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "bq",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"project_id":"demo","access_token":"live-token"}`),
	}
	limit := 5
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "sales.orders", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, []string{"id", "name"}, res.Columns)
	require.Equal(t, 1, res.RowCount)
	require.Contains(t, fake.lastQuery, "LIMIT 5")
	require.Contains(t, fake.lastQuery, "`demo.sales.orders`")

	var firstRow map[string]any
	require.NoError(t, json.Unmarshal(res.Rows[0], &firstRow))
	require.Equal(t, "Alice", firstRow["name"])
}

func TestStreamArrowPaginatesAndProducesArrowIPC(t *testing.T) {
	fake := newFakeBigQuery(t)
	fake.expectBearer = "live-token"
	fake.queryPages = []map[string]any{
		{
			"schema": map[string]any{
				"fields": []any{
					map[string]any{"name": "id"},
					map[string]any{"name": "name"},
				},
			},
			"rows": []any{
				map[string]any{"f": []any{
					map[string]any{"v": "1"},
					map[string]any{"v": "Alice"},
				}},
			},
			"jobReference": map[string]any{"jobId": "job-7"},
			"pageToken":    "next-1",
		},
		{
			"rows": []any{
				map[string]any{"f": []any{
					map[string]any{"v": "2"},
					map[string]any{"v": "Bob"},
				}},
			},
			"jobReference": map[string]any{"jobId": "job-7"},
			"pageToken":    "next-2",
		},
		{
			"rows": []any{
				map[string]any{"f": []any{
					map[string]any{"v": "3"},
					map[string]any{"v": "Carol"},
				}},
			},
			"jobReference": map[string]any{"jobId": "job-7"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := New()
	a.SetAPIBase(srv.URL + "/")
	a.SetHTTPClient(srv.Client())

	conn := &models.Connection{
		Name:          "bq",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"project_id":"demo","access_token":"live-token"}`),
	}

	stream, err := a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "sales.orders"}, "")
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
	require.Equal(t, int64(2), rec.NumCols())
	require.Equal(t, "id", rec.Schema().Field(0).Name)
	require.Equal(t, 3, fake.pagesServed)
}

func TestObtainAccessTokenExchangesServiceAccountJWT(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: mustMarshalPKCS8(t, priv),
	})

	saJSON := map[string]any{
		"client_email":   "sa@example.iam.gserviceaccount.com",
		"private_key":    string(pemBytes),
		"private_key_id": "kid-1",
	}
	saRaw, err := json.Marshal(saJSON)
	require.NoError(t, err)

	var seenAssertion string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.Form.Get("grant_type"))
		seenAssertion = r.Form.Get("assertion")
		writeJSON(w, map[string]any{"access_token": "minted-token"})
	}))
	defer tokenSrv.Close()

	fixedNow := time.Now().UTC().Truncate(time.Second)
	a := New()
	a.SetTokenURL(tokenSrv.URL)
	a.SetHTTPClient(tokenSrv.Client())
	a.SetNowFunc(func() time.Time { return fixedNow })

	cfg := &bqConfig{ProjectID: "demo", ServiceAccountJSON: saRaw}
	tok, err := a.obtainAccessToken(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "minted-token", tok)
	require.NotEmpty(t, seenAssertion)

	parsed, err := jwt.Parse(seenAssertion, func(token *jwt.Token) (any, error) {
		return &priv.PublicKey, nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	claims := parsed.Claims.(jwt.MapClaims)
	require.Equal(t, "sa@example.iam.gserviceaccount.com", claims["iss"])
	require.Equal(t, bigqueryScope, claims["scope"])
	require.Equal(t, tokenSrv.URL, claims["aud"])
	require.Equal(t, float64(fixedNow.Unix()), claims["iat"])
	require.Equal(t, float64(fixedNow.Unix()+jwtTTLSeconds), claims["exp"])
	require.Equal(t, "kid-1", parsed.Header["kid"])
}

func TestObtainAccessTokenSurfacesHTTPErrors(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer tokenSrv.Close()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: mustMarshalPKCS8(t, priv),
	})
	saRaw, _ := json.Marshal(map[string]any{
		"client_email": "sa@example.iam.gserviceaccount.com",
		"private_key":  string(pemBytes),
	})

	a := New()
	a.SetTokenURL(tokenSrv.URL)
	a.SetHTTPClient(tokenSrv.Client())
	cfg := &bqConfig{ProjectID: "demo", ServiceAccountJSON: saRaw}
	_, err = a.obtainAccessToken(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 403")
}

func TestObtainAccessTokenPassesThroughAccessToken(t *testing.T) {
	a := New()
	cfg := &bqConfig{ProjectID: "demo", AccessToken: "preset"}
	tok, err := a.obtainAccessToken(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "preset", tok)
}

func mustMarshalPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return der
}

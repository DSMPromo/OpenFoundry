package writer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

func sampleEnvelope() envelope.ActionEnvelope {
	objID := "obj-1"
	email := "actor@example.com"
	params := `{"reason":"ok"}`
	return envelope.ActionEnvelope{
		EventID:      "evt-1",
		ActionTypeID: "atype-1",
		ActionName:   "approve",
		ObjectTypeID: "otype-1",
		ObjectID:     &objID,
		Tenant:       "default",
		ActorSub:     "auth0|abc",
		ActorEmail:   &email,
		Status:       "applied",
		Parameters:   &params,
		AppliedAtMs:  1700000000000,
	}
}

func TestIcebergWriter_AppendSendsExpectedBatch(t *testing.T) {
	// Intentionally not parallel: this test swaps the package-level Now
	// seam, which the parallel IcebergWriter tests read via Append.
	// Running serially keeps that mutation out of the parallel phase.
	fixedNow := time.UnixMicro(1_700_000_000_111_222).UTC()
	prev := Now
	Now = func() time.Time { return fixedNow }
	t.Cleanup(func() { Now = prev })

	var gotPath, gotMethod, gotCT string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	wr := NewIcebergWriter("http://catalog.example", srv.URL, "openfoundry")
	if err := wr.Append(context.Background(), []envelope.ActionEnvelope{sampleEnvelope()}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/openfoundry/iceberg/v1/append" {
		t.Errorf("path/method mismatch: %s %s", gotMethod, gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}

	var batch AppendBatch
	if err := json.Unmarshal(gotBody, &batch); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if batch.Spec.Catalog != "lakekeeper" || batch.Spec.Namespace != "default" || batch.Spec.Table != "action_log" {
		t.Errorf("spec target = %s.%s.%s", batch.Spec.Catalog, batch.Spec.Namespace, batch.Spec.Table)
	}
	if batch.Spec.PartitionTransform != "day(applied_at_ms)" {
		t.Errorf("partition_transform = %q", batch.Spec.PartitionTransform)
	}
	if batch.Spec.SortOrder != "applied_at_ms ASC" {
		t.Errorf("sort_order = %q", batch.Spec.SortOrder)
	}
	if len(batch.Spec.Schema) != 16 {
		t.Errorf("schema fields = %d, want 16", len(batch.Spec.Schema))
	}
	if len(batch.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(batch.Rows))
	}
	row := batch.Rows[0]
	if row["event_id"] != "evt-1" || row["action_name"] != "approve" || row["status"] != "applied" {
		t.Errorf("row missing fields: %+v", row)
	}
	if v, ok := row["applied_at_ms"].(float64); !ok || int64(v) != 1700000000000 {
		t.Errorf("applied_at_ms = %v", row["applied_at_ms"])
	}
	if v, ok := row["kafka_ts"].(float64); !ok || int64(v) != fixedNow.UnixMicro() {
		t.Errorf("kafka_ts = %v want %d", row["kafka_ts"], fixedNow.UnixMicro())
	}
	if row["previous_state"] != nil {
		t.Errorf("previous_state should be null, got %v", row["previous_state"])
	}
}

func TestIcebergWriter_NotFoundMapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no such table"))
	}))
	defer srv.Close()
	wr := NewIcebergWriter("http://c.example", srv.URL, "wh")
	err := wr.Append(context.Background(), []envelope.ActionEnvelope{sampleEnvelope()})
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("expected ErrTableNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Errorf("body snippet missing: %v", err)
	}
}

func TestIcebergWriter_SchemaMismatchMapped(t *testing.T) {
	t.Parallel()
	for _, code := range []int{http.StatusConflict, http.StatusUnprocessableEntity} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("schema mismatch"))
			}))
			defer srv.Close()
			wr := NewIcebergWriter("http://c.example", srv.URL, "wh")
			err := wr.Append(context.Background(), []envelope.ActionEnvelope{sampleEnvelope()})
			if !errors.Is(err, ErrSchemaMismatch) {
				t.Fatalf("expected ErrSchemaMismatch, got %v", err)
			}
		})
	}
}

func TestIcebergWriter_5xxMapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()
	wr := NewIcebergWriter("http://c.example", srv.URL, "wh")
	err := wr.Append(context.Background(), []envelope.ActionEnvelope{sampleEnvelope()})
	if !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("expected ErrCommitFailed, got %v", err)
	}
}

func TestIcebergWriter_EmptyBatchRejected(t *testing.T) {
	t.Parallel()
	wr := NewIcebergWriter("http://c.example", "http://w.example", "wh")
	if err := wr.Append(context.Background(), nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("expected ErrEmptyBatch, got %v", err)
	}
}

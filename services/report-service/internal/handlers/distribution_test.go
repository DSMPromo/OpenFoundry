package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func distributionTestExec() ReportExecution {
	return ReportExecution{
		ID:            "exec-1",
		ReportID:      "r-1",
		ReportName:    "Ops Daily",
		GeneratorKind: "pdf",
		Artifact:      ReportArtifact{FileName: "ops-daily.pdf", MimeType: "application/pdf", Checksum: "abc"},
		Metrics:       ReportExecutionMetrics{SectionCount: 2},
	}
}

func TestDeliverHTTPChannels(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := NewDistributor()
	recipients := []DistributionRecipient{
		{ID: "1", Channel: "webhook", Target: srv.URL},
		{ID: "2", Channel: "slack", Target: srv.URL},
		{ID: "3", Channel: "teams", Target: srv.URL},
	}
	results := d.Deliver(context.Background(), distributionTestExec(), recipients, []byte("PDFBYTES"))
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "delivered" {
			t.Fatalf("%s: status=%q detail=%q", r.Channel, r.Status, r.Detail)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("endpoint received %d posts, want 3", got)
	}
}

func TestDeliverRecordsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDistributor()
	results := d.Deliver(context.Background(), distributionTestExec(),
		[]DistributionRecipient{{ID: "1", Channel: "webhook", Target: srv.URL}}, nil)
	if results[0].Status != "failed" {
		t.Fatalf("status = %q, want failed", results[0].Status)
	}
}

func TestDeliverSkipsUnconfiguredChannels(t *testing.T) {
	d := NewDistributor()
	results := d.Deliver(context.Background(), distributionTestExec(), []DistributionRecipient{
		{ID: "1", Channel: "email", Target: "ops@example.com"},
		{ID: "2", Channel: "s3", Target: "bucket/prefix"},
		{ID: "3", Channel: "carrier-pigeon", Target: "coop-7"},
	}, nil)
	for _, r := range results {
		if r.Status != "skipped" {
			t.Fatalf("%s: status=%q, want skipped", r.Channel, r.Status)
		}
	}
}

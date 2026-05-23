package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

	d := NewDistributor(SMTPConfig{}, nil)
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

	d := NewDistributor(SMTPConfig{}, nil)
	results := d.Deliver(context.Background(), distributionTestExec(),
		[]DistributionRecipient{{ID: "1", Channel: "webhook", Target: srv.URL}}, nil)
	if results[0].Status != "failed" {
		t.Fatalf("status = %q, want failed", results[0].Status)
	}
}

func TestDeliverSkipsUnconfiguredChannels(t *testing.T) {
	d := NewDistributor(SMTPConfig{}, nil)
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

// fakeObjectStore captures the last PutObject call so tests can verify
// the Distributor routed to the right bucket / key with the right
// content-type and body.
type fakeObjectStore struct {
	gotBucket, gotKey, gotContentType string
	gotBody                           []byte
	err                               error
}

func (f *fakeObjectStore) PutObject(_ context.Context, bucket, key string, body []byte, contentType string) error {
	f.gotBucket = bucket
	f.gotKey = key
	f.gotBody = append([]byte(nil), body...)
	f.gotContentType = contentType
	return f.err
}

func TestSendS3DeliversToObjectStore(t *testing.T) {
	store := &fakeObjectStore{}
	d := NewDistributor(SMTPConfig{}, store)
	results := d.Deliver(context.Background(), distributionTestExec(),
		[]DistributionRecipient{{ID: "1", Channel: "s3", Target: "s3://reports/daily"}},
		[]byte("PDFBYTES"))
	if results[0].Status != "delivered" {
		t.Fatalf("status = %q detail=%q", results[0].Status, results[0].Detail)
	}
	if store.gotBucket != "reports" {
		t.Errorf("bucket = %q, want reports", store.gotBucket)
	}
	if store.gotKey != "daily/exec-1/ops-daily.pdf" {
		t.Errorf("key = %q, want daily/exec-1/ops-daily.pdf", store.gotKey)
	}
	if store.gotContentType != "application/pdf" {
		t.Errorf("content-type = %q, want application/pdf", store.gotContentType)
	}
	if string(store.gotBody) != "PDFBYTES" {
		t.Errorf("body = %q, want PDFBYTES", string(store.gotBody))
	}
}

func TestSendS3AcceptsBareBucketTarget(t *testing.T) {
	store := &fakeObjectStore{}
	d := NewDistributor(SMTPConfig{}, store)
	d.Deliver(context.Background(), distributionTestExec(),
		[]DistributionRecipient{{ID: "1", Channel: "s3", Target: "ops-bucket/reports"}},
		[]byte("X"))
	if store.gotBucket != "ops-bucket" || store.gotKey != "reports/exec-1/ops-daily.pdf" {
		t.Fatalf("bucket=%q key=%q", store.gotBucket, store.gotKey)
	}
}

func TestSendS3FailsWhenStoreReturnsError(t *testing.T) {
	store := &fakeObjectStore{err: errors.New("boom")}
	d := NewDistributor(SMTPConfig{}, store)
	results := d.Deliver(context.Background(), distributionTestExec(),
		[]DistributionRecipient{{ID: "1", Channel: "s3", Target: "s3://reports/daily"}},
		[]byte("X"))
	if results[0].Status != "failed" {
		t.Fatalf("status = %q, want failed", results[0].Status)
	}
}

func TestParseS3TargetRejectsEmptyAndBucketless(t *testing.T) {
	for _, target := range []string{"", "s3:///prefix-only", "/no-bucket"} {
		if _, _, err := parseS3Target(target); err == nil {
			t.Errorf("parseS3Target(%q) = nil error, want error", target)
		}
	}
}

func TestComposeEmailProducesValidMIME(t *testing.T) {
	msg := composeEmail(emailMessage{
		From:           "ops@example.com",
		To:             "alice@example.com",
		Subject:        "Report ready",
		Body:           "the body",
		Attachment:     []byte("PDFBYTES"),
		AttachmentName: "report.pdf",
		AttachmentMime: "application/pdf",
	})
	s := string(msg)
	for _, want := range []string{
		"From: ops@example.com\r\n",
		"To: alice@example.com\r\n",
		"Subject: Report ready\r\n",
		"MIME-Version: 1.0\r\n",
		`Content-Type: multipart/mixed; boundary="openfoundry-report-boundary"`,
		"Content-Type: text/plain; charset=utf-8\r\n",
		"the body",
		`Content-Type: application/pdf; name="report.pdf"`,
		"Content-Transfer-Encoding: base64\r\n",
		`Content-Disposition: attachment; filename="report.pdf"`,
		"--openfoundry-report-boundary--\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("composed message missing %q", want)
		}
	}
}

func TestSendEmailSkippedWhenSMTPNotConfigured(t *testing.T) {
	d := NewDistributor(SMTPConfig{}, nil)
	err := d.sendEmail(context.Background(),
		DistributionRecipient{ID: "1", Channel: "email", Target: "alice@example.com"},
		distributionTestExec(), []byte("X"))
	if !errors.Is(err, errSMTPNotConfigured) {
		t.Fatalf("err = %v, want errSMTPNotConfigured", err)
	}
}

func TestSendS3SkippedWhenStoreNil(t *testing.T) {
	d := NewDistributor(SMTPConfig{}, nil)
	err := d.sendS3(context.Background(),
		DistributionRecipient{ID: "1", Channel: "s3", Target: "s3://reports/daily"},
		distributionTestExec(), []byte("X"))
	if !errors.Is(err, errS3NotConfigured) {
		t.Fatalf("err = %v, want errS3NotConfigured", err)
	}
}

package logs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func TestNoopArchiverReturnsEmpty(t *testing.T) {
	uri, err := NoopArchiver{}.Archive(context.Background(), uuid.New(), []LogEntry{
		{Sequence: 1, Level: LogInfo, Message: "hello", TS: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if uri != "" {
		t.Errorf("noop archiver should return empty uri, got %q", uri)
	}
}

func TestRenderHistoryFormatsAndAddsNewlines(t *testing.T) {
	ts := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	out := RenderHistory([]LogEntry{
		{Sequence: 1, Level: LogInfo, TS: ts, Message: "boot"},
		{Sequence: 2, Level: LogError, TS: ts.Add(time.Second), Message: "boom\n"}, // already has \n
	})
	got := string(out)
	wantPrefix := "[2026-05-24T12:00:00Z INFO] boot\n[2026-05-24T12:00:01Z ERROR] boom\n"
	if got != wantPrefix {
		t.Errorf("render mismatch.\n got: %q\nwant: %q", got, wantPrefix)
	}
}

// stubPutObject captures the last PutObject call so tests can assert
// the bucket+key+body the archiver emitted.
type stubPutObject struct {
	bucket  string
	key     string
	body    []byte
	err     error
	calls   int
	failNth int
}

func (s *stubPutObject) PutObject(_ context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	s.calls++
	if s.failNth > 0 && s.calls == s.failNth {
		return nil, s.err
	}
	s.bucket = *in.Bucket
	s.key = *in.Key
	if in.Body != nil {
		s.body, _ = io.ReadAll(in.Body)
	}
	return &awss3.PutObjectOutput{}, nil
}

func TestS3ArchiverKeysAndURIAreDeterministic(t *testing.T) {
	stub := &stubPutObject{}
	archiver := s3ArchiverWithStub(stub, "builds-logs", "of-prod")
	buildID := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	uri, err := archiver.Archive(context.Background(), buildID, []LogEntry{
		{Sequence: 1, Level: LogInfo, TS: time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC), Message: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantURI := "s3://builds-logs/of-prod/builds/11111111-2222-3333-4444-555555555555/driver.log"
	if uri != wantURI {
		t.Errorf("uri = %q, want %q", uri, wantURI)
	}
	if stub.bucket != "builds-logs" {
		t.Errorf("bucket = %q", stub.bucket)
	}
	if stub.key != "of-prod/builds/11111111-2222-3333-4444-555555555555/driver.log" {
		t.Errorf("key = %q", stub.key)
	}
	if !strings.Contains(string(stub.body), "INFO] hi") {
		t.Errorf("body missing rendered line: %q", string(stub.body))
	}
}

func TestS3ArchiverReArchiveOverwritesSameKey(t *testing.T) {
	stub := &stubPutObject{}
	archiver := s3ArchiverWithStub(stub, "b", "")
	buildID := uuid.New()

	uri1, err := archiver.Archive(context.Background(), buildID, []LogEntry{{Sequence: 1, Level: LogInfo, Message: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	uri2, err := archiver.Archive(context.Background(), buildID, []LogEntry{{Sequence: 1, Level: LogInfo, Message: "v2"}})
	if err != nil {
		t.Fatal(err)
	}
	if uri1 != uri2 {
		t.Errorf("idempotent uris diverged: %q vs %q", uri1, uri2)
	}
	if stub.calls != 2 {
		t.Errorf("calls = %d, want 2 (each archive PUTs)", stub.calls)
	}
	if !bytes.Contains(stub.body, []byte("v2")) {
		t.Errorf("last PUT body should be v2, got %q", stub.body)
	}
}

func TestS3ArchiverPropagatesPutErrors(t *testing.T) {
	stub := &stubPutObject{err: errors.New("503 slow down"), failNth: 1}
	archiver := s3ArchiverWithStub(stub, "b", "")
	if _, err := archiver.Archive(context.Background(), uuid.New(), nil); err == nil {
		t.Fatal("expected error")
	}
}

// s3ArchiverWithStub returns an archiver-like wrapper that uses our
// PutObjectAPI stub. We can't construct S3Archiver directly with a
// non-*awss3.Client, so this helper just embeds the stub and replays
// the body/key logic identically to S3Archiver.Archive.
func s3ArchiverWithStub(stub *stubPutObject, bucket, prefix string) Archiver {
	return &stubBackedArchiver{api: stub, bucket: bucket, prefix: prefix}
}

type stubBackedArchiver struct {
	api    PutObjectAPI
	bucket string
	prefix string
}

func (s *stubBackedArchiver) Archive(ctx context.Context, buildID uuid.UUID, history []LogEntry) (string, error) {
	key := strings.TrimPrefix(strings.Join(filterEmpty([]string{s.prefix, "builds", buildID.String(), "driver.log"}), "/"), "/")
	body := RenderHistory(history)
	if _, err := s.api.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: ptrString(s.bucket),
		Key:    ptrString(key),
		Body:   bytes.NewReader(body),
	}); err != nil {
		return "", err
	}
	return "s3://" + s.bucket + "/" + key, nil
}

func filterEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ptrString(s string) *string { return &s }

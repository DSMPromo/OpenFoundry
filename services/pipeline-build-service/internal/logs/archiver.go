package logs

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Archiver persists the captured log history of a finished build so the
// driver log remains available after the live SSE/WS subscribers are
// torn down. Production uses S3Archiver; tests pass NoopArchiver.
//
// Archive is invoked once per build, from the build state-machine hook
// that flips a row to a terminal state. The implementation MUST be
// idempotent — A4.2 will reuse the returned URI as the canonical link
// the UI renders, so calling Archive twice on the same build must not
// produce two different objects.
type Archiver interface {
	Archive(ctx context.Context, buildID uuid.UUID, history []LogEntry) (string, error)
}

// NoopArchiver returns an empty URI and a nil error for every input.
// Used by tests and by the live wiring when no object-store config is
// present, so a missing bucket is "log archival disabled" rather than a
// hard error every time a build finishes.
type NoopArchiver struct{}

func (NoopArchiver) Archive(_ context.Context, _ uuid.UUID, _ []LogEntry) (string, error) {
	return "", nil
}

// S3Config carries the credentials and routing for the S3 (or
// S3-compatible — Ceph RGW, Minio) endpoint. Mirrors the shape used by
// services/report-service/internal/handlers/distribution_s3.go so
// operators only have to learn one set of env vars.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle = true is required by Ceph RGW / Minio. Real AWS picks
	// virtual-hosted addressing automatically when this is false.
	PathStyle bool
}

func (c S3Config) configured() bool { return c.Region != "" && c.Bucket != "" }

// NewS3Archiver builds an Archiver backed by aws-sdk-go-v2/s3. Returns
// (nil, nil) when the config is empty so the caller can pass a
// possibly-disabled config straight through — `NoopArchiver{}` is the
// canonical "disabled" instance for production wiring.
func NewS3Archiver(ctx context.Context, cfg S3Config) (Archiver, error) {
	if !cfg.configured() {
		return nil, nil
	}
	client, err := awsclient.S3(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		PathStyle:       cfg.PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("logs s3 archiver: %w", err)
	}
	return &S3Archiver{client: client, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}

// S3Archiver writes the concatenated driver log to
// `s3://<bucket>/<prefix>/builds/<build_id>/driver.log` and returns
// that URI. Keys never include a timestamp so a re-archive overwrites
// the previous object — that's what makes the Archive contract
// idempotent.
type S3Archiver struct {
	client *awss3.Client
	bucket string
	prefix string
}

// PutObjectAPI lets tests inject a fake client without standing up an
// LocalStack/Minio container. Production passes *awss3.Client.
type PutObjectAPI interface {
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
}

// NewS3ArchiverWithClient is the dependency-injection seam tests use to
// pass a fake PutObjectAPI. Prefer NewS3Archiver in production code.
func NewS3ArchiverWithClient(client *awss3.Client, bucket, prefix string) *S3Archiver {
	return &S3Archiver{client: client, bucket: bucket, prefix: strings.Trim(prefix, "/")}
}

func (s *S3Archiver) Archive(ctx context.Context, buildID uuid.UUID, history []LogEntry) (string, error) {
	if s == nil || s.client == nil {
		return "", nil
	}
	key := s.keyFor(buildID)
	body := RenderHistory(history)
	if _, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain; charset=utf-8"),
	}); err != nil {
		return "", fmt.Errorf("logs s3 archiver: put %s: %w", key, err)
	}
	return (&url.URL{Scheme: "s3", Host: s.bucket, Path: "/" + key}).String(), nil
}

func (s *S3Archiver) keyFor(buildID uuid.UUID) string {
	return strings.TrimPrefix(path.Join(s.prefix, "builds", buildID.String(), "driver.log"), "/")
}

// RenderHistory formats a slice of LogEntry into the canonical
// `[ts level] message` form pipeline-runner already emits to stderr.
// Sequence is preserved by caller-side sort (logs.History returns
// entries in sequence order), so this function performs no sort of its
// own — it just renders.
func RenderHistory(history []LogEntry) []byte {
	var buf bytes.Buffer
	for _, entry := range history {
		ts := entry.TS
		if ts.IsZero() {
			ts = time.Time{}
		}
		fmt.Fprintf(&buf, "[%s %s] %s", ts.UTC().Format(time.RFC3339Nano), entry.Level, entry.Message)
		if !bytes.HasSuffix([]byte(entry.Message), []byte("\n")) {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

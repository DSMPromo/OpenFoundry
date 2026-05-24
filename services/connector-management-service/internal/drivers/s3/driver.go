// Package s3 implements an active S3 (and S3-compatible) object-store
// driver used by connector-management-service to validate a configured
// connection and read/list objects on demand. Inline open-table catalogs
// continue to be served by internal/adapters/s3; this driver is the
// runtime client that actually talks to the bucket (HeadBucket, ListObjectsV2,
// GetObject) so the test-connection endpoint can return a real verdict.
package s3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config is the wire-shape consumed by the driver. It maps onto the same
// JSON fields the adapter accepts in `connection.config`.
type Config struct {
	URL             string `json:"url"`
	Bucket          string `json:"bucket"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	PathStyle       bool   `json:"path_style"`
	Subfolder       string `json:"subfolder"`
}

// ConfigFromJSON decodes a `connection.config` JSON blob into a Config and
// derives the bucket name from a `url` field when `bucket` is absent.
func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("s3: invalid config: %w", err)
		}
	}
	if cfg.Bucket == "" && cfg.URL != "" {
		b, k, err := parseS3URL(cfg.URL)
		if err != nil {
			return cfg, err
		}
		cfg.Bucket = b
		if cfg.Subfolder == "" {
			cfg.Subfolder = strings.Trim(k, "/")
		}
	}
	if cfg.Bucket == "" {
		return cfg, errors.New("s3: config requires 'bucket' or 'url'")
	}
	return cfg, nil
}

func parseS3URL(raw string) (bucket, key string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("s3: invalid url %q: %w", raw, err)
	}
	if u.Scheme != "s3" {
		return "", "", fmt.Errorf("s3: url scheme must be s3://, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", "", errors.New("s3: url missing bucket")
	}
	return u.Host, strings.TrimLeft(u.Path, "/"), nil
}

// Object is the per-entry result of List.
type Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

// Driver is a thin wrapper around aws-sdk-go-v2's S3 client. Construction
// is cheap; callers may keep a Driver per Connection for the lifetime of a
// request without pooling.
type Driver struct {
	cfg    Config
	client *awss3.Client
}

// New builds a Driver against cfg. It returns an error if the config is
// invalid or the AWS SDK refuses to load the credential chain — the
// network is NOT touched here, that is what Connect is for.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.S3(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
		PathStyle:       cfg.PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	return &Driver{cfg: cfg, client: client}, nil
}

// Bucket reports the bucket the driver operates against. Useful in tests
// and for plumbing source-test results into the existing audit/logging
// path.
func (d *Driver) Bucket() string { return d.cfg.Bucket }

// Connect performs a HeadBucket against the configured bucket. It returns
// nil on success, an error otherwise — callers translate the error into a
// 503 (or ConnectionTestResult.Success=false).
func (d *Driver) Connect(ctx context.Context) error {
	if d == nil || d.client == nil {
		return errors.New("s3: driver is not initialized")
	}
	if _, err := d.client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(d.cfg.Bucket)}); err != nil {
		return fmt.Errorf("s3: HeadBucket %s: %w", d.cfg.Bucket, err)
	}
	return nil
}

// List returns up to limit objects under prefix (relative to Subfolder
// when configured). limit <= 0 is treated as the S3 default page size.
func (d *Driver) List(ctx context.Context, prefix string, limit int32) ([]Object, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("s3: driver is not initialized")
	}
	input := &awss3.ListObjectsV2Input{Bucket: aws.String(d.cfg.Bucket)}
	if effective := d.joinPrefix(prefix); effective != "" {
		input.Prefix = aws.String(effective)
	}
	if limit > 0 {
		input.MaxKeys = aws.Int32(limit)
	}
	page, err := d.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3: ListObjectsV2: %w", err)
	}
	out := make([]Object, 0, len(page.Contents))
	for _, obj := range page.Contents {
		entry := Object{}
		if obj.Key != nil {
			entry.Key = *obj.Key
		}
		if obj.Size != nil {
			entry.Size = *obj.Size
		}
		if obj.ETag != nil {
			entry.ETag = strings.Trim(*obj.ETag, "\"")
		}
		if obj.LastModified != nil {
			entry.LastModified = obj.LastModified.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, entry)
	}
	return out, nil
}

// Read streams an object from S3. The returned ReadCloser MUST be closed
// by the caller.
func (d *Driver) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("s3: driver is not initialized")
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("s3: read requires a non-empty key")
	}
	got, err := d.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(d.cfg.Bucket),
		Key:    aws.String(d.joinPrefix(key)),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: GetObject %s: %w", key, err)
	}
	return got.Body, nil
}

func (d *Driver) joinPrefix(p string) string {
	base := strings.Trim(d.cfg.Subfolder, "/")
	rel := strings.TrimLeft(p, "/")
	switch {
	case base == "" && rel == "":
		return ""
	case base == "":
		return rel
	case rel == "":
		return base + "/"
	default:
		return base + "/" + rel
	}
}

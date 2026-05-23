package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// ObjectStore is the tiny PUT-only contract the Distributor needs to
// deliver to an object store. The production implementation wraps
// aws-sdk-go-v2's S3 client; tests inject a fake.
type ObjectStore interface {
	PutObject(ctx context.Context, bucket, key string, body []byte, contentType string) error
}

// S3Config carries the credentials and routing for the S3 (or
// S3-compatible — Ceph RGW, Minio) endpoint. An empty Region keeps the
// object store disabled, so the Distributor records every s3 recipient
// as "skipped — s3 not configured" until the operator wires real values.
type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle = true is needed by most S3-compatible servers (Ceph
	// RGW, Minio); false uses virtual-hosted addressing (AWS default).
	PathStyle bool
}

func (c S3Config) configured() bool { return c.Region != "" }

// errS3NotConfigured is returned when an s3 recipient is asked to
// deliver but no ObjectStore is wired. The Distributor translates it
// into a recipient-level "skipped", not a failure.
var errS3NotConfigured = errors.New("s3 not configured")

// NewS3ObjectStore builds an ObjectStore backed by aws-sdk-go-v2/s3.
// Returns nil + nil when the config is empty so callers can pass a
// possibly-disabled config straight through to NewDistributor.
func NewS3ObjectStore(cfg S3Config) (ObjectStore, error) {
	if !cfg.configured() {
		return nil, nil
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}
	clientOpts := []func(*awss3.Options){}
	if cfg.Endpoint != "" {
		ep := cfg.Endpoint
		clientOpts = append(clientOpts, func(o *awss3.Options) { o.BaseEndpoint = aws.String(ep) })
	}
	if cfg.PathStyle {
		clientOpts = append(clientOpts, func(o *awss3.Options) { o.UsePathStyle = true })
	}
	return &awsObjectStore{client: awss3.NewFromConfig(awsCfg, clientOpts...)}, nil
}

type awsObjectStore struct{ client *awss3.Client }

func (s *awsObjectStore) PutObject(ctx context.Context, bucket, key string, body []byte, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	return err
}

// sendS3 uploads the artifact to the bucket+key implied by a recipient
// target of the form `s3://bucket/prefix` (prefix optional). The
// object key is `prefix/<execution_id>/<file_name>`.
func (d *Distributor) sendS3(ctx context.Context, r DistributionRecipient, e ReportExecution, artifact []byte) error {
	if d.store == nil {
		return errS3NotConfigured
	}
	bucket, prefix, err := parseS3Target(r.Target)
	if err != nil {
		return err
	}
	name := e.Artifact.FileName
	if name == "" {
		name = "report"
	}
	key := path.Join(prefix, e.ID, name)
	return d.store.PutObject(ctx, bucket, key, artifact, e.Artifact.MimeType)
}

// parseS3Target parses `s3://bucket/prefix` (and tolerates the bare
// `bucket/prefix` shape some operators use in config). Returns the
// bucket and the cleaned prefix (no leading or trailing slash).
func parseS3Target(target string) (bucket, prefix string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("s3 recipient target is empty")
	}
	if strings.HasPrefix(target, "s3://") {
		u, perr := url.Parse(target)
		if perr != nil {
			return "", "", fmt.Errorf("s3: invalid target %q: %w", target, perr)
		}
		if u.Host == "" {
			return "", "", fmt.Errorf("s3: target %q is missing a bucket", target)
		}
		return u.Host, strings.Trim(u.Path, "/"), nil
	}
	bucket, prefix, _ = strings.Cut(target, "/")
	if bucket == "" {
		return "", "", fmt.Errorf("s3: target %q is missing a bucket", target)
	}
	return bucket, strings.Trim(prefix, "/"), nil
}

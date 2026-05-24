package awsclient

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Config carries the platform-wide AWS connection settings. Every
// field is optional; an empty Config returns clients that pick up the
// standard AWS credential chain (IRSA / instance role / env / ~/.aws).
//
// Set EndpointURL to point at LocalStack, MinIO, or any other
// S3/AWS-compatible service. When EndpointURL is set, callers SHOULD
// also set Region (the SDK requires one even for non-AWS endpoints;
// "us-east-1" is the conventional placeholder).
type Config struct {
	// EndpointURL routes every AWS API call through a custom endpoint —
	// LocalStack at http://localhost:4566, MinIO at http://minio:9000,
	// a Ceph RGW URL, … An empty string keeps the SDK pointed at real
	// AWS.
	EndpointURL string `koanf:"endpoint_url"`

	// Region is the AWS region. Falls back to the standard chain
	// (AWS_REGION env, ~/.aws/config) when empty. LocalStack accepts
	// any region but typically expects "us-east-1".
	Region string `koanf:"region"`

	// Static credentials. Leave both empty to use the SDK's default
	// credential chain (preferred in production via IRSA / instance
	// role).
	AccessKeyID     string `koanf:"access_key_id"`
	SecretAccessKey string `koanf:"secret_access_key"`
	SessionToken    string `koanf:"session_token"`

	// Profile selects a named profile from ~/.aws/credentials when no
	// static credentials are supplied. Ignored when AccessKeyID is set.
	Profile string `koanf:"profile"`

	// PathStyle forces S3 path-style addressing
	// (https://endpoint/bucket/key) instead of virtual-hosted
	// (https://bucket.endpoint/key). Required by MinIO, Ceph RGW, and
	// LocalStack. When EndpointURL is set and PathStyle is its zero
	// value, factories that care (currently just S3) opt-in to
	// path-style automatically — operators almost always want it.
	PathStyle bool `koanf:"path_style"`
}

// Configured reports whether the caller supplied any non-default
// field. An unconfigured Config still produces a working aws.Config
// via the standard credential chain — Configured exists so service
// constructors can return `nil, nil` cleanly when the operator has
// not opted in (mirrors the pattern in services/report-service's
// NewS3ObjectStore).
func (c Config) Configured() bool {
	return c.EndpointURL != "" ||
		c.Region != "" ||
		c.AccessKeyID != "" ||
		c.Profile != ""
}

// UsesCustomEndpoint reports whether EndpointURL routes traffic
// somewhere other than real AWS. Factories use this to flip on
// path-style addressing and similar S3-compatible knobs.
func (c Config) UsesCustomEndpoint() bool {
	return strings.TrimSpace(c.EndpointURL) != ""
}

// Build resolves the Config into a ready-to-use aws.Config. The
// returned value can be passed straight to any aws-sdk-go-v2 service
// constructor; the per-service helpers in this package wrap that
// boilerplate.
func (c Config) Build(ctx context.Context) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{}

	region := strings.TrimSpace(c.Region)
	if region == "" && c.UsesCustomEndpoint() {
		// LocalStack and most S3-compatibles want *some* region
		// string; "us-east-1" is the de-facto default and avoids a
		// confusing "no region" error from the SDK.
		region = "us-east-1"
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	if c.AccessKeyID != "" || c.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken),
		))
	} else if c.Profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(c.Profile))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("awsclient: load aws config: %w", err)
	}
	return cfg, nil
}

// LoadFromEnv reads the canonical OF_AWS__* environment variables and
// returns a Config. Services that already bind a koanf block do not
// need this; it is offered as a one-liner for tests and ad-hoc tools.
//
//	OF_AWS__ENDPOINT_URL      e.g. http://localhost:4566
//	OF_AWS__REGION            e.g. us-east-1
//	OF_AWS__ACCESS_KEY_ID
//	OF_AWS__SECRET_ACCESS_KEY
//	OF_AWS__SESSION_TOKEN
//	OF_AWS__PROFILE
//	OF_AWS__PATH_STYLE        "true" / "false"
func LoadFromEnv() Config {
	return Config{
		EndpointURL:     os.Getenv("OF_AWS__ENDPOINT_URL"),
		Region:          os.Getenv("OF_AWS__REGION"),
		AccessKeyID:     os.Getenv("OF_AWS__ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("OF_AWS__SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("OF_AWS__SESSION_TOKEN"),
		Profile:         os.Getenv("OF_AWS__PROFILE"),
		PathStyle:       strings.EqualFold(os.Getenv("OF_AWS__PATH_STYLE"), "true"),
	}
}

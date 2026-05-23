package awsclient

import (
	"context"
	"testing"
)

func TestConfigConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"empty", Config{}, false},
		{"endpoint set", Config{EndpointURL: "http://localhost:4566"}, true},
		{"region only", Config{Region: "us-east-1"}, true},
		{"static creds", Config{AccessKeyID: "AKIA…"}, true},
		{"profile only", Config{Profile: "dev"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Configured(); got != tc.want {
				t.Fatalf("Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigUsesCustomEndpoint(t *testing.T) {
	empty := Config{}
	if empty.UsesCustomEndpoint() {
		t.Fatal("empty config should not use a custom endpoint")
	}
	withEndpoint := Config{EndpointURL: "http://localhost:4566"}
	if !withEndpoint.UsesCustomEndpoint() {
		t.Fatal("EndpointURL should opt-in")
	}
	whitespace := Config{EndpointURL: "   "}
	if whitespace.UsesCustomEndpoint() {
		t.Fatal("whitespace-only EndpointURL must not opt-in")
	}
}

func TestBuildFallsBackToUSEast1ForCustomEndpoint(t *testing.T) {
	cfg, err := Config{EndpointURL: "http://localhost:4566"}.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("region = %q, want us-east-1 fallback when endpoint set", cfg.Region)
	}
}

func TestBuildHonorsExplicitRegion(t *testing.T) {
	cfg, err := Config{Region: "eu-west-1"}.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("region = %q, want eu-west-1", cfg.Region)
	}
}

func TestBuildWithStaticCredentials(t *testing.T) {
	cfg, err := Config{
		Region:          "us-east-1",
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		SessionToken:    "session",
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Credentials.Retrieve: %v", err)
	}
	if creds.AccessKeyID != "AKIATEST" {
		t.Errorf("access key = %q", creds.AccessKeyID)
	}
	if creds.SessionToken != "session" {
		t.Errorf("session token routed wrong: %q", creds.SessionToken)
	}
}

func TestLoadFromEnvReadsCanonicalVariables(t *testing.T) {
	t.Setenv("OF_AWS__ENDPOINT_URL", "http://localhost:4566")
	t.Setenv("OF_AWS__REGION", "eu-west-1")
	t.Setenv("OF_AWS__ACCESS_KEY_ID", "AKIA")
	t.Setenv("OF_AWS__SECRET_ACCESS_KEY", "secret")
	t.Setenv("OF_AWS__PATH_STYLE", "true")

	c := LoadFromEnv()
	if c.EndpointURL != "http://localhost:4566" || c.Region != "eu-west-1" ||
		c.AccessKeyID != "AKIA" || c.SecretAccessKey != "secret" || !c.PathStyle {
		t.Fatalf("LoadFromEnv = %+v", c)
	}
}

func TestS3FactoryEnablesPathStyleOnCustomEndpoint(t *testing.T) {
	client, err := S3(context.Background(), Config{EndpointURL: "http://localhost:4566"})
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
	opts := client.Options()
	if opts.BaseEndpoint == nil || *opts.BaseEndpoint != "http://localhost:4566" {
		t.Errorf("BaseEndpoint = %v, want http://localhost:4566", opts.BaseEndpoint)
	}
	if !opts.UsePathStyle {
		t.Error("UsePathStyle should be true for custom endpoint")
	}
}

func TestS3FactoryLeavesVirtualHostedForRealAWS(t *testing.T) {
	client, err := S3(context.Background(), Config{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	opts := client.Options()
	if opts.BaseEndpoint != nil {
		t.Errorf("BaseEndpoint should be nil for real AWS, got %v", *opts.BaseEndpoint)
	}
	if opts.UsePathStyle {
		t.Error("UsePathStyle should default to false for real AWS")
	}
}

# libs/aws-client

The shared **AWS-SDK-v2 client factory** for OpenFoundry services.

One Config struct, one `Build(ctx)` call → a ready `aws.Config`. Per-service
helpers (`S3`, `Lambda`, `Bedrock`, …) wrap the SDK boilerplate so the
business code stays small.

## Why it exists

Several services need to talk to AWS or AWS-compatible APIs:

- `services/report-service` — uploads rendered PDFs to S3 (or Ceph RGW / MinIO)
- `services/connector-management-service` — tests the S3 driver and will soon
  carry Lambda / SQS / DynamoDB / Athena drivers too
- `services/llm-catalog-service` — Bedrock invoker, peer to Anthropic / OpenAI
- `services/pipeline-build-service` — future `lambda` transform_type

Without a shared lib, each service re-implemented the same five lines of
endpoint / region / credentials wiring. Now they all share this one path.

## Local development with LocalStack

Every factory respects `Config.EndpointURL`. Point that at LocalStack and
the SDK routes traffic through `http://localhost:4566` instead of
`*.amazonaws.com` — same code in dev and prod.

```yaml
# infra/compose/docker-compose.yml (under the "intelligence" profile)
localstack:
  image: localstack/localstack:3
  ports: ["4566:4566"]
  environment:
    SERVICES: s3,lambda,sqs,sns,dynamodb,iam,sts,athena
```

```bash
# Run any AWS-using service against LocalStack:
export OF_AWS__ENDPOINT_URL=http://localhost:4566
export OF_AWS__REGION=us-east-1
export OF_AWS__ACCESS_KEY_ID=test
export OF_AWS__SECRET_ACCESS_KEY=test
./bin/report-service
```

In production the env is unset, the standard AWS credential chain takes
over (IRSA / instance role / shared config), and the same binary hits
real AWS.

## Adding a new service factory

Drop a single file alongside `s3.go`:

```go
// libs/aws-client/lambda.go
package awsclient

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/lambda"
)

func Lambda(ctx context.Context, c Config) (*lambda.Client, error) {
    awsCfg, err := c.Build(ctx)
    if err != nil { return nil, err }
    opts := []func(*lambda.Options){}
    if c.UsesCustomEndpoint() {
        ep := c.EndpointURL
        opts = append(opts, func(o *lambda.Options) { o.BaseEndpoint = aws.String(ep) })
    }
    return lambda.NewFromConfig(awsCfg, opts...), nil
}
```

Add a test that asserts `BaseEndpoint` is wired when a custom endpoint is
set, and `go mod tidy` picks up the new SDK module. Done.

## Config reference

| Field | Env var | Purpose |
|---|---|---|
| `EndpointURL` | `OF_AWS__ENDPOINT_URL` | LocalStack / MinIO / Ceph RGW URL. Empty = real AWS. |
| `Region` | `OF_AWS__REGION` | AWS region. Defaults to `us-east-1` when `EndpointURL` is set. |
| `AccessKeyID` | `OF_AWS__ACCESS_KEY_ID` | Static access key (dev only — prefer IRSA in prod). |
| `SecretAccessKey` | `OF_AWS__SECRET_ACCESS_KEY` | Static secret. |
| `SessionToken` | `OF_AWS__SESSION_TOKEN` | Optional STS session token. |
| `Profile` | `OF_AWS__PROFILE` | Named profile from `~/.aws/credentials`. |
| `PathStyle` | `OF_AWS__PATH_STYLE` | Force S3 path-style URLs. Auto-on when `EndpointURL` is set. |

// Package awsclient is the shared AWS-SDK-v2 client factory used by
// every OpenFoundry service that talks to an AWS-style API.
//
// It serves two equally important runtimes:
//
//   - Production AWS — `Config{}` zero-value plus the standard
//     environment (IRSA, instance role, AWS_PROFILE, …). The factories
//     return clients that hit the real `*.amazonaws.com` endpoints.
//
//   - LocalStack (or any S3-compatible / API-compatible service like
//     MinIO, Ceph RGW, Pulumi-LocalStack) — set `Config.EndpointURL`
//     and every factory routes through that endpoint. Static
//     credentials, path-style addressing, and the `us-east-1` default
//     region are applied automatically so a developer can run
//     `OF_AWS__ENDPOINT_URL=http://localhost:4566 ./bin/<service>` and
//     have the service hit LocalStack with zero code change.
//
// The Config is `koanf` / env-friendly: services either embed it in
// their YAML config block or call `LoadFromEnv` to read the canonical
// `OF_AWS__*` variables directly. Either way the same Config flows
// through `Build(ctx)` → `aws.Config` → the per-service factories
// (`S3`, `Lambda`, `Bedrock`, …) declared in this package.
//
// New service factories are added one per file (`lambda.go`,
// `bedrock.go`, `sqs.go`, …) so the build graph of any consumer only
// pulls in the AWS SDK modules it actually uses; `go mod tidy` keeps
// the dependency surface minimal.
package awsclient

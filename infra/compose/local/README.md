# Local Runtime Assets

This directory contains support files consumed by the root Compose files.
The Compose entrypoints stay at `infra/docker-compose.yml` and
`infra/docker-compose.dev.yml`; this directory keeps their mounted assets
out of the infrastructure root.

| Path | Consumed by |
| --- | --- |
| `postgres-init/` | `postgres` service, mounted at `/docker-entrypoint-initdb.d` |
| `nginx/` | `nginx` app-profile edge proxy |
| `localstack-init/` | `localstack-init` (opt-in `localstack` profile); seeds S3 buckets, SQS queues, an SNS topic, a DynamoDB table, a Lambda, and one SecretsManager secret. See [LocalStack section in libs/aws-client](../../../libs/aws-client/README.md#local-development-with-localstack). |

Kubernetes-only manifests belong under `infra/k8s/platform/`.

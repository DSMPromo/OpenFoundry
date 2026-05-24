# OpenFoundry Terraform Provider

This directory contains the provider schema and starter examples for managing OpenFoundry resources as infrastructure as code.

Generate the latest schema with:

```bash
just terraform-schema
```

The provider currently covers repository integrations, audit policies, Nexus peers, product-delivery DevOps resources such as rollout fleets and enrollment branches, and deployment-fabric primitives for:

- multi-cloud deployment cells
- geo-fence / residency policies
- air-gapped release bundles
- Apollo rollout automation
- **AWS connectors** (`openfoundry_aws_connector`) registering s3, lambda, sqs, sns, dynamodb, or athena resources in connector-management-service — supports LocalStack-backed dev (`endpoint` inside `config`) and real AWS in prod (IRSA / instance role)

The developer portal consumes the generated schema from `apps/web/static/generated/terraform/openfoundry-provider.json`.

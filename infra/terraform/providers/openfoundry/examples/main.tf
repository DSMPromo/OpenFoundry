terraform {
  required_version = ">= 1.7.0"

  required_providers {
    openfoundry = {
      source  = "openfoundry/openfoundry"
      version = "~> 0.1"
    }
  }
}

provider "openfoundry" {
  api_url   = "https://platform.openfoundry.local"
  token     = var.openfoundry_token
  workspace = "production"
}

resource "openfoundry_repository_integration" "github_widget_kit" {
  repository_id       = "0196839d-d210-7f8c-8a1d-7ab001030001"
  provider            = "github"
  external_project    = "foundry-widget-kit"
  sync_mode           = "bidirectional_mirror"
  ci_trigger_strategy = "github_actions"
}

resource "openfoundry_nexus_peer" "acme_health" {
  slug          = "acme-health"
  endpoint_url  = "https://nexus.acme-health.example"
  auth_mode     = "mtls+jwt"
  shared_scopes = ["claims", "audit"]
}

resource "openfoundry_audit_policy" "pii_retention" {
  name           = "PII retention"
  classification = "pii"
  retention_days = 365
  purge_mode     = "redact-then-retain-hash"
}

resource "openfoundry_product_fleet" "ops_center" {
  listing_id            = "01968b70-4f20-71b8-a6f0-0f4000001001"
  name                  = "Ops Center Fleet"
  environment           = "production"
  workspace_targets     = ["Operations Center - EU", "Operations Center - US"]
  release_channel       = "stable"
  auto_upgrade_enabled  = true
  maintenance_window    = jsonencode({
    timezone         = "UTC"
    days             = ["sun"]
    start_hour_utc   = 2
    duration_minutes = 180
  })
  branch_strategy       = "isolated_branch_per_feature"
  rollout_strategy      = "rolling"
}

resource "openfoundry_enrollment_branch" "ops_shift_handovers" {
  fleet_id           = openfoundry_product_fleet.ops_center.id
  name               = "feature/shift-handovers"
  repository_branch  = "release/ops-center/feature-shift-handovers"
  notes              = "Sandbox branch for handover widgets before rollout promotion."
}

resource "openfoundry_deployment_cell" "eu_regulated" {
  name              = "eu-regulated"
  cloud             = "aws"
  region            = "eu-west-1"
  environment       = "production"
  workspace_targets = ["Operations Center - EU", "Regulated Analytics - EU"]
  workload_identity = jsonencode({
    aws_role_arn = "arn:aws:iam::123456789012:role/openfoundry-platform"
  })
  failover_priority = 10
}

resource "openfoundry_geo_fence_policy" "eu_only" {
  name                  = "EU Residency Fence"
  allowed_countries     = ["ES", "FR", "DE", "IT", "NL"]
  allowed_ingress_cidrs = ["185.10.0.0/16", "194.25.0.0/16"]
  allowed_egress_cidrs  = ["10.40.0.0/16", "10.41.0.0/16"]
  required_node_labels  = jsonencode({
    "topology.kubernetes.io/region" = "eu-west-1"
    "openfoundry.io/residency"      = "eu"
  })
  default_action        = "deny"
}

resource "openfoundry_airgap_bundle" "sovereign_release" {
  name                    = "madrid-dc-release"
  bundle_version          = "2026.04.25-offline"
  private_registry        = "registry.airgap.local/openfoundry-mirror"
  mirror_registries       = ["registry.airgap.local/dockerhub", "registry.airgap.local/ghcr"]
  release_manifest_sha256 = "4db5d85f38b7e57df7bca91d6d7f8c4a3d3915f1b97f59d4780e87c7b2b2ad7a"
  public_egress_disabled  = true
}

resource "openfoundry_aws_connector" "echo_lambda" {
  name           = "echo-lambda"
  connector_type = "lambda"
  config = jsonencode({
    function_name = "openfoundry-echo"
    region        = "us-east-1"
    # Leave endpoint empty in production to use real AWS via IRSA /
    # instance role; set it for LocalStack / a Bedrock emulator.
    # endpoint = "http://localhost:4566"
  })
}

resource "openfoundry_aws_connector" "ops_bucket" {
  name           = "ops-reports"
  connector_type = "s3"
  config = jsonencode({
    bucket    = "openfoundry-reports"
    region    = "us-east-1"
    path_style = false
  })
}

resource "openfoundry_aws_connector" "pipeline_events" {
  name           = "pipeline-events"
  connector_type = "sqs"
  config = jsonencode({
    queue_url = "https://sqs.us-east-1.amazonaws.com/123456789012/openfoundry-pipeline-events"
    region    = "us-east-1"
  })
}

resource "openfoundry_aws_connector" "broadcast_topic" {
  name           = "broadcast"
  connector_type = "sns"
  config = jsonencode({
    topic_arn = "arn:aws:sns:us-east-1:123456789012:openfoundry-broadcast"
    region    = "us-east-1"
  })
}

resource "openfoundry_aws_connector" "kv_table" {
  name           = "kv"
  connector_type = "dynamodb"
  config = jsonencode({
    table_name = "openfoundry-kv"
    region     = "us-east-1"
  })
}

resource "openfoundry_aws_connector" "analytics_athena" {
  name           = "analytics-warehouse"
  connector_type = "athena"
  config = jsonencode({
    workgroup       = "primary"
    database        = "analytics"
    output_location = "s3://openfoundry-reports/athena-results/"
    region          = "us-east-1"
  })
}

resource "openfoundry_apollo_rollout" "ops_center" {
  name                         = "Ops Center Apollo"
  schedule                     = "*/10 * * * *"
  action_mode                  = "gated_sync"
  control_panel_readiness_path = "/api/v1/control-panel/upgrade-readiness"
  fleet_ids                    = [openfoundry_product_fleet.ops_center.id]
  promote_webhook_path         = "/api/v1/marketplace/devops/fleets/${openfoundry_product_fleet.ops_center.id}/sync"
  release_channels             = ["stable", "canary"]
}

variable "openfoundry_token" {
  type      = string
  sensitive = true
}

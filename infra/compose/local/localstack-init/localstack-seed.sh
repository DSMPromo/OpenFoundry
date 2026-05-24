#!/bin/sh
# LocalStack seed fixtures.
#
# Idempotent — each AWS create call is conditional on the resource
# being absent. Re-runs after `docker compose up` flaps are safe.
#
# Resources match the AWS-integration plan in the PR description of
# libs/aws-client: an S3 bucket for report-service / connector
# fixtures, a Lambda for the future `lambda` pipeline transform_type,
# an SQS queue and SNS topic for messaging connectors, a DynamoDB
# table for the storage connector.
set -eu

LOCALSTACK_ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localstack:4566}"
AWS_CLI="aws --endpoint-url=${LOCALSTACK_ENDPOINT} --no-cli-pager"

echo "==> LocalStack seed starting against ${LOCALSTACK_ENDPOINT}"

# ── S3 ─────────────────────────────────────────────────────────────────
for bucket in openfoundry-reports openfoundry-test-fixtures openfoundry-pipeline-artifacts; do
  if ${AWS_CLI} s3api head-bucket --bucket "${bucket}" >/dev/null 2>&1; then
    echo "  S3 bucket already exists: ${bucket}"
  else
    ${AWS_CLI} s3api create-bucket --bucket "${bucket}" >/dev/null
    echo "  created S3 bucket: ${bucket}"
  fi
done

# ── SQS ────────────────────────────────────────────────────────────────
for queue in openfoundry-pipeline-events openfoundry-deadletter; do
  url=$(${AWS_CLI} sqs get-queue-url --queue-name "${queue}" 2>/dev/null | grep QueueUrl | sed 's/.*"QueueUrl": "\(.*\)".*/\1/' || true)
  if [ -n "${url}" ]; then
    echo "  SQS queue already exists: ${queue}"
  else
    ${AWS_CLI} sqs create-queue --queue-name "${queue}" >/dev/null
    echo "  created SQS queue: ${queue}"
  fi
done

# ── SNS ────────────────────────────────────────────────────────────────
arn=$(${AWS_CLI} sns list-topics 2>/dev/null | grep "openfoundry-broadcast" || true)
if [ -n "${arn}" ]; then
  echo "  SNS topic already exists: openfoundry-broadcast"
else
  ${AWS_CLI} sns create-topic --name openfoundry-broadcast >/dev/null
  echo "  created SNS topic: openfoundry-broadcast"
fi

# ── DynamoDB ───────────────────────────────────────────────────────────
if ${AWS_CLI} dynamodb describe-table --table-name openfoundry-kv >/dev/null 2>&1; then
  echo "  DynamoDB table already exists: openfoundry-kv"
else
  ${AWS_CLI} dynamodb create-table \
    --table-name openfoundry-kv \
    --attribute-definitions AttributeName=id,AttributeType=S \
    --key-schema AttributeName=id,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST >/dev/null
  echo "  created DynamoDB table: openfoundry-kv"
fi

# ── Lambda (stub function that echoes the input event) ─────────────────
if ${AWS_CLI} lambda get-function --function-name openfoundry-echo >/dev/null 2>&1; then
  echo "  Lambda function already exists: openfoundry-echo"
else
  # Write a tiny Node 20 handler to a temporary zip the aws-cli image can read.
  workdir=$(mktemp -d)
  cat >"${workdir}/index.js" <<'JS'
exports.handler = async (event) => ({
  statusCode: 200,
  body: JSON.stringify({ echoed: event, runtime: 'localstack' }),
});
JS
  (cd "${workdir}" && zip -q -r function.zip index.js)
  ${AWS_CLI} lambda create-function \
    --function-name openfoundry-echo \
    --runtime nodejs20.x \
    --role arn:aws:iam::000000000000:role/lambda-role \
    --handler index.handler \
    --zip-file "fileb://${workdir}/function.zip" >/dev/null
  echo "  created Lambda function: openfoundry-echo"
fi

# ── SecretsManager (one example secret) ────────────────────────────────
if ${AWS_CLI} secretsmanager describe-secret --secret-id openfoundry-dev-shared >/dev/null 2>&1; then
  echo "  SecretsManager secret already exists: openfoundry-dev-shared"
else
  ${AWS_CLI} secretsmanager create-secret \
    --name openfoundry-dev-shared \
    --secret-string '{"hint":"localstack dev placeholder; not a real secret"}' >/dev/null
  echo "  created SecretsManager secret: openfoundry-dev-shared"
fi

echo "==> LocalStack seed complete."

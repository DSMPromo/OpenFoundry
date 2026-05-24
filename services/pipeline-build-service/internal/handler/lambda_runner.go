package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
)

// LambdaRunner is the seam pipeline-build-service uses to invoke AWS
// Lambda as a transform_type. main.go injects an awsLambdaRunner
// backed by libs/aws-client.Lambda; tests inject a fake without
// hitting AWS / LocalStack.
//
// The transform contract is intentionally narrow:
//
//   - The pipeline runtime serializes the upstream rows as the
//     `inputs[]` array on the request payload (one entry per
//     dependency, with `node_id` + `rows[]`).
//   - The pipeline runtime forwards the node's `logic_payload.config`
//     verbatim as the request `config` field.
//   - The Lambda is expected to respond with a JSON object containing
//     a `rows[]` array. Any other JSON shape is accepted but the
//     pipeline records zero rows + stashes the raw response in
//     metadata so node authors can debug from the build inspector.
type LambdaRunner interface {
	InvokeForTransform(ctx context.Context, req LambdaTransformRequest) (*LambdaTransformResult, error)
}

// LambdaTransformRequest is everything the runner needs to invoke a
// Lambda for one pipeline node.
type LambdaTransformRequest struct {
	// FunctionName is the canonical Lambda identifier — name, ARN,
	// or partial ARN. Required.
	FunctionName string

	// Qualifier optionally pins an alias or version (e.g. "PROD").
	Qualifier string

	// Payload is the JSON bytes sent verbatim as the Lambda
	// InvokeInput.Payload. The default builder ships
	// {node_id, config, inputs[]} but callers can inject any
	// shape they want for a custom Lambda contract.
	Payload []byte
}

// LambdaTransformResult is the unwrapped Lambda response. StatusCode
// + Payload come straight from the SDK; FunctionError is non-empty
// when the Lambda raised (Handled/Unhandled).
type LambdaTransformResult struct {
	StatusCode      int32
	Payload         []byte
	FunctionError   string
	ExecutedVersion string
}

// LambdaRunnerConfig carries the env-driven settings main.go reads to
// build the production runner. Empty BedrockRegion-style: leave
// Region empty to fall back to the standard AWS chain, and leave
// EndpointURL empty to hit real AWS instead of LocalStack.
type LambdaRunnerConfig struct {
	Region          string
	EndpointURL     string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// NewLambdaRunner builds an AWS-backed LambdaRunner. Returns nil +
// nil when the config is empty so callers can pass a possibly-
// disabled config straight through to ExecutionPorts.
func NewLambdaRunner(ctx context.Context, cfg LambdaRunnerConfig) (LambdaRunner, error) {
	if strings.TrimSpace(cfg.Region) == "" && strings.TrimSpace(cfg.EndpointURL) == "" {
		return nil, nil
	}
	client, err := awsclient.Lambda(ctx, awsclient.Config{
		Region:          cfg.Region,
		EndpointURL:     cfg.EndpointURL,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("lambda runner: %w", err)
	}
	return &awsLambdaRunner{client: client}, nil
}

// lambdaInvokeAPI is the minimum SDK surface the runner needs.
type lambdaInvokeAPI interface {
	Invoke(ctx context.Context, in *awslambda.InvokeInput, opts ...func(*awslambda.Options)) (*awslambda.InvokeOutput, error)
}

type awsLambdaRunner struct{ client lambdaInvokeAPI }

func (r *awsLambdaRunner) InvokeForTransform(ctx context.Context, req LambdaTransformRequest) (*LambdaTransformResult, error) {
	if strings.TrimSpace(req.FunctionName) == "" {
		return nil, errors.New("lambda_transform: function_name is required")
	}
	in := &awslambda.InvokeInput{
		FunctionName:   aws.String(req.FunctionName),
		InvocationType: lambdatypes.InvocationTypeRequestResponse,
		Payload:        req.Payload,
	}
	if req.Qualifier != "" {
		in.Qualifier = aws.String(req.Qualifier)
	}
	out, err := r.client.Invoke(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("lambda_transform: invoke %s: %w", req.FunctionName, err)
	}
	res := &LambdaTransformResult{
		StatusCode: out.StatusCode,
		Payload:    append([]byte(nil), out.Payload...),
	}
	if out.FunctionError != nil {
		res.FunctionError = *out.FunctionError
	}
	if out.ExecutedVersion != nil {
		res.ExecutedVersion = *out.ExecutedVersion
	}
	return res, nil
}

// lambdaTransformInputs is the JSON shape sent to the Lambda's
// `inputs[]` field. Mirrors python_transform's pythonPreparedInput
// vocabulary so users porting a Python transform to Lambda only
// have to swap the runtime, not the request shape.
type lambdaTransformInputs struct {
	NodeID string                       `json:"node_id"`
	Rows   []map[string]json.RawMessage `json:"rows"`
}

// lambdaTransformRequestEnvelope is the default payload shape sent
// when the user does not override it with a custom payload field on
// the node's logic_payload.
type lambdaTransformRequestEnvelope struct {
	NodeID string                  `json:"node_id"`
	Config json.RawMessage         `json:"config,omitempty"`
	Inputs []lambdaTransformInputs `json:"inputs"`
}

// lambdaTransformResponse is the contract the Lambda response is
// expected to follow. Both fields are optional — a response with
// neither is recorded as "function ran, produced no rows".
type lambdaTransformResponse struct {
	Rows     []map[string]json.RawMessage `json:"rows"`
	Metadata json.RawMessage              `json:"metadata,omitempty"`
}

// buildLambdaPayload assembles the default JSON payload the runner
// sends. Exported only via runLambda below — kept in this file so
// the request/response shapes live next to each other.
func buildLambdaPayload(nodeID string, cfg map[string]json.RawMessage, inputs []lambdaTransformInputs) ([]byte, error) {
	env := lambdaTransformRequestEnvelope{NodeID: nodeID, Inputs: inputs}
	if raw, ok := cfg["config"]; ok && len(raw) > 0 {
		env.Config = raw
	}
	return json.Marshal(env)
}

// parseLambdaResponse decodes the Lambda's JSON reply into rows +
// raw metadata. Non-JSON / unexpected shapes don't error — they
// degrade to "zero rows; raw response stashed in metadata" so the
// build inspector can show the operator what the function actually
// returned.
func parseLambdaResponse(raw []byte) lambdaTransformResponse {
	if len(raw) == 0 {
		return lambdaTransformResponse{}
	}
	var parsed lambdaTransformResponse
	if err := json.Unmarshal(raw, &parsed); err == nil && (len(parsed.Rows) > 0 || len(parsed.Metadata) > 0) {
		return parsed
	}
	// Either non-JSON, or JSON of a shape that didn't carry the
	// expected fields. Wrap whatever we got so the operator can see
	// it from the inspector.
	parsed = lambdaTransformResponse{}
	parsed.Metadata, _ = json.Marshal(map[string]any{"raw_response": string(raw)})
	return parsed
}

// runLambda is the runtimeNodeRunner method invoked for transform_
// type=="lambda". Defined here (rather than on execution.go) so the
// Lambda-specific knowledge stays in one place.
func (r runtimeNodeRunner) runLambda(ctx context.Context, node executor.NodeContext, payload json.RawMessage) (executor.NodeResult, error) {
	if r.Lambda == nil {
		return executor.NodeResult{}, errors.New("lambda_runner_not_configured: set BEDROCK_REGION/OF_AWS__REGION or OF_AWS__ENDPOINT_URL to execute Lambda transforms")
	}
	var cfg map[string]json.RawMessage
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &cfg)
	}
	functionName := unquoteString(cfg["function_name"])
	if functionName == "" {
		return executor.NodeResult{}, errors.New("lambda_transform: logic_payload.function_name is required")
	}
	qualifier := unquoteString(cfg["qualifier"])

	inputs := lambdaInputsForNode(r.Table, node)
	requestBody, err := buildLambdaPayload(node.Node.ID, cfg, inputs)
	if err != nil {
		return executor.NodeResult{}, fmt.Errorf("lambda_transform: build payload: %w", err)
	}

	out, err := r.Lambda.InvokeForTransform(ctx, LambdaTransformRequest{
		FunctionName: functionName,
		Qualifier:    qualifier,
		Payload:      requestBody,
	})
	if err != nil {
		return executor.NodeResult{}, err
	}
	if out.FunctionError != "" {
		return executor.NodeResult{}, fmt.Errorf("lambda_transform: %s raised %q: %s", functionName, out.FunctionError, string(out.Payload))
	}

	parsed := parseLambdaResponse(out.Payload)
	if r.Table != nil {
		r.Table.storeRows(node.Node.ID, pythonRuntimeRows(parsed.Rows))
	}

	hashInput := append([]byte(functionName), requestBody...)
	hashInput = append(hashInput, out.Payload...)
	hash := sha256.Sum256(hashInput)

	sample := parsed.Rows
	if len(sample) > 5 {
		sample = sample[:5]
	}
	meta := map[string]any{
		"runtime":          "lambda",
		"engine":           "aws_lambda",
		"transform_type":   "lambda",
		"function_name":    functionName,
		"qualifier":        qualifier,
		"status_code":      out.StatusCode,
		"executed_version": out.ExecutedVersion,
		"columns":          inferResultColumns(parsed.Rows),
		"sample_rows":      sample,
		"data_rows":        parsed.Rows,
	}
	if len(parsed.Metadata) > 0 {
		meta["function_metadata"] = parsed.Metadata
	}
	return executor.NodeResult{
		OutputContentHash: hex.EncodeToString(hash[:]),
		Metadata:          meta,
	}, nil
}

// lambdaInputsForNode pulls upstream rows from the lightweight table
// runtime and projects them into the lambda input shape. Mirrors
// the python pipeline's prepared-inputs path but stays JSON-only
// (no schema field — Lambda authors can call their own inference if
// they need it).
func lambdaInputsForNode(table *lightweightTableRuntime, node executor.NodeContext) []lambdaTransformInputs {
	if table == nil || len(node.Node.DependsOn) == 0 {
		return nil
	}
	out := make([]lambdaTransformInputs, 0, len(node.Node.DependsOn))
	for _, dep := range node.Node.DependsOn {
		rows, err := table.dependencyRows(dep)
		if err != nil {
			continue
		}
		out = append(out, lambdaTransformInputs{NodeID: dep, Rows: rowsToMaps(rows)})
	}
	return out
}

// unquoteString tolerantly extracts a string from a JSON-encoded
// value the user typed into logic_payload. Accepts both quoted
// strings and bare identifiers.
func unquoteString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	return string(trimmed)
}

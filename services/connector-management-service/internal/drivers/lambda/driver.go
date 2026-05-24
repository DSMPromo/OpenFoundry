// Package lambda is the runtime client for the AWS Lambda connector.
// It mirrors the shape of the s3 driver (services/connector-management-
// service/internal/drivers/s3): a tiny Config + a [Driver] with the
// minimum surface a TestConnection / Invoke path needs.
//
// Operators register a Lambda function as a connection, and the
// management service uses this driver to:
//   - verify the function exists (Connect → GetFunction)
//   - invoke it synchronously and return the response payload
//     (Invoke)
//   - list candidate functions for discovery (List)
//
// Production paths construct the [*lambda.Client] via
// libs/aws-client.Lambda so a single Config-shaped switch lets the
// same code talk to LocalStack in dev and real AWS in prod.
package lambda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
)

// Config is the wire shape consumed by the driver. The same JSON
// fields are accepted by the matching adapter in
// internal/adapters/lambda — keep them in sync.
type Config struct {
	// FunctionName is the Lambda function identifier: name, ARN, or
	// partial ARN. Required.
	FunctionName string `json:"function_name"`

	// Qualifier optionally pins an alias or version (e.g. "PROD" or
	// "$LATEST"). Empty = the unqualified function (whatever Lambda
	// resolves at invoke time).
	Qualifier string `json:"qualifier"`

	// AWS connection knobs. Endpoint is the LocalStack /
	// API-Gateway-Compatible hook (e.g. http://localstack:4566).
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// ConfigFromJSON parses a `connection.config` JSON blob into a Config
// and validates the required fields.
func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("lambda: invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.FunctionName) == "" {
		return cfg, errors.New("lambda: config requires 'function_name'")
	}
	return cfg, nil
}

// InvokeResult is the structured outcome of a synchronous invoke.
type InvokeResult struct {
	// StatusCode is the Lambda InvokeOutput status code (200 on
	// success, 202 on async). Unwrapped here so callers don't have
	// to import the SDK.
	StatusCode int32 `json:"status_code"`
	// Payload is the raw response body the function returned. JSON
	// when the function emits JSON; opaque bytes otherwise.
	Payload []byte `json:"payload"`
	// FunctionError is non-empty when the function itself raised
	// ("Handled" or "Unhandled"). Distinct from a transport error.
	FunctionError string `json:"function_error,omitempty"`
	// ExecutedVersion echoes the function version Lambda actually
	// ran (useful when Qualifier="$LATEST" resolves to a numbered
	// version).
	ExecutedVersion string `json:"executed_version,omitempty"`
}

// FunctionSummary is a thin projection of lambdatypes.FunctionConfiguration
// returned by List. Callers don't need the full 30+ AWS fields, only
// the ones an operator sees in a picker UI.
type FunctionSummary struct {
	Name        string `json:"name"`
	ARN         string `json:"arn"`
	Runtime     string `json:"runtime"`
	Description string `json:"description,omitempty"`
	Handler     string `json:"handler,omitempty"`
}

// invokeClient is the minimum surface the driver needs from the
// aws-sdk-go-v2 client. Tests inject a fake without pulling the AWS
// SDK into the test process.
type invokeClient interface {
	GetFunction(ctx context.Context, in *awslambda.GetFunctionInput, opts ...func(*awslambda.Options)) (*awslambda.GetFunctionOutput, error)
	Invoke(ctx context.Context, in *awslambda.InvokeInput, opts ...func(*awslambda.Options)) (*awslambda.InvokeOutput, error)
	ListFunctions(ctx context.Context, in *awslambda.ListFunctionsInput, opts ...func(*awslambda.Options)) (*awslambda.ListFunctionsOutput, error)
}

// Driver wraps an aws-sdk-go-v2 Lambda client + a parsed Config.
// Construction is cheap; one Driver per Connection is fine.
type Driver struct {
	cfg    Config
	client invokeClient
}

// New builds a Driver from cfg. The network is NOT touched here —
// Connect / Invoke / List perform the actual API calls.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	client, err := awsclient.Lambda(ctx, awsclient.Config{
		EndpointURL:     cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		SessionToken:    cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("lambda: %w", err)
	}
	return &Driver{cfg: cfg, client: client}, nil
}

// NewWithClient is the test-friendly constructor. Production code
// uses [New]; unit tests inject a fakeInvokeClient.
func NewWithClient(cfg Config, client invokeClient) *Driver {
	return &Driver{cfg: cfg, client: client}
}

// FunctionName reports the function the driver is bound to. Useful
// for plumbing the connection-test result into the existing audit /
// logging surface.
func (d *Driver) FunctionName() string { return d.cfg.FunctionName }

// Connect performs a GetFunction against the configured function.
// Returns nil when the function exists and the caller is authorized
// to describe it.
func (d *Driver) Connect(ctx context.Context) error {
	if d == nil || d.client == nil {
		return errors.New("lambda: driver is not initialized")
	}
	in := &awslambda.GetFunctionInput{FunctionName: aws.String(d.cfg.FunctionName)}
	if d.cfg.Qualifier != "" {
		in.Qualifier = aws.String(d.cfg.Qualifier)
	}
	if _, err := d.client.GetFunction(ctx, in); err != nil {
		return fmt.Errorf("lambda: GetFunction %s: %w", d.cfg.FunctionName, err)
	}
	return nil
}

// Invoke calls the configured function synchronously with payload as
// the request body. payload is sent verbatim — callers wanting JSON
// should marshal first.
func (d *Driver) Invoke(ctx context.Context, payload []byte) (*InvokeResult, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("lambda: driver is not initialized")
	}
	in := &awslambda.InvokeInput{
		FunctionName:   aws.String(d.cfg.FunctionName),
		Payload:        payload,
		InvocationType: lambdatypes.InvocationTypeRequestResponse,
	}
	if d.cfg.Qualifier != "" {
		in.Qualifier = aws.String(d.cfg.Qualifier)
	}
	out, err := d.client.Invoke(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("lambda: Invoke %s: %w", d.cfg.FunctionName, err)
	}
	res := &InvokeResult{
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

// List returns up to `limit` candidate functions visible to the
// configured credentials. Used by the connector discovery path so
// operators can pick a function from a list rather than hand-typing
// the ARN. Pagination is intentionally not exposed here — the
// connector UI shows the first page only and the operator can refine
// by typing the function name directly.
func (d *Driver) List(ctx context.Context, limit int32) ([]FunctionSummary, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("lambda: driver is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	out, err := d.client.ListFunctions(ctx, &awslambda.ListFunctionsInput{MaxItems: aws.Int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("lambda: ListFunctions: %w", err)
	}
	res := make([]FunctionSummary, 0, len(out.Functions))
	for _, fn := range out.Functions {
		res = append(res, FunctionSummary{
			Name:        aws.ToString(fn.FunctionName),
			ARN:         aws.ToString(fn.FunctionArn),
			Runtime:     string(fn.Runtime),
			Description: aws.ToString(fn.Description),
			Handler:     aws.ToString(fn.Handler),
		})
	}
	return res, nil
}

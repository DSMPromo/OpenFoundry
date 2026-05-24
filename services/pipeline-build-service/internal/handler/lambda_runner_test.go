package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// fakeLambdaInvokeAPI captures the SDK InvokeInput and returns a
// caller-supplied response. Exercises the production
// awsLambdaRunner without an AWS account.
type fakeLambdaInvokeAPI struct {
	gotInput *awslambda.InvokeInput
	resp     *awslambda.InvokeOutput
	err      error
}

func (f *fakeLambdaInvokeAPI) Invoke(_ context.Context, in *awslambda.InvokeInput, _ ...func(*awslambda.Options)) (*awslambda.InvokeOutput, error) {
	f.gotInput = in
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &awslambda.InvokeOutput{StatusCode: 200, Payload: []byte(`{"rows":[]}`)}, nil
}

// fakeLambdaRunner is the runtimeNodeRunner-level fake. Lighter
// than the SDK-level fake — used to exercise the dispatch path.
type fakeLambdaRunner struct {
	gotReq  LambdaTransformRequest
	resp    *LambdaTransformResult
	respErr error
}

func (f *fakeLambdaRunner) InvokeForTransform(_ context.Context, req LambdaTransformRequest) (*LambdaTransformResult, error) {
	f.gotReq = req
	if f.respErr != nil {
		return nil, f.respErr
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &LambdaTransformResult{StatusCode: 200, Payload: []byte(`{"rows":[]}`)}, nil
}

func TestNewLambdaRunnerReturnsNilWhenUnconfigured(t *testing.T) {
	r, err := NewLambdaRunner(context.Background(), LambdaRunnerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("want nil runner when region+endpoint both empty")
	}
}

func TestAWSLambdaRunnerRoutesFunctionAndPayload(t *testing.T) {
	fake := &fakeLambdaInvokeAPI{
		resp: &awslambda.InvokeOutput{
			StatusCode:      200,
			Payload:         []byte(`{"rows":[{"k":"v"}]}`),
			ExecutedVersion: aws.String("3"),
		},
	}
	runner := &awsLambdaRunner{client: fake}
	got, err := runner.InvokeForTransform(context.Background(), LambdaTransformRequest{
		FunctionName: "openfoundry-echo",
		Qualifier:    "PROD",
		Payload:      []byte(`{"hi":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != 200 || got.ExecutedVersion != "3" {
		t.Errorf("result wrong: %+v", got)
	}
	if aws.ToString(fake.gotInput.FunctionName) != "openfoundry-echo" || aws.ToString(fake.gotInput.Qualifier) != "PROD" {
		t.Errorf("routing wrong: %+v", fake.gotInput)
	}
	if string(fake.gotInput.Payload) != `{"hi":1}` {
		t.Errorf("payload not verbatim: %s", string(fake.gotInput.Payload))
	}
}

func TestAWSLambdaRunnerErrorsOnEmptyFunctionName(t *testing.T) {
	runner := &awsLambdaRunner{client: &fakeLambdaInvokeAPI{}}
	_, err := runner.InvokeForTransform(context.Background(), LambdaTransformRequest{})
	if err == nil || !strings.Contains(err.Error(), "function_name") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLambdaResponseHandlesRowsAndJSONShape(t *testing.T) {
	parsed := parseLambdaResponse([]byte(`{"rows":[{"a":1}],"metadata":{"trace":"x"}}`))
	if len(parsed.Rows) != 1 {
		t.Fatalf("rows = %d", len(parsed.Rows))
	}
	if !strings.Contains(string(parsed.Metadata), "trace") {
		t.Errorf("metadata not preserved: %s", string(parsed.Metadata))
	}
}

func TestParseLambdaResponseDegradesNonJSONToMetadata(t *testing.T) {
	parsed := parseLambdaResponse([]byte(`hello world`))
	if len(parsed.Rows) != 0 {
		t.Errorf("expected zero rows, got %d", len(parsed.Rows))
	}
	if !strings.Contains(string(parsed.Metadata), `"raw_response":"hello world"`) {
		t.Errorf("raw response not wrapped: %s", string(parsed.Metadata))
	}
}

func TestParseLambdaResponseDegradesUnknownShape(t *testing.T) {
	parsed := parseLambdaResponse([]byte(`{"hello":"world"}`))
	if len(parsed.Rows) != 0 {
		t.Errorf("expected zero rows")
	}
	if !strings.Contains(string(parsed.Metadata), `"raw_response":"{\"hello\":\"world\"}"`) {
		t.Errorf("unknown shape not wrapped into raw_response: %s", string(parsed.Metadata))
	}
}

func TestUnquoteString(t *testing.T) {
	cases := map[string]string{
		`"x"`:   "x",
		`x`:     "x",
		`"a b"`: "a b",
		``:      "",
	}
	for input, want := range cases {
		got := unquoteString(json.RawMessage(input))
		if got != want {
			t.Errorf("unquoteString(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRunLambdaErrorsWithoutRunner(t *testing.T) {
	r := runtimeNodeRunner{}
	_, err := r.runLambda(context.Background(), executor.NodeContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "lambda_runner_not_configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunLambdaErrorsWhenFunctionNameMissing(t *testing.T) {
	r := runtimeNodeRunner{Lambda: &fakeLambdaRunner{}}
	_, err := r.runLambda(context.Background(), executor.NodeContext{
		Node: executor.Node{ID: "n1"},
	}, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "function_name") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunLambdaSurfacesFunctionError(t *testing.T) {
	r := runtimeNodeRunner{Lambda: &fakeLambdaRunner{
		resp: &LambdaTransformResult{
			StatusCode:    200,
			Payload:       []byte(`{"errorMessage":"boom"}`),
			FunctionError: "Handled",
		},
	}}
	_, err := r.runLambda(context.Background(), executor.NodeContext{
		Node: executor.Node{ID: "n1"},
	}, json.RawMessage(`{"function_name":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "Handled") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunLambdaHappyPathBuildsMetadata(t *testing.T) {
	fake := &fakeLambdaRunner{resp: &LambdaTransformResult{
		StatusCode:      200,
		ExecutedVersion: "$LATEST",
		Payload:         []byte(`{"rows":[{"id":1},{"id":2}]}`),
	}}
	r := runtimeNodeRunner{Lambda: fake, Table: newLightweightTableRuntime(nil)}
	res, err := r.runLambda(
		context.Background(),
		executor.NodeContext{Node: executor.Node{ID: "n1"}},
		json.RawMessage(`{"function_name":"openfoundry-echo","qualifier":"PROD","config":{"k":"v"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputContentHash == "" {
		t.Error("content hash missing")
	}
	meta := res.Metadata
	if meta["function_name"] != "openfoundry-echo" || meta["qualifier"] != "PROD" {
		t.Errorf("metadata routing wrong: %+v", meta)
	}
	if meta["transform_type"] != "lambda" {
		t.Errorf("transform_type = %v", meta["transform_type"])
	}
	// Payload routed verbatim to the runner.
	if fake.gotReq.FunctionName != "openfoundry-echo" || fake.gotReq.Qualifier != "PROD" {
		t.Errorf("function routing: %+v", fake.gotReq)
	}
	var sent lambdaTransformRequestEnvelope
	if err := json.Unmarshal(fake.gotReq.Payload, &sent); err != nil {
		t.Fatalf("sent payload not JSON: %v", err)
	}
	if sent.NodeID != "n1" || !strings.Contains(string(sent.Config), `"k":"v"`) {
		t.Errorf("payload envelope: %+v", sent)
	}
}

func TestValidateLambdaNodeRequiresFunctionName(t *testing.T) {
	report := &pipelineStrictValidationReport{}
	node := models.PipelineIRNode{ID: "n1", Config: json.RawMessage(`{}`)}
	validateLambdaNode(node, tableRuntimeConfig{}, pipelineStrictSchema{}, report)
	if !report.hasError("lambda_missing_function_name") {
		t.Fatalf("expected lambda_missing_function_name error, got %+v", report.Errors)
	}

	report = &pipelineStrictValidationReport{}
	node.Config = json.RawMessage(`{"function_name":"f"}`)
	validateLambdaNode(node, tableRuntimeConfig{}, pipelineStrictSchema{}, report)
	if len(report.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", report.Errors)
	}
}

// hasError is a thin helper for the test above; matches the report
// shape used elsewhere in the package.
func (r *pipelineStrictValidationReport) hasError(code string) bool {
	for _, e := range r.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

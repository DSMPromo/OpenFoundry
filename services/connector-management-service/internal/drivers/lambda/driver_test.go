package lambda

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// fakeInvokeClient captures the SDK arguments and returns caller-
// supplied canned responses. Lets the driver be tested without an
// AWS account or LocalStack.
type fakeInvokeClient struct {
	getFunctionInput   *awslambda.GetFunctionInput
	getFunctionErr     error
	getFunctionResp    *awslambda.GetFunctionOutput
	invokeInput        *awslambda.InvokeInput
	invokeErr          error
	invokeResp         *awslambda.InvokeOutput
	listFunctionsInput *awslambda.ListFunctionsInput
	listFunctionsResp  *awslambda.ListFunctionsOutput
}

func (f *fakeInvokeClient) GetFunction(_ context.Context, in *awslambda.GetFunctionInput, _ ...func(*awslambda.Options)) (*awslambda.GetFunctionOutput, error) {
	f.getFunctionInput = in
	if f.getFunctionErr != nil {
		return nil, f.getFunctionErr
	}
	if f.getFunctionResp != nil {
		return f.getFunctionResp, nil
	}
	return &awslambda.GetFunctionOutput{}, nil
}

func (f *fakeInvokeClient) Invoke(_ context.Context, in *awslambda.InvokeInput, _ ...func(*awslambda.Options)) (*awslambda.InvokeOutput, error) {
	f.invokeInput = in
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeResp != nil {
		return f.invokeResp, nil
	}
	return &awslambda.InvokeOutput{StatusCode: 200, Payload: []byte(`{}`)}, nil
}

func (f *fakeInvokeClient) ListFunctions(_ context.Context, in *awslambda.ListFunctionsInput, _ ...func(*awslambda.Options)) (*awslambda.ListFunctionsOutput, error) {
	f.listFunctionsInput = in
	if f.listFunctionsResp != nil {
		return f.listFunctionsResp, nil
	}
	return &awslambda.ListFunctionsOutput{}, nil
}

func TestConfigFromJSONRequiresFunctionName(t *testing.T) {
	if _, err := ConfigFromJSON(nil); err == nil {
		t.Fatal("want error on empty config")
	}
	if _, err := ConfigFromJSON(json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error when function_name missing")
	}
	cfg, err := ConfigFromJSON(json.RawMessage(`{"function_name":"my-fn"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FunctionName != "my-fn" {
		t.Fatalf("function_name = %q", cfg.FunctionName)
	}
}

func TestConnectGetsFunctionAndHonorsQualifier(t *testing.T) {
	client := &fakeInvokeClient{}
	d := NewWithClient(Config{FunctionName: "my-fn", Qualifier: "PROD"}, client)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := aws.ToString(client.getFunctionInput.FunctionName); got != "my-fn" {
		t.Errorf("function = %q, want my-fn", got)
	}
	if got := aws.ToString(client.getFunctionInput.Qualifier); got != "PROD" {
		t.Errorf("qualifier = %q, want PROD", got)
	}
}

func TestConnectWrapsSDKErrors(t *testing.T) {
	client := &fakeInvokeClient{getFunctionErr: errors.New("ResourceNotFoundException")}
	d := NewWithClient(Config{FunctionName: "missing"}, client)
	err := d.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ResourceNotFoundException") {
		t.Fatalf("err = %v, want wrapped SDK error", err)
	}
}

func TestInvokeRoutesPayloadAndReturnsResult(t *testing.T) {
	client := &fakeInvokeClient{
		invokeResp: &awslambda.InvokeOutput{
			StatusCode:      200,
			Payload:         []byte(`{"echoed":true}`),
			ExecutedVersion: aws.String("1"),
		},
	}
	d := NewWithClient(Config{FunctionName: "echo"}, client)
	got, err := d.Invoke(context.Background(), []byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got.StatusCode != 200 {
		t.Errorf("status = %d", got.StatusCode)
	}
	if string(got.Payload) != `{"echoed":true}` {
		t.Errorf("payload = %q", string(got.Payload))
	}
	if got.ExecutedVersion != "1" {
		t.Errorf("executed version = %q", got.ExecutedVersion)
	}
	if client.invokeInput.InvocationType != lambdatypes.InvocationTypeRequestResponse {
		t.Errorf("invocation type = %v, want RequestResponse", client.invokeInput.InvocationType)
	}
	if string(client.invokeInput.Payload) != `{"x":1}` {
		t.Errorf("payload not routed verbatim: %s", string(client.invokeInput.Payload))
	}
}

func TestInvokeSurfacesFunctionError(t *testing.T) {
	client := &fakeInvokeClient{
		invokeResp: &awslambda.InvokeOutput{
			StatusCode:    200,
			Payload:       []byte(`{"errorMessage":"boom"}`),
			FunctionError: aws.String("Handled"),
		},
	}
	d := NewWithClient(Config{FunctionName: "broken"}, client)
	got, err := d.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got.FunctionError != "Handled" {
		t.Errorf("function error = %q", got.FunctionError)
	}
}

func TestListReturnsProjectedSummaries(t *testing.T) {
	client := &fakeInvokeClient{
		listFunctionsResp: &awslambda.ListFunctionsOutput{
			Functions: []lambdatypes.FunctionConfiguration{
				{
					FunctionName: aws.String("alpha"),
					FunctionArn:  aws.String("arn:aws:lambda:us-east-1:0:function:alpha"),
					Runtime:      lambdatypes.RuntimeNodejs20x,
					Handler:      aws.String("index.handler"),
				},
				{
					FunctionName: aws.String("beta"),
					Runtime:      lambdatypes.RuntimePython312,
				},
			},
		},
	}
	d := NewWithClient(Config{FunctionName: "any"}, client)
	got, err := d.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("projection wrong: %+v", got)
	}
	if got[0].Runtime != string(lambdatypes.RuntimeNodejs20x) {
		t.Errorf("runtime not stringified: %q", got[0].Runtime)
	}
	if client.listFunctionsInput == nil || aws.ToInt32(client.listFunctionsInput.MaxItems) != 10 {
		t.Errorf("MaxItems not routed: %+v", client.listFunctionsInput)
	}
}

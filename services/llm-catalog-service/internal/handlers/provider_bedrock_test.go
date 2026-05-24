package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/models"
)

// fakeBedrockClient captures the InvokeModel arguments and returns a
// caller-supplied response body. Lets the invoker be unit-tested
// without an AWS account or LocalStack Pro.
type fakeBedrockClient struct {
	gotInput *bedrockruntime.InvokeModelInput
	respBody []byte
	respErr  error
}

func (f *fakeBedrockClient) InvokeModel(
	_ context.Context,
	input *bedrockruntime.InvokeModelInput,
	_ ...func(*bedrockruntime.Options),
) (*bedrockruntime.InvokeModelOutput, error) {
	f.gotInput = input
	if f.respErr != nil {
		return nil, f.respErr
	}
	return &bedrockruntime.InvokeModelOutput{Body: f.respBody}, nil
}

func TestBedrockFamilyDispatch(t *testing.T) {
	cases := []struct {
		modelID string
		family  string
		err     bool
	}{
		{"anthropic.claude-3-5-sonnet-20240620-v1:0", "anthropic", false},
		{"amazon.titan-text-lite-v1", "amazon", false},
		{"meta.llama3-70b-instruct-v1:0", "meta", false},
		{"mistral.mistral-7b-instruct-v0:2", "", true}, // not wired yet
		{"cohere.command-r-v1:0", "", true},            // not wired yet
		{"unknown-model", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.modelID, func(t *testing.T) {
			f, err := bedrockFamilyFor(tc.modelID)
			if tc.err {
				if err == nil {
					t.Fatalf("want error for %q, got family=%q", tc.modelID, f.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.name != tc.family {
				t.Fatalf("family = %q, want %q", f.name, tc.family)
			}
		})
	}
}

func TestEncodeBedrockAnthropicPinsVersionAndSplitsSystem(t *testing.T) {
	body, err := encodeBedrockAnthropic(models.Model{ModelID: "anthropic.claude-3-5-sonnet-20240620-v1:0"}, models.InvokeRequest{
		Messages: []models.Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
		},
		MaxTokens:   256,
		Temperature: 0.4,
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	mustUnmarshal(t, body, &parsed)

	if parsed["anthropic_version"] != "bedrock-2023-05-31" {
		t.Errorf("anthropic_version = %v, want bedrock-2023-05-31", parsed["anthropic_version"])
	}
	if parsed["max_tokens"].(float64) != 256 {
		t.Errorf("max_tokens = %v, want 256", parsed["max_tokens"])
	}
	if parsed["system"] != "be terse" {
		t.Errorf("system = %v, want 'be terse'", parsed["system"])
	}
	if _, hasModel := parsed["model"]; hasModel {
		t.Error("body should not include model — ModelId goes in InvokeModelInput")
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages shape = %T %v", parsed["messages"], parsed["messages"])
	}
}

func TestEncodeBedrockAnthropicDefaultsMaxTokens(t *testing.T) {
	body, err := encodeBedrockAnthropic(models.Model{ModelID: "anthropic.claude-instant-v1"}, models.InvokeRequest{
		Messages: []models.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	mustUnmarshal(t, body, &parsed)
	if parsed["max_tokens"].(float64) != 1024 {
		t.Errorf("default max_tokens = %v, want 1024", parsed["max_tokens"])
	}
}

func TestDecodeBedrockAnthropic(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"hello"},{"type":"text","text":" world"}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	got, err := decodeBedrockAnthropic(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello world" {
		t.Errorf("content = %q, want 'hello world'", got.Content)
	}
	if got.PromptTokens != 5 || got.CompletionTokens != 2 {
		t.Errorf("tokens = %d/%d, want 5/2", got.PromptTokens, got.CompletionTokens)
	}
}

func TestEncodeBedrockTitanFlattensSystem(t *testing.T) {
	body, err := encodeBedrockTitan(models.Model{ModelID: "amazon.titan-text-lite-v1"}, models.InvokeRequest{
		Messages: []models.Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	mustUnmarshal(t, body, &parsed)
	prompt, _ := parsed["inputText"].(string)
	if !strings.Contains(prompt, "be terse") || !strings.Contains(prompt, "User: hi") {
		t.Errorf("prompt missing flattened messages: %q", prompt)
	}
	gen, _ := parsed["textGenerationConfig"].(map[string]any)
	if gen["maxTokenCount"].(float64) != 64 {
		t.Errorf("maxTokenCount = %v, want 64", gen["maxTokenCount"])
	}
}

func TestDecodeBedrockTitan(t *testing.T) {
	raw := []byte(`{"inputTextTokenCount":7,"results":[{"outputText":"hello","tokenCount":2},{"outputText":" world","tokenCount":1}]}`)
	got, err := decodeBedrockTitan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello world" {
		t.Errorf("content = %q", got.Content)
	}
	if got.PromptTokens != 7 || got.CompletionTokens != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", got.PromptTokens, got.CompletionTokens)
	}
}

func TestEncodeBedrockLlamaFlattensAndDefaults(t *testing.T) {
	body, err := encodeBedrockLlama(models.Model{ModelID: "meta.llama3-70b-instruct-v1:0"}, models.InvokeRequest{
		Messages: []models.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "next"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	mustUnmarshal(t, body, &parsed)
	prompt := parsed["prompt"].(string)
	if !strings.Contains(prompt, "User: first") || !strings.Contains(prompt, "Assistant: ok") || !strings.HasSuffix(prompt, "Assistant: ") {
		t.Errorf("prompt missing pieces: %q", prompt)
	}
	if parsed["max_gen_len"].(float64) != 512 {
		t.Errorf("default max_gen_len = %v, want 512", parsed["max_gen_len"])
	}
}

func TestDecodeBedrockLlama(t *testing.T) {
	raw := []byte(`{"generation":"hello","prompt_token_count":4,"generation_token_count":1}`)
	got, err := decodeBedrockLlama(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello" || got.PromptTokens != 4 || got.CompletionTokens != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestBedrockInvokerEndToEndAnthropic(t *testing.T) {
	respBody := []byte(`{"content":[{"type":"text","text":"reply"}],"usage":{"input_tokens":10,"output_tokens":3}}`)
	fake := &fakeBedrockClient{respBody: respBody}
	invoker := &bedrockInvoker{client: fake, region: "us-east-1"}

	got, err := invoker.Invoke(context.Background(),
		models.Model{ModelID: "anthropic.claude-3-5-sonnet-20240620-v1:0"},
		models.InvokeRequest{
			Messages:  []models.Message{{Role: "user", Content: "ping"}},
			MaxTokens: 128,
		})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got.Content != "reply" {
		t.Errorf("content = %q", got.Content)
	}
	if got.PromptTokens != 10 || got.CompletionTokens != 3 {
		t.Errorf("tokens = %d/%d", got.PromptTokens, got.CompletionTokens)
	}
	if fake.gotInput == nil || *fake.gotInput.ModelId != "anthropic.claude-3-5-sonnet-20240620-v1:0" {
		t.Errorf("ModelId routed wrong: %+v", fake.gotInput)
	}
	// Verify the body actually contains the bedrock_version pin.
	var sent map[string]any
	mustUnmarshal(t, fake.gotInput.Body, &sent)
	if sent["anthropic_version"] != "bedrock-2023-05-31" {
		t.Errorf("anthropic_version not pinned in sent body: %v", sent)
	}
}

func TestBedrockInvokerSurfacesClientErrors(t *testing.T) {
	fake := &fakeBedrockClient{respErr: errors.New("AccessDeniedException")}
	invoker := &bedrockInvoker{client: fake}
	_, err := invoker.Invoke(context.Background(),
		models.Model{ModelID: "anthropic.claude-instant-v1"},
		models.InvokeRequest{Messages: []models.Message{{Role: "user", Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") {
		t.Fatalf("err = %v, want wrapped AccessDeniedException", err)
	}
}

func TestLookupReturnsErrUnimplementedWhenBedrockClientNil(t *testing.T) {
	r := &ProviderRegistry{}
	_, err := r.Lookup(models.ProviderBedrock)
	if !errors.Is(err, ErrProviderUnimplemented) {
		t.Fatalf("err = %v, want ErrProviderUnimplemented", err)
	}
}

func TestLookupReturnsBedrockInvokerWhenWired(t *testing.T) {
	r := &ProviderRegistry{BedrockClient: &fakeBedrockClient{}, BedrockRegion: "us-east-1"}
	got, err := r.Lookup(models.ProviderBedrock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(*bedrockInvoker); !ok {
		t.Fatalf("type = %T, want *bedrockInvoker", got)
	}
}

func mustUnmarshal(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, string(raw))
	}
}

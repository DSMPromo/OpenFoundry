package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/models"
)

// bedrockClient is the minimum surface bedrockInvoker needs from the
// aws-sdk-go-v2 client. The real client satisfies it; tests inject a
// fake so the invoker can be exercised without an AWS account.
type bedrockClient interface {
	InvokeModel(
		ctx context.Context,
		input *bedrockruntime.InvokeModelInput,
		opts ...func(*bedrockruntime.Options),
	) (*bedrockruntime.InvokeModelOutput, error)
}

// bedrockInvoker is the providerInvoker that dispatches against Amazon
// Bedrock's foundation-model API. Bedrock fans out to many model
// families and each family has its own request/response wire shape;
// the invoker picks the right pair by the model.ModelID prefix.
//
// Supported families (MVP):
//   - anthropic.*       (Claude — the only family that matches the
//     existing catalog's Anthropic invoker shape,
//     so users get a consistent experience)
//   - amazon.titan-*    (Amazon's native models)
//   - meta.llama*       (open-source models hosted by AWS)
//
// Other families (mistral.*, ai21.*, cohere.*) return a structured
// "unsupported_bedrock_family" error pointing at the follow-up issue;
// adding them is mechanical and tracked in the AWS-integration plan.
type bedrockInvoker struct {
	client bedrockClient
	region string
}

// Invoke routes the request to the family-specific encoder, calls
// InvokeModel, then decodes the matching family-specific response.
func (b *bedrockInvoker) Invoke(ctx context.Context, model models.Model, req models.InvokeRequest) (providerResult, error) {
	family, err := bedrockFamilyFor(model.ModelID)
	if err != nil {
		return providerResult{}, err
	}
	body, err := family.encode(model, req)
	if err != nil {
		return providerResult{}, fmt.Errorf("bedrock encode: %w", err)
	}
	out, err := b.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model.ModelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return providerResult{}, fmt.Errorf("bedrock invoke: %w", err)
	}
	return family.decode(out.Body)
}

// bedrockFamily bundles a per-model-family encoder + decoder so the
// dispatch table stays one line per family.
type bedrockFamily struct {
	name   string
	encode func(model models.Model, req models.InvokeRequest) ([]byte, error)
	decode func(raw []byte) (providerResult, error)
}

// bedrockFamilyFor inspects the model_id prefix and returns the
// matching encoder/decoder pair. Unknown families are an explicit
// error rather than a silent fallback — operators should see "this
// Bedrock family is not wired yet" instead of garbage output.
func bedrockFamilyFor(modelID string) (bedrockFamily, error) {
	prefix := strings.SplitN(modelID, ".", 2)[0]
	switch prefix {
	case "anthropic":
		return bedrockFamily{name: "anthropic", encode: encodeBedrockAnthropic, decode: decodeBedrockAnthropic}, nil
	case "amazon":
		return bedrockFamily{name: "amazon", encode: encodeBedrockTitan, decode: decodeBedrockTitan}, nil
	case "meta":
		return bedrockFamily{name: "meta", encode: encodeBedrockLlama, decode: decodeBedrockLlama}, nil
	default:
		return bedrockFamily{}, fmt.Errorf("bedrock: model family %q is not supported yet (model_id=%q)", prefix, modelID)
	}
}

// ---------------------------------------------------------------------------
// anthropic.* (Claude) — the wire shape is the standard Anthropic
// Messages API plus an "anthropic_version" pin Bedrock requires.
// ---------------------------------------------------------------------------

func encodeBedrockAnthropic(model models.Model, req models.InvokeRequest) ([]byte, error) {
	system, userMessages := splitMessagesAnthropic(req.Messages)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := map[string]any{
		// Required by Bedrock for Claude models. Distinct from the
		// "2023-06-01" header on the direct Anthropic API — Bedrock
		// pins to its own version string.
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        maxTokens,
		"messages":          userMessages,
	}
	if system != "" {
		body["system"] = system
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	_ = model // model.ModelID goes in InvokeModelInput.ModelId, not the body
	return json.Marshal(body)
}

func decodeBedrockAnthropic(raw []byte) (providerResult, error) {
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int32 `json:"input_tokens"`
			OutputTokens int32 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return providerResult{}, fmt.Errorf("bedrock anthropic decode: %w", err)
	}
	var sb strings.Builder
	for _, part := range parsed.Content {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	return providerResult{
		Content:          sb.String(),
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// amazon.titan-* — single inputText prompt + textGenerationConfig
// envelope. System messages are concatenated to the prompt because
// Titan has no native system role.
// ---------------------------------------------------------------------------

func encodeBedrockTitan(model models.Model, req models.InvokeRequest) ([]byte, error) {
	prompt := bedrockFlattenMessages(req.Messages)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	gen := map[string]any{
		"maxTokenCount": maxTokens,
	}
	if req.Temperature > 0 {
		gen["temperature"] = req.Temperature
	}
	body := map[string]any{
		"inputText":            prompt,
		"textGenerationConfig": gen,
	}
	_ = model
	return json.Marshal(body)
}

func decodeBedrockTitan(raw []byte) (providerResult, error) {
	var parsed struct {
		InputTextTokenCount int32 `json:"inputTextTokenCount"`
		Results             []struct {
			OutputText string `json:"outputText"`
			TokenCount int32  `json:"tokenCount"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return providerResult{}, fmt.Errorf("bedrock titan decode: %w", err)
	}
	var sb strings.Builder
	var outTokens int32
	for _, r := range parsed.Results {
		sb.WriteString(r.OutputText)
		outTokens += r.TokenCount
	}
	return providerResult{
		Content:          sb.String(),
		PromptTokens:     parsed.InputTextTokenCount,
		CompletionTokens: outTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// meta.llama* — single prompt string + max_gen_len. System messages
// flatten the same way as Titan.
// ---------------------------------------------------------------------------

func encodeBedrockLlama(model models.Model, req models.InvokeRequest) ([]byte, error) {
	prompt := bedrockFlattenMessages(req.Messages)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}
	body := map[string]any{
		"prompt":      prompt,
		"max_gen_len": maxTokens,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	_ = model
	return json.Marshal(body)
}

func decodeBedrockLlama(raw []byte) (providerResult, error) {
	var parsed struct {
		Generation           string `json:"generation"`
		PromptTokenCount     int32  `json:"prompt_token_count"`
		GenerationTokenCount int32  `json:"generation_token_count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return providerResult{}, fmt.Errorf("bedrock llama decode: %w", err)
	}
	return providerResult{
		Content:          parsed.Generation,
		PromptTokens:     parsed.PromptTokenCount,
		CompletionTokens: parsed.GenerationTokenCount,
	}, nil
}

// bedrockFlattenMessages folds a multi-turn message list into a single
// prompt string for families that lack a structured Messages API
// (Titan, Llama, etc.). Format mirrors the de-facto Llama chat shape:
// system prompt on top, then "Role: content" lines.
func bedrockFlattenMessages(messages []models.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		switch strings.ToLower(m.Role) {
		case "system":
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		default: // user, or unspecified
			sb.WriteString("User: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("Assistant: ")
	return sb.String()
}

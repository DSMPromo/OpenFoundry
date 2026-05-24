package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/models"
)

// ErrProviderUnimplemented is returned when invoke is called against a
// provider whose runtime is not wired (Bedrock in v1).
var ErrProviderUnimplemented = errors.New("provider not implemented")

// providerResult is the normalized output every provider returns. The
// handler folds this into the OpenAI-compatible InvokeResponse.
type providerResult struct {
	Content          string
	PromptTokens     int32
	CompletionTokens int32
}

// providerInvoker is the single seam between the invoke handler and a
// concrete LLM client. Streaming is a follow-up slice; v1 returns the
// full reply in one shot.
type providerInvoker interface {
	Invoke(ctx context.Context, model models.Model, req models.InvokeRequest) (providerResult, error)
}

// ProviderRegistry wires the providerInvoker per provider token. main.go
// builds the production registry from env (ANTHROPIC_API_KEY,
// OPENAI_API_KEY, ...); tests construct it pointed at httptest servers.
type ProviderRegistry struct {
	HTTPClient *http.Client

	AnthropicAPIKey  string
	AnthropicBaseURL string

	OpenAIAPIKey  string
	OpenAIBaseURL string

	OllamaBaseURL string

	// BedrockClient is the optional Amazon Bedrock runtime client.
	// Built in cmd/llm-catalog-service/main.go via libs/aws-client
	// when BEDROCK_REGION (or OF_AWS__ENDPOINT_URL for LocalStack)
	// is configured. Leave nil to disable Bedrock — Lookup returns
	// ErrProviderUnimplemented in that case, matching the pre-3a
	// behavior so the catalog still accepts BEDROCK rows for
	// planning without a runtime.
	BedrockClient bedrockClient
	BedrockRegion string
}

// Lookup returns the invoker for the model's declared provider. Returns
// ErrProviderUnimplemented for Bedrock and any unknown provider token —
// the catalog still allows registration so the org can plan ahead.
func (r *ProviderRegistry) Lookup(p models.Provider) (providerInvoker, error) {
	if r == nil {
		return nil, ErrProviderUnimplemented
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	switch p {
	case models.ProviderAnthropic:
		return &anthropicInvoker{
			client:  client,
			apiKey:  r.AnthropicAPIKey,
			baseURL: firstNonEmpty(r.AnthropicBaseURL, "https://api.anthropic.com/v1"),
		}, nil
	case models.ProviderOpenAI:
		return &openAICompatibleInvoker{
			client:  client,
			apiKey:  r.OpenAIAPIKey,
			baseURL: firstNonEmpty(r.OpenAIBaseURL, "https://api.openai.com/v1"),
		}, nil
	case models.ProviderOllama:
		return &openAICompatibleInvoker{
			client:  client,
			baseURL: firstNonEmpty(r.OllamaBaseURL, "http://localhost:11434/v1"),
		}, nil
	case models.ProviderBedrock:
		if r.BedrockClient == nil {
			return nil, ErrProviderUnimplemented
		}
		return &bedrockInvoker{client: r.BedrockClient, region: r.BedrockRegion}, nil
	default:
		return nil, ErrProviderUnimplemented
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Anthropic Messages API
// ---------------------------------------------------------------------------

type anthropicInvoker struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

func (a *anthropicInvoker) Invoke(ctx context.Context, model models.Model, req models.InvokeRequest) (providerResult, error) {
	if strings.TrimSpace(a.apiKey) == "" {
		return providerResult{}, fmt.Errorf("anthropic: ANTHROPIC_API_KEY not configured")
	}

	system, userMessages := splitMessagesAnthropic(req.Messages)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := map[string]any{
		"model":      model.ModelID,
		"max_tokens": maxTokens,
		"messages":   userMessages,
	}
	if system != "" {
		body["system"] = system
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return providerResult{}, fmt.Errorf("anthropic encode: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		strings.TrimRight(a.baseURL, "/")+"/messages",
		bytes.NewReader(encoded),
	)
	if err != nil {
		return providerResult{}, fmt.Errorf("anthropic build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return providerResult{}, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return providerResult{}, fmt.Errorf("anthropic read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerResult{}, fmt.Errorf("anthropic returned %d: %s", resp.StatusCode, string(payload))
	}

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
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return providerResult{}, fmt.Errorf("anthropic parse: %w", err)
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

func splitMessagesAnthropic(messages []models.Message) (string, []map[string]any) {
	var systemParts []string
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		if strings.EqualFold(m.Role, "system") {
			systemParts = append(systemParts, m.Content)
			continue
		}
		out = append(out, map[string]any{
			"role":    strings.ToLower(m.Role),
			"content": m.Content,
		})
	}
	return strings.Join(systemParts, "\n\n"), out
}

// ---------------------------------------------------------------------------
// OpenAI-compatible (also serves Ollama, vLLM, OpenAI itself)
// ---------------------------------------------------------------------------

type openAICompatibleInvoker struct {
	client  *http.Client
	apiKey  string // empty for Ollama / vLLM
	baseURL string
}

func (o *openAICompatibleInvoker) Invoke(ctx context.Context, model models.Model, req models.InvokeRequest) (providerResult, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]any{
			"role":    strings.ToLower(m.Role),
			"content": m.Content,
		})
	}
	body := map[string]any{
		"model":    model.ModelID,
		"messages": msgs,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return providerResult{}, fmt.Errorf("openai encode: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		strings.TrimRight(o.baseURL, "/")+"/chat/completions",
		bytes.NewReader(encoded),
	)
	if err != nil {
		return providerResult{}, fmt.Errorf("openai build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(o.apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return providerResult{}, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return providerResult{}, fmt.Errorf("openai read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerResult{}, fmt.Errorf("openai returned %d: %s", resp.StatusCode, string(payload))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int32 `json:"prompt_tokens"`
			CompletionTokens int32 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return providerResult{}, fmt.Errorf("openai parse: %w", err)
	}
	content := ""
	if len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
	}
	return providerResult{
		Content:          content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

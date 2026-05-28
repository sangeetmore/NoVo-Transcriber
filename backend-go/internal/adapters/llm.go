package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/rs/zerolog/log"
	openai "github.com/sashabaranov/go-openai"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

const (
	llmTemperature float32 = 0.1
	llmMaxTokens   int     = 1200
)

// Client is the unified LLM adapter.  It wraps sashabaranov/go-openai and routes
// requests to whichever backend (Groq, Ollama, OpenAI, or any OpenAI-compatible
// endpoint) is described by cfg.  Groq and Ollama both expose an OpenAI-compatible
// chat completions API; switching between them only requires a different BaseURL
// and APIKey in config.
type Client struct {
	inner *openai.Client
	cfg   *config.Config
}

// NewClient constructs a Client from cfg.  The underlying openai.Client is
// initialised with cfg.LLMBaseURL as BaseURL and cfg.LLMAPIKey as the API key.
// When the provider is Ollama and no API key is configured, the placeholder
// string "ollama" is used because the HTTP client requires a non-empty value
// even though Ollama does not validate it.
func NewClient(cfg *config.Config) *Client {
	apiKey := cfg.LLMAPIKey
	if cfg.LLMProvider == config.ProviderOllama {
		if apiKey == "" {
			apiKey = "ollama"
		}
	}

	ocfg := openai.DefaultConfig(apiKey)
	ocfg.BaseURL = cfg.LLMBaseURL

	log.Info().
		Str("provider", string(cfg.LLMProvider)).
		Str("base_url", cfg.LLMBaseURL).
		Msg("llm adapter initialised")

	return &Client{
		inner: openai.NewClientWithConfig(ocfg),
		cfg:   cfg,
	}
}

// ─── CallLLMJSON ─────────────────────────────────────────────────────────────

// CallLLMJSON sends a chat completion request with the json_object response
// format and returns the raw content string.  Callers are responsible for
// unmarshalling the result into a concrete type.
func (c *Client) CallLLMJSON(
	ctx context.Context,
	systemPrompt, userPrompt, model string,
) (string, error) {
	temp := llmTemperature
	resp, err := c.inner.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Temperature: temp,
		MaxTokens:   llmMaxTokens,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: buildMessages(systemPrompt, userPrompt),
	})
	if err != nil {
		return "", fmt.Errorf("llm json call (model=%s): %w", model, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm json call returned no choices (model=%s)", model)
	}
	return resp.Choices[0].Message.Content, nil
}

// ─── CallStructuredOutput ────────────────────────────────────────────────────

// CallStructuredOutput generates a JSON schema from target via
// invopop/jsonschema reflection and sends it as the response_format so the
// model returns a conforming object.  The response is unmarshalled directly
// into target (which must be a pointer).
//
// Provider-specific behaviour:
//   - Groq / OpenAI / Custom: uses the standard json_schema response format.
//   - Ollama: Ollama's OpenAI-compat layer also accepts json_schema in the
//     response_format field; strict is set to false for broadest model support.
//     The schema is additionally embedded in the system prompt as a safety net
//     for older Ollama builds that ignore the response_format extension.
func (c *Client) CallStructuredOutput(
	ctx context.Context,
	systemPrompt, userPrompt, model string,
	target any,
) error {
	r := &jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(target)

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal json schema: %w", err)
	}

	name := schemaName(target)
	temp := llmTemperature

	var req openai.ChatCompletionRequest

	if c.cfg.LLMProvider == config.ProviderOllama {
		// For Ollama, augment the system prompt with the schema as a fallback and
		// pass the schema via response_format for Ollama builds that support it.
		augmented := systemPrompt + "\n\nRespond ONLY with valid JSON matching this schema:\n" + string(schemaBytes)
		req = openai.ChatCompletionRequest{
			Model:       model,
			Temperature: temp,
			MaxTokens:   llmMaxTokens,
			Messages:    buildMessages(augmented, userPrompt),
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   name,
					Schema: schema, // *jsonschema.Schema implements json.Marshaler
					Strict: false,
				},
			},
		}
	} else {
		// Groq / OpenAI / Custom: standard json_schema structured output.
		req = openai.ChatCompletionRequest{
			Model:       model,
			Temperature: temp,
			MaxTokens:   llmMaxTokens,
			Messages:    buildMessages(systemPrompt, userPrompt),
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   name,
					Schema: schema, // *jsonschema.Schema implements json.Marshaler
					Strict: false,  // strict=true not universally supported across Groq models
				},
			},
		}
	}

	log.Debug().
		Str("model", model).
		Str("schema_name", name).
		RawJSON("schema", schemaBytes).
		Msg("calling structured output")

	resp, err := c.inner.CreateChatCompletion(ctx, req)
	if err != nil {
		return fmt.Errorf("structured output call (model=%s): %w", model, err)
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("structured output call returned no choices (model=%s)", model)
	}

	content := resp.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return fmt.Errorf("structured output unmarshal (model=%s): %w — raw: %s",
			model, err, truncate(content, 200))
	}
	return nil
}

// ─── CallWithTools ───────────────────────────────────────────────────────────

// CallWithTools sends a function-calling request and returns every ToolCall in
// the first choice.  Callers are responsible for executing the returned calls
// and continuing the conversation if needed.
func (c *Client) CallWithTools(
	ctx context.Context,
	systemPrompt, userPrompt, model string,
	tools []openai.Tool,
) ([]openai.ToolCall, error) {
	resp, err := c.inner.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:      model,
		Messages:   buildMessages(systemPrompt, userPrompt),
		Tools:      tools,
		ToolChoice: "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("tool call (model=%s): %w", model, err)
	}
	if len(resp.Choices) == 0 {
		return nil, nil
	}

	calls := resp.Choices[0].Message.ToolCalls
	log.Debug().
		Str("model", model).
		Int("tool_calls", len(calls)).
		Msg("tool call completed")

	return calls, nil
}

// ─── CallVision ──────────────────────────────────────────────────────────────

// CallVision sends a multimodal message containing a base64-encoded image and
// a text prompt.  The image is transmitted as a data URI so it works with
// Groq's LLaMA 4 vision models and Ollama's qwen2.5-vl / gemma3 family.
// Returns the model's textual description string.
func (c *Client) CallVision(
	ctx context.Context,
	systemPrompt string,
	imageBase64 string,
	imageMimeType string,
	textPrompt string,
	model string,
) (string, error) {
	if imageMimeType == "" {
		imageMimeType = "image/jpeg"
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", imageMimeType, imageBase64)

	temp := llmTemperature
	resp, err := c.inner.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Temperature: temp,
		MaxTokens:   llmMaxTokens,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role: openai.ChatMessageRoleUser,
				MultiContent: []openai.ChatMessagePart{
					{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL:    dataURI,
							Detail: openai.ImageURLDetailAuto,
						},
					},
					{
						Type: openai.ChatMessagePartTypeText,
						Text: textPrompt,
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("vision call (model=%s): %w", model, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("vision call returned no choices (model=%s)", model)
	}
	return resp.Choices[0].Message.Content, nil
}

// ─── CuratorModels ───────────────────────────────────────────────────────────

// CuratorModels returns [cfg.CuratorModel, cfg.CuratorFallback] deduplicated.
// When both values are identical only the primary model is included.
func (c *Client) CuratorModels() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range []string{c.cfg.CuratorModel, c.cfg.CuratorFallback} {
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildMessages assembles the two-message slice (system + user) used by every
// chat completion call.
func buildMessages(systemPrompt, userPrompt string) []openai.ChatCompletionMessage {
	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}
}

// schemaName derives a stable, API-safe name from the concrete type of v by
// converting its PascalCase type name to snake_case.  Anonymous structs and
// unnamed types fall back to "schema".
func schemaName(v any) string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil || t.Name() == "" {
		return "schema"
	}
	return toSnakeCase(t.Name())
}

// toSnakeCase converts a PascalCase or camelCase identifier to snake_case.
func toSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32) // to lower
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncate returns at most n bytes of s, appending "..." when truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

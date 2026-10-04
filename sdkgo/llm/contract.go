// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

// MessageRole identifies who wrote one conversation message.
type MessageRole string

const (
	// MessageRoleUser marks a message written by the application's user.
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant marks an earlier model reply that the application replays.
	MessageRoleAssistant MessageRole = "assistant"
)

// Message is one text-only conversation turn.
type Message struct {
	// Role is MessageRoleUser or MessageRoleAssistant. Every other value selects defect.
	Role MessageRole `json:"role"`
	// Text is the message text. Empty text selects defect.
	Text string `json:"text"`
}

// StructuredOutput asks the model for one JSON object that matches Schema.
//
// Schema must stay inside the portable subset documented on
// TextGenerationQuery: the root is an object, every object sets
// additionalProperties to false and lists all of its properties in required,
// and only the listed keywords appear. A schema outside the subset selects
// defect before any request. The pipeline validates the returned text against
// this original schema even when a provider cannot enforce part of it.
type StructuredOutput struct {
	// Name identifies the schema to the provider and must match ^[A-Za-z0-9_-]{1,64}$.
	Name string `json:"name"`
	// Description optionally explains the expected object to the model.
	Description string `json:"description,omitempty"`
	// Schema is the JSON Schema of the expected object. It must be JSON serializable.
	Schema map[string]any `json:"schema"`
}

// ReasoningEffort asks a reasoning model to spend less or more effort. Each
// wire format maps the values its models accept to provider wire values; any
// other value selects defect.
type ReasoningEffort string

const (
	// ReasoningEffortNone asks the model not to reason where it allows that.
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortMinimal asks for the least reasoning above none.
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	// ReasoningEffortLow asks for brief reasoning.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium asks for moderate reasoning.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh asks for thorough reasoning.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortExtraHigh asks for more reasoning than high, where a provider offers it.
	ReasoningEffortExtraHigh ReasoningEffort = "xhigh"
	// ReasoningEffortMax asks for the provider's maximum reasoning.
	ReasoningEffortMax ReasoningEffort = "max"
)

// TextGenerationRequest is the provider-neutral input of every lab
// connector's generateText Query.
//
// Every optional field is sent only when the connector's wire format declares
// it in RequestFeatures and the selected model accepts it. A non-zero field
// that the wire format or model does not support selects defect before any
// provider request, so a field is never ignored silently.
type TextGenerationRequest struct {
	// Model is the provider model ID for this request. Surrounding whitespace is
	// trimmed; empty uses the connection's configured model.
	Model string `json:"model,omitempty"`
	// Instructions is optional system or developer text that frames every message.
	Instructions string `json:"instructions,omitempty"`
	// Messages holds the conversation in order. At least one message is required.
	Messages []Message `json:"messages"`
	// StructuredOutput requests one JSON object matching a schema. Nil requests free text.
	StructuredOutput *StructuredOutput `json:"structuredOutput,omitempty"`
	// MaxOutputTokens caps generated tokens, including reasoning tokens where the
	// provider counts them. Zero uses the provider default; negative selects defect.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	// Temperature sets sampling temperature. Nil never sends one, which is the
	// only portable choice for models with fixed sampling.
	Temperature *float64 `json:"temperature,omitempty"`
	// ReasoningEffort selects reasoning effort. Empty uses the provider default.
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty"`
}

// FinishReason is the provider-neutral reason generation stopped.
type FinishReason string

const (
	// FinishReasonStop means the model finished normally. It selects generated.
	FinishReasonStop FinishReason = "stop"
	// FinishReasonLength means the output token limit stopped the model. It selects truncated.
	FinishReasonLength FinishReason = "length"
	// FinishReasonContentPolicy means a provider content policy stopped the model. It selects blocked.
	FinishReasonContentPolicy FinishReason = "contentPolicy"
	// FinishReasonRefusal means the model refused to answer. It selects blocked.
	FinishReasonRefusal FinishReason = "refusal"
)

// Usage is the token usage a provider reported for one request. A count the
// provider did not report is zero. TotalTokens is InputTokens plus
// OutputTokens when the provider reports no total.
type Usage struct {
	// InputTokens counts prompt tokens, including CachedInputTokens.
	InputTokens int64 `json:"inputTokens"`
	// CachedInputTokens is the cached subset of InputTokens.
	CachedInputTokens int64 `json:"cachedInputTokens,omitempty"`
	// OutputTokens counts generated tokens, including ReasoningTokens.
	OutputTokens int64 `json:"outputTokens"`
	// ReasoningTokens is the reasoning subset of OutputTokens.
	ReasoningTokens int64 `json:"reasoningTokens,omitempty"`
	// TotalTokens is the provider's billed total.
	TotalTokens int64 `json:"totalTokens"`
}

// TextGenerationResponse is the provider-neutral result of generateText.
//
// Only the generated and truncated branches carry Text. The other branches
// keep the model identity, finish reason, and usage that are known, without
// content. RequestedModel is set on every branch once the model is valid.
type TextGenerationResponse struct {
	// Text joins the model's non-reasoning text. With StructuredOutput it is the
	// JSON object text, without Markdown fences, validated against the schema.
	Text string `json:"text"`
	// RequestedModel is the validated model the request was sent for: the
	// request's Model when set, otherwise the connection's model.
	RequestedModel string `json:"requestedModel,omitempty"`
	// ServedModel is the model the provider reports it used, or empty when the
	// provider reports none.
	ServedModel string `json:"servedModel,omitempty"`
	// ResponseID is the provider's response identifier, also recorded as the
	// Receipt provider object ID.
	ResponseID string `json:"responseId,omitempty"`
	// FinishReason is the provider-neutral reason generation stopped.
	FinishReason FinishReason `json:"finishReason,omitempty"`
	// ProviderFinishReason is the provider's own finish token, such as
	// "content_filter". It always matches ^[A-Za-z0-9_.:-]{1,64}$.
	ProviderFinishReason string `json:"providerFinishReason,omitempty"`
	// Usage is the token usage the provider reported.
	Usage Usage `json:"usage"`
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mistral

import (
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

// Endpoints are the documented Mistral API base URLs from
// https://docs.mistral.ai/inference/regional-inference; chatProfile appends
// /chat/completions.
const (
	// GlobalEndpoint is the default endpoint. Mistral commits to no inference location for it.
	GlobalEndpoint = "https://api.mistral.ai/v1"
	// EUEndpoint processes inference within the European Union and EFTA at 1.1 times list price.
	EUEndpoint = "https://api.eu.mistral.ai/v1"
	// USEndpoint processes inference within the United States at 1.1 times list price.
	USEndpoint = "https://api.us.mistral.ai/v1"
)

// allowedEndpoints keep the API key on Mistral's own hosts; one key works on all three.
var allowedEndpoints = []string{GlobalEndpoint, EUEndpoint, USEndpoint}

// adjustableReasoningEfforts are the two efforts Mistral documents for adjustable-reasoning models.
var adjustableReasoningEfforts = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortNone: "none", llm.ReasoningEffortHigh: "high",
}

// chatProfile declares Mistral's Chat Completions dialect, as documented at
// https://docs.mistral.ai/studio/conversations/chat-completion and in
// https://docs.mistral.ai/openapi.yaml.
var chatProfile = openaichat.Profile{
	ProviderName:        ConnectorID,
	ChatCompletionsPath: "/chat/completions",
	InstructionsRole:    openaichat.InstructionsRoleSystem,
	MaxTokensField:      openaichat.MaxTokensFieldMaxTokens,
	Temperature:         llm.TemperatureRange(0, 1.5),
	// Mistral documents no strict-mode keyword limits; the pipeline still validates every returned object.
	StructuredOutput:           llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	// Mistral documents 504 for requests that run too long; a stream sends tokens as they arrive.
	Streaming: openaichat.StreamingPolicyAlways,
	// ChatCompletionRequest has no stream_options; the final chunk carries usage without it.
	ShouldRequestStreamUsage: false,
	// ChatCompletionRequest sets additionalProperties: false, so Mistral rejects any other field.
	AllowedRequestFields: []string{
		"model", "messages", "max_tokens", "temperature", "reasoning_effort", "response_format", "stream",
	},
	// model_length means the prompt plus output reached the model's context length.
	FinishReasons: map[string]llm.FinishReason{"model_length": llm.FinishReasonLength},
	// Mistral errors are a top-level {"object": "error", "type", "code"} envelope.
	ErrorTokenPointers: []string{"/type", "/code"},
	RequestIDHeaders:   []string{"mistral-correlation-id"},
	RateLimitHeaders:   []string{"x-ratelimit-remaining"},
	ModelRules: []openaichat.ModelRule{
		{ModelIDPattern: `^mistral-small-(2603|latest)$`, ReasoningEfforts: adjustableReasoningEfforts},
		{ModelIDPattern: `^mistral-medium-(3|3-5|latest)$`, ReasoningEfforts: adjustableReasoningEfforts},
	},
}

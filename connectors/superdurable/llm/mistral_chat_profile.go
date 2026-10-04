// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

// Endpoints are the documented Mistral API base URLs from
// https://docs.mistral.ai/inference/regional-inference; mistralChatProfile appends
// /chat/completions.
const (
	// mistralGlobalEndpoint is the default endpoint. Mistral commits to no inference location for it.
	mistralGlobalEndpoint = "https://api.mistral.ai/v1"
	// mistralEUEndpoint processes inference within the European Union and EFTA at 1.1 times list price.
	mistralEUEndpoint = "https://api.eu.mistral.ai/v1"
	// mistralUSEndpoint processes inference within the United States at 1.1 times list price.
	mistralUSEndpoint = "https://api.us.mistral.ai/v1"
)

// mistralAdjustableReasoningEfforts are the two efforts Mistral documents for adjustable-reasoning models.
var mistralAdjustableReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortNone: "none", textgen.ReasoningEffortHigh: "high",
}

// mistralChatProfile declares Mistral's Chat Completions dialect, as documented at
// https://docs.mistral.ai/studio/conversations/chat-completion and in
// https://docs.mistral.ai/openapi.yaml.
var mistralChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderMistral),
	ChatCompletionsPath: "/chat/completions",
	InstructionsRole:    openaichat.InstructionsRoleSystem,
	MaxTokensField:      openaichat.MaxTokensFieldMaxTokens,
	Temperature:         textgen.TemperatureRange(0, 1.5),
	// Mistral documents no strict-mode keyword limits; the pipeline still validates every returned object.
	StructuredOutput:           textgen.StructuredOutputRules{Mode: textgen.StructuredOutputModeJSONSchema},
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
	FinishReasons: map[string]textgen.FinishReason{"model_length": textgen.FinishReasonLength},
	// Mistral errors are a top-level {"object": "error", "type", "code"} envelope.
	ErrorTokenPointers: []string{"/type", "/code"},
	RequestIDHeaders:   []string{"mistral-correlation-id"},
	RateLimitHeaders:   []string{"x-ratelimit-remaining"},
	ModelRules: []openaichat.ModelRule{
		{ModelIDPattern: `^mistral-small-(2603|latest)$`, ReasoningEfforts: mistralAdjustableReasoningEfforts},
		{ModelIDPattern: `^mistral-medium-(3|3-5|latest)$`, ReasoningEfforts: mistralAdjustableReasoningEfforts},
	},
}

// newMistralWireFormat returns Mistral's Chat Completions wire format.
func newMistralWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&mistralChatProfile)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"math"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

const (
	// xaiGlobalEndpoint is xAI's global API base URL; xAI may route its requests between regions.
	xaiGlobalEndpoint = "https://api.x.ai/v1"
	// xaiUSRegionalEndpoint keeps request handling and inference in the United States. API keys work on both.
	xaiUSRegionalEndpoint = "https://us.api.x.ai/v1"
)

// xaiFrontierReasoningEfforts are the efforts grok-4.7 and grok-4.6 accept. Reasoning cannot be disabled on them.
var xaiFrontierReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortHigh: "high",
	textgen.ReasoningEffortExtraHigh: "xhigh",
}

// grok43ReasoningEfforts add "none", because grok-4.3 can answer without reasoning.
var grok43ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortNone: "none", textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium",
	textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortExtraHigh: "xhigh",
}

// grok45ReasoningEfforts omit "xhigh", which xAI silently serves as "high" on grok-4.5.
var grok45ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortHigh: "high",
}

// xaiNoReasoningEfforts rejects every effort for models whose documentation lists none.
var xaiNoReasoningEfforts = map[textgen.ReasoningEffort]string{}

// xaiChatProfile declares xAI's Chat Completions dialect, as documented at
// https://docs.x.ai/developers/rest-api-reference/inference/chat-completions.
var xaiChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderXai),
	ChatCompletionsPath: "/chat/completions",
	// max_tokens is deprecated; max_completion_tokens bounds only visible output, not reasoning.
	MaxTokensField:   openaichat.MaxTokensFieldMaxCompletionTokens,
	ReasoningEfforts: xaiFrontierReasoningEfforts,
	Temperature:      textgen.TemperatureRange(0, 2),
	// xAI enforces length bounds to 2,048 and item bounds to 256; the pipeline post-validates beyond.
	StructuredOutput:           textgen.StructuredOutputRules{Mode: textgen.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	// Streamed deltas keep a long reasoning exchange visibly alive and feed the text Stream.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	// The API reference lists end_turn beside stop as a normal finish.
	FinishReasons: map[string]textgen.FinishReason{"end_turn": textgen.FinishReasonStop},
	// xAI errors are {"code": "invalid-argument", "error": "<message>"}, with the message a string.
	ErrorTokenPointers: []string{"/code"},
	ModelRules: []openaichat.ModelRule{
		{ModelIDPattern: `^grok-4\.3(-latest)?$`, ReasoningEfforts: grok43ReasoningEfforts},
		{ModelIDPattern: `^(grok-4\.5|grok-4\.5-latest|grok-build-latest)$`, ReasoningEfforts: grok45ReasoningEfforts},
		// Grok 4.20, including its many aliases, and Grok Build 0.1 document no reasoning_effort values.
		{ModelIDPattern: `^grok-4\.20(-.+)?$`, ReasoningEfforts: xaiNoReasoningEfforts},
		{ModelIDPattern: `^(grok-build-0\.1|grok-code-fast|grok-code-fast-1|grok-code-fast-1-0825)$`, ReasoningEfforts: xaiNoReasoningEfforts},
	},
}

// newXAIWireFormat returns xAI's Chat Completions wire format, whose usage counts reasoning in OutputTokens.
func newXAIWireFormat(*Config) (textgen.WireFormat, error) {
	wireFormat, err := openaichat.NewWireFormat(&xaiChatProfile)
	if err != nil {
		return textgen.WireFormat{}, err
	}
	return withReasoningInOutputTokens(wireFormat), nil
}

// reasoningCountingDecoders wrap the openaichat decoders to add reasoning to OutputTokens.
type reasoningCountingDecoders struct {
	decodeChatResponse func(body []byte) (textgen.DecodedResponse, error)
	decodeChatStream   func(events *providerhttp.ServerSentEventReader, writeTextDelta func(delta string) error) (textgen.DecodedResponse, error)
}

// withReasoningInOutputTokens restores the textgen.Usage contract, because xAI's completion_tokens omit reasoning tokens.
func withReasoningInOutputTokens(wireFormat textgen.WireFormat) textgen.WireFormat {
	decoders := &reasoningCountingDecoders{decodeChatResponse: wireFormat.DecodeResponse, decodeChatStream: wireFormat.DecodeStream}
	if decoders.decodeChatResponse != nil {
		wireFormat.DecodeResponse = decoders.decodeResponse
	}
	if decoders.decodeChatStream != nil {
		wireFormat.DecodeStream = decoders.decodeStream
	}
	return wireFormat
}

func (decoders *reasoningCountingDecoders) decodeResponse(body []byte) (textgen.DecodedResponse, error) {
	decoded, err := decoders.decodeChatResponse(body)
	if err == nil {
		decoded.Usage = countReasoningInOutputTokens(decoded.Usage)
	}
	return decoded, err
}

func (decoders *reasoningCountingDecoders) decodeStream(
	events *providerhttp.ServerSentEventReader, writeTextDelta func(delta string) error,
) (textgen.DecodedResponse, error) {
	decoded, err := decoders.decodeChatStream(events, writeTextDelta)
	if err == nil {
		decoded.Usage = countReasoningInOutputTokens(decoded.Usage)
	}
	return decoded, err
}

// countReasoningInOutputTokens skips a total equal to input plus output, which already counts reasoning.
func countReasoningInOutputTokens(usage textgen.Usage) textgen.Usage {
	if usage.ReasoningTokens <= 0 || usage.OutputTokens < 0 || usage.ReasoningTokens > math.MaxInt64-usage.OutputTokens {
		return usage
	}
	isReasoningAlreadyCounted := usage.TotalTokens > 0 && usage.TotalTokens >= usage.InputTokens &&
		usage.TotalTokens-usage.InputTokens == usage.OutputTokens
	if !isReasoningAlreadyCounted {
		usage.OutputTokens += usage.ReasoningTokens
	}
	return usage
}

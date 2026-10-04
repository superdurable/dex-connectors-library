// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

// deepSeekAPIBaseURL is the only DeepSeek API host. Its Chat Completions path has no /v1 segment.
const deepSeekAPIBaseURL = "https://api.deepseek.com"

// deepSeekStallTimeout ends an attempt whose stream, keep-alive comments included, goes
// silent this long. DeepSeek documents no keep-alive interval.
const deepSeekStallTimeout = 5 * time.Minute

// deepSeekReasoningEfforts are DeepSeek's documented efforts. It silently coerces
// minimal, medium, and xhigh, so those select defect.
var deepSeekReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortNone: "none", textgen.ReasoningEffortLow: "low",
	textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortMax: "max",
}

// deepSeekChatProfile declares DeepSeek's dialect from https://api-docs.deepseek.com/api/create-chat-completion.
// Unmapped insufficient_system_resource and aborted finishes select invalidResponse.
var deepSeekChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderDeepseek),
	ChatCompletionsPath: "/chat/completions",
	InstructionsRole:    openaichat.InstructionsRoleSystem,
	MaxTokensField:      openaichat.MaxTokensFieldMaxTokens,
	ReasoningEfforts:    deepSeekReasoningEfforts,
	// Thinking mode, the default, silently ignores temperature; a Profile cannot allow it for effort none alone.
	Temperature: textgen.TemperatureNotAccepted(),
	// response_format accepts only text and json_object, so the schema travels in the instructions.
	StructuredOutput: textgen.StructuredOutputRules{Mode: textgen.StructuredOutputModeJSONObjectWithInstruction},
	// A stream carries keep-alive comments while DeepSeek queues the request, which the stall rule counts.
	Streaming: openaichat.StreamingPolicyAlways,
	// The last content chunk carries usage without stream_options, so none is sent.
	ShouldRequestStreamUsage: false,
	// x-ds-trace-id is undocumented but present on every observed api.deepseek.com response.
	RequestIDHeaders: []string{"x-ds-trace-id"},
	StallTimeout:     deepSeekStallTimeout,
}

// newDeepSeekWireFormat returns DeepSeek's Chat Completions wire format.
func newDeepSeekWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&deepSeekChatProfile)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deepseek

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

// apiBaseURL is the only DeepSeek API host. Its Chat Completions path has no /v1 segment.
const apiBaseURL = "https://api.deepseek.com"

// stallTimeout ends an attempt whose stream, keep-alive comments included, goes
// silent this long. DeepSeek documents no keep-alive interval.
const stallTimeout = 5 * time.Minute

// reasoningEfforts are DeepSeek's documented efforts. It silently coerces
// minimal, medium, and xhigh, so those select defect.
var reasoningEfforts = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortNone: "none", llm.ReasoningEffortLow: "low",
	llm.ReasoningEffortHigh: "high", llm.ReasoningEffortMax: "max",
}

// chatProfile declares DeepSeek's dialect from https://api-docs.deepseek.com/api/create-chat-completion.
// Unmapped insufficient_system_resource and aborted finishes select invalidResponse.
var chatProfile = openaichat.Profile{
	ProviderName:        ConnectorID,
	ChatCompletionsPath: "/chat/completions",
	InstructionsRole:    openaichat.InstructionsRoleSystem,
	MaxTokensField:      openaichat.MaxTokensFieldMaxTokens,
	ReasoningEfforts:    reasoningEfforts,
	// Thinking mode, the default, silently ignores temperature; a Profile cannot allow it for effort none alone.
	Temperature: llm.TemperatureNotAccepted(),
	// response_format accepts only text and json_object, so the schema travels in the instructions.
	StructuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONObjectWithInstruction},
	// A stream carries keep-alive comments while DeepSeek queues the request, which the stall rule counts.
	Streaming: openaichat.StreamingPolicyAlways,
	// The last content chunk carries usage without stream_options, so none is sent.
	ShouldRequestStreamUsage: false,
	// x-ds-trace-id is undocumented but present on every observed api.deepseek.com response.
	RequestIDHeaders: []string{"x-ds-trace-id"},
	StallTimeout:     stallTimeout,
}

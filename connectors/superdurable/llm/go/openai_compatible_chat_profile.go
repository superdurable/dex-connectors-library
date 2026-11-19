// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

// openAICompatibleChatProfile uses the shared Chat Completions codec without assuming vendor-specific model rules.
var openAICompatibleChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderOpenaiCompatible),
	ChatCompletionsPath: "/chat/completions",
	InstructionsRole:    openaichat.InstructionsRoleSystem,
	MaxTokensField:      openaichat.MaxTokensFieldMaxTokens,
	Temperature:         textgen.TemperatureRange(0, 2),
	StructuredOutput: textgen.StructuredOutputRules{
		Mode: textgen.StructuredOutputModeJSONObjectWithInstruction,
	},
	Streaming: openaichat.StreamingPolicyAlways,
}

// newOpenAICompatibleWireFormat returns the shared OpenAI Chat Completions wire
// format, or the Responses API wire format when config.OpenAICompatibleAPI is responses.
func newOpenAICompatibleWireFormat(config *Config) (textgen.WireFormat, error) {
	switch config.OpenAICompatibleAPI {
	case "", OpenAICompatibleAPIChatCompletions:
		return openaichat.NewWireFormat(&openAICompatibleChatProfile)
	case OpenAICompatibleAPIResponses:
		return responsesWireFormatFor(ProviderOpenaiCompatible), nil
	default:
		return textgen.WireFormat{}, fmt.Errorf("configuration openaiCompatibleApi is invalid")
	}
}

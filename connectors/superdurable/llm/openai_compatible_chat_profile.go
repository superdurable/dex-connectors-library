// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
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

// newOpenAICompatibleWireFormat returns the shared OpenAI Chat Completions wire format.
func newOpenAICompatibleWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&openAICompatibleChatProfile)
}

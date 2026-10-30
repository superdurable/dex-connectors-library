// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"math"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

// The fixed DashScope OpenAI-compatible endpoints documented at
// https://www.alibabacloud.com/help/en/model-studio/regions; qwenChatProfile appends the path.
const (
	qwenSingaporeEndpoint = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	qwenHongKongEndpoint  = "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1"
	qwenBeijingEndpoint   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

// qwenTemperatureBelowTwo is Model Studio's half-open [0, 2) range; the maximum is the largest float64 below 2.
var qwenTemperatureBelowTwo = textgen.TemperatureRange(0, math.Nextafter(2, 0))

// hybridQwen38ReasoningEfforts are the native Qwen3.8 efforts; "none" turns thinking off.
var hybridQwen38ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortNone: "none", textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium",
	textgen.ReasoningEffortExtraHigh: "xhigh",
}

// thinkingOnlyQwen38ReasoningEfforts omit "none", because qwen3.8-2.4t-a95b always thinks.
var thinkingOnlyQwen38ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortExtraHigh: "xhigh",
}

// qwenChatProfile declares Model Studio's OpenAI-compatible Chat Completions dialect, as
// documented at https://www.alibabacloud.com/help/en/model-studio/qwen-api-via-openai-chat-completions.
var qwenChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderQwen),
	ChatCompletionsPath: "/chat/completions",
	// Instructions go first, as the system message Model Studio documents.
	InstructionsRole: openaichat.InstructionsRoleSystem,
	MaxTokensField:   openaichat.MaxTokensFieldMaxTokens,
	Temperature:      qwenTemperatureBelowTwo,
	// json_schema is limited to Qwen3.7 and later and unsupported in Singapore; the json_object instruction names JSON.
	StructuredOutput: textgen.StructuredOutputRules{Mode: textgen.StructuredOutputModeJSONObjectWithInstruction},
	// Open-source models reject non-streaming calls while thinking, and streams avoid the non-streaming timeout.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	ErrorRules: []textgen.ErrorRule{
		// Content moderation of input or output; no status, so an error object inside a stream also matches.
		{ErrorToken: "data_inspection_failed", Outcome: textgen.BlockedOutcome()},
		{ErrorToken: "DataInspectionFailed", Outcome: textgen.BlockedOutcome()},
		// Account and billing states that waiting does not fix.
		{StatusCode: http.StatusBadRequest, ErrorToken: "Arrearage", Outcome: textgen.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusForbidden, ErrorToken: "AllocationQuota.FreeTierOnly", Outcome: textgen.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "CommodityNotPurchased", Outcome: textgen.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "PrepaidBillOverdue", Outcome: textgen.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "PostpaidBillOverdue", Outcome: textgen.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "BudgetLimitExceeded", Outcome: textgen.QuotaExhaustedOutcome()},
	},
	ModelRules: []openaichat.ModelRule{
		{
			ModelIDPattern: `^qwen3\.8-(?:max|max-0902|flash)$`, ReasoningEfforts: hybridQwen38ReasoningEfforts,
			MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens,
		},
		{ModelIDPattern: `^qwen3\.8-27b$`, ReasoningEfforts: hybridQwen38ReasoningEfforts},
		{ModelIDPattern: `^qwen3\.8-2\.4t-a95b$`, ReasoningEfforts: thinkingOnlyQwen38ReasoningEfforts},
		// max_completion_tokens also caps thinking; Model Studio documents it for these families only.
		{
			ModelIDPattern: `^qwen3\.[5-9]-(?:plus|flash)(?:-\d{4}-\d{2}-\d{2})?$`,
			MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens,
		},
		{
			ModelIDPattern: `^qwen3\.[7-9]-max(?:-preview|-\d{4}-\d{2}-\d{2})?$`,
			MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens,
		},
	},
}

// newQwenWireFormat returns Model Studio's Chat Completions wire format.
func newQwenWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&qwenChatProfile)
}

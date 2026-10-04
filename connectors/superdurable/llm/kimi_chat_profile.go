// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

const (
	// kimiGlobalEndpoint serves keys issued on platform.kimi.ai.
	kimiGlobalEndpoint = "https://api.moonshot.ai/v1"
	// kimiChinaEndpoint serves keys issued on platform.kimi.com; keys never work on the other platform.
	kimiChinaEndpoint = "https://api.moonshot.cn/v1"
)

// kimiK3ReasoningEfforts are the only efforts Kimi K3 accepts; K3 always reasons, so none is rejected.
var kimiK3ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortMax: "max",
}

// kimiChatProfile declares the Kimi API's Chat Completions dialect, as documented at
// https://platform.kimi.ai/docs/api/chat and https://platform.kimi.ai/docs/api/models-overview.
var kimiChatProfile = openaichat.Profile{
	ProviderName: string(ProviderKimi),
	// Kimi deprecated max_tokens in favor of max_completion_tokens, which also counts reasoning tokens.
	MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens,
	// Every Kimi model fixes its sampling and returns an error for any explicit temperature.
	Temperature: textgen.TemperatureNotAccepted(),
	// MFJS omits title and format, and rejects bounds beside a nullable type, so bounds become descriptions.
	StructuredOutput: textgen.StructuredOutputRules{
		Mode:                       textgen.StructuredOutputModeJSONSchema,
		KeywordsMovedToDescription: []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format"},
		KeywordsRemoved:            []string{"title"},
	},
	ShouldSendStrictJSONSchema: true,
	// Kimi's gateway ends a request that sends nothing for 900 seconds; a stream keeps sending bytes.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	// Rules match error.type on any status, so an error object inside a stream classifies like its HTTP form.
	ErrorRules: []textgen.ErrorRule{
		// Keeps the shared 501 default, because the server_error rule below matches every status.
		{StatusCode: http.StatusNotImplemented, Outcome: textgen.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
		// Kimi's content safety review rejects the input or the output with 400 content_filter.
		{ErrorToken: "content_filter", Outcome: textgen.BlockedOutcome()},
		// A 429 is exhausted balance, engine overload, or an organization rate limit; only the type tells them apart.
		{ErrorToken: "exceeded_current_quota_error", Outcome: textgen.QuotaExhaustedOutcome()},
		{ErrorToken: "engine_overloaded_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		// The type also covers the daily token limit, which only the unread message names, so that case exhausts retry.
		{ErrorToken: "rate_limit_reached_error", Outcome: textgen.RetryOutcome(sdkgo.FailureRateLimit)},
		{ErrorToken: "server_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "unexpected_output", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "server_unavailable", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
	},
	ModelRules: []openaichat.ModelRule{
		// K2 models select reasoning with a thinking switch that openaichat cannot send, so they reject every effort.
		{ModelIDPrefix: "kimi-k3", ReasoningEfforts: kimiK3ReasoningEfforts},
	},
}

// newKimiWireFormat returns the Kimi API's Chat Completions wire format.
func newKimiWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&kimiChatProfile)
}

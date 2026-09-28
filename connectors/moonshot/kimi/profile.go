// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kimi

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

const (
	// globalEndpoint serves keys issued on platform.kimi.ai.
	globalEndpoint = "https://api.moonshot.ai/v1"
	// chinaEndpoint serves keys issued on platform.kimi.com; keys never work on the other platform.
	chinaEndpoint = "https://api.moonshot.cn/v1"
)

// k3ReasoningEfforts are the only efforts Kimi K3 accepts; K3 always reasons, so none is rejected.
var k3ReasoningEfforts = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortLow: "low", llm.ReasoningEffortHigh: "high", llm.ReasoningEffortMax: "max",
}

// chatProfile declares the Kimi API's Chat Completions dialect, as documented at
// https://platform.kimi.ai/docs/api/chat and https://platform.kimi.ai/docs/api/models-overview.
var chatProfile = openaichat.Profile{
	ProviderName: ConnectorID,
	// Kimi deprecated max_tokens in favor of max_completion_tokens, which also counts reasoning tokens.
	MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens,
	// Every Kimi model fixes its sampling and returns an error for any explicit temperature.
	Temperature: llm.TemperatureNotAccepted(),
	// MFJS omits title and format, and rejects bounds beside a nullable type, so bounds become descriptions.
	StructuredOutput: llm.StructuredOutputRules{
		Mode:                       llm.StructuredOutputModeJSONSchema,
		KeywordsMovedToDescription: []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format"},
		KeywordsRemoved:            []string{"title"},
	},
	ShouldSendStrictJSONSchema: true,
	// Kimi's gateway ends a request that sends nothing for 900 seconds; a stream keeps sending bytes.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	// Rules match error.type on any status, so an error object inside a stream classifies like its HTTP form.
	ErrorRules: []llm.ErrorRule{
		// Keeps the shared 501 default, because the server_error rule below matches every status.
		{StatusCode: http.StatusNotImplemented, Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
		// Kimi's content safety review rejects the input or the output with 400 content_filter.
		{ErrorToken: "content_filter", Outcome: llm.BlockedOutcome()},
		// A 429 is exhausted balance, engine overload, or an organization rate limit; only the type tells them apart.
		{ErrorToken: "exceeded_current_quota_error", Outcome: llm.QuotaExhaustedOutcome()},
		{ErrorToken: "engine_overloaded_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		// The type also covers the daily token limit, which only the unread message names, so that case exhausts retry.
		{ErrorToken: "rate_limit_reached_error", Outcome: llm.RetryOutcome(sdkgo.FailureRateLimit)},
		{ErrorToken: "server_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "unexpected_output", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "server_unavailable", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
	},
	ModelRules: []openaichat.ModelRule{
		// K2 models select reasoning with a thinking switch that openaichat cannot send, so they reject every effort.
		{ModelIDPrefix: "kimi-k3", ReasoningEfforts: k3ReasoningEfforts},
	},
}

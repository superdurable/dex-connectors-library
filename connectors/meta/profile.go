// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meta

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

// apiBaseURL is the only Meta Model API host; chatProfile appends the path.
const apiBaseURL = "https://api.meta.ai"

// reasoningEfforts are the documented Chat Completions efforts. Muse Spark
// always reasons, so Meta rejects "none" with HTTP 400.
var reasoningEfforts = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortMinimal: "minimal", llm.ReasoningEffortLow: "low", llm.ReasoningEffortMedium: "medium",
	llm.ReasoningEffortHigh: "high", llm.ReasoningEffortExtraHigh: "xhigh",
}

// standardMuseSpark13ReasoningEfforts add "max", which Meta offers only on Standard-tier muse-spark-1.3.
var standardMuseSpark13ReasoningEfforts = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortMinimal: "minimal", llm.ReasoningEffortLow: "low", llm.ReasoningEffortMedium: "medium",
	llm.ReasoningEffortHigh: "high", llm.ReasoningEffortExtraHigh: "xhigh", llm.ReasoningEffortMax: "max",
}

// chatProfile declares Meta Model API's Chat Completions dialect, as
// documented at https://dev.meta.ai/docs/protocols/chat-completions.
var chatProfile = openaichat.Profile{
	ProviderName:        ConnectorID,
	ChatCompletionsPath: "/v1/chat/completions",
	// Meta gives developer the highest instruction precedence; system is kept only for compatibility.
	InstructionsRole: openaichat.InstructionsRoleDeveloper,
	MaxTokensField:   openaichat.MaxTokensFieldMaxCompletionTokens,
	ReasoningEfforts: reasoningEfforts,
	Temperature:      llm.TemperatureRange(0, 2),
	// The portable schema subset already satisfies Meta's strict subset.
	StructuredOutput:           llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	// A non-streaming request that runs past Meta's server time limit returns 504; streams are exempt.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	ErrorRules: []llm.ErrorRule{
		{StatusCode: http.StatusBadRequest, ErrorToken: "content_policy_violation", Outcome: llm.BlockedOutcome()},
		// Keeps the shared 501 default, because the server_error rule below also matches every status.
		{StatusCode: http.StatusNotImplemented, Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
		// Meta's retryable 503 and 429 types and codes; a mid-stream error object may carry only its code.
		{ErrorToken: "server_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "server_shutting_down", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "service_overloaded", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "backend_unavailable", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "rate_limit_error", Outcome: llm.RetryOutcome(sdkgo.FailureRateLimit)},
		{ErrorToken: "rate_limit_exceeded", Outcome: llm.RetryOutcome(sdkgo.FailureRateLimit)},
	},
	RateLimitHeaders: []string{
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens",
	},
	ModelRules: []openaichat.ModelRule{
		{ModelIDPattern: `^muse-spark-1\.3$`, ReasoningEfforts: standardMuseSpark13ReasoningEfforts},
	},
}

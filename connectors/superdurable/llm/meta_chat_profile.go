// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
)

// metaAPIBaseURL is the only Meta Model API host; metaChatProfile appends the path.
const metaAPIBaseURL = "https://api.meta.ai"

// metaReasoningEfforts are the documented Chat Completions efforts. Muse Spark
// always reasons, so Meta rejects "none" with HTTP 400.
var metaReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortMinimal: "minimal", textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium",
	textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortExtraHigh: "xhigh",
}

// standardMuseSpark13ReasoningEfforts add "max", which Meta offers only on Standard-tier muse-spark-1.3.
var standardMuseSpark13ReasoningEfforts = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortMinimal: "minimal", textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium",
	textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortExtraHigh: "xhigh", textgen.ReasoningEffortMax: "max",
}

// metaChatProfile declares Meta Model API's Chat Completions dialect, as
// documented at https://dev.meta.ai/docs/protocols/chat-completions.
var metaChatProfile = openaichat.Profile{
	ProviderName:        string(ProviderMeta),
	ChatCompletionsPath: "/v1/chat/completions",
	// Meta gives developer the highest instruction precedence; system is kept only for compatibility.
	InstructionsRole: openaichat.InstructionsRoleDeveloper,
	MaxTokensField:   openaichat.MaxTokensFieldMaxCompletionTokens,
	ReasoningEfforts: metaReasoningEfforts,
	Temperature:      textgen.TemperatureRange(0, 2),
	// The portable schema subset already satisfies Meta's strict subset.
	StructuredOutput:           textgen.StructuredOutputRules{Mode: textgen.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	// A non-streaming request that runs past Meta's server time limit returns 504; streams are exempt.
	Streaming:                openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage: true,
	ErrorRules: []textgen.ErrorRule{
		{StatusCode: http.StatusBadRequest, ErrorToken: "content_policy_violation", Outcome: textgen.BlockedOutcome()},
		// Keeps the shared 501 default, because the server_error rule below also matches every status.
		{StatusCode: http.StatusNotImplemented, Outcome: textgen.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
		// Meta's retryable 503 and 429 types and codes; a mid-stream error object may carry only its code.
		{ErrorToken: "server_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "server_shutting_down", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "service_overloaded", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "backend_unavailable", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
		{ErrorToken: "rate_limit_error", Outcome: textgen.RetryOutcome(sdkgo.FailureRateLimit)},
		{ErrorToken: "rate_limit_exceeded", Outcome: textgen.RetryOutcome(sdkgo.FailureRateLimit)},
	},
	RateLimitHeaders: []string{
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens",
	},
	ModelRules: []openaichat.ModelRule{
		{ModelIDPattern: `^muse-spark-1\.3$`, ReasoningEfforts: standardMuseSpark13ReasoningEfforts},
	},
}

// newMetaWireFormat returns Meta Model API's Chat Completions wire format.
func newMetaWireFormat(*Config) (textgen.WireFormat, error) {
	return openaichat.NewWireFormat(&metaChatProfile)
}

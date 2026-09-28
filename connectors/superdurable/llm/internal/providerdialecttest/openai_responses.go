// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package providerdialecttest

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const (
	// OpenAIConnectionModel is the OpenAI connector's default model, which a provider-only selection uses.
	OpenAIConnectionModel = "gpt-6-sol"
	// OpenAIAlternateModel is a second valid OpenAI model ID.
	OpenAIAlternateModel = "gpt-6-luna"
	// openAIReasoningSummary proves a reasoning item never reaches the Result text.
	openAIReasoningSummary = "openai dialect reasoning summary that never reaches the text"
)

// OpenAICredentialHeader is where the OpenAI connector sends the API key.
var OpenAICredentialHeader = llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}

// NewOpenAIResponsesDialect answers like the streamed Responses API, including 429 billing and 403 safety codes, with the default model.
func NewOpenAIResponsesDialect() llmtest.ProviderDialect {
	return llmtest.ProviderDialect{
		CredentialHeader: OpenAICredentialHeader, ConnectionModel: OpenAIConnectionModel, AlternateModel: OpenAIAlternateModel,
		RequestIDHeader:  "x-request-id",
		ReadRequestModel: readJSONBodyModel,
		GeneratedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			return openAIEventStreamReply(reply, openAIResponseObject(reply, "completed", nil))
		},
		TruncatedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			return openAIEventStreamReply(reply, openAIResponseObject(reply, "incomplete", map[string]any{"reason": "max_output_tokens"}))
		},
		BlockedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			reply.Text = ""
			return openAIEventStreamReply(reply, openAIResponseObject(reply, "incomplete", map[string]any{"reason": "content_filter"}))
		},
		ErrorReply: func(statusCode int, message string) llmtest.FakeReply {
			token := fmt.Sprintf("llmtest_http_%d", statusCode)
			return openAIErrorReply(statusCode, message, token, token)
		},
		QuotaExhaustedReply: func(message string) llmtest.FakeReply {
			return openAIErrorReply(http.StatusTooManyRequests, message, "insufficient_quota", "credit_balance_exhausted")
		},
		QuotaExhaustedErrorToken: "credit_balance_exhausted",
		MalformedReply: func() llmtest.FakeReply {
			return eventStreamReply("event: response.created\ndata: {not json\n\n")
		},
		ContentPolicyErrorReply: func(message string) llmtest.FakeReply {
			return openAIErrorReply(http.StatusForbidden, message, "invalid_request_error", "misalignment_policy_violation")
		},
		ReportedErrorReply: openAIReportedErrorReply,
		InterruptedStreamReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			return eventStreamReply(openAICreatedEvent(reply) + openAITextDeltaEvent(reply.Text))
		},
		UnstreamedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			return llmtest.FakeReply{Header: jsonHeader(), Body: mustEncodeJSON(openAIResponseObject(reply, "completed", nil))}
		},
		OptionalRequestFieldPointers: []string{
			"/instructions", "/max_output_tokens", "/temperature", "/top_p", "/reasoning", "/text", "/tools", "/tool_choice",
			"/truncation", "/service_tier", "/metadata", "/previous_response_id", "/include", "/background", "/stream_options",
			"/prompt_cache_key", "/safety_identifier", "/user",
		},
	}
}

// openAIEventStreamReply streams the text in two deltas between a reasoning item and the terminal event.
func openAIEventStreamReply(reply llmtest.GeneratedReply, terminal map[string]any) llmtest.FakeReply {
	var stream strings.Builder
	stream.WriteString(": keep-alive\n\n")
	stream.WriteString(openAICreatedEvent(reply))
	stream.WriteString(openAIEvent("response.output_item.added", map[string]any{
		"output_index": 0, "item": map[string]any{"id": "rs_1", "type": "reasoning", "summary": []any{}},
	}))
	middle := len(reply.Text) / 2
	for _, delta := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if delta != "" {
			stream.WriteString(openAITextDeltaEvent(delta))
		}
	}
	eventType := "response.completed"
	if terminal["status"] == "incomplete" {
		eventType = "response.incomplete"
	}
	stream.WriteString(openAIEvent(eventType, map[string]any{"response": terminal}))
	return eventStreamReply(stream.String())
}

// openAIResponseObject renders a Response with a reasoning item and, when reply has text, one assistant message.
func openAIResponseObject(reply llmtest.GeneratedReply, status string, incompleteDetails map[string]any) map[string]any {
	output := []any{map[string]any{
		"id": "rs_1", "type": "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": openAIReasoningSummary}},
	}}
	if reply.Text != "" {
		output = append(output, map[string]any{
			"id": "msg_1", "type": "message", "status": status, "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": reply.Text, "annotations": []any{}}},
		})
	}
	return map[string]any{
		"id": reply.ResponseID, "object": "response", "created_at": 1790000000, "status": status,
		"error": nil, "incomplete_details": incompleteDetails, "model": reply.ServedModel, "store": false,
		"output": output, "usage": map[string]any{
			"input_tokens": reply.Usage.InputTokens, "input_tokens_details": map[string]any{"cached_tokens": reply.Usage.CachedInputTokens},
			"output_tokens": reply.Usage.OutputTokens, "output_tokens_details": map[string]any{"reasoning_tokens": reply.Usage.ReasoningTokens},
			"total_tokens": reply.Usage.TotalTokens,
		},
	}
}

func openAIReportedErrorReply(token string, message string) llmtest.FakeReply {
	reply := llmtest.GeneratedReply{Text: "Partial", ServedModel: OpenAIConnectionModel, ResponseID: "resp_reported_error"}
	failed := openAIResponseObject(reply, "failed", nil)
	failed["output"] = []any{}
	failed["error"] = map[string]any{"code": token, "message": message}
	return eventStreamReply(openAICreatedEvent(reply) + openAITextDeltaEvent(reply.Text) +
		openAIEvent("response.failed", map[string]any{"response": failed}))
}

func openAICreatedEvent(reply llmtest.GeneratedReply) string {
	return openAIEvent("response.created", map[string]any{"response": map[string]any{
		"id": reply.ResponseID, "object": "response", "status": "in_progress", "model": reply.ServedModel, "output": []any{},
	}})
}

func openAITextDeltaEvent(delta string) string {
	return openAIEvent("response.output_text.delta", map[string]any{
		"item_id": "msg_1", "output_index": 1, "content_index": 0, "delta": delta, "logprobs": []any{},
	})
}

// openAIEvent renders one server-sent event with its type in both the event field and the data.
func openAIEvent(eventType string, fields map[string]any) string {
	fields["type"] = eventType
	return "event: " + eventType + "\ndata: " + mustEncodeJSON(fields) + "\n\n"
}

func openAIErrorReply(statusCode int, message string, errorType string, errorCode string) llmtest.FakeReply {
	return llmtest.FakeReply{StatusCode: statusCode, Header: jsonHeader(), Body: mustEncodeJSON(map[string]any{
		"error": map[string]any{"message": message, "type": errorType, "param": nil, "code": errorCode},
	})}
}

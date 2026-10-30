// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const (
	dialectConnectionModel = "gpt-6-sol"
	dialectAlternateModel  = "gpt-6-luna"
	// dialectReasoningSummary proves a reasoning item never reaches the Result text.
	dialectReasoningSummary = "openai dialect reasoning summary that never reaches the text"
)

// responsesDialect answers like the Responses API: streamed semantic events,
// OpenAI's error envelope, and the 429 billing and 403 safety codes.
var responsesDialect = textgentest.ProviderDialect{
	CredentialHeader: textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
	ConnectionModel:  dialectConnectionModel, AlternateModel: dialectAlternateModel,
	RequestIDHeader:  "x-request-id",
	ReadRequestModel: readResponsesRequestModel,
	GeneratedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return responsesEventStreamReply(reply, completedResponsesObject(reply))
	},
	TruncatedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return responsesEventStreamReply(reply, incompleteResponsesObject(reply, "max_output_tokens"))
	},
	BlockedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		reply.Text = ""
		return responsesEventStreamReply(reply, incompleteResponsesObject(reply, "content_filter"))
	},
	ErrorReply: func(statusCode int, message string) textgentest.FakeReply {
		token := fmt.Sprintf("llmtest_http_%d", statusCode)
		return responsesErrorReply(statusCode, message, token, token)
	},
	QuotaExhaustedReply: func(message string) textgentest.FakeReply {
		return responsesErrorReply(http.StatusTooManyRequests, message, "insufficient_quota", "credit_balance_exhausted")
	},
	QuotaExhaustedErrorToken: "credit_balance_exhausted",
	MalformedReply: func() textgentest.FakeReply {
		return responsesEventStreamFakeReply("event: response.created\ndata: {not json\n\n")
	},
	ContentPolicyErrorReply: func(message string) textgentest.FakeReply {
		return responsesErrorReply(http.StatusForbidden, message, "invalid_request_error", "misalignment_policy_violation")
	},
	ReportedErrorReply: func(token string, message string) textgentest.FakeReply {
		reply := textgentest.GeneratedReply{Text: "Partial", ServedModel: dialectConnectionModel, ResponseID: "resp_reported_error"}
		failed := responsesObject(reply, "failed", nil)
		failed["output"] = []any{}
		failed["error"] = map[string]any{"code": token, "message": message}
		return responsesEventStreamFakeReply(responsesCreatedEvent(reply) + responsesTextDeltaEvent(reply.Text) + responsesEvent("response.failed", map[string]any{"response": failed}))
	},
	InterruptedStreamReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return responsesEventStreamFakeReply(responsesCreatedEvent(reply) + responsesTextDeltaEvent(reply.Text))
	},
	UnstreamedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return textgentest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: mustEncodeResponsesJSON(completedResponsesObject(reply))}
	},
	OptionalRequestFieldPointers: []string{
		"/instructions", "/max_output_tokens", "/temperature", "/top_p", "/reasoning", "/text", "/tools", "/tool_choice",
		"/truncation", "/service_tier", "/metadata", "/previous_response_id", "/include", "/background", "/stream_options",
		"/prompt_cache_key", "/safety_identifier", "/user",
	},
}

// responsesEventStreamReply streams the text in two deltas between a reasoning item and the terminal event.
func responsesEventStreamReply(reply textgentest.GeneratedReply, terminal map[string]any) textgentest.FakeReply {
	var stream strings.Builder
	stream.WriteString(": keep-alive\n\n")
	stream.WriteString(responsesCreatedEvent(reply))
	stream.WriteString(responsesEvent("response.output_item.added", map[string]any{
		"output_index": 0, "item": map[string]any{"id": "rs_1", "type": "reasoning", "summary": []any{}},
	}))
	middle := len(reply.Text) / 2
	for _, delta := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if delta != "" {
			stream.WriteString(responsesTextDeltaEvent(delta))
		}
	}
	eventType := "response.completed"
	if terminal["status"] == "incomplete" {
		eventType = "response.incomplete"
	}
	stream.WriteString(responsesEvent(eventType, map[string]any{"response": terminal}))
	return responsesEventStreamFakeReply(stream.String())
}

func completedResponsesObject(reply textgentest.GeneratedReply) map[string]any {
	return responsesObject(reply, "completed", nil)
}

func incompleteResponsesObject(reply textgentest.GeneratedReply, reason string) map[string]any {
	return responsesObject(reply, "incomplete", map[string]any{"reason": reason})
}

// responsesObject renders a Response with a reasoning item and, when reply has text, one assistant message.
func responsesObject(reply textgentest.GeneratedReply, status string, incompleteDetails map[string]any) map[string]any {
	output := []any{map[string]any{
		"id": "rs_1", "type": "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": dialectReasoningSummary}},
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
		"output": output, "usage": responsesUsage(reply.Usage),
	}
}

func responsesCreatedEvent(reply textgentest.GeneratedReply) string {
	return responsesEvent("response.created", map[string]any{"response": map[string]any{
		"id": reply.ResponseID, "object": "response", "status": "in_progress", "model": reply.ServedModel, "output": []any{},
	}})
}

func responsesTextDeltaEvent(delta string) string {
	return responsesEvent("response.output_text.delta", map[string]any{
		"item_id": "msg_1", "output_index": 1, "content_index": 0, "delta": delta, "logprobs": []any{},
	})
}

// responsesEvent renders one server-sent event with its type in both the event field and the data.
func responsesEvent(eventType string, fields map[string]any) string {
	fields["type"] = eventType
	return "event: " + eventType + "\ndata: " + mustEncodeResponsesJSON(fields) + "\n\n"
}

func responsesErrorReply(statusCode int, message string, errorType string, errorCode string) textgentest.FakeReply {
	return textgentest.FakeReply{
		StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}},
		Body: mustEncodeResponsesJSON(map[string]any{"error": map[string]any{
			"message": message, "type": errorType, "param": nil, "code": errorCode,
		}}),
	}
}

func responsesUsage(usage textgen.Usage) map[string]any {
	return map[string]any{
		"input_tokens": usage.InputTokens, "input_tokens_details": map[string]any{"cached_tokens": usage.CachedInputTokens},
		"output_tokens": usage.OutputTokens, "output_tokens_details": map[string]any{"reasoning_tokens": usage.ReasoningTokens},
		"total_tokens": usage.TotalTokens,
	}
}

func responsesEventStreamFakeReply(body string) textgentest.FakeReply {
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
}

func readResponsesRequestModel(request textgentest.RecordedRequest) (string, error) {
	var body struct {
		Model *string `json:"model"`
	}
	if err := json.Unmarshal(request.Body, &body); err != nil {
		return "", fmt.Errorf("responses request body is not JSON: %w", err)
	}
	if body.Model == nil {
		return "", fmt.Errorf("responses request has no model")
	}
	return *body.Model, nil
}

func mustEncodeResponsesJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode Responses dialect reply: %v", err))
	}
	return string(encoded)
}

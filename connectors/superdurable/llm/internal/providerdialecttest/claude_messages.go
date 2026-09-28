// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package providerdialecttest renders OpenAI, Claude, and Gemini replies for the llmtest suites, following each pinned connector's unexported test dialect.
package providerdialecttest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const (
	// ClaudeConnectionModel is the Claude connector's default model, which a provider-only selection uses.
	ClaudeConnectionModel = "claude-sonnet-5"
	// ClaudeAlternateModel is a second valid Claude model ID.
	ClaudeAlternateModel = "claude-haiku-4-5"
	claudeReasoningText  = "llmtest reasoning that never reaches the text"
	// claudeSpendLimitErrorCode is the error code of Claude's tier spend-cap 429.
	claudeSpendLimitErrorCode = "enforced_spend_limit_reached"
)

// ClaudeCredentialHeader is where the Claude connector sends the API key.
var ClaudeCredentialHeader = llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}

// claudeErrorTypes are the error types https://platform.claude.com/docs/en/api/errors documents per status.
var claudeErrorTypes = map[int]string{
	http.StatusBadRequest: "invalid_request_error", http.StatusUnauthorized: "authentication_error",
	http.StatusPaymentRequired: "billing_error", http.StatusForbidden: "permission_error",
	http.StatusNotFound: "not_found_error", http.StatusConflict: "conflict_error",
	http.StatusRequestEntityTooLarge: "request_too_large", http.StatusTooManyRequests: "rate_limit_error",
	http.StatusInternalServerError: "api_error", http.StatusGatewayTimeout: "timeout_error", 529: "overloaded_error",
}

// NewClaudeMessagesDialect answers like the streamed Claude Messages API, with the Claude connector's default as connection model.
func NewClaudeMessagesDialect() llmtest.ProviderDialect {
	return llmtest.ProviderDialect{
		CredentialHeader: ClaudeCredentialHeader, ConnectionModel: ClaudeConnectionModel, AlternateModel: ClaudeAlternateModel,
		RequestIDHeader:  "request-id",
		ReadRequestModel: readJSONBodyModel,
		GeneratedReply:   func(reply llmtest.GeneratedReply) llmtest.FakeReply { return ClaudeStreamReply(reply, "end_turn") },
		TruncatedReply:   func(reply llmtest.GeneratedReply) llmtest.FakeReply { return ClaudeStreamReply(reply, "max_tokens") },
		BlockedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			reply.Text = ""
			return ClaudeStreamReply(reply, "refusal")
		},
		ErrorReply: claudeErrorReply,
		QuotaExhaustedReply: func(message string) llmtest.FakeReply {
			return claudeErrorBodyReply(http.StatusTooManyRequests, "rate_limit_error", message, claudeSpendLimitErrorCode)
		},
		QuotaExhaustedErrorToken: claudeSpendLimitErrorCode,
		MalformedReply:           func() llmtest.FakeReply { return eventStreamReply("event: message_start\ndata: {not json\n\n") },
		ReportedErrorReply:       claudeReportedErrorReply,
		InterruptedStreamReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			return eventStreamReply(claudeMessageStartEvent(reply) + claudeTextBlockStartEvent(0) + claudeTextDeltaEvent(0, reply.Text))
		},
		UnstreamedReply: claudeUnstreamedReply,
		OptionalRequestFieldPointers: []string{
			"/system", "/temperature", "/top_p", "/top_k", "/output_config", "/stop_sequences", "/thinking",
			"/tools", "/tool_choice", "/metadata", "/service_tier",
		},
	}
}

// ClaudeStreamReply renders a thinking block, two text deltas around a ping, stopReason, and usage.
func ClaudeStreamReply(reply llmtest.GeneratedReply, stopReason string) llmtest.FakeReply {
	var stream strings.Builder
	stream.WriteString(claudeMessageStartEvent(reply))
	stream.WriteString(claudeEvent("content_block_start", map[string]any{
		"index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""},
	}))
	stream.WriteString(claudeEvent("content_block_delta", map[string]any{
		"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": claudeReasoningText},
	}))
	stream.WriteString(claudeEvent("content_block_stop", map[string]any{"index": 0}))
	stream.WriteString(claudeTextBlockStartEvent(1))
	middle := len(reply.Text) / 2
	for position, text := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if text != "" {
			stream.WriteString(claudeTextDeltaEvent(1, text))
		}
		if position == 0 {
			stream.WriteString(claudeEvent("ping", map[string]any{}))
		}
	}
	stream.WriteString(claudeEvent("content_block_stop", map[string]any{"index": 1}))
	stream.WriteString(claudeEvent("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": claudeFinalUsage(reply.Usage),
	}))
	stream.WriteString(claudeEvent("message_stop", map[string]any{}))
	return eventStreamReply(stream.String())
}

func claudeMessageStartEvent(reply llmtest.GeneratedReply) string {
	return claudeEvent("message_start", map[string]any{"message": map[string]any{
		"id": reply.ResponseID, "type": "message", "role": "assistant", "content": []any{}, "model": reply.ServedModel,
		"stop_reason": nil, "stop_sequence": nil, "usage": claudeStartUsage(reply.Usage),
	}})
}

func claudeTextBlockStartEvent(index int) string {
	return claudeEvent("content_block_start", map[string]any{"index": index, "content_block": map[string]any{"type": "text", "text": ""}})
}

func claudeTextDeltaEvent(index int, text string) string {
	return claudeEvent("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "text_delta", "text": text}})
}

func claudeEvent(eventType string, fields map[string]any) string {
	data := map[string]any{"type": eventType}
	for name, value := range fields {
		data[name] = value
	}
	return "event: " + eventType + "\ndata: " + mustEncodeJSON(data) + "\n\n"
}

func claudeErrorReply(statusCode int, message string) llmtest.FakeReply {
	errorType, isDocumented := claudeErrorTypes[statusCode]
	switch {
	case isDocumented:
	case statusCode >= 500:
		errorType = "api_error"
	default:
		errorType = "invalid_request_error"
	}
	return claudeErrorBodyReply(statusCode, errorType, message, "")
}

func claudeErrorBodyReply(statusCode int, errorType string, message string, errorCode string) llmtest.FakeReply {
	return llmtest.FakeReply{
		StatusCode: statusCode, Header: jsonHeader(), Body: mustEncodeJSON(claudeErrorBody(errorType, message, errorCode)),
	}
}

// claudeReportedErrorReply streams partial text and then an error event whose type is token.
func claudeReportedErrorReply(token string, message string) llmtest.FakeReply {
	reply := llmtest.GeneratedReply{ServedModel: "llmtest-reported-error", ResponseID: "msg_llmtest_reported_error"}
	return eventStreamReply(claudeMessageStartEvent(reply) + claudeTextBlockStartEvent(0) + claudeTextDeltaEvent(0, "Partial") +
		"event: error\ndata: " + mustEncodeJSON(claudeErrorBody(token, message, "")) + "\n\n")
}

// claudeUnstreamedReply is the complete Message a gateway returns when it ignores "stream": true.
func claudeUnstreamedReply(reply llmtest.GeneratedReply) llmtest.FakeReply {
	usage := claudeStartUsage(reply.Usage)
	for name, value := range claudeFinalUsage(reply.Usage) {
		usage[name] = value
	}
	return llmtest.FakeReply{Header: jsonHeader(), Body: mustEncodeJSON(map[string]any{
		"id": reply.ResponseID, "type": "message", "role": "assistant", "model": reply.ServedModel,
		"content": []any{
			map[string]any{"type": "thinking", "thinking": claudeReasoningText, "signature": "llmtest-signature"},
			map[string]any{"type": "text", "text": reply.Text},
		},
		"stop_reason": "end_turn", "stop_sequence": nil, "usage": usage,
	})}
}

func claudeErrorBody(errorType string, message string, errorCode string) map[string]any {
	errorObject := map[string]any{"type": errorType, "message": message}
	if errorCode != "" {
		errorObject["details"] = map[string]any{"error_code": errorCode}
	}
	return map[string]any{"type": "error", "error": errorObject, "request_id": "req_llmtest_error"}
}

// claudeStartUsage reports uncached input apart from cache reads, as Claude does.
func claudeStartUsage(usage llm.Usage) map[string]any {
	return map[string]any{
		"input_tokens": usage.InputTokens - usage.CachedInputTokens, "cache_creation_input_tokens": 0,
		"cache_read_input_tokens": usage.CachedInputTokens, "output_tokens": 1,
	}
}

func claudeFinalUsage(usage llm.Usage) map[string]any {
	return map[string]any{
		"output_tokens": usage.OutputTokens, "output_tokens_details": map[string]any{"thinking_tokens": usage.ReasoningTokens},
	}
}

// readJSONBodyModel reads the model that OpenAI Responses and Claude Messages send in the JSON body.
func readJSONBodyModel(request llmtest.RecordedRequest) (string, error) {
	var body struct {
		Model *string `json:"model"`
	}
	if err := json.Unmarshal(request.Body, &body); err != nil {
		return "", fmt.Errorf("request body is not JSON: %w", err)
	}
	if body.Model == nil {
		return "", fmt.Errorf("request body has no model")
	}
	return *body.Model, nil
}

func eventStreamReply(body string) llmtest.FakeReply {
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
}

func jsonHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

func mustEncodeJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("provider dialect reply is not JSON serializable: %v", err))
	}
	return string(encoded)
}

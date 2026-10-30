// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package messagestest renders Claude Messages API replies for the textgentest
// suites, the way openaichattest does for Chat Completions connectors.
package messagestest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const (
	// ReasoningText is the thinking every generated reply streams, which a conforming connector never returns as text.
	ReasoningText = "llmtest reasoning that never reaches the text"
	// SpendLimitErrorCode is the error code of Claude's tier spend-cap 429.
	SpendLimitErrorCode = "enforced_spend_limit_reached"
)

// CredentialHeader is where the connector sends the API key.
var CredentialHeader = textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}

// optionalRequestFieldPointers are the body fields a request without optional fields never sends.
var optionalRequestFieldPointers = []string{
	"/system", "/temperature", "/top_p", "/top_k", "/output_config", "/stop_sequences", "/thinking",
	"/tools", "/tool_choice", "/metadata", "/service_tier",
}

// errorTypes are the error types https://platform.claude.com/docs/en/api/errors documents per status.
var errorTypes = map[int]string{
	http.StatusBadRequest: "invalid_request_error", http.StatusUnauthorized: "authentication_error",
	http.StatusPaymentRequired: "billing_error", http.StatusForbidden: "permission_error",
	http.StatusNotFound: "not_found_error", http.StatusConflict: "conflict_error",
	http.StatusRequestEntityTooLarge: "request_too_large", http.StatusTooManyRequests: "rate_limit_error",
	http.StatusInternalServerError: "api_error", http.StatusGatewayTimeout: "timeout_error", 529: "overloaded_error",
}

// NewProviderDialect returns an textgentest.ProviderDialect that answers like the
// Claude Messages API with streamed replies. It panics when a model is empty,
// because that is static test wiring.
func NewProviderDialect(connectionModel string, alternateModel string) textgentest.ProviderDialect {
	if connectionModel == "" || alternateModel == "" {
		panic("messagestest provider dialect requires a connection model and an alternate model")
	}
	return textgentest.ProviderDialect{
		CredentialHeader: CredentialHeader, ConnectionModel: connectionModel, AlternateModel: alternateModel,
		RequestIDHeader:  "request-id",
		ReadRequestModel: readRequestModel,
		GeneratedReply:   func(reply textgentest.GeneratedReply) textgentest.FakeReply { return StreamReply(reply, "end_turn") },
		TruncatedReply:   func(reply textgentest.GeneratedReply) textgentest.FakeReply { return StreamReply(reply, "max_tokens") },
		BlockedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
			reply.Text = ""
			return StreamReply(reply, "refusal")
		},
		ErrorReply:                   ErrorReply,
		QuotaExhaustedReply:          spendLimitReply,
		QuotaExhaustedErrorToken:     SpendLimitErrorCode,
		MalformedReply:               func() textgentest.FakeReply { return EventStreamReply("event: message_start\ndata: {not json\n\n") },
		ReportedErrorReply:           reportedErrorReply,
		InterruptedStreamReply:       interruptedStreamReply,
		UnstreamedReply:              unstreamedReply,
		OptionalRequestFieldPointers: append([]string(nil), optionalRequestFieldPointers...),
	}
}

// StreamReply renders the documented event flow: a thinking block, the text in
// two deltas with a ping between them, stopReason, and cumulative usage.
func StreamReply(reply textgentest.GeneratedReply, stopReason string) textgentest.FakeReply {
	var stream strings.Builder
	stream.WriteString(MessageStartEvent(reply))
	stream.WriteString(Event("content_block_start", map[string]any{
		"index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""},
	}))
	stream.WriteString(Event("content_block_delta", map[string]any{
		"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": ReasoningText},
	}))
	stream.WriteString(Event("content_block_delta", map[string]any{
		"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "llmtest-signature"},
	}))
	stream.WriteString(Event("content_block_stop", map[string]any{"index": 0}))
	stream.WriteString(Event("content_block_start", map[string]any{
		"index": 1, "content_block": map[string]any{"type": "text", "text": ""},
	}))
	middle := len(reply.Text) / 2
	for position, text := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if text != "" {
			stream.WriteString(TextDeltaEvent(1, text))
		}
		if position == 0 {
			stream.WriteString(Event("ping", map[string]any{}))
		}
	}
	stream.WriteString(Event("content_block_stop", map[string]any{"index": 1}))
	stream.WriteString(Event("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": finalUsageBody(reply.Usage),
	}))
	stream.WriteString(Event("message_stop", map[string]any{}))
	return EventStreamReply(stream.String())
}

// MessageStartEvent renders message_start with the reply's identity and input usage.
func MessageStartEvent(reply textgentest.GeneratedReply) string {
	return Event("message_start", map[string]any{"message": map[string]any{
		"id": reply.ResponseID, "type": "message", "role": "assistant", "content": []any{}, "model": reply.ServedModel,
		"stop_reason": nil, "stop_sequence": nil, "usage": startUsageBody(reply.Usage),
	}})
}

// TextDeltaEvent renders one text_delta for the text block at index.
func TextDeltaEvent(index int, text string) string {
	return Event("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "text_delta", "text": text}})
}

// Event renders one server-sent event whose data carries eventType as its "type".
func Event(eventType string, fields map[string]any) string {
	data := map[string]any{"type": eventType}
	for name, value := range fields {
		data[name] = value
	}
	return "event: " + eventType + "\ndata: " + mustEncodeJSON(data) + "\n\n"
}

// ErrorEvent renders a mid-stream error event with errorType and message.
func ErrorEvent(errorType string, message string) string {
	return "event: error\ndata: " + mustEncodeJSON(errorBody(errorType, message, "")) + "\n\n"
}

// ErrorReply renders the documented error envelope for statusCode.
func ErrorReply(statusCode int, message string) textgentest.FakeReply {
	errorType, isDocumented := errorTypes[statusCode]
	switch {
	case isDocumented:
	case statusCode >= 500:
		errorType = "api_error"
	default:
		errorType = "invalid_request_error"
	}
	return ErrorBodyReply(statusCode, errorType, message, "")
}

// ErrorBodyReply renders an error envelope with errorType, message, and an optional details.error_code.
func ErrorBodyReply(statusCode int, errorType string, message string, errorCode string) textgentest.FakeReply {
	return textgentest.FakeReply{
		StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}},
		Body: mustEncodeJSON(errorBody(errorType, message, errorCode)),
	}
}

// EventStreamReply is a 200 text/event-stream reply with body.
func EventStreamReply(body string) textgentest.FakeReply {
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
}

// spendLimitReply is the tier spend-cap 429, which carries no retry-after header.
func spendLimitReply(message string) textgentest.FakeReply {
	return ErrorBodyReply(http.StatusTooManyRequests, "rate_limit_error", message, SpendLimitErrorCode)
}

// reportedErrorReply streams partial text and then an error event whose type is token.
func reportedErrorReply(token string, message string) textgentest.FakeReply {
	reply := textgentest.GeneratedReply{ServedModel: "llmtest-reported-error", ResponseID: "msg_llmtest_reported_error"}
	return EventStreamReply(MessageStartEvent(reply) +
		Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) +
		TextDeltaEvent(0, "Partial") + ErrorEvent(token, message))
}

// interruptedStreamReply sends the text in one delta and ends before message_stop.
func interruptedStreamReply(reply textgentest.GeneratedReply) textgentest.FakeReply {
	return EventStreamReply(MessageStartEvent(reply) +
		Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) +
		TextDeltaEvent(0, reply.Text))
}

// unstreamedReply is the complete Message a gateway returns when it ignores "stream": true.
func unstreamedReply(reply textgentest.GeneratedReply) textgentest.FakeReply {
	usage := startUsageBody(reply.Usage)
	for name, value := range finalUsageBody(reply.Usage) {
		usage[name] = value
	}
	body := map[string]any{
		"id": reply.ResponseID, "type": "message", "role": "assistant", "model": reply.ServedModel,
		"content": []any{
			map[string]any{"type": "thinking", "thinking": ReasoningText, "signature": "llmtest-signature"},
			map[string]any{"type": "text", "text": reply.Text},
		},
		"stop_reason": "end_turn", "stop_sequence": nil, "usage": usage,
	}
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: mustEncodeJSON(body)}
}

func errorBody(errorType string, message string, errorCode string) map[string]any {
	errorObject := map[string]any{"type": errorType, "message": message}
	if errorCode != "" {
		errorObject["details"] = map[string]any{"error_code": errorCode}
	}
	return map[string]any{"type": "error", "error": errorObject, "request_id": "req_llmtest_error"}
}

// startUsageBody reports uncached input apart from cache reads, as Claude does.
func startUsageBody(usage textgen.Usage) map[string]any {
	return map[string]any{
		"input_tokens": usage.InputTokens - usage.CachedInputTokens, "cache_creation_input_tokens": 0,
		"cache_read_input_tokens": usage.CachedInputTokens, "output_tokens": 1,
	}
}

func finalUsageBody(usage textgen.Usage) map[string]any {
	return map[string]any{
		"output_tokens": usage.OutputTokens, "output_tokens_details": map[string]any{"thinking_tokens": usage.ReasoningTokens},
	}
}

func readRequestModel(request textgentest.RecordedRequest) (string, error) {
	var body struct {
		Model *string `json:"model"`
	}
	if err := json.Unmarshal(request.Body, &body); err != nil {
		return "", fmt.Errorf("messages request body is not JSON: %w", err)
	}
	if body.Model == nil {
		return "", fmt.Errorf("messages request body has no model")
	}
	return *body.Model, nil
}

func mustEncodeJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("messagestest reply is not JSON serializable: %v", err))
	}
	return string(encoded)
}

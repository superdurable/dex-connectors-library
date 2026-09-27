// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openaichattest renders OpenAI-compatible Chat Completions replies
// for the llmtest suites, so a connector built on openaichat describes its
// provider in a few fields instead of writing reply closures:
//
//	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
//		ConnectionModel: "fixture-model-a", AlternateModel: "fixture-reasoner-b", IsStreaming: true,
//	})
//	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{Dialect: dialect, NewQuery: newQuery})
//
// Replies use neutral error tokens such as "llmtest_http_429", so a
// connector's token-specific ErrorRules only match the quota and
// content-policy replies it configures. Every generated reply also carries
// reasoning_content, which a conforming connector never returns as text.
package openaichattest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const reasoningText = "llmtest reasoning that never reaches the text"

// optionalRequestFieldPointers are the body fields a Chat Completions request without optional fields never sends.
var optionalRequestFieldPointers = []string{
	"/temperature", "/top_p", "/max_tokens", "/max_completion_tokens", "/reasoning_effort", "/response_format",
	"/stop", "/seed", "/n", "/presence_penalty", "/frequency_penalty",
}

// ProviderDialectConfig describes one Chat Completions provider to NewProviderDialect.
type ProviderDialectConfig struct {
	// CredentialHeader is where the connector sends the API key. An empty
	// Name uses Authorization with the "Bearer " prefix, the openaichat default.
	CredentialHeader llm.CredentialHeader
	// ConnectionModel is the connection's model ID. It is required and must
	// accept structured output under the connector's Profile.
	ConnectionModel string
	// AlternateModel is a second valid model ID. It is required.
	AlternateModel string
	// IsStreaming renders chat.completion.chunk event streams instead of one
	// chat.completion body, and adds the interrupted-stream and
	// complete-body-for-a-stream cases. Match the Profile's Streaming policy
	// for both models.
	IsStreaming bool
	// RequestIDHeader is the response header that carries the request ID.
	// Empty uses "x-request-id", the openaichat default.
	RequestIDHeader string
	// ErrorTokenPointers mirror the Profile's ErrorTokenPointers: error
	// replies place their token at each pointer, and their message beside the
	// first one. Nil uses "/error/type" and "/error/code", the openaichat
	// default; a top-level envelope such as Mistral's uses "/type" and "/code".
	// The case for an error inside a 2xx response runs only when every pointer
	// is under "/error/", the envelope openaichat recognizes there.
	ErrorTokenPointers []string
	// QuotaExhaustedStatusCode is the status of the quota reply. Zero uses 402.
	QuotaExhaustedStatusCode int
	// QuotaExhaustedErrorToken is placed in the quota reply's error type and
	// code, for a provider whose Profile recognizes quota by token. Empty
	// uses "llmtest_quota_exhausted". The quota Failure must name it.
	QuotaExhaustedErrorToken string
	// BlockedFinishReason is the finish token of the blocked reply. Empty uses
	// "content_filter".
	BlockedFinishReason string
	// ContentPolicyErrorToken is the error token of a content-policy block
	// that the provider reports as an error, such as "content_filter" or
	// "content_policy_violation". Empty skips the content-policy error case.
	ContentPolicyErrorToken string
	// ContentPolicyErrorStatusCode is the status of the content-policy error.
	// Zero uses 400.
	ContentPolicyErrorStatusCode int
}

// NewProviderDialect returns an llmtest.ProviderDialect that answers like a
// Chat Completions provider. It panics when config is nil, a model is empty,
// or an error-token pointer is invalid, because those are static test wiring.
func NewProviderDialect(config *ProviderDialectConfig) llmtest.ProviderDialect {
	if config == nil || config.ConnectionModel == "" || config.AlternateModel == "" {
		panic("openaichattest provider dialect requires ConnectionModel and AlternateModel")
	}
	dialect := &chatDialect{config: *config, errorTokenPointers: config.ErrorTokenPointers}
	if dialect.errorTokenPointers == nil {
		dialect.errorTokenPointers = []string{"/error/type", "/error/code"}
	}
	for _, pointer := range dialect.errorTokenPointers {
		if !strings.HasPrefix(pointer, "/") || len(pointer) < 2 {
			panic("openaichattest error-token pointers must start with / and name a field")
		}
	}
	credentialHeader := config.CredentialHeader
	if credentialHeader.Name == "" {
		credentialHeader = llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}
	}
	providerDialect := llmtest.ProviderDialect{
		CredentialHeader: credentialHeader,
		ConnectionModel:  config.ConnectionModel, AlternateModel: config.AlternateModel,
		RequestIDHeader:              cmp.Or(config.RequestIDHeader, "x-request-id"),
		ReadRequestModel:             readRequestModel,
		GeneratedReply:               func(reply llmtest.GeneratedReply) llmtest.FakeReply { return dialect.completionReply(reply, "stop") },
		TruncatedReply:               func(reply llmtest.GeneratedReply) llmtest.FakeReply { return dialect.completionReply(reply, "length") },
		BlockedReply:                 dialect.blockedReply,
		ErrorReply:                   dialect.errorReply,
		QuotaExhaustedReply:          dialect.quotaExhaustedReply,
		QuotaExhaustedErrorToken:     dialect.quotaExhaustedErrorToken(),
		MalformedReply:               dialect.malformedReply,
		OptionalRequestFieldPointers: append([]string(nil), optionalRequestFieldPointers...),
	}
	if dialect.hasNestedErrorEnvelope() {
		providerDialect.ReportedErrorReply = dialect.reportedErrorReply
	}
	if config.ContentPolicyErrorToken != "" {
		providerDialect.ContentPolicyErrorReply = dialect.contentPolicyErrorReply
	}
	if config.IsStreaming {
		providerDialect.InterruptedStreamReply = dialect.interruptedStreamReply
		providerDialect.UnstreamedReply = dialect.unstreamedReply
	}
	return providerDialect
}

// chatDialect renders replies for one ProviderDialectConfig.
type chatDialect struct {
	config             ProviderDialectConfig
	errorTokenPointers []string
}

func (dialect *chatDialect) completionReply(reply llmtest.GeneratedReply, finishReason string) llmtest.FakeReply {
	if dialect.config.IsStreaming {
		return dialect.streamReply(reply, finishReason)
	}
	return completionBodyReply(reply, finishReason)
}

func completionBodyReply(reply llmtest.GeneratedReply, finishReason string) llmtest.FakeReply {
	body := map[string]any{
		"id": reply.ResponseID, "object": "chat.completion", "created": 1790000000, "model": reply.ServedModel,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": reply.Text, "reasoning_content": reasoningText},
			"finish_reason": finishReason,
		}},
		"usage": usageBody(reply.Usage),
	}
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: mustEncodeJSON(body)}
}

// streamReply splits the text across two deltas and sends usage in a final choiceless chunk.
func (dialect *chatDialect) streamReply(reply llmtest.GeneratedReply, finishReason string) llmtest.FakeReply {
	var stream strings.Builder
	stream.WriteString(": keep-alive\n\n\n")
	stream.WriteString(chunkEvent(reply, deltaChoices(map[string]any{"role": "assistant", "reasoning_content": reasoningText}), nil))
	middle := len(reply.Text) / 2
	for _, text := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if text != "" {
			stream.WriteString(chunkEvent(reply, deltaChoices(map[string]any{"content": text}), nil))
		}
	}
	stream.WriteString(chunkEvent(reply, []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}, nil))
	stream.WriteString(chunkEvent(reply, []any{}, usageBody(reply.Usage)))
	stream.WriteString("data: [DONE]\n\n")
	return eventStreamReply(stream.String())
}

// interruptedStreamReply sends the text in one delta and then ends without a finish reason or [DONE].
func (dialect *chatDialect) interruptedStreamReply(reply llmtest.GeneratedReply) llmtest.FakeReply {
	return eventStreamReply(chunkEvent(reply, deltaChoices(map[string]any{"role": "assistant", "content": reply.Text}), nil))
}

// unstreamedReply is the complete chat.completion a gateway returns when it ignores "stream": true.
func (dialect *chatDialect) unstreamedReply(reply llmtest.GeneratedReply) llmtest.FakeReply {
	return completionBodyReply(reply, "stop")
}

func (dialect *chatDialect) blockedReply(reply llmtest.GeneratedReply) llmtest.FakeReply {
	reply.Text = ""
	return dialect.completionReply(reply, cmp.Or(dialect.config.BlockedFinishReason, "content_filter"))
}

func (dialect *chatDialect) errorReply(statusCode int, message string) llmtest.FakeReply {
	return dialect.errorBodyReply(statusCode, message, fmt.Sprintf("llmtest_http_%d", statusCode))
}

func (dialect *chatDialect) quotaExhaustedReply(message string) llmtest.FakeReply {
	statusCode := cmp.Or(dialect.config.QuotaExhaustedStatusCode, http.StatusPaymentRequired)
	return dialect.errorBodyReply(statusCode, message, dialect.quotaExhaustedErrorToken())
}

func (dialect *chatDialect) quotaExhaustedErrorToken() string {
	return cmp.Or(dialect.config.QuotaExhaustedErrorToken, "llmtest_quota_exhausted")
}

func (dialect *chatDialect) contentPolicyErrorReply(message string) llmtest.FakeReply {
	statusCode := cmp.Or(dialect.config.ContentPolicyErrorStatusCode, http.StatusBadRequest)
	return dialect.errorBodyReply(statusCode, message, dialect.config.ContentPolicyErrorToken)
}

// hasNestedErrorEnvelope reports whether every token lives under "/error/", the only envelope openaichat recognizes inside a 2xx response.
func (dialect *chatDialect) hasNestedErrorEnvelope() bool {
	for _, pointer := range dialect.errorTokenPointers {
		if !strings.HasPrefix(pointer, "/error/") {
			return false
		}
	}
	return true
}

// reportedErrorReply sends a 200 error body, or for a streaming dialect a content delta followed by an error event.
func (dialect *chatDialect) reportedErrorReply(token string, message string) llmtest.FakeReply {
	if !dialect.config.IsStreaming {
		return dialect.errorBodyReply(http.StatusOK, message, token)
	}
	reply := llmtest.GeneratedReply{ServedModel: "llmtest-reported-error", ResponseID: "resp-llmtest-reported-error"}
	return eventStreamReply(chunkEvent(reply, deltaChoices(map[string]any{"role": "assistant", "content": "Partial"}), nil) +
		"event: error\ndata: " + mustEncodeJSON(dialect.errorBody(message, token)) + "\n\n")
}

func (dialect *chatDialect) malformedReply() llmtest.FakeReply {
	if dialect.config.IsStreaming {
		return eventStreamReply("data: {not json\n\n")
	}
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: `{"id":"x","choices":"not-an-array"}`}
}

func (dialect *chatDialect) errorBodyReply(statusCode int, message string, token string) llmtest.FakeReply {
	return llmtest.FakeReply{
		StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}},
		Body: mustEncodeJSON(dialect.errorBody(message, token)),
	}
}

// errorBody places token at every error-token pointer and message beside the first one.
func (dialect *chatDialect) errorBody(message string, token string) map[string]any {
	body := map[string]any{}
	first := dialect.errorTokenPointers[0]
	setAtJSONPointer(body, first[:strings.LastIndex(first, "/")]+"/message", message)
	for _, pointer := range dialect.errorTokenPointers {
		setAtJSONPointer(body, pointer, token)
	}
	return body
}

// setAtJSONPointer sets value at an object-only RFC 6901 pointer, creating objects on the way.
func setAtJSONPointer(document map[string]any, pointer string, value any) {
	segments := strings.Split(pointer[1:], "/")
	current := document
	for _, escaped := range segments[:len(segments)-1] {
		segment := strings.ReplaceAll(strings.ReplaceAll(escaped, "~1", "/"), "~0", "~")
		next, isObject := current[segment].(map[string]any)
		if !isObject {
			next = map[string]any{}
			current[segment] = next
		}
		current = next
	}
	last := segments[len(segments)-1]
	current[strings.ReplaceAll(strings.ReplaceAll(last, "~1", "/"), "~0", "~")] = value
}

func chunkEvent(reply llmtest.GeneratedReply, choices []any, usage map[string]any) string {
	body := map[string]any{
		"id": reply.ResponseID, "object": "chat.completion.chunk", "created": 1790000000,
		"model": reply.ServedModel, "choices": choices,
	}
	if usage != nil {
		body["usage"] = usage
	}
	return "data: " + mustEncodeJSON(body) + "\n\n"
}

func deltaChoices(fields map[string]any) []any {
	return []any{map[string]any{"index": 0, "delta": fields, "finish_reason": nil}}
}

func eventStreamReply(body string) llmtest.FakeReply {
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
}

func usageBody(usage llm.Usage) map[string]any {
	return map[string]any{
		"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens, "total_tokens": usage.TotalTokens,
		"prompt_tokens_details":     map[string]any{"cached_tokens": usage.CachedInputTokens},
		"completion_tokens_details": map[string]any{"reasoning_tokens": usage.ReasoningTokens},
	}
}

func readRequestModel(request llmtest.RecordedRequest) (string, error) {
	var body struct {
		Model *string `json:"model"`
	}
	if err := json.Unmarshal(request.Body, &body); err != nil {
		return "", fmt.Errorf("chat completion request body is not JSON: %w", err)
	}
	if body.Model == nil {
		return "", fmt.Errorf("chat completion request has no model")
	}
	return *body.Model, nil
}

func mustEncodeJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode openaichattest reply: %v", err))
	}
	return string(encoded)
}

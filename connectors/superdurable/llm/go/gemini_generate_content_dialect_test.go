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
	geminiRequestIDHeader = "X-Goog-Request-Id"
	// geminiQuotaErrorToken is a neutral status token, so the quota case proves the error-token pointers read the envelope.
	geminiQuotaErrorToken = "llmtest_quota_exhausted"
	geminiThoughtText     = "llmtest thought that never reaches the text"
)

// geminiDialect answers like models.generateContent: one complete JSON body per request, the google.rpc error envelope, and a thought part.
var geminiDialect = textgentest.ProviderDialect{
	CredentialHeader: textgen.CredentialHeader{Name: "x-goog-api-key"},
	ConnectionModel:  "gemini-3.5-flash-lite", AlternateModel: "gemini-3.8-flash",
	RequestIDHeader:  geminiRequestIDHeader,
	ReadRequestModel: readGenerateContentRequestModel,
	GeneratedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return geminiCandidateReply(reply, "STOP")
	},
	TruncatedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		return geminiCandidateReply(reply, "MAX_TOKENS")
	},
	BlockedReply: func(reply textgentest.GeneratedReply) textgentest.FakeReply {
		reply.Text = ""
		return geminiCandidateReply(reply, "SAFETY")
	},
	ErrorReply: func(statusCode int, message string) textgentest.FakeReply {
		return geminiErrorReply(statusCode, message, googleStatusName(statusCode))
	},
	QuotaExhaustedReply: func(message string) textgentest.FakeReply {
		return geminiErrorReply(http.StatusPaymentRequired, message, geminiQuotaErrorToken)
	},
	QuotaExhaustedErrorToken: geminiQuotaErrorToken,
	MalformedReply: func() textgentest.FakeReply {
		return textgentest.FakeReply{Header: geminiJSONHeader(), Body: `{"candidates":"not-an-array"}`}
	},
	ReportedErrorReply: func(token string, message string) textgentest.FakeReply {
		return geminiErrorReply(http.StatusOK, message, token)
	},
	OptionalRequestFieldPointers: []string{
		"/systemInstruction", "/generationConfig", "/safetySettings", "/tools", "/toolConfig", "/cachedContent",
	},
}

// geminiCandidateReply sends one candidate whose text is split across two parts after a thought part.
func geminiCandidateReply(reply textgentest.GeneratedReply, finishReason string) textgentest.FakeReply {
	parts := []any{map[string]any{"text": geminiThoughtText, "thought": true}}
	middle := len(reply.Text) / 2
	for _, text := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
	}
	body := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": parts}, "finishReason": finishReason, "index": 0,
		}},
		"usageMetadata": geminiUsageMetadata(reply.Usage),
		"modelVersion":  reply.ServedModel, "responseId": reply.ResponseID,
	}
	return textgentest.FakeReply{Header: geminiJSONHeader(), Body: mustEncodeGeminiJSON(body)}
}

// geminiUsageMetadata reports output without thoughts, because candidatesTokenCount excludes thoughtsTokenCount.
func geminiUsageMetadata(usage textgen.Usage) map[string]any {
	return map[string]any{
		"promptTokenCount": usage.InputTokens, "cachedContentTokenCount": usage.CachedInputTokens,
		"candidatesTokenCount": usage.OutputTokens - usage.ReasoningTokens, "thoughtsTokenCount": usage.ReasoningTokens,
		"totalTokenCount": usage.TotalTokens,
	}
}

func geminiErrorReply(statusCode int, message string, status string) textgentest.FakeReply {
	return textgentest.FakeReply{StatusCode: statusCode, Header: geminiJSONHeader(), Body: mustEncodeGeminiJSON(map[string]any{
		"error": map[string]any{"code": statusCode, "message": message, "status": status},
	})}
}

// googleStatusName returns the google.rpc code Gemini sends with an HTTP status.
func googleStatusName(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusInternalServerError:
		return "INTERNAL"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		return "UNKNOWN"
	}
}

// readGenerateContentRequestModel reads {model} from a .../models/{model}:generateContent path.
func readGenerateContentRequestModel(request textgentest.RecordedRequest) (string, error) {
	path, _, _ := strings.Cut(request.Path, "?")
	_, afterModels, hasModels := strings.Cut(path, "/models/")
	model, isGenerateContent := strings.CutSuffix(afterModels, ":generateContent")
	if !hasModels || !isGenerateContent || model == "" {
		return "", fmt.Errorf("request path %q is not a models.generateContent path", path)
	}
	return model, nil
}

func geminiJSONHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

func mustEncodeGeminiJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode Gemini reply: %v", err))
	}
	return string(encoded)
}

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
	// GeminiConnectionModel is the Gemini connector's default model, which a provider-only selection uses.
	GeminiConnectionModel = "gemini-3.5-flash-lite"
	// GeminiAlternateModel is a second valid Gemini model ID.
	GeminiAlternateModel = "gemini-3.8-flash"
	// geminiQuotaErrorToken is a neutral status token, so the quota case proves the error-token pointers read the envelope.
	geminiQuotaErrorToken = "llmtest_quota_exhausted"
	geminiThoughtText     = "llmtest thought that never reaches the text"
)

// GeminiCredentialHeader is where the Gemini connector sends the API key.
var GeminiCredentialHeader = llm.CredentialHeader{Name: "x-goog-api-key"}

// NewGeminiGenerateContentDialect answers like models.generateContent, with google.rpc errors, a thought part, and the connector's default model.
func NewGeminiGenerateContentDialect() llmtest.ProviderDialect {
	return llmtest.ProviderDialect{
		CredentialHeader: GeminiCredentialHeader, ConnectionModel: GeminiConnectionModel, AlternateModel: GeminiAlternateModel,
		RequestIDHeader:  "X-Goog-Request-Id",
		ReadRequestModel: readGenerateContentPathModel,
		GeneratedReply:   func(reply llmtest.GeneratedReply) llmtest.FakeReply { return GeminiCandidateReply(reply, "STOP") },
		TruncatedReply:   func(reply llmtest.GeneratedReply) llmtest.FakeReply { return GeminiCandidateReply(reply, "MAX_TOKENS") },
		BlockedReply: func(reply llmtest.GeneratedReply) llmtest.FakeReply {
			reply.Text = ""
			return GeminiCandidateReply(reply, "SAFETY")
		},
		ErrorReply: func(statusCode int, message string) llmtest.FakeReply {
			return geminiErrorReply(statusCode, message, googleStatusName(statusCode))
		},
		QuotaExhaustedReply: func(message string) llmtest.FakeReply {
			return geminiErrorReply(http.StatusPaymentRequired, message, geminiQuotaErrorToken)
		},
		QuotaExhaustedErrorToken: geminiQuotaErrorToken,
		MalformedReply: func() llmtest.FakeReply {
			return llmtest.FakeReply{Header: jsonHeader(), Body: `{"candidates":"not-an-array"}`}
		},
		ReportedErrorReply: func(token string, message string) llmtest.FakeReply {
			return geminiErrorReply(http.StatusOK, message, token)
		},
		OptionalRequestFieldPointers: []string{
			"/systemInstruction", "/generationConfig", "/safetySettings", "/tools", "/toolConfig", "/cachedContent",
		},
	}
}

// GeminiCandidateReply sends one candidate whose text is split across two parts after a thought part.
func GeminiCandidateReply(reply llmtest.GeneratedReply, finishReason string) llmtest.FakeReply {
	parts := []any{map[string]any{"text": geminiThoughtText, "thought": true}}
	middle := len(reply.Text) / 2
	for _, text := range []string{reply.Text[:middle], reply.Text[middle:]} {
		if text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
	}
	// candidatesTokenCount excludes thoughtsTokenCount.
	return llmtest.FakeReply{Header: jsonHeader(), Body: mustEncodeJSON(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": parts}, "finishReason": finishReason, "index": 0,
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount": reply.Usage.InputTokens, "cachedContentTokenCount": reply.Usage.CachedInputTokens,
			"candidatesTokenCount": reply.Usage.OutputTokens - reply.Usage.ReasoningTokens,
			"thoughtsTokenCount":   reply.Usage.ReasoningTokens, "totalTokenCount": reply.Usage.TotalTokens,
		},
		"modelVersion": reply.ServedModel, "responseId": reply.ResponseID,
	})}
}

func geminiErrorReply(statusCode int, message string, status string) llmtest.FakeReply {
	return llmtest.FakeReply{StatusCode: statusCode, Header: jsonHeader(), Body: mustEncodeJSON(map[string]any{
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

// readGenerateContentPathModel reads {model} from a .../models/{model}:generateContent path.
func readGenerateContentPathModel(request llmtest.RecordedRequest) (string, error) {
	path, _, _ := strings.Cut(request.Path, "?")
	_, afterModels, hasModels := strings.Cut(path, "/models/")
	model, isGenerateContent := strings.CutSuffix(afterModels, ":generateContent")
	if !hasModels || !isGenerateContent || model == "" {
		return "", fmt.Errorf("request path %q is not a models.generateContent path", path)
	}
	return model, nil
}

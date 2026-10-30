// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	geminiTestAPIKey       = "AIzaSENTINEL-test-key_0123456789"
	geminiProviderSentinel = "SENTINEL-PROVIDER-BODY"
)

var geminiTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "gemini-test"}

func TestGeminiGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  geminiDialect,
		NewQuery: newGeminiGenerateTextQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none, which Gemini 3 thinking levels cannot express", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "extra-high reasoning effort", Request: textgen.TextGenerationRequest{ReasoningEffort: textgen.ReasoningEffortExtraHigh}},
			{Name: "minimal thinking on gemini-3.8-flash", Request: textgen.TextGenerationRequest{
				Model: "gemini-3.8-flash", ReasoningEffort: textgen.ReasoningEffortMinimal,
			}},
			{Name: "minimal thinking on gemini-flash-latest, which follows the newest Flash", Request: textgen.TextGenerationRequest{
				Model: "gemini-flash-latest", ReasoningEffort: textgen.ReasoningEffortMinimal,
			}},
			{Name: "thinking level on a Gemini 2.5 model", Request: textgen.TextGenerationRequest{
				Model: "gemini-2.5-flash", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
		},
	})
}

// TestGeminiGenerateTextSendsTheDocumentedGenerateContentRequest pins the REST body and the response mapping.
func TestGeminiGenerateTextSendsTheDocumentedGenerateContentRequest(t *testing.T) {
	const answerText = `{"answer":"TCP is reliable.","confidence":9,"checkedAt":["2026-09-27T12:00:00Z"]}`
	provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
	reply := geminiCandidateReply(textgentest.GeneratedReply{
		Text: answerText, ServedModel: "gemini-3.8-flash-001", ResponseID: "resp-gemini-1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	}, "STOP")
	reply.Header.Set(geminiRequestIDHeader, "req-gemini-1")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runGeminiGenerateText(t, newGeminiClient(t, provider.BaseURL()+"/v1beta", ""), llm.GenerateTextRequest{
		Model:        "models/gemini-3.8-flash",
		Instructions: "Answer in one sentence.",
		Messages: []textgen.Message{
			{Role: textgen.MessageRoleUser, Text: "What is TCP?"},
			{Role: textgen.MessageRoleAssistant, Text: "A transport protocol."},
			{Role: textgen.MessageRoleUser, Text: "How does it differ from UDP?"},
		},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Description: "A one-sentence answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
				"checkedAt": map[string]any{
					"type": "array", "items": map[string]any{"type": "string", "format": "date-time"}, "minItems": 1, "maxItems": 3,
				},
			},
			"required": []any{"answer", "confidence", "checkedAt"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortLow,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, answerText, result.Value.Text, "thought parts never reach the text")
	require.Equal(t, "gemini-3.8-flash", result.Value.RequestedModel, "one models/ prefix is removed")
	require.Equal(t, "gemini-3.8-flash-001", result.Value.ServedModel)
	require.Equal(t, "resp-gemini-1", result.Value.ResponseID)
	require.Equal(t, textgen.FinishReasonStop, result.Value.FinishReason)
	require.Equal(t, "STOP", result.Value.ProviderFinishReason)
	require.Equal(t, textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
	require.Equal(t, "gemini", result.Receipt.Provider)
	require.Equal(t, "resp-gemini-1", result.Receipt.ProviderObjectID)
	require.Equal(t, "req-gemini-1", result.Receipt.ProviderRequestID)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/v1beta/models/gemini-3.8-flash:generateContent", requests[0].Path, "the key never travels in the URL")
	require.Equal(t, "application/json", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels only in x-goog-api-key")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "What is TCP?"}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"text": "A transport protocol."}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "How does it differ from UDP?"}}},
		},
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "Answer in one sentence."}}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseJsonSchema": map[string]any{
				"type": "object", "description": "A one-sentence answer.",
				"properties": map[string]any{
					"answer":     map[string]any{"type": "string", "description": "(maxLength: 200)"},
					"confidence": map[string]any{"type": "integer", "minimum": float64(0), "maximum": float64(10)},
					"checkedAt": map[string]any{
						"type": "array", "items": map[string]any{"type": "string", "description": `(format: "date-time")`},
						"minItems": float64(1), "maxItems": float64(3),
					},
				},
				"required": []any{"answer", "confidence", "checkedAt"}, "additionalProperties": false,
			},
			"temperature":     0.4,
			"maxOutputTokens": float64(2048),
			"thinkingConfig":  map[string]any{"thinkingLevel": "LOW"},
		},
	}, body)
}

func TestGeminiGenerateTextMapsReasoningEffortToThinkingLevel(t *testing.T) {
	for _, testCase := range []struct {
		model  string
		effort textgen.ReasoningEffort
		level  string
	}{
		{"gemini-3.5-flash-lite", textgen.ReasoningEffortMinimal, "MINIMAL"},
		{"gemini-3.5-flash", textgen.ReasoningEffortMedium, "MEDIUM"},
		{"gemini-3.8-flash", textgen.ReasoningEffortHigh, "HIGH"},
		{"gemini-3.1-pro-preview", textgen.ReasoningEffortLow, "LOW"},
		{"gemini-4-flash", textgen.ReasoningEffortMinimal, "MINIMAL"},
		{"gemini-flash-latest", textgen.ReasoningEffortLow, "LOW"},
		{"gemini-flash-lite-latest", textgen.ReasoningEffortMinimal, "MINIMAL"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
			provider.EnqueueReplies(geminiDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			request := geminiUserRequest("Plan the migration.")
			request.Model, request.ReasoningEffort = testCase.model, testCase.effort
			result := runGeminiGenerateText(t, newGeminiClient(t, provider.BaseURL(), ""), request)
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body struct {
				GenerationConfig struct {
					ThinkingConfig map[string]any `json:"thinkingConfig"`
				} `json:"generationConfig"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, map[string]any{"thinkingLevel": testCase.level}, body.GenerationConfig.ThinkingConfig,
				"generateText sends thinkingLevel, never the legacy thinkingBudget")
		})
	}
}

// TestGeminiGenerateTextClassifiesGeminiFinishAndBlockReasons keeps generateContent's branch for every documented reason.
func TestGeminiGenerateTextClassifiesGeminiFinishAndBlockReasons(t *testing.T) {
	usage := `"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":0,"totalTokenCount":9},"modelVersion":"gemini-3.5-flash-lite","responseId":"resp-block"`
	for _, testCase := range []struct {
		name, body           string
		branch               sdkgo.BranchID
		providerFinishReason string
	}{
		{"prompt blocked for safety", `{"promptFeedback":{"blockReason":"SAFETY"},` + usage + `}`, llm.GenerateTextBranchBlocked, "blockReason:SAFETY"},
		{"prompt blocked for another reason", `{"promptFeedback":{"blockReason":"OTHER"},` + usage + `}`, llm.GenerateTextBranchBlocked, "blockReason:OTHER"},
		{"prompt blocked for prohibited content", `{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},` + usage + `}`, llm.GenerateTextBranchBlocked, "blockReason:PROHIBITED_CONTENT"},
		{"candidate stopped for recitation", geminiCandidateBody("RECITATION", "Partial"), llm.GenerateTextBranchBlocked, "RECITATION"},
		{"candidate stopped for SPII", geminiCandidateBody("SPII", ""), llm.GenerateTextBranchBlocked, "SPII"},
		{"candidate stopped for an escalation rule", geminiCandidateBody("ESCALATION", ""), llm.GenerateTextBranchBlocked, "ESCALATION"},
		{"candidate stopped for an unknown reason", geminiCandidateBody("OTHER", "Partial"), llm.GenerateTextBranchInvalidResponse, "OTHER"},
		{"malformed function call", geminiCandidateBody("MALFORMED_FUNCTION_CALL", ""), llm.GenerateTextBranchInvalidResponse, "MALFORMED_FUNCTION_CALL"},
		{"undocumented block reason", `{"promptFeedback":{"blockReason":"NEW_REASON"},` + usage + `}`, llm.GenerateTextBranchBlocked, "blockReason:UNDOCUMENTED"},
		{"ill-formed block reason", `{"promptFeedback":{"blockReason":"new reason"},` + usage + `}`, llm.GenerateTextBranchInvalidResponse, ""},
		{"no candidate", `{` + usage + `}`, llm.GenerateTextBranchInvalidResponse, ""},
		{"STOP with only thought parts", `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]},"finishReason":"STOP"}],` + usage + `}`,
			llm.GenerateTextBranchInvalidResponse, "STOP"},
		{"candidate without a finish reason", geminiCandidateBody("", "Partial"), llm.GenerateTextBranchInvalidResponse, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{Header: geminiJSONHeader(), Body: testCase.body})
			result := runGeminiGenerateText(t, newGeminiClient(t, provider.BaseURL(), ""), geminiUserRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Empty(t, result.Value.Text, "only generated and truncated carry text")
			require.Equal(t, testCase.providerFinishReason, result.Value.ProviderFinishReason)
			require.Equal(t, "gemini-3.5-flash-lite", result.Value.ServedModel, "every branch keeps the served model")
			require.Positive(t, result.Value.Usage.InputTokens, "every branch keeps the usage")
			if testCase.branch == llm.GenerateTextBranchBlocked {
				require.Equal(t, textgen.FinishReasonContentPolicy, result.Value.FinishReason)
				require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
			}
		})
	}
}

// TestGeminiGenerateTextKeepsTheResponseIdentityWithoutACandidate matches generateContent, which keeps both on invalidResponse.
func TestGeminiGenerateTextKeepsTheResponseIdentityWithoutACandidate(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{Header: geminiJSONHeader(), Body: `{"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9},` +
		`"modelVersion":"gemini-3.5-flash-lite-001","responseId":"resp-no-candidate"}`})
	result := runGeminiGenerateText(t, newGeminiClient(t, provider.BaseURL(), ""), geminiUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Equal(t, "provider returned no recognizable finish reason", result.Failure.Message)
	require.Equal(t, "gemini-3.5-flash-lite-001", result.Value.ServedModel)
	require.Equal(t, "resp-no-candidate", result.Value.ResponseID)
	require.Equal(t, textgen.Usage{InputTokens: 9, TotalTokens: 9}, result.Value.Usage)
	require.Equal(t, "resp-no-candidate", result.Receipt.ProviderObjectID)
}

func TestGeminiGenerateTextHonorsGoogleRetryInfo(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(statusCode), func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{StatusCode: statusCode, Header: http.Header{
				"Content-Type": {"application/json"}, "Retry-After": {"5"},
			}, Body: googleErrorBody(statusCode, googleStatusName(statusCode),
				`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"37s"}`)})
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-"+t.Name(), "step-"+t.Name()),
				newGeminiClient(t, provider.BaseURL(), "").GenerateText(), geminiTestConnection, geminiUserRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, "provider returned HTTP "+fmt.Sprint(statusCode)+" "+googleStatusName(statusCode), retryError.Failure.Message)
			var retryAfter *dex.RetryAfterError
			require.True(t, errors.As(err, &retryAfter), "RetryInfo schedules the Dex retry")
			require.Equal(t, 37*time.Second, retryAfter.After, "RetryInfo replaces Retry-After")
		})
	}
}

func TestGeminiGenerateTextClassifiesAnInvalidAPIKeyAsAuthentication(t *testing.T) {
	invalidKey := googleErrorBody(http.StatusBadRequest, "INVALID_ARGUMENT",
		`{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID","domain":"googleapis.com"}`)
	for name, body := range map[string]string{"object": invalidKey, "array-wrapped": "[" + invalidKey + "]"} {
		t.Run(name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, geminiDialect.CredentialHeader, geminiTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{StatusCode: http.StatusBadRequest, Header: geminiJSONHeader(), Body: body})
			result := runGeminiGenerateText(t, newGeminiClient(t, provider.BaseURL(), ""), geminiUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "provider returned HTTP 400 INVALID_ARGUMENT API_KEY_INVALID", result.Failure.Message)
			require.NotContains(t, result.Failure.Message, geminiProviderSentinel)
		})
	}
}

func newGeminiGenerateTextQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newGeminiFakeProviderClient(t, connection).GenerateText()
}

// newGeminiFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newGeminiFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderGemini, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newGeminiClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderGemini, Model: model}, sdkgo.StaticCredentialProvider[llm.Credentials]{
		geminiTestConnection: {APIKey: sdkgo.NewSecretString(geminiTestAPIKey)},
	}, llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func runGeminiGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("gemini-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("gemini-flow", stepExecutionID), client.GenerateText(), geminiTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), geminiTestAPIKey), "a Result contains the API key")
	return result
}

func geminiUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}

func geminiCandidateBody(finishReason string, text string) string {
	parts := `[]`
	if text != "" {
		parts = `[{"text":` + quoteGeminiJSON(text) + `}]`
	}
	return `{"candidates":[{"content":{"role":"model","parts":` + parts + `},"finishReason":` + quoteGeminiJSON(finishReason) + `}],` +
		`"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":1,"totalTokenCount":10},"modelVersion":"gemini-3.5-flash-lite"}`
}

// googleErrorBody renders a google.rpc error whose message echoes the key, which must stay out of Failures.
func googleErrorBody(code int, status string, details ...string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s echoed key %s","status":"%s","details":[%s]}}`,
		code, geminiProviderSentinel, geminiTestAPIKey, status, strings.Join(details, ","))
}

func quoteGeminiJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

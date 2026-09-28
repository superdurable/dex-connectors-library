// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  geminiDialect,
		NewQuery: newGeminiGenerateTextQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none, which Gemini 3 thinking levels cannot express", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "extra-high reasoning effort", Request: llm.TextGenerationRequest{ReasoningEffort: llm.ReasoningEffortExtraHigh}},
			{Name: "minimal thinking on gemini-3.8-flash", Request: llm.TextGenerationRequest{
				Model: "gemini-3.8-flash", ReasoningEffort: llm.ReasoningEffortMinimal,
			}},
			{Name: "minimal thinking on gemini-flash-latest, which follows the newest Flash", Request: llm.TextGenerationRequest{
				Model: "gemini-flash-latest", ReasoningEffort: llm.ReasoningEffortMinimal,
			}},
			{Name: "thinking level on a Gemini 2.5 model", Request: llm.TextGenerationRequest{
				Model: "gemini-2.5-flash", ReasoningEffort: llm.ReasoningEffortLow,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedGenerateContentRequest pins the REST body and the response mapping.
func TestGenerateTextSendsTheDocumentedGenerateContentRequest(t *testing.T) {
	const answerText = `{"answer":"TCP is reliable.","confidence":9,"checkedAt":["2026-09-27T12:00:00Z"]}`
	provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
	reply := geminiCandidateReply(llmtest.GeneratedReply{
		Text: answerText, ServedModel: "gemini-3.8-flash-001", ResponseID: "resp-gemini-1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	}, "STOP")
	reply.Header.Set(geminiRequestIDHeader, "req-gemini-1")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL()+"/v1beta", ""), gemini.GenerateTextRequest{
		Model:        "models/gemini-3.8-flash",
		Instructions: "Answer in one sentence.",
		Messages: []llm.Message{
			{Role: llm.MessageRoleUser, Text: "What is TCP?"},
			{Role: llm.MessageRoleAssistant, Text: "A transport protocol."},
			{Role: llm.MessageRoleUser, Text: "How does it differ from UDP?"},
		},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Description: "A one-sentence answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
				"checkedAt": map[string]any{
					"type": "array", "items": map[string]any{"type": "string", "format": "date-time"}, "minItems": 1, "maxItems": 3,
				},
			},
			"required": []any{"answer", "confidence", "checkedAt"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortLow,
	})

	require.Equal(t, gemini.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, answerText, result.Value.Text, "thought parts never reach the text")
	require.Equal(t, "gemini-3.8-flash", result.Value.RequestedModel, "one models/ prefix is removed")
	require.Equal(t, "gemini-3.8-flash-001", result.Value.ServedModel)
	require.Equal(t, "resp-gemini-1", result.Value.ResponseID)
	require.Equal(t, llm.FinishReasonStop, result.Value.FinishReason)
	require.Equal(t, "STOP", result.Value.ProviderFinishReason)
	require.Equal(t, llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
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

func TestGenerateTextMapsReasoningEffortToThinkingLevel(t *testing.T) {
	for _, testCase := range []struct {
		model  string
		effort llm.ReasoningEffort
		level  string
	}{
		{"gemini-3.5-flash-lite", llm.ReasoningEffortMinimal, "MINIMAL"},
		{"gemini-3.5-flash", llm.ReasoningEffortMedium, "MEDIUM"},
		{"gemini-3.8-flash", llm.ReasoningEffortHigh, "HIGH"},
		{"gemini-3.1-pro-preview", llm.ReasoningEffortLow, "LOW"},
		{"gemini-4-flash", llm.ReasoningEffortMinimal, "MINIMAL"},
		{"gemini-flash-latest", llm.ReasoningEffortLow, "LOW"},
		{"gemini-flash-lite-latest", llm.ReasoningEffortMinimal, "MINIMAL"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(geminiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			request := userTextRequest("Plan the migration.")
			request.Model, request.ReasoningEffort = testCase.model, testCase.effort
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), request)
			require.Equal(t, gemini.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestGenerateTextClassifiesGeminiFinishAndBlockReasons keeps generateContent's branch for every documented reason.
func TestGenerateTextClassifiesGeminiFinishAndBlockReasons(t *testing.T) {
	usage := `"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":0,"totalTokenCount":9},"modelVersion":"gemini-3.5-flash-lite","responseId":"resp-block"`
	for _, testCase := range []struct {
		name, body           string
		branch               sdkgo.BranchID
		providerFinishReason string
	}{
		{"prompt blocked for safety", `{"promptFeedback":{"blockReason":"SAFETY"},` + usage + `}`, gemini.GenerateTextBranchBlocked, "blockReason:SAFETY"},
		{"prompt blocked for another reason", `{"promptFeedback":{"blockReason":"OTHER"},` + usage + `}`, gemini.GenerateTextBranchBlocked, "blockReason:OTHER"},
		{"prompt blocked for prohibited content", `{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},` + usage + `}`, gemini.GenerateTextBranchBlocked, "blockReason:PROHIBITED_CONTENT"},
		{"candidate stopped for recitation", candidateBody("RECITATION", "Partial"), gemini.GenerateTextBranchBlocked, "RECITATION"},
		{"candidate stopped for SPII", candidateBody("SPII", ""), gemini.GenerateTextBranchBlocked, "SPII"},
		{"candidate stopped for an escalation rule", candidateBody("ESCALATION", ""), gemini.GenerateTextBranchBlocked, "ESCALATION"},
		{"candidate stopped for an unknown reason", candidateBody("OTHER", "Partial"), gemini.GenerateTextBranchInvalidResponse, "OTHER"},
		{"malformed function call", candidateBody("MALFORMED_FUNCTION_CALL", ""), gemini.GenerateTextBranchInvalidResponse, "MALFORMED_FUNCTION_CALL"},
		{"undocumented block reason", `{"promptFeedback":{"blockReason":"NEW_REASON"},` + usage + `}`, gemini.GenerateTextBranchBlocked, "blockReason:UNDOCUMENTED"},
		{"ill-formed block reason", `{"promptFeedback":{"blockReason":"new reason"},` + usage + `}`, gemini.GenerateTextBranchInvalidResponse, ""},
		{"no candidate", `{` + usage + `}`, gemini.GenerateTextBranchInvalidResponse, ""},
		{"STOP with only thought parts", `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]},"finishReason":"STOP"}],` + usage + `}`,
			gemini.GenerateTextBranchInvalidResponse, "STOP"},
		{"candidate without a finish reason", candidateBody("", "Partial"), gemini.GenerateTextBranchInvalidResponse, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{Header: jsonHeader(), Body: testCase.body})
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), userTextRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Empty(t, result.Value.Text, "only generated and truncated carry text")
			require.Equal(t, testCase.providerFinishReason, result.Value.ProviderFinishReason)
			require.Equal(t, "gemini-3.5-flash-lite", result.Value.ServedModel, "every branch keeps the served model")
			require.Positive(t, result.Value.Usage.InputTokens, "every branch keeps the usage")
			if testCase.branch == gemini.GenerateTextBranchBlocked {
				require.Equal(t, llm.FinishReasonContentPolicy, result.Value.FinishReason)
				require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
			}
		})
	}
}

// TestGenerateTextKeepsTheResponseIdentityWithoutACandidate matches generateContent, which keeps both on invalidResponse.
func TestGenerateTextKeepsTheResponseIdentityWithoutACandidate(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(llmtest.FakeReply{Header: jsonHeader(), Body: `{"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9},` +
		`"modelVersion":"gemini-3.5-flash-lite-001","responseId":"resp-no-candidate"}`})
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), userTextRequest("Hello"))
	require.Equal(t, gemini.GenerateTextBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Equal(t, "provider returned no recognizable finish reason", result.Failure.Message)
	require.Equal(t, "gemini-3.5-flash-lite-001", result.Value.ServedModel)
	require.Equal(t, "resp-no-candidate", result.Value.ResponseID)
	require.Equal(t, llm.Usage{InputTokens: 9, TotalTokens: 9}, result.Value.Usage)
	require.Equal(t, "resp-no-candidate", result.Receipt.ProviderObjectID)
}

func TestGenerateTextHonorsGoogleRetryInfo(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(statusCode), func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{StatusCode: statusCode, Header: http.Header{
				"Content-Type": {"application/json"}, "Retry-After": {"5"},
			}, Body: googleErrorBody(statusCode, googleStatusName(statusCode),
				`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"37s"}`)})
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-"+t.Name(), "step-"+t.Name()),
				newGenerateTextClient(t, provider.BaseURL(), "").GenerateText(), geminiConnection, userTextRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, "provider returned HTTP "+fmt.Sprint(statusCode)+" "+googleStatusName(statusCode), retryError.Failure.Message)
			var retryAfter *dex.RetryAfterError
			require.True(t, errors.As(err, &retryAfter), "RetryInfo schedules the Dex retry")
			require.Equal(t, 37*time.Second, retryAfter.After, "RetryInfo replaces Retry-After")
		})
	}
}

func TestGenerateTextClassifiesAnInvalidAPIKeyAsAuthentication(t *testing.T) {
	invalidKey := googleErrorBody(http.StatusBadRequest, "INVALID_ARGUMENT",
		`{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID","domain":"googleapis.com"}`)
	for name, body := range map[string]string{"object": invalidKey, "array-wrapped": "[" + invalidKey + "]"} {
		t.Run(name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, geminiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{StatusCode: http.StatusBadRequest, Header: jsonHeader(), Body: body})
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), userTextRequest("Hello"))
			require.Equal(t, gemini.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "provider returned HTTP 400 INVALID_ARGUMENT API_KEY_INVALID", result.Failure.Message)
			require.NotContains(t, result.Failure.Message, providerSentinel)
		})
	}
}

// TestGenerateTextCallsTheGeminiAPIHost proves the production URL and default model without a network call.
func TestGenerateTextCallsTheGeminiAPIHost(t *testing.T) {
	reply := geminiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "gemini-3.5-flash-lite"})
	var requestedURLs []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
		}, nil
	})
	client, err := gemini.New(gemini.Config{}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		geminiConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, gemini.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result := runGenerateText(t, client, userTextRequest("Hello"))
	require.Equal(t, gemini.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, gemini.DefaultConfig().Model, result.Value.RequestedModel, "a blank connection model uses the manifest default")
	require.Equal(t, []string{"https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent"}, requestedURLs)
}

// TestNewBoundsAnHTTPClientTimeoutThatOutlastsGenerateText keeps v0.2.0 callers with long client timeouts working.
func TestNewBoundsAnHTTPClientTimeoutThatOutlastsGenerateText(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[gemini.Credentials]{}
	for _, timeout := range []time.Duration{600 * time.Second, 900 * time.Second, 20 * time.Minute} {
		callerClient := &http.Client{Timeout: timeout}
		_, err := gemini.New(gemini.Config{}, credentials, gemini.WithHTTPClient(callerClient))
		require.NoError(t, err, timeout)
		require.Equal(t, timeout, callerClient.Timeout, "the connector bounds its own copy")
	}
}

func newGeminiGenerateTextQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *gemini.Client {
	t.Helper()
	client, err := gemini.New(gemini.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[gemini.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}

func newGenerateTextClient(t *testing.T, endpoint string, model string) *gemini.Client {
	t.Helper()
	return newTestClientWithConfig(t, gemini.Config{Endpoint: endpoint, Model: model})
}

func runGenerateText(t *testing.T, client *gemini.Client, request gemini.GenerateTextRequest) gemini.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("gemini-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("gemini-flow", stepExecutionID), client.GenerateText(), geminiConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userTextRequest(text string) gemini.GenerateTextRequest {
	return gemini.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

func candidateBody(finishReason string, text string) string {
	parts := `[]`
	if text != "" {
		parts = `[{"text":` + quoteJSON(text) + `}]`
	}
	return `{"candidates":[{"content":{"role":"model","parts":` + parts + `},"finishReason":` + quoteJSON(finishReason) + `}],` +
		`"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":1,"totalTokenCount":10},"modelVersion":"gemini-3.5-flash-lite"}`
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

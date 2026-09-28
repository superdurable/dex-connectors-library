// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

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
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIKey          = "sk-pipeline-test-key"
	testConnectionModel = "test-model"
)

var (
	testCredentialHeader = llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}
	eventStream          = http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}
	testConnection       = sdkgo.ConnectionRef{Provider: "test", Name: "pipeline"}
	testDefinition       = sdkgo.QueryDefinition{
		Operation:    sdkgo.OperationRef{ConnectorID: "test-lab", OperationID: llm.TextGenerationOperationID},
		Branches:     llm.TextGenerationBranchDefinitions(),
		StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync},
	}
)

// testResponse is the JSON body the test wire format decodes.
type testResponse struct {
	Text      string          `json:"text,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
	Finish    string          `json:"finish,omitempty"`
	Model     string          `json:"model,omitempty"`
	ID        string          `json:"id,omitempty"`
	IsRefusal bool            `json:"refusal,omitempty"`
	Usage     llm.Usage       `json:"usage"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// newTestWireFormat declares every feature and sends the validated request as JSON to /generate.
func newTestWireFormat(isStreaming bool) llm.WireFormat {
	return llm.WireFormat{
		ProviderName: "test-lab",
		ModelIDRule:  llm.ModelIDRuleBody,
		Features: llm.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true, SupportsTemperature: true,
			SupportsReasoningEffort: true,
		},
		CredentialHeader: testCredentialHeader,
		RulesForModel: func(model string) llm.ModelRequestRules {
			rules := llm.ModelRequestRules{
				Temperature:      llm.TemperatureRange(0, 1),
				ReasoningEfforts: map[llm.ReasoningEffort]string{llm.ReasoningEffortLow: "wire-low"},
				StructuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
			}
			if strings.HasPrefix(model, "fixed-") {
				rules.Temperature = llm.TemperatureNotAccepted()
			}
			return rules
		},
		EncodeRequest: func(input llm.EncodeRequestInput) (llm.EncodedRequest, error) {
			body, err := json.Marshal(input.Request)
			if err != nil {
				return llm.EncodedRequest{}, err
			}
			return llm.EncodedRequest{Path: "/generate", Body: body, IsStreaming: isStreaming}, nil
		},
		DecodeResponse: func(body []byte) (llm.DecodedResponse, error) {
			var response testResponse
			if err := json.Unmarshal(body, &response); err != nil {
				return llm.DecodedResponse{}, err
			}
			if len(response.Error) > 0 {
				return llm.DecodedResponse{}, &llm.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, []string{"/error/code"})}
			}
			return llm.DecodedResponse{
				Parts:       []llm.ResponsePart{{Text: response.Reasoning, IsReasoning: true}, {Text: response.Text}},
				ServedModel: response.Model, ResponseID: response.ID, ProviderFinishReason: response.Finish,
				IsRefusal: response.IsRefusal, Usage: response.Usage,
			}, nil
		},
		DecodeStream:       decodeTestStream,
		FinishReasons:      map[string]llm.FinishReason{"done": llm.FinishReasonStop, "cut": llm.FinishReasonLength, "filtered": llm.FinishReasonContentPolicy},
		ErrorTokenPointers: []string{"/error/code"},
		RequestIDHeaders:   []string{"x-first-request-id", "x-request-id"},
		RateLimitHeaders:   []string{"x-ratelimit-remaining", "x-ratelimit-echo"},
	}
}

func decodeTestStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(string) error) (llm.DecodedResponse, error) {
	var decoded llm.DecodedResponse
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			return llm.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return llm.DecodedResponse{}, fmt.Errorf("read test stream: %w", err)
		}
		var chunk testResponse
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return llm.DecodedResponse{}, err
		}
		if len(chunk.Error) > 0 {
			return llm.DecodedResponse{}, &llm.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens([]byte(event.Data), []string{"/error/code"})}
		}
		if chunk.Text != "" {
			decoded.Parts = append(decoded.Parts, llm.ResponsePart{Text: chunk.Text})
			if err := writeTextDelta(chunk.Text); err != nil {
				return llm.DecodedResponse{}, err
			}
		}
		if chunk.Finish != "" {
			decoded.ProviderFinishReason = chunk.Finish
			return decoded, nil
		}
	}
}

type pipelineTest struct {
	provider *llmtest.FakeProvider
	query    *llm.TextGenerationQuery
}

func newPipelineTest(t *testing.T, configure func(*llm.TextGenerationQueryConfig)) *pipelineTest {
	t.Helper()
	provider := llmtest.NewFakeProvider(t, testCredentialHeader, testAPIKey)
	config := &llm.TextGenerationQueryConfig{
		Definition: testDefinition, WireFormat: newTestWireFormat(false), BaseURL: provider.BaseURL() + "/v1/",
		ConnectionModel: " " + testConnectionModel + " ", RequestTimeout: 10 * time.Second,
		ResolveCredential: func(sdkgo.Call) (sdkgo.SecretString, error) { return sdkgo.NewSecretString(testAPIKey), nil },
		MaxResponseBytes:  1 << 16, MaxStreamEventBytes: 1 << 12,
	}
	if configure != nil {
		configure(config)
	}
	query, err := llm.NewTextGenerationQuery(config)
	require.NoError(t, err)
	return &pipelineTest{provider: provider, query: query}
}

func (test *pipelineTest) invoke(t *testing.T, request llm.TextGenerationRequest) (sdkgo.QueryResult[llm.TextGenerationResponse], error) {
	t.Helper()
	ctx := testsupport.NewDexContext("pipeline-flow", fmt.Sprintf("step-%d", time.Now().UnixNano()))
	return sdkgo.RunQuery(ctx, test.query, testConnection, request)
}

func (test *pipelineTest) requireBranch(t *testing.T, request llm.TextGenerationRequest, branch sdkgo.BranchID, kind sdkgo.FailureKind) sdkgo.QueryResult[llm.TextGenerationResponse] {
	t.Helper()
	result, err := test.invoke(t, request)
	require.NoError(t, err)
	require.Equal(t, branch, result.Branch, "failure: %+v", result.Failure)
	if kind == "" {
		require.Nil(t, result.Failure)
	} else {
		require.NotNil(t, result.Failure)
		require.Equal(t, kind, result.Failure.Kind, result.Failure.Message)
	}
	return result
}

func (test *pipelineTest) requireRetry(t *testing.T, request llm.TextGenerationRequest, kind sdkgo.FailureKind) (sdkgo.Failure, time.Duration) {
	t.Helper()
	_, err := test.invoke(t, request)
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, kind, retryError.Failure.Kind, retryError.Failure.Message)
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		return retryError.Failure, retryAfter.After
	}
	return retryError.Failure, 0
}

func userRequest() llm.TextGenerationRequest {
	return llm.TextGenerationRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "hello"}}}
}

func jsonReply(statusCode int, header http.Header, body any) llmtest.FakeReply {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return llmtest.FakeReply{StatusCode: statusCode, Header: header, Body: string(encoded)}
}

func doneReply(text string) llmtest.FakeReply {
	return jsonReply(http.StatusOK, nil, testResponse{Text: text, Reasoning: "hidden reasoning", Finish: "done", Model: "served", ID: "resp-1"})
}

func TestTextGenerationQueryRunsThePipeline(t *testing.T) {
	test := newPipelineTest(t, nil)
	test.provider.EnqueueReplies(jsonReply(http.StatusOK, http.Header{
		"X-Request-Id": {"req-2"}, "X-Ratelimit-Remaining": {"41"}, "X-Ratelimit-Echo": {"has " + testAPIKey},
		"X-Unlisted": {"dropped"},
	}, testResponse{
		Text: "answer", Reasoning: "hidden reasoning", Finish: "done", Model: "served-model", ID: "resp-1",
		Usage: llm.Usage{InputTokens: 5, CachedInputTokens: 1, OutputTokens: 3, ReasoningTokens: 1},
	}))
	temperature := 0.25
	request := llm.TextGenerationRequest{
		Instructions: "Be brief.", Messages: []llm.Message{
			{Role: llm.MessageRoleUser, Text: "hi"}, {Role: llm.MessageRoleAssistant, Text: "hello"}, {Role: llm.MessageRoleUser, Text: "and?"},
		},
		MaxOutputTokens: 32, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortLow,
	}
	result := test.requireBranch(t, request, llm.GeneratedBranchID, "")
	require.Equal(t, llm.TextGenerationResponse{
		Text: "answer", RequestedModel: testConnectionModel, ServedModel: "served-model", ResponseID: "resp-1",
		FinishReason: llm.FinishReasonStop, ProviderFinishReason: "done",
		Usage: llm.Usage{InputTokens: 5, CachedInputTokens: 1, OutputTokens: 3, ReasoningTokens: 1, TotalTokens: 8},
	}, result.Value)
	require.Equal(t, "test-lab", result.Receipt.Provider)
	require.Equal(t, "resp-1", result.Receipt.ProviderObjectID)
	require.Equal(t, "req-2", result.Receipt.ProviderRequestID)
	require.Equal(t, map[string]string{"x-ratelimit-remaining": "41"}, result.Receipt.Metadata)

	requests := test.provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1/generate", requests[0].Path)
	require.True(t, requests[0].HasCredentialInSlot)
	require.False(t, requests[0].HasCredentialOutsideSlot)
	require.Equal(t, "application/json", requests[0].Header.Get("Content-Type"))
	var sent llm.TextGenerationRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &sent))
	require.Equal(t, testConnectionModel, sent.Model, "the connection model is trimmed and sent")
	require.Equal(t, request.Messages, sent.Messages)
}

func TestTextGenerationQueryResolvesModelPrecedence(t *testing.T) {
	test := newPipelineTest(t, nil)
	test.provider.EnqueueReplies(doneReply("one"))
	request := userRequest()
	request.Model = "\trequest-model "
	require.Equal(t, "request-model", test.requireBranch(t, request, llm.GeneratedBranchID, "").Value.RequestedModel)

	for _, connectionModel := range []string{"", " \t "} {
		blank := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) { config.ConnectionModel = connectionModel })
		result := blank.requireBranch(t, userRequest(), llm.DefectBranchID, sdkgo.FailureValidation)
		require.Contains(t, result.Failure.Message, "no model is selected", "a blank connection model %q is unset", connectionModel)
		require.Empty(t, blank.provider.Requests())
	}

	require.Equal(t, "request", llm.ResolveModel(" request ", "connection"))
	require.Equal(t, "connection", llm.ResolveModel(" \t", "connection"))
	require.Empty(t, llm.ResolveModel("", ""))
}

func TestTextGenerationQuerySelectsDefectWithoutARequest(t *testing.T) {
	temperature, outOfRange, negative := 0.5, 1.5, -0.1
	cases := map[string]struct {
		configure func(*llm.TextGenerationQueryConfig)
		request   func(*llm.TextGenerationRequest)
		kind      sdkgo.FailureKind
		message   string
	}{
		"no messages":            {request: func(request *llm.TextGenerationRequest) { request.Messages = nil }, message: "at least one message"},
		"unknown role":           {request: func(request *llm.TextGenerationRequest) { request.Messages[0].Role = "system" }, message: "role"},
		"empty message":          {request: func(request *llm.TextGenerationRequest) { request.Messages[0].Text = "" }, message: "requires text"},
		"negative token limit":   {request: func(request *llm.TextGenerationRequest) { request.MaxOutputTokens = -1 }, message: "maxOutputTokens"},
		"temperature over range": {request: func(request *llm.TextGenerationRequest) { request.Temperature = &outOfRange }, message: "from 0 through 1"},
		"negative temperature":   {request: func(request *llm.TextGenerationRequest) { request.Temperature = &negative }, message: "non-negative"},
		"temperature on a fixed model": {request: func(request *llm.TextGenerationRequest) {
			request.Model, request.Temperature = "fixed-sampler", &temperature
		}, message: "does not accept a temperature"},
		"unmapped effort": {request: func(request *llm.TextGenerationRequest) { request.ReasoningEffort = llm.ReasoningEffortHigh }, message: `"high"`},
		"unknown effort":  {request: func(request *llm.TextGenerationRequest) { request.ReasoningEffort = "ultra" }, message: "must be none"},
		"undeclared feature": {
			configure: func(config *llm.TextGenerationQueryConfig) { config.WireFormat.Features.SupportsTemperature = false },
			request:   func(request *llm.TextGenerationRequest) { request.Temperature = &temperature }, message: "Temperature",
		},
		"credential unavailable": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.ResolveCredential = func(sdkgo.Call) (sdkgo.SecretString, error) {
					return sdkgo.SecretString{}, errors.New("vault is down")
				}
			},
			kind: sdkgo.FailureAuthentication, message: "unavailable",
		},
		"credential not header safe": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.ResolveCredential = func(sdkgo.Call) (sdkgo.SecretString, error) {
					return sdkgo.NewSecretString("sk-line\r\nX-Injected: 1"), nil
				}
			},
			kind: sdkgo.FailureAuthentication, message: "invalid",
		},
		"encode failure": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.WireFormat.EncodeRequest = func(llm.EncodeRequestInput) (llm.EncodedRequest, error) {
					return llm.EncodedRequest{}, errors.New("field is not accepted")
				}
			},
			message: "field is not accepted",
		},
		"encoded request sets the credential header": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.WireFormat.EncodeRequest = func(llm.EncodeRequestInput) (llm.EncodedRequest, error) {
					return llm.EncodedRequest{Path: "/generate", Header: http.Header{"Authorization": {"Bearer other"}}}, nil
				}
			},
			kind: sdkgo.FailureLocalDefect, message: "reserved",
		},
		"encoded path is relative": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.WireFormat.EncodeRequest = func(llm.EncodeRequestInput) (llm.EncodedRequest, error) {
					return llm.EncodedRequest{Path: "generate"}, nil
				}
			},
			kind: sdkgo.FailureLocalDefect, message: "start with /",
		},
		"stream without a stream decoder": {
			configure: func(config *llm.TextGenerationQueryConfig) {
				config.WireFormat = newTestWireFormat(true)
				config.WireFormat.DecodeStream = nil
			},
			kind: sdkgo.FailureLocalDefect, message: "cannot decode",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			test := newPipelineTest(t, testCase.configure)
			request := userRequest()
			if testCase.request != nil {
				testCase.request(&request)
			}
			kind := testCase.kind
			if kind == "" {
				kind = sdkgo.FailureValidation
			}
			result := test.requireBranch(t, request, llm.DefectBranchID, kind)
			require.Contains(t, result.Failure.Message, testCase.message)
			require.NotContains(t, result.Failure.Message, testAPIKey)
			require.Equal(t, llm.ResolveModel(request.Model, testConnectionModel), result.Value.RequestedModel,
				"RequestedModel is set once the model is valid")
			require.Empty(t, test.provider.Requests())
		})
	}
}

func TestTextGenerationQueryClassifiesErrorStatuses(t *testing.T) {
	errorBody := func(code string) map[string]any {
		return map[string]any{"error": map[string]any{"code": code, "message": "secret provider text"}}
	}
	cases := []struct {
		name       string
		rules      []llm.ErrorRule
		statusCode int
		code       string
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
	}{
		{name: "408 retries", statusCode: 408, kind: sdkgo.FailureAvailability},
		{name: "429 retries", statusCode: 429, kind: sdkgo.FailureRateLimit},
		{name: "500 retries", statusCode: 500, kind: sdkgo.FailureAvailability},
		{name: "501 is a rejection", statusCode: 501, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureProviderRejection},
		{name: "400 is a rejection", statusCode: 400, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureProviderRejection},
		{name: "401 is authentication", statusCode: 401, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureAuthentication},
		{name: "402 is quota", statusCode: 402, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureQuotaExhausted},
		{name: "403 is authorization", statusCode: 403, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureAuthorization},
		{name: "404 is not found", statusCode: 404, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureNotFound},
		{name: "409 is conflict", statusCode: 409, branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureConflict},
		{name: "a redirect is never followed", statusCode: 307, branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{
			name: "a token rule overrides the status default", statusCode: 429, code: "billing_exhausted",
			rules:  []llm.ErrorRule{{StatusCode: 429, ErrorToken: "billing_exhausted", Outcome: llm.QuotaExhaustedOutcome()}},
			branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureQuotaExhausted,
		},
		{
			name: "the first matching rule wins", statusCode: 400, code: "overloaded",
			rules: []llm.ErrorRule{
				{ErrorToken: "overloaded", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
				{StatusCode: 400, Outcome: llm.InvalidResponseOutcome()},
			},
			kind: sdkgo.FailureAvailability,
		},
		{
			name: "a status rule without a token matches any body", statusCode: 504, code: "gateway_timeout",
			rules:  []llm.ErrorRule{{StatusCode: 504, Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureAvailability)}},
			branch: llm.ProviderRejectedBranchID, kind: sdkgo.FailureAvailability,
		},
		{
			name: "a content-policy rule selects blocked", statusCode: 400, code: "content_filter",
			rules:  []llm.ErrorRule{{StatusCode: 400, ErrorToken: "content_filter", Outcome: llm.BlockedOutcome()}},
			branch: llm.BlockedBranchID, kind: sdkgo.FailureProviderRejection,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			test := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) { config.WireFormat.ErrorRules = testCase.rules })
			test.provider.EnqueueReplies(jsonReply(testCase.statusCode, http.Header{"Location": {"https://elsewhere.example"}}, errorBody(testCase.code)))
			if testCase.branch == "" {
				failure, _ := test.requireRetry(t, userRequest(), testCase.kind)
				require.NotContains(t, failure.Message, "secret provider text")
			} else {
				result := test.requireBranch(t, userRequest(), testCase.branch, testCase.kind)
				require.Contains(t, result.Failure.Message, fmt.Sprintf("HTTP %d", testCase.statusCode))
				if testCase.code != "" {
					require.Contains(t, result.Failure.Message, testCase.code)
				}
				require.NotContains(t, result.Failure.Message, "secret provider text")
				if testCase.branch == llm.BlockedBranchID {
					require.Equal(t, llm.FinishReasonContentPolicy, result.Value.FinishReason)
					require.Empty(t, result.Value.Text)
				}
			}
			require.Len(t, test.provider.Requests(), 1, "a redirect is never followed")
		})
	}
}

func TestTextGenerationQueryPrefersTheProviderRetryDelay(t *testing.T) {
	test := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat.ReadErrorRetryDelay = func(body []byte) time.Duration {
			if strings.Contains(string(body), "wait-long") {
				return 48 * time.Hour
			}
			return 3 * time.Second
		}
	})
	retryAfter := http.Header{"Retry-After": {"20"}}
	test.provider.EnqueueReplies(
		jsonReply(503, retryAfter, map[string]any{"error": map[string]any{"code": "wait"}}),
		jsonReply(503, retryAfter, map[string]any{"error": map[string]any{"code": "wait-long"}}),
	)
	_, delay := test.requireRetry(t, userRequest(), sdkgo.FailureAvailability)
	require.Equal(t, 3*time.Second, delay)
	_, delay = test.requireRetry(t, userRequest(), sdkgo.FailureAvailability)
	require.Equal(t, time.Hour, delay, "provider delays are capped at one hour")
}

func TestTextGenerationQueryClassifiesProviderReportedErrors(t *testing.T) {
	test := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat.ErrorRules = []llm.ErrorRule{
			{StatusCode: 500, ErrorToken: "overloaded", Outcome: llm.QuotaExhaustedOutcome()},
			{ErrorToken: "overloaded", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
		}
	})
	test.provider.EnqueueReplies(
		jsonReply(http.StatusOK, nil, map[string]any{"error": map[string]any{"code": "overloaded", "message": "secret provider text"}}),
		jsonReply(http.StatusOK, nil, map[string]any{"error": map[string]any{"code": "mystery", "message": "secret provider text"}}),
		jsonReply(http.StatusOK, nil, map[string]any{"error": map[string]any{"code": testAPIKey}}),
	)
	failure, _ := test.requireRetry(t, userRequest(), sdkgo.FailureAvailability)
	require.Contains(t, failure.Message, "overloaded", "only status-independent rules match a 2xx error")
	result := test.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Equal(t, "provider reported an error inside a 2xx response mystery", result.Failure.Message)
	result = test.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.NotContains(t, result.Failure.Message, testAPIKey)
}

func TestTextGenerationQueryClassifiesSuccessfulResponses(t *testing.T) {
	cases := []struct {
		name     string
		response testResponse
		body     string
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
		text     string
		finish   llm.FinishReason
	}{
		{name: "truncated keeps partial text", response: testResponse{Text: "par", Finish: "cut"},
			branch: llm.TruncatedBranchID, kind: sdkgo.FailureResponseTooLarge, text: "par", finish: llm.FinishReasonLength},
		{name: "content policy drops text", response: testResponse{Text: "partial", Finish: "filtered"},
			branch: llm.BlockedBranchID, kind: sdkgo.FailureProviderRejection, finish: llm.FinishReasonContentPolicy},
		{name: "refusal is blocked", response: testResponse{Text: "no", Finish: "done", IsRefusal: true},
			branch: llm.BlockedBranchID, kind: sdkgo.FailureProviderRejection, finish: llm.FinishReasonRefusal},
		{name: "unknown finish token", response: testResponse{Text: "x", Finish: "tool_calls"},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "missing finish token", response: testResponse{Text: "x"},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "unbounded finish token", response: testResponse{Text: "x", Finish: "done because I said so"},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "stop without text", response: testResponse{Reasoning: "only thinking", Finish: "done"},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol, finish: llm.FinishReasonStop},
		{name: "served model with spaces", response: testResponse{Text: "x", Finish: "done", Model: "a model"},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "served model holds the credential", response: testResponse{Text: "x", Finish: "done", Model: "echo-" + testAPIKey},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "response ID holds the credential", response: testResponse{Text: "x", Finish: "done", ID: testAPIKey},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "finish token holds the credential", response: testResponse{Text: "x", Finish: testAPIKey},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "negative usage", response: testResponse{Text: "x", Finish: "done", Usage: llm.Usage{OutputTokens: -1}},
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol, finish: ""},
		{name: "not JSON", body: "<html>ok</html>", branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureProtocol},
		{name: "oversized", body: `{"text":"` + strings.Repeat("x", 1<<16) + `","finish":"done"}`,
			branch: llm.InvalidResponseBranchID, kind: sdkgo.FailureResponseTooLarge},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			test := newPipelineTest(t, nil)
			reply := jsonReply(http.StatusOK, nil, testCase.response)
			if testCase.body != "" {
				reply.Body = testCase.body
			}
			test.provider.EnqueueReplies(reply)
			result := test.requireBranch(t, userRequest(), testCase.branch, testCase.kind)
			require.Equal(t, testCase.text, result.Value.Text)
			require.Equal(t, testCase.finish, result.Value.FinishReason)
			require.Equal(t, testConnectionModel, result.Value.RequestedModel)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), testAPIKey, "provider fields never carry the credential into a Result")
		})
	}
}

func TestTextGenerationQueryDecodesACompleteBodyForAStreamingRequest(t *testing.T) {
	streaming := func(config *llm.TextGenerationQueryConfig) { config.WireFormat = newTestWireFormat(true) }
	test := newPipelineTest(t, streaming)
	applicationJSON := http.Header{"Content-Type": {"application/json"}}
	test.provider.EnqueueReplies(
		jsonReply(http.StatusOK, applicationJSON, testResponse{Text: "whole answer", Finish: "done", ID: "resp-9"}),
		jsonReply(http.StatusOK, applicationJSON, map[string]any{"error": map[string]any{"code": "insufficient_quota"}}),
	)
	result := test.requireBranch(t, userRequest(), llm.GeneratedBranchID, "")
	require.Equal(t, "whole answer", result.Value.Text, "a gateway that ignores streaming returns a finished generation")
	require.Equal(t, "resp-9", result.Value.ResponseID)
	result = test.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "insufficient_quota", "a 2xx error object is a reported error, not an interrupted stream")

	streamOnly := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat = newTestWireFormat(true)
		config.WireFormat.DecodeResponse = nil
	})
	streamOnly.provider.EnqueueReplies(jsonReply(http.StatusOK, applicationJSON, testResponse{Text: "whole answer", Finish: "done"}))
	result = streamOnly.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "event stream")
	require.Len(t, streamOnly.provider.Requests(), 1)
}

func TestTextGenerationQueryValidatesStructuredOutput(t *testing.T) {
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"score"},
		"properties": map[string]any{"score": map[string]any{"type": "integer", "minimum": 0, "maximum": 10}},
	}
	test := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		rulesForModel := config.WireFormat.RulesForModel
		config.WireFormat.RulesForModel = func(model string) llm.ModelRequestRules {
			rules := rulesForModel(model)
			rules.StructuredOutput.KeywordsMovedToDescription = []string{"minimum", "maximum"}
			return rules
		}
	})
	test.provider.EnqueueReplies(doneReply(`{"score": 11}`), doneReply("```JSON\n{\"score\": 4}\n```"))
	request := userRequest()
	request.StructuredOutput = &llm.StructuredOutput{Name: "score", Schema: schema}
	result := test.requireBranch(t, request, llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Equal(t, "structured output does not match its schema at /score: the value is above maximum", result.Failure.Message)
	require.Equal(t, `{"score": 4}`, test.requireBranch(t, request, llm.GeneratedBranchID, "").Value.Text)

	var sent llm.TextGenerationRequest
	require.NoError(t, json.Unmarshal(test.provider.Requests()[0].Body, &sent))
	providerProperty := sent.StructuredOutput.Schema["properties"].(map[string]any)["score"].(map[string]any)
	require.Equal(t, map[string]any{"type": "integer", "description": "(maximum: 10, minimum: 0)"}, providerProperty,
		"the provider schema describes moved constraints; post-validation still enforces them")
	require.Equal(t, 10, schema["properties"].(map[string]any)["score"].(map[string]any)["maximum"], "the application schema is not modified")
}

func TestTextGenerationQueryAddsTheJSONObjectInstruction(t *testing.T) {
	test := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat.RulesForModel = func(string) llm.ModelRequestRules {
			return llm.ModelRequestRules{StructuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONObjectWithInstruction}}
		}
	})
	test.provider.EnqueueReplies(doneReply(`{"ok":true}`))
	request := userRequest()
	request.Instructions = "Be terse."
	request.StructuredOutput = &llm.StructuredOutput{Name: "verdict", Description: "A verdict.", Schema: map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"ok"},
		"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
	}}
	test.requireBranch(t, request, llm.GeneratedBranchID, "")
	var sent llm.TextGenerationRequest
	require.NoError(t, json.Unmarshal(test.provider.Requests()[0].Body, &sent))
	require.True(t, strings.HasPrefix(sent.Instructions, "Be terse.\n\nRespond with only one JSON object"), sent.Instructions)
	require.Contains(t, sent.Instructions, `named "verdict" (A verdict.)`)
	require.Contains(t, sent.Instructions, `"properties":{"ok":{"type":"boolean"}}`)

	none := newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat.RulesForModel = func(string) llm.ModelRequestRules { return llm.ModelRequestRules{} }
	})
	result := none.requireBranch(t, request, llm.DefectBranchID, sdkgo.FailureValidation)
	require.Contains(t, result.Failure.Message, "does not support structured output")
}

func TestTextGenerationQueryStreamsAndRetriesInterruptedStreams(t *testing.T) {
	streaming := func(config *llm.TextGenerationQueryConfig) { config.WireFormat = newTestWireFormat(true) }
	test := newPipelineTest(t, streaming)
	test.provider.EnqueueReplies(
		llmtest.FakeReply{Header: eventStream, Body: ": keep-alive\n\ndata: {\"text\":\"Hel\"}\n\n", StreamChunks: []llmtest.FakeStreamChunk{
			{Data: "data: {\"text\":\"lo\"}\n\n"}, {Data: "data: {\"finish\":\"done\"}\n\n"},
		}},
		llmtest.FakeReply{Header: eventStream, Body: "data: {\"text\":\"Hel\"}\n\n"},
		llmtest.FakeReply{Header: eventStream, Body: "data: {\"text\":\"Hel\"}\n\ndata: {\"error\":{\"code\":\"stream_failed\"}}\n\n"},
		llmtest.FakeReply{Header: eventStream, Body: "data: {\"text\":\"" + strings.Repeat("x", 1<<12) + "\"}\n\n"},
	)
	require.Equal(t, "Hello", test.requireBranch(t, userRequest(), llm.GeneratedBranchID, "").Value.Text)
	require.Equal(t, "text/event-stream", test.provider.Requests()[0].Header.Get("Accept"))
	test.requireRetry(t, userRequest(), sdkgo.FailureTransport)
	result := test.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "stream_failed")
	test.requireBranch(t, userRequest(), llm.InvalidResponseBranchID, sdkgo.FailureResponseTooLarge)
}

func TestTextGenerationQueryStallRuleCountsEveryByte(t *testing.T) {
	stalling := func(config *llm.TextGenerationQueryConfig) {
		config.WireFormat = newTestWireFormat(true)
		config.WireFormat.StallTimeout = 300 * time.Millisecond
	}
	keepAlives := make([]llmtest.FakeStreamChunk, 0, 8)
	for range 6 {
		keepAlives = append(keepAlives, llmtest.FakeStreamChunk{Delay: 100 * time.Millisecond, Data: ": keep-alive\n\n"})
	}
	keepAlives = append(keepAlives, llmtest.FakeStreamChunk{Data: "data: {\"text\":\"late\",\"finish\":\"done\"}\n\n"})
	test := newPipelineTest(t, stalling)
	test.provider.EnqueueReplies(
		llmtest.FakeReply{Header: eventStream, StreamChunks: keepAlives},
		llmtest.FakeReply{Header: eventStream, Body: "data: {\"text\":\"Hel\"}\n\n", StreamChunks: []llmtest.FakeStreamChunk{
			{Delay: 2 * time.Second, Data: "data: {\"finish\":\"done\"}\n\n"},
		}},
		llmtest.FakeReply{Delay: 2 * time.Second},
	)
	require.Equal(t, "late", test.requireBranch(t, userRequest(), llm.GeneratedBranchID, "").Value.Text,
		"600 ms of keep-alives outlast a 300 ms stall rule because every byte counts")
	failure, _ := test.requireRetry(t, userRequest(), sdkgo.FailureAvailability)
	require.Contains(t, failure.Message, "stall")
	failure, _ = test.requireRetry(t, userRequest(), sdkgo.FailureAvailability)
	require.Contains(t, failure.Message, "stall", "the rule also covers the wait for response headers")
}

func TestNewTextGenerationQueryRejectsInvalidConfiguration(t *testing.T) {
	cases := map[string]func(*llm.TextGenerationQueryConfig){
		"operation ID": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.Operation.OperationID = "generateContent"
		},
		"missing branch": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.Branches = config.Definition.Branches[:5]
		},
		"required optional branch": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.Branches = llm.TextGenerationBranchDefinitions()
			config.Definition.Branches[1].Optional = false
		},
		"no encoder":        func(config *llm.TextGenerationQueryConfig) { config.WireFormat.EncodeRequest = nil },
		"credential header": func(config *llm.TextGenerationQueryConfig) { config.WireFormat.CredentialHeader.Name = "Bad Header" },
		"finish mapping":    func(config *llm.TextGenerationQueryConfig) { config.WireFormat.FinishReasons["odd"] = "maybe" },
		"error rule outcome": func(config *llm.TextGenerationQueryConfig) {
			config.WireFormat.ErrorRules = []llm.ErrorRule{{StatusCode: 400}}
		},
		"error rule status": func(config *llm.TextGenerationQueryConfig) {
			config.WireFormat.ErrorRules = []llm.ErrorRule{{StatusCode: 200, Outcome: llm.InvalidResponseOutcome()}}
		},
		"error rule token": func(config *llm.TextGenerationQueryConfig) {
			config.WireFormat.ErrorRules = []llm.ErrorRule{{ErrorToken: "has space", Outcome: llm.InvalidResponseOutcome()}}
		},
		"plain HTTP base URL": func(config *llm.TextGenerationQueryConfig) { config.BaseURL = "http://api.example.test/v1" },
		"base URL user info":  func(config *llm.TextGenerationQueryConfig) { config.BaseURL = "https://user:hunter2@api.example.test" },
		"model ID rule":       func(config *llm.TextGenerationQueryConfig) { config.WireFormat.ModelIDRule = 0 },
		"connection model":    func(config *llm.TextGenerationQueryConfig) { config.ConnectionModel = "has space" },
		"request timeout":     func(config *llm.TextGenerationQueryConfig) { config.RequestTimeout = 0 },
		"credential resolver": func(config *llm.TextGenerationQueryConfig) { config.ResolveCredential = nil },
		"response limit":      func(config *llm.TextGenerationQueryConfig) { config.MaxResponseBytes = 0 },
		"stream event limit":  func(config *llm.TextGenerationQueryConfig) { config.MaxStreamEventBytes = 0 },
		"negative stall":      func(config *llm.TextGenerationQueryConfig) { config.WireFormat.StallTimeout = -time.Second },
		"rate-limit header": func(config *llm.TextGenerationQueryConfig) {
			config.WireFormat.RateLimitHeaders = []string{"bad header"}
		},
		"error-token pointer": func(config *llm.TextGenerationQueryConfig) {
			config.WireFormat.ErrorTokenPointers = []string{"error/code"}
		},
		"missing provider name": func(config *llm.TextGenerationQueryConfig) { config.WireFormat.ProviderName = "" },
		"async durability": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.StepDefaults.ExecuteDurability = dex.StepDurabilityAsync
		},
		"flow-default durability": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.StepDefaults.ExecuteDurability = dex.StepDurabilityDefault
		},
		"heartbeat timeout below two beats": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.StepDefaults.HeartbeatTimeout = 9 * time.Second
		},
		"Execute timeout within the request timeout": func(config *llm.TextGenerationQueryConfig) {
			config.Definition.StepDefaults.ExecuteMethodTimeout = config.RequestTimeout
		},
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			config := &llm.TextGenerationQueryConfig{
				Definition: testDefinition, WireFormat: newTestWireFormat(false), BaseURL: "https://api.example.test/v1",
				ConnectionModel: testConnectionModel, RequestTimeout: time.Second,
				ResolveCredential: func(sdkgo.Call) (sdkgo.SecretString, error) { return sdkgo.NewSecretString(testAPIKey), nil },
				MaxResponseBytes:  1, MaxStreamEventBytes: 1,
			}
			config.Definition.Branches = llm.TextGenerationBranchDefinitions()
			configure(config)
			_, err := llm.NewTextGenerationQuery(config)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "hunter2")
		})
	}
	_, err := llm.NewTextGenerationQuery(nil)
	require.Error(t, err)
}

func TestNewTextGenerationQueryAcceptsDexHeartbeatTimeouts(t *testing.T) {
	for _, heartbeatTimeout := range []time.Duration{0, 10 * time.Second, time.Minute} {
		newPipelineTest(t, func(config *llm.TextGenerationQueryConfig) {
			config.Definition.StepDefaults.HeartbeatTimeout = heartbeatTimeout
		})
	}
}

func TestNewTextGenerationQueryCopiesTheWireFormat(t *testing.T) {
	var config *llm.TextGenerationQueryConfig
	test := newPipelineTest(t, func(configured *llm.TextGenerationQueryConfig) { config = configured })
	config.WireFormat.FinishReasons["done"] = llm.FinishReason("invalid")
	config.WireFormat.RequestIDHeaders[0] = "bad header"
	test.provider.EnqueueReplies(jsonReply(http.StatusOK, http.Header{"X-First-Request-Id": {"req-1"}},
		testResponse{Text: "still stops", Finish: "done"}))
	result := test.requireBranch(t, userRequest(), llm.GeneratedBranchID, "")
	require.Equal(t, "still stops", result.Value.Text, "a later change to the caller's map bypasses no validation")
	require.Equal(t, "req-1", result.Receipt.ProviderRequestID, "a later change to the caller's slices has no effect")
}

func TestTextGenerationBranchDefinitionsMatchTheContract(t *testing.T) {
	branches := llm.TextGenerationBranchDefinitions()
	require.NoError(t, sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "test-lab", OperationID: llm.TextGenerationOperationID}, Branches: branches,
	}.Validate())
	var ids []sdkgo.BranchID
	for _, branch := range branches {
		ids = append(ids, branch.ID)
		require.Equal(t, branch.ID != llm.GeneratedBranchID, branch.Optional, "only generated is required")
	}
	require.Equal(t, []sdkgo.BranchID{"generated", "truncated", "blocked", "providerRejected", "invalidResponse", "defect"}, ids)
	branches[0].Description = "changed"
	require.NotEqual(t, "changed", llm.TextGenerationBranchDefinitions()[0].Description, "every call returns a new slice")
}

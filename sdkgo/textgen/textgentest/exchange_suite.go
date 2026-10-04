// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgentest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	credentialCanary      = "sk-llmtest-credential-canary-7f3a9c"
	providerMessageCanary = "llmtest-provider-message-canary"
	promptCanary          = "llmtest-prompt-canary"
	structuredValueCanary = "llmtest-structured-value-canary"
	requestIDCanary       = "req-llmtest-exchange-1"
	servedModel           = "llmtest-served-model"
	responseID            = "resp-llmtest-1"
	generatedText         = "The llmtest answer."
	partialText           = "The partial llmtest"
	// unmatchedReportedErrorToken is a token no connector's ErrorRules match.
	unmatchedReportedErrorToken = "llmtest_unmatched_reported_error"
	// fakeMaxResponseBytes keeps the oversized-response case small.
	fakeMaxResponseBytes = 64 << 10
)

// reportedUsage has distinct counts, so a swapped field fails the assertions.
var reportedUsage = textgen.Usage{InputTokens: 11, CachedInputTokens: 3, OutputTokens: 7, ReasoningTokens: 2, TotalTokens: 18}

// ProviderDialect describes how one connector's provider API looks on the
// wire, so the fake provider can answer like it and the suites can read what
// the connector sent. Every field is required unless its comment says
// otherwise; an optional reply closure that is nil skips its case.
type ProviderDialect struct {
	// CredentialHeader is where the connector must send the API key, such as
	// Authorization with the "Bearer " prefix.
	CredentialHeader textgen.CredentialHeader
	// ConnectionModel is the connection's configured model ID.
	ConnectionModel string
	// AlternateModel is a second valid model ID, used to prove that a
	// request's Model overrides the connection's.
	AlternateModel string
	// RequestIDHeader is the response header the connector reads as the
	// provider request ID. Empty skips the request-ID assertion.
	RequestIDHeader string
	// ReadRequestModel returns the model ID the connector sent in one
	// recorded request, from its body or path.
	ReadRequestModel func(request RecordedRequest) (string, error)
	// GeneratedReply renders a normal finish with reply's text, served model,
	// response ID, and every usage count the provider reports.
	GeneratedReply func(reply GeneratedReply) FakeReply
	// TruncatedReply renders a finish at the output token limit with reply's partial text.
	TruncatedReply func(reply GeneratedReply) FakeReply
	// BlockedReply renders a content-policy stop or refusal without text.
	BlockedReply func(reply GeneratedReply) FakeReply
	// ErrorReply renders the provider's error body for statusCode, holding
	// message as the provider's human-readable text and the provider's usual
	// error type and code for that status. The suite adds Retry-After itself.
	ErrorReply func(statusCode int, message string) FakeReply
	// QuotaExhaustedReply renders the provider's credit, spend, or billing
	// exhaustion error with message as its text, such as a 402, or a 429 with
	// a quota error code.
	QuotaExhaustedReply func(message string) FakeReply
	// QuotaExhaustedErrorToken is the error token QuotaExhaustedReply carries
	// at the connector's error-token pointers. The quota Failure must name it,
	// which proves the pointers read the provider's error envelope. It is
	// optional; empty skips that assertion.
	QuotaExhaustedErrorToken string
	// MalformedReply renders a 2xx response that violates the provider's
	// documented format, such as a body that is not JSON.
	MalformedReply func() FakeReply
	// ContentPolicyErrorReply optionally renders a content-policy block that
	// the provider reports as an error, such as a 400 with the token
	// "content_filter", with message as its text. The connector's ErrorRules
	// must select blocked with textgen.BlockedOutcome.
	ContentPolicyErrorReply func(message string) FakeReply
	// ReportedErrorReply optionally renders a 2xx response, or a streamed
	// error event, that carries a provider error object with token as its
	// machine-readable type or code and message as its text. An unmatched
	// token must select invalidResponse.
	ReportedErrorReply func(token string, message string) FakeReply
	// InterruptedStreamReply renders a 2xx event stream that sends reply's
	// Text as content and ends before its terminal event; the attempt must
	// return Retry with sdkgo.FailureTransport. It is required when the
	// connection model streams, which the generated case detects from the
	// request's Accept header, and optional otherwise.
	InterruptedStreamReply func(reply GeneratedReply) FakeReply
	// UnstreamedReply optionally renders reply as one complete 2xx JSON body,
	// which a gateway that ignores a streaming request returns. Set it for a
	// streaming connector whose wire format also has DecodeResponse; the
	// attempt must select generated instead of retrying.
	UnstreamedReply func(reply GeneratedReply) FakeReply
	// OptionalRequestFieldPointers are RFC 6901 pointers into the JSON
	// request body where optional request fields or sampling parameters would
	// appear, such as "/temperature" and "/max_tokens". A request that sets no
	// optional field must hold none of them. It is optional.
	OptionalRequestFieldPointers []string
}

// GeneratedReply is the content the suites ask a ProviderDialect to render.
type GeneratedReply struct {
	// Text is the model output to return.
	Text string
	// ServedModel is the model the provider reports it used.
	ServedModel string
	// ResponseID is the provider's response identifier.
	ResponseID string
	// Usage holds the token counts to report.
	Usage textgen.Usage
}

// FakeConnection is what a suite gives a connector's closure to build its
// Query or Step against the fake provider.
type FakeConnection struct {
	// Reference is the connection reference the suite runs the Query with.
	// The connector's credential provider must resolve APIKey for it.
	Reference sdkgo.ConnectionRef
	// BaseURL is the fake provider's loopback base URL. Use it as the
	// connection's endpoint; the connector appends its own API path.
	BaseURL string
	// Model is the connection's configured model.
	Model string
	// APIKey is a canary credential that must never appear outside the credential header.
	APIKey sdkgo.SecretString
	// MaxResponseBytes is the response size limit the connection must use.
	MaxResponseBytes int64
}

// NamedTextGenerationRequest is one request the connector's own rules must
// reject locally, such as a temperature its models do not accept.
type NamedTextGenerationRequest struct {
	// Name labels the subtest.
	Name string
	// Request is the rejected request. Empty Messages use one user message.
	Request textgen.TextGenerationRequest
}

// TextGenerationExchangeSuite configures RunTextGenerationExchangeSuite for one connector.
type TextGenerationExchangeSuite struct {
	// Dialect describes the provider API. It is required.
	Dialect ProviderDialect
	// NewQuery builds the connector's generateText Query for connection, the
	// way the connector's GenerateText method does. It is required.
	NewQuery func(t testing.TB, connection FakeConnection) *textgen.TextGenerationQuery
	// LocallyRejectedRequests are connector-specific requests that must select
	// defect without a provider request, such as the rows of a temperature or
	// reasoning-effort table. It is optional.
	LocallyRejectedRequests []NamedTextGenerationRequest
}

// RunTextGenerationExchangeSuite runs the provider-exchange conformance
// cases against the connector's generateText Query and a FakeProvider. It
// fails the test when suite is nil or incomplete.
//
// Every case builds a new Query and fake provider and runs through
// sdkgo.RunQuery. The cases cover a normal finish with usage and receipt,
// truncation, a content-policy stop, 400, 401, 403, and quota rejections,
// 429 with both Retry-After forms, 500, 502, 503, 504, and an HTML 503, a
// redirect that is never followed, a dropped connection, malformed and
// oversized responses, structured output that matches, is fenced, does not
// match, or leaves the portable subset, model precedence, invalid models and
// the shared model-ID cases, and every request field the wire format does not
// declare. The optional ProviderDialect closures add a content-policy error,
// a provider error inside a 2xx response, an interrupted stream, which a
// streaming connection model must provide, a streaming request answered with
// a complete body, and a request without optional fields. Every case also
// proves that the canary API key, provider message,
// prompt, and structured value never appear in the Result, Failure, Receipt,
// errors, or their %v, %+v, %#v, and JSON forms, and that the key never
// leaves the credential header.
//
// The suite runs without Dex, so it records no text Stream. The streamed text
// and its order are proven by RunTextGenerationDexScenarios.
func RunTextGenerationExchangeSuite(t *testing.T, suite *TextGenerationExchangeSuite) {
	t.Helper()
	validateExchangeSuite(t, suite)
	run := exchangeSuiteRun{suite: suite}
	t.Run("generated", run.testGenerated)
	t.Run("truncated", run.testTruncated)
	t.Run("blocked", run.testBlocked)
	t.Run("provider rejection", func(t *testing.T) {
		run.testErrorStatus(t, http.StatusBadRequest, sdkgo.FailureProviderRejection)
	})
	t.Run("authentication", func(t *testing.T) {
		run.testErrorStatus(t, http.StatusUnauthorized, sdkgo.FailureAuthentication)
	})
	t.Run("authorization", func(t *testing.T) {
		run.testErrorStatus(t, http.StatusForbidden, sdkgo.FailureAuthorization)
	})
	t.Run("quota exhausted", run.testQuotaExhausted)
	t.Run("rate limit with Retry-After seconds", run.testRetryAfterSeconds)
	t.Run("rate limit with Retry-After date", run.testRetryAfterDate)
	t.Run("server errors", run.testServerErrors)
	t.Run("HTML server error", run.testHTMLServerError)
	t.Run("redirect is not followed", run.testRedirect)
	t.Run("transport failure", run.testTransportFailure)
	t.Run("malformed response", run.testMalformedResponse)
	t.Run("oversized response", run.testOversizedResponse)
	t.Run("structured output", run.testStructuredOutput)
	t.Run("request model overrides connection model", run.testRequestModelPrecedence)
	t.Run("invalid request model", run.testInvalidRequestModel)
	t.Run("shared model ID cases", run.testSharedModelIDCases)
	t.Run("undeclared request features", run.testUndeclaredRequestFeatures)
	t.Run("locally rejected requests", run.testLocallyRejectedRequests)
	dialect := &suite.Dialect
	if dialect.ContentPolicyErrorReply != nil {
		t.Run("content-policy error", run.testContentPolicyError)
	}
	if dialect.ReportedErrorReply != nil {
		t.Run("unmatched error inside a 2xx response", run.testUnmatchedReportedError)
	}
	if dialect.InterruptedStreamReply != nil {
		t.Run("interrupted stream", run.testInterruptedStream)
	}
	if dialect.UnstreamedReply != nil {
		t.Run("streaming request answered with a complete body", run.testUnstreamedReply)
	}
	if len(dialect.OptionalRequestFieldPointers) > 0 {
		t.Run("request without optional fields", run.testRequestWithoutOptionalFields)
	}
}

func validateExchangeSuite(t *testing.T, suite *TextGenerationExchangeSuite) {
	t.Helper()
	if suite == nil {
		t.Fatal("llmtest exchange suite is required")
	}
	validateProviderDialect(t, &suite.Dialect)
	if suite.NewQuery == nil {
		t.Fatal("llmtest exchange suite requires NewQuery")
	}
}

func validateProviderDialect(t *testing.T, dialect *ProviderDialect) {
	t.Helper()
	if dialect.CredentialHeader.Name == "" || dialect.ConnectionModel == "" || dialect.AlternateModel == "" {
		t.Fatal("llmtest provider dialect requires a credential header, a connection model, and an alternate model")
	}
	if dialect.ReadRequestModel == nil || dialect.GeneratedReply == nil || dialect.TruncatedReply == nil ||
		dialect.BlockedReply == nil || dialect.ErrorReply == nil || dialect.QuotaExhaustedReply == nil ||
		dialect.MalformedReply == nil {
		t.Fatal("llmtest provider dialect requires every reply closure and ReadRequestModel")
	}
}

type exchangeSuiteRun struct {
	suite *TextGenerationExchangeSuite
}

func (run exchangeSuiteRun) testGenerated(t *testing.T) {
	exchange := run.newExchange(t)
	reply := run.suite.Dialect.GeneratedReply(GeneratedReply{
		Text: generatedText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	})
	if run.suite.Dialect.RequestIDHeader != "" {
		reply.Header = cloneHeaderWith(reply.Header, run.suite.Dialect.RequestIDHeader, requestIDCanary)
	}
	exchange.provider.EnqueueReplies(reply)
	request := baseRequest()
	if exchange.query.RequestFeatures().SupportsInstructions {
		request.Instructions = "Answer in one sentence."
	}
	result := exchange.requireBranch(t, request, textgen.GeneratedBranchID, "")
	value := result.Value
	require.Equal(t, generatedText, value.Text)
	require.Equal(t, exchange.connectionModel, value.RequestedModel)
	require.Equal(t, servedModel, value.ServedModel)
	require.Equal(t, responseID, value.ResponseID)
	require.Equal(t, textgen.FinishReasonStop, value.FinishReason)
	require.NotEmpty(t, value.ProviderFinishReason)
	require.Equal(t, reportedUsage.InputTokens, value.Usage.InputTokens)
	require.Equal(t, reportedUsage.OutputTokens, value.Usage.OutputTokens)
	require.Equal(t, reportedUsage.TotalTokens, value.Usage.TotalTokens)
	require.Contains(t, []int64{0, reportedUsage.CachedInputTokens}, value.Usage.CachedInputTokens)
	require.Contains(t, []int64{0, reportedUsage.ReasoningTokens}, value.Usage.ReasoningTokens)
	require.NoError(t, result.Receipt.CallID.Validate())
	require.NotEmpty(t, result.Receipt.Provider)
	require.Equal(t, responseID, result.Receipt.ProviderObjectID)
	if run.suite.Dialect.RequestIDHeader != "" {
		require.Equal(t, requestIDCanary, result.Receipt.ProviderRequestID)
	}
	requests := exchange.requireRequestCount(t, 1)
	require.True(t, requests[0].HasCredentialInSlot, "the API key must travel in the declared credential header")
	exchange.requireRequestModel(t, requests[0], exchange.connectionModel)
	if requests[0].Header.Get("Accept") == "text/event-stream" {
		require.NotNil(t, run.suite.Dialect.InterruptedStreamReply,
			"a streaming connector's ProviderDialect must set InterruptedStreamReply, so an interrupted stream is proven to Retry")
	}
}

func (run exchangeSuiteRun) testTruncated(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.TruncatedReply(GeneratedReply{
		Text: partialText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	result := exchange.requireBranch(t, baseRequest(), textgen.TruncatedBranchID, sdkgo.FailureResponseTooLarge)
	require.Equal(t, partialText, result.Value.Text)
	require.Equal(t, textgen.FinishReasonLength, result.Value.FinishReason)
	require.Equal(t, exchange.connectionModel, result.Value.RequestedModel)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testBlocked(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.BlockedReply(GeneratedReply{
		ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	result := exchange.requireBranch(t, baseRequest(), textgen.BlockedBranchID, sdkgo.FailureProviderRejection)
	require.Empty(t, result.Value.Text)
	require.Contains(t, []textgen.FinishReason{textgen.FinishReasonContentPolicy, textgen.FinishReasonRefusal}, result.Value.FinishReason)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testErrorStatus(t *testing.T, statusCode int, kind sdkgo.FailureKind) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.ErrorReply(statusCode, providerMessageCanary))
	result := exchange.requireBranch(t, baseRequest(), textgen.ProviderRejectedBranchID, kind)
	require.Equal(t, exchange.connectionModel, result.Value.RequestedModel)
	require.Contains(t, result.Failure.Message, fmt.Sprint(statusCode))
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testQuotaExhausted(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.QuotaExhaustedReply(providerMessageCanary))
	result := exchange.requireBranch(t, baseRequest(), textgen.ProviderRejectedBranchID, sdkgo.FailureQuotaExhausted)
	if token := run.suite.Dialect.QuotaExhaustedErrorToken; token != "" {
		require.Contains(t, result.Failure.Message, token, "the connector's error-token pointers must read the quota token")
	}
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testRetryAfterSeconds(t *testing.T) {
	exchange := run.newExchange(t)
	reply := run.suite.Dialect.ErrorReply(http.StatusTooManyRequests, providerMessageCanary)
	reply.Header = cloneHeaderWith(reply.Header, "Retry-After", "7")
	exchange.provider.EnqueueReplies(reply)
	delay := exchange.requireRetry(t, baseRequest(), sdkgo.FailureRateLimit)
	require.Equal(t, 7*time.Second, delay)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testRetryAfterDate(t *testing.T) {
	exchange := run.newExchange(t)
	reply := run.suite.Dialect.ErrorReply(http.StatusTooManyRequests, providerMessageCanary)
	reply.Header = cloneHeaderWith(reply.Header, "Retry-After", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))
	exchange.provider.EnqueueReplies(reply)
	delay := exchange.requireRetry(t, baseRequest(), sdkgo.FailureRateLimit)
	require.GreaterOrEqual(t, delay, 25*time.Second)
	require.LessOrEqual(t, delay, 30*time.Second)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testServerErrors(t *testing.T) {
	statusCodes := []int{
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	}
	for _, statusCode := range statusCodes {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			exchange := run.newExchange(t)
			exchange.provider.EnqueueReplies(run.suite.Dialect.ErrorReply(statusCode, providerMessageCanary))
			require.Zero(t, exchange.requireRetry(t, baseRequest(), sdkgo.FailureAvailability))
			exchange.requireRequestCount(t, 1)
		})
	}
}

// testHTMLServerError sends the HTML page a gateway returns instead of the provider's error body.
func (run exchangeSuiteRun) testHTMLServerError(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(FakeReply{
		StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body: "<html><body><h1>503 Service Unavailable</h1><p>" + providerMessageCanary + "</p></body></html>",
	})
	require.Zero(t, exchange.requireRetry(t, baseRequest(), sdkgo.FailureAvailability))
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testRedirect(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(FakeReply{
		StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {exchange.provider.BaseURL() + "/llmtest-redirected"}},
	})
	exchange.requireBranch(t, baseRequest(), textgen.InvalidResponseBranchID, sdkgo.FailureProtocol)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testTransportFailure(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(FakeReply{ShouldDropConnection: true})
	require.Zero(t, exchange.requireRetry(t, baseRequest(), sdkgo.FailureTransport))
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testMalformedResponse(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.MalformedReply())
	result := exchange.requireBranch(t, baseRequest(), textgen.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Equal(t, exchange.connectionModel, result.Value.RequestedModel)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testOversizedResponse(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.GeneratedReply(GeneratedReply{
		Text: strings.Repeat("x", fakeMaxResponseBytes+1), ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	exchange.requireBranch(t, baseRequest(), textgen.InvalidResponseBranchID, sdkgo.FailureResponseTooLarge)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testStructuredOutput(t *testing.T) {
	if !run.newExchange(t).query.RequestFeatures().SupportsStructuredOutput {
		// The undeclared-features case proves that a structured request selects defect.
		return
	}
	matching := `{"answer":"forty-two","score":7}`
	t.Run("matching object", func(t *testing.T) {
		exchange := run.newExchange(t)
		exchange.provider.EnqueueReplies(run.generatedReply(matching))
		result := exchange.requireBranch(t, structuredRequest(answerSchema()), textgen.GeneratedBranchID, "")
		require.JSONEq(t, matching, result.Value.Text)
		exchange.requireRequestCount(t, 1)
	})
	t.Run("fenced object", func(t *testing.T) {
		exchange := run.newExchange(t)
		exchange.provider.EnqueueReplies(run.generatedReply("```json\n" + matching + "\n```"))
		result := exchange.requireBranch(t, structuredRequest(answerSchema()), textgen.GeneratedBranchID, "")
		require.Equal(t, matching, result.Value.Text)
	})
	t.Run("mismatched object", func(t *testing.T) {
		exchange := run.newExchange(t)
		exchange.provider.EnqueueReplies(run.generatedReply(`{"answer":"` + structuredValueCanary + `","score":42}`))
		result := exchange.requireBranch(t, structuredRequest(answerSchema()), textgen.InvalidResponseBranchID, sdkgo.FailureProtocol)
		require.Contains(t, result.Failure.Message, "/score")
		require.Empty(t, result.Value.Text)
	})
	t.Run("schema outside the portable subset", func(t *testing.T) {
		exchange := run.newExchange(t)
		schema := answerSchema()
		schema["properties"].(map[string]any)["answer"] = map[string]any{
			"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}},
		}
		result := exchange.requireBranch(t, structuredRequest(schema), textgen.DefectBranchID, sdkgo.FailureValidation)
		require.Contains(t, result.Failure.Message, "anyOf")
		exchange.requireRequestCount(t, 0)
	})
}

func (run exchangeSuiteRun) testRequestModelPrecedence(t *testing.T) {
	exchange := run.newExchange(t)
	alternateModel := exchange.canonicalModel(t, run.suite.Dialect.AlternateModel)
	require.NotEqual(t, exchange.connectionModel, alternateModel, "AlternateModel must differ from ConnectionModel")
	exchange.provider.EnqueueReplies(run.generatedReply(generatedText))
	request := baseRequest()
	request.Model = "  " + run.suite.Dialect.AlternateModel + "\t"
	result := exchange.requireBranch(t, request, textgen.GeneratedBranchID, "")
	require.Equal(t, alternateModel, result.Value.RequestedModel)
	exchange.requireRequestModel(t, exchange.requireRequestCount(t, 1)[0], alternateModel)
}

func (run exchangeSuiteRun) testInvalidRequestModel(t *testing.T) {
	exchange := run.newExchange(t)
	request := baseRequest()
	request.Model = "llmtest invalid model"
	result := exchange.requireBranch(t, request, textgen.DefectBranchID, sdkgo.FailureValidation)
	require.Empty(t, result.Value.RequestedModel)
	exchange.requireRequestCount(t, 0)
}

func (run exchangeSuiteRun) testSharedModelIDCases(t *testing.T) {
	cases, err := modelIDCasesForRule(run.newExchange(t).query.ModelIDRule())
	require.NoError(t, err)
	for _, modelCase := range cases {
		t.Run(modelCase.Name, func(t *testing.T) {
			exchange := run.newExchange(t)
			request := baseRequest()
			request.Model = modelCase.Input
			expectedModel := modelCase.ModelID
			if strings.TrimSpace(modelCase.Input) == "" {
				// A blank request model is unset, so precedence selects the connection model.
				expectedModel = exchange.connectionModel
			} else if !modelCase.IsValid {
				exchange.requireBranch(t, request, textgen.DefectBranchID, sdkgo.FailureValidation)
				exchange.requireRequestCount(t, 0)
				return
			}
			exchange.provider.EnqueueReplies(run.generatedReply(generatedText))
			result := exchange.requireBranch(t, request, textgen.GeneratedBranchID, "")
			require.Equal(t, expectedModel, result.Value.RequestedModel)
			exchange.requireRequestModel(t, exchange.requireRequestCount(t, 1)[0], expectedModel)
		})
	}
}

// featureRequest sets exactly one optional request field, which the defect message must name.
type featureRequest struct {
	requestField string
	setField     func(*textgen.TextGenerationRequest)
}

// featureRequests is keyed by RequestFeatures field name.
var featureRequests = map[string]featureRequest{
	"SupportsInstructions": {requestField: "Instructions", setField: func(request *textgen.TextGenerationRequest) {
		request.Instructions = "Answer in one sentence."
	}},
	"SupportsStructuredOutput": {requestField: "StructuredOutput", setField: func(request *textgen.TextGenerationRequest) {
		*request = structuredRequest(answerSchema())
	}},
	"SupportsMaxOutputTokens": {requestField: "MaxOutputTokens", setField: func(request *textgen.TextGenerationRequest) {
		request.MaxOutputTokens = 64
	}},
	"SupportsTemperature": {requestField: "Temperature", setField: func(request *textgen.TextGenerationRequest) {
		temperature := 0.5
		request.Temperature = &temperature
	}},
	"SupportsReasoningEffort": {requestField: "ReasoningEffort", setField: func(request *textgen.TextGenerationRequest) {
		request.ReasoningEffort = textgen.ReasoningEffortLow
	}},
}

func (run exchangeSuiteRun) testUndeclaredRequestFeatures(t *testing.T) {
	declared := reflect.ValueOf(run.newExchange(t).query.RequestFeatures())
	for index := 0; index < declared.NumField(); index++ {
		feature := declared.Type().Field(index).Name
		known, isKnown := featureRequests[feature]
		require.True(t, isKnown, "llmtest has no request for RequestFeatures.%s; update the suite with the SDK", feature)
		if declared.Field(index).Bool() {
			continue
		}
		t.Run(known.requestField, func(t *testing.T) {
			exchange := run.newExchange(t)
			request := baseRequest()
			known.setField(&request)
			result := exchange.requireBranch(t, request, textgen.DefectBranchID, sdkgo.FailureValidation)
			require.Contains(t, result.Failure.Message, known.requestField)
			exchange.requireRequestCount(t, 0)
		})
	}
}

func (run exchangeSuiteRun) testLocallyRejectedRequests(t *testing.T) {
	for _, rejected := range run.suite.LocallyRejectedRequests {
		t.Run(rejected.Name, func(t *testing.T) {
			exchange := run.newExchange(t)
			request := rejected.Request
			if len(request.Messages) == 0 {
				request.Messages = baseRequest().Messages
			}
			exchange.requireBranch(t, request, textgen.DefectBranchID, "")
			exchange.requireRequestCount(t, 0)
		})
	}
}

func (run exchangeSuiteRun) testContentPolicyError(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.ContentPolicyErrorReply(providerMessageCanary))
	result := exchange.requireBranch(t, baseRequest(), textgen.BlockedBranchID, sdkgo.FailureProviderRejection)
	require.Empty(t, result.Value.Text)
	require.Equal(t, textgen.FinishReasonContentPolicy, result.Value.FinishReason)
	require.Equal(t, exchange.connectionModel, result.Value.RequestedModel)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testUnmatchedReportedError(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.ReportedErrorReply(unmatchedReportedErrorToken, providerMessageCanary))
	result := exchange.requireBranch(t, baseRequest(), textgen.InvalidResponseBranchID, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, unmatchedReportedErrorToken, "the Failure names the provider's error token")
	require.Empty(t, result.Value.Text)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testInterruptedStream(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.InterruptedStreamReply(GeneratedReply{
		Text: partialText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	require.Zero(t, exchange.requireRetry(t, baseRequest(), sdkgo.FailureTransport))
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testUnstreamedReply(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.suite.Dialect.UnstreamedReply(GeneratedReply{
		Text: generatedText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	result := exchange.requireBranch(t, baseRequest(), textgen.GeneratedBranchID, "")
	require.Equal(t, generatedText, result.Value.Text)
	require.Equal(t, responseID, result.Value.ResponseID)
	exchange.requireRequestCount(t, 1)
}

func (run exchangeSuiteRun) testRequestWithoutOptionalFields(t *testing.T) {
	exchange := run.newExchange(t)
	exchange.provider.EnqueueReplies(run.generatedReply(generatedText))
	exchange.requireBranch(t, baseRequest(), textgen.GeneratedBranchID, "")
	decoder := json.NewDecoder(bytes.NewReader(exchange.requireRequestCount(t, 1)[0].Body))
	decoder.UseNumber()
	var body any
	require.NoError(t, decoder.Decode(&body), "the request body must be JSON")
	for _, pointer := range run.suite.Dialect.OptionalRequestFieldPointers {
		require.False(t, hasJSONPointer(body, pointer), "a request without optional fields sent %s", pointer)
	}
}

func (run exchangeSuiteRun) generatedReply(text string) FakeReply {
	return run.suite.Dialect.GeneratedReply(GeneratedReply{
		Text: text, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	})
}

func (run exchangeSuiteRun) newExchange(t *testing.T) *providerExchange {
	t.Helper()
	dialect := &run.suite.Dialect
	provider := NewFakeProvider(t, dialect.CredentialHeader, credentialCanary)
	connection := FakeConnection{
		Reference: sdkgo.ConnectionRef{Provider: "llmtest", Name: "exchange"},
		BaseURL:   provider.BaseURL(), Model: dialect.ConnectionModel,
		APIKey: sdkgo.NewSecretString(credentialCanary), MaxResponseBytes: fakeMaxResponseBytes,
	}
	query := run.suite.NewQuery(t, connection)
	require.NotNil(t, query, "NewQuery must return the Query built by textgen.NewTextGenerationQuery")
	exchange := &providerExchange{
		dialect: dialect, provider: provider, query: query, connection: connection.Reference,
	}
	exchange.connectionModel = exchange.canonicalModel(t, dialect.ConnectionModel)
	return exchange
}

// providerExchange is one case's Query, fake provider, and canonical connection model.
type providerExchange struct {
	dialect         *ProviderDialect
	provider        *FakeProvider
	query           *textgen.TextGenerationQuery
	connection      sdkgo.ConnectionRef
	connectionModel string
	invocations     int
}

func (exchange *providerExchange) requireBranch(
	t *testing.T, request textgen.TextGenerationRequest, branch sdkgo.BranchID, kind sdkgo.FailureKind,
) textgen.TextGenerationResult {
	t.Helper()
	result, err := exchange.invoke(t, request)
	require.NoError(t, err, "the attempt must select a branch, not Retry")
	require.Equal(t, branch, result.Branch, "failure: %+v", result.Failure)
	if kind == "" && branch == textgen.GeneratedBranchID {
		require.Nil(t, result.Failure)
		return result
	}
	require.NotNil(t, result.Failure)
	if kind != "" {
		require.Equal(t, kind, result.Failure.Kind, "failure message: %s", result.Failure.Message)
	}
	return result
}

// requireRetry returns the provider-requested delay, or zero when the Step policy applies.
func (exchange *providerExchange) requireRetry(t *testing.T, request textgen.TextGenerationRequest, kind sdkgo.FailureKind) time.Duration {
	t.Helper()
	_, err := exchange.invoke(t, request)
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError, "the attempt must return Retry")
	require.Equal(t, kind, retryError.Failure.Kind, "failure message: %s", retryError.Failure.Message)
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		return retryAfter.After
	}
	return 0
}

func (exchange *providerExchange) invoke(t *testing.T, request textgen.TextGenerationRequest) (textgen.TextGenerationResult, error) {
	t.Helper()
	exchange.invocations++
	ctx := testsupport.NewDexContext("llmtest-flow", fmt.Sprintf("llmtest-step-%d-%d", time.Now().UnixNano(), exchange.invocations))
	result, err := sdkgo.RunQuery(ctx, exchange.query, exchange.connection, request)
	exchange.requireRedacted(t, result, err)
	return result, err
}

func (exchange *providerExchange) requireRedacted(t *testing.T, result textgen.TextGenerationResult, err error) {
	t.Helper()
	encoded, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)
	renderings := []string{
		string(encoded), fmt.Sprintf("%v", result), fmt.Sprintf("%+v", result), fmt.Sprintf("%#v", result),
		fmt.Sprintf("%v", exchange.query), fmt.Sprintf("%+v", exchange.query), fmt.Sprintf("%#v", exchange.query),
	}
	if err != nil {
		renderings = append(renderings, err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err))
	}
	for _, rendering := range renderings {
		for _, canary := range []string{credentialCanary, providerMessageCanary, promptCanary, structuredValueCanary} {
			require.NotContains(t, rendering, canary, "a canary leaked into a Result, error, or formatted value")
		}
	}
	for _, recorded := range exchange.provider.Requests() {
		require.False(t, recorded.HasCredentialOutsideSlot, "the API key left the credential header")
	}
}

func (exchange *providerExchange) requireRequestCount(t *testing.T, count int) []RecordedRequest {
	t.Helper()
	requests := exchange.provider.Requests()
	require.Len(t, requests, count, "provider request count")
	return requests
}

func (exchange *providerExchange) requireRequestModel(t *testing.T, request RecordedRequest, model string) {
	t.Helper()
	sent, err := exchange.dialect.ReadRequestModel(request)
	require.NoError(t, err)
	require.Equal(t, model, sent, "model sent to the provider")
}

func (exchange *providerExchange) canonicalModel(t *testing.T, model string) string {
	t.Helper()
	canonical, err := exchange.query.ModelIDRule().ValidateModelID(model)
	require.NoError(t, err, "dialect model IDs must be valid for the connector's model ID rule")
	return canonical
}

func baseRequest() textgen.TextGenerationRequest {
	return textgen.TextGenerationRequest{Messages: []textgen.Message{
		{Role: textgen.MessageRoleUser, Text: "Answer the question in " + promptCanary + "."},
	}}
}

func structuredRequest(schema map[string]any) textgen.TextGenerationRequest {
	request := baseRequest()
	request.StructuredOutput = &textgen.StructuredOutput{
		Name: "llmtest_answer", Description: "An answer with a score.", Schema: schema,
	}
	return request
}

func answerSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answer": map[string]any{"type": "string", "description": "The short answer."},
			"score":  map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
		},
		"required":             []any{"answer", "score"},
		"additionalProperties": false,
	}
}

func cloneHeaderWith(header http.Header, name string, value string) http.Header {
	cloned := header.Clone()
	if cloned == nil {
		cloned = http.Header{}
	}
	cloned.Set(name, value)
	return cloned
}

// hasJSONPointer reports whether an RFC 6901 pointer resolves inside a decoded JSON document.
func hasJSONPointer(document any, pointer string) bool {
	if pointer == "" {
		return true
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	current := document
	for _, escaped := range strings.Split(pointer[1:], "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(escaped, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, found := typed[segment]
			if !found {
				return false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return false
			}
			current = typed[index]
		default:
			return false
		}
	}
	return true
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// responsePage omits formId, as Google documents for list requests; answers arrive keyed by question ID.
const responsePage = `{"responses":[` +
	`{"responseId":"ACYDBNj_r1","createTime":"2026-09-30T08:00:00.123Z","lastSubmittedTime":"2026-09-30T09:15:00.123456Z",` +
	`"respondentEmail":"buyer@example.com","totalScore":2,"answers":{` +
	`"q_tier":{"questionId":"q_tier","grade":{"score":2,"correct":true},"textAnswers":{"answers":[{"value":"Gold"}]}},` +
	`"q_regions":{"questionId":"q_regions","textAnswers":{"answers":[{"value":"EU"},{"value":"US"}]}},` +
	`"q_contract":{"questionId":"q_contract","fileUploadAnswers":{"answers":[{"fileId":"file_msa","fileName":"MSA.pdf","mimeType":"application/pdf"}]}}}},` +
	`{"responseId":"ACYDBNj_r2","createTime":"2026-09-30T10:00:00Z","lastSubmittedTime":"2026-09-30T10:00:00Z","answers":{}}],` +
	`"nextPageToken":"page-2"}`

func TestListResponsesDecodesAnswersAndPaging(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, responsePage)
	})
	client := newFormsClient(t, fake.URL)

	result, err := sdkgo.RunQuery(newDexContext("list"), client.ListResponses(), formsConnection, forms.ListResponsesInput{FormID: "1FAIpQLintake"})
	require.NoError(t, err)
	require.Equal(t, forms.ListResponsesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	page := result.Value
	require.Equal(t, "1FAIpQLintake", page.FormID)
	require.Equal(t, "page-2", page.NextPageToken)
	require.Len(t, page.Responses, 2)
	score := 2.0
	require.Equal(t, forms.FormResponse{
		ResponseID: "ACYDBNj_r1", FormID: "1FAIpQLintake",
		CreatedAt:       time.Date(2026, time.September, 30, 8, 0, 0, 123000000, time.UTC),
		LastSubmittedAt: time.Date(2026, time.September, 30, 9, 15, 0, 123456000, time.UTC),
		RespondentEmail: "buyer@example.com", TotalScore: &score,
		Answers: []forms.FormAnswer{
			{QuestionID: "q_contract", Files: []forms.FormAnswerFile{{FileID: "file_msa", FileName: "MSA.pdf", MimeType: "application/pdf"}}},
			{QuestionID: "q_regions", Values: []string{"EU", "US"}},
			{QuestionID: "q_tier", Values: []string{"Gold"}, Grade: &forms.FormAnswerGrade{Score: 2, IsCorrect: true}},
		},
	}, page.Responses[0])
	regions, ok := page.Responses[0].AnswerByQuestionID("q_regions")
	require.True(t, ok)
	require.Equal(t, []string{"EU", "US"}, regions.Values)
	_, ok = page.Responses[1].AnswerByQuestionID("q_regions")
	require.False(t, ok)
	require.NotNil(t, page.Responses[1].Answers)
	require.Equal(t, "1FAIpQLintake", result.Receipt.ProviderObjectID)

	request := fake.recorded()[0]
	require.Equal(t, "/v1/forms/1FAIpQLintake/responses", request.path)
	require.Equal(t, []string{"50"}, request.query["pageSize"])
	require.Empty(t, request.query["filter"])
	require.Empty(t, request.query["pageToken"])
}

func TestListResponsesSendsGoogleSubmissionTimeFiltersInUTC(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newFormsClient(t, fake.URL)
	pacific := time.FixedZone("PDT", -7*60*60)
	for _, test := range []struct {
		name       string
		input      forms.ListResponsesInput
		wantFilter string
	}{
		{name: "after", input: forms.ListResponsesInput{SubmittedAfter: time.Date(2026, time.September, 30, 2, 15, 0, 123456789, pacific)}, wantFilter: "timestamp > 2026-09-30T09:15:00.123456789Z"},
		{name: "at or after", input: forms.ListResponsesInput{SubmittedAtOrAfter: time.Date(2026, time.September, 30, 9, 15, 0, 0, time.UTC)}, wantFilter: "timestamp >= 2026-09-30T09:15:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.input.FormID, test.input.PageSize, test.input.PageToken = "f1", 7, "next-token"
			result, err := sdkgo.RunQuery(newDexContext("list-filter-"+test.name), client.ListResponses(), formsConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, forms.ListResponsesBranchListed, result.Branch)
			require.Empty(t, result.Value.Responses)
			require.NotNil(t, result.Value.Responses)
			request := fake.recorded()[len(fake.recorded())-1]
			require.Equal(t, []string{test.wantFilter}, request.query["filter"])
			require.Equal(t, []string{"7"}, request.query["pageSize"])
			require.Equal(t, []string{"next-token"}, request.query["pageToken"])
		})
	}
}

func TestListResponsesRejectsInvalidInputWithoutProviderRequest(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newFormsClient(t, fake.URL)
	now := time.Date(2026, time.September, 30, 9, 0, 0, 0, time.UTC)
	for name, input := range map[string]forms.ListResponsesInput{
		"blank form ID":          {},
		"form ID injection":      {FormID: "f1/responses/r1"},
		"both filters":           {FormID: "f1", SubmittedAfter: now, SubmittedAtOrAfter: now},
		"page size above limit":  {FormID: "f1", PageSize: 1001},
		"negative page size":     {FormID: "f1", PageSize: -1},
		"page token with spaces": {FormID: "f1", PageToken: "a b"},
		"oversized page token":   {FormID: "f1", PageToken: strings.Repeat("a", 4097)},
		"year beyond RFC 3339":   {FormID: "f1", SubmittedAfter: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newDexContext("list-invalid-"+name), client.ListResponses(), formsConnection, input)
			require.NoError(t, err)
			require.Equal(t, forms.ListResponsesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, fake.recorded())
}

func TestListResponsesRetriesRateLimitsAndOutages(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		wantKind   sdkgo.FailureKind
		wantDelay  time.Duration
	}{
		{name: "429 with Retry-After", status: http.StatusTooManyRequests, body: googleError(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), retryAfter: "7", wantKind: sdkgo.FailureRateLimit, wantDelay: 7 * time.Second},
		{name: "403 rate limit reason", status: http.StatusForbidden, body: googleError(403, "PERMISSION_DENIED", "RATE_LIMIT_EXCEEDED"), wantKind: sdkgo.FailureRateLimit},
		{name: "408 timeout", status: http.StatusRequestTimeout, body: `{}`, wantKind: sdkgo.FailureAvailability},
		{name: "503 outage", status: http.StatusServiceUnavailable, body: googleError(503, "UNAVAILABLE", ""), wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			_, err := sdkgo.RunQuery(newDexContext("list-retry"), newFormsClient(t, fake.URL).ListResponses(), formsConnection, forms.ListResponsesInput{FormID: "f1"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.wantKind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, "SENTINEL")
			var delayedRetry *dex.RetryAfterError
			if test.wantDelay == 0 {
				require.False(t, errors.As(err, &delayedRetry))
				return
			}
			require.ErrorAs(t, err, &delayedRetry)
			require.Equal(t, test.wantDelay, delayedRetry.After)
		})
	}
}

func TestListResponsesClassifiesRejectedAndInvalidPages(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		config     forms.Config
		pageSize   int
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "missing form", status: http.StatusNotFound, body: googleError(404, "NOT_FOUND", ""), wantBranch: forms.ListResponsesBranchNotFound, wantKind: sdkgo.FailureNotFound},
		{name: "responses not readable", status: http.StatusForbidden, body: googleError(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT"), wantBranch: forms.ListResponsesBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "expired page token", status: http.StatusBadRequest, body: googleError(400, "INVALID_ARGUMENT", ""), wantBranch: forms.ListResponsesBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
		{name: "malformed JSON", status: http.StatusOK, body: `{"responses":`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "response without ID", status: http.StatusOK, body: `{"responses":[{"lastSubmittedTime":"2026-09-30T09:00:00Z"}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "response without submission time", status: http.StatusOK, body: `{"responses":[{"responseId":"r1"}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "response of another form", status: http.StatusOK, body: `{"responses":[{"formId":"other","responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z"}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "repeated response", status: http.StatusOK, body: `{"responses":[{"responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z"},{"responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z"}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "answer keyed by another question", status: http.StatusOK, body: `{"responses":[{"responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z","answers":{"q1":{"questionId":"q2"}}}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "file without Drive ID", status: http.StatusOK, body: `{"responses":[{"responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z","answers":{"q1":{"fileUploadAnswers":{"answers":[{"fileName":"a.pdf"}]}}}}]}`, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "more responses than the page size", status: http.StatusOK, body: responsePage, pageSize: 1, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "oversized page", status: http.StatusOK, body: responsePage, config: forms.Config{MaxResponseBytes: 32}, wantBranch: forms.ListResponsesBranchInvalidResponse, wantKind: sdkgo.FailureResponseTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newFormsClient(t, fake.URL, test.config)
			result, err := sdkgo.RunQuery(newDexContext("list-rejected"), client.ListResponses(), formsConnection, forms.ListResponsesInput{FormID: "f1", PageSize: test.pageSize})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Len(t, fake.recorded(), 1)
		})
	}
}

func TestNewRejectsUnsafeEndpointsAndOutOfRangeLimits(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[forms.Credentials]{}
	for name, config := range map[string]forms.Config{
		"plain HTTP remote endpoint": {Endpoint: "http://forms.example.com"},
		"endpoint with query":        {Endpoint: "https://forms.googleapis.com/?key=secret"},
		"page size above limit":      {ResponsePageSize: 1001},
		"negative page size":         {ResponsePageSize: -1},
		"negative response limit":    {MaxResponseBytes: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := forms.New(config, credentials)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
	_, err := forms.New(forms.Config{}, nil)
	require.Error(t, err)
	_, err = forms.New(forms.Config{}, credentials, nil)
	require.Error(t, err)
	client, err := forms.New(forms.Config{}, credentials, forms.WithHTTPClient(&http.Client{}))
	require.NoError(t, err)
	require.NotNil(t, client)
}

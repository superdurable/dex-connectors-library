// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewValidatesDomainOptionsAndLimits(t *testing.T) {
	credentials := testCredentialProvider()
	for _, domain := range []string{"", "Acme", "acme.freshdesk.com", "https://acme.freshdesk.com", "-acme", "acme-", "ac_me", strings.Repeat("a", 64)} {
		_, err := freshdesk.New(freshdesk.Config{Domain: domain}, credentials)
		require.Error(t, err, domain)
	}
	_, err := freshdesk.New(freshdesk.Config{Domain: testDomain, MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = freshdesk.New(freshdesk.Config{Domain: testDomain}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = freshdesk.New(freshdesk.Config{Domain: testDomain}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	for _, baseURL := range []string{"http://freshdesk.example.com/api/v2", "https://user:secret@acme.freshdesk.com/api/v2", "https://acme.freshdesk.com/api/v2?x=1"} {
		_, err = freshdesk.New(freshdesk.Config{Domain: testDomain}, credentials, freshdesk.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
		require.NotContains(t, err.Error(), "secret")
	}
	client, err := freshdesk.New(freshdesk.Config{Domain: "acme-support2"}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsSendTheAPIKeyAsBasicUserNameWithPasswordX(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(35436, 2, nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `[]`)
	})
	_, err := sdkgo.RunQuery(newFreshdeskDexContext("basic-auth"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
	require.NoError(t, err)
	request := provider.request(0)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(testAPIKey+":X")), request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Accept"))
	require.Equal(t, "/api/v2/tickets/35436", request.path)
}

func TestFailureStatusesSelectBranchesWithOnlyFreshdeskErrorTokens(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{
			name: "validation", status: http.StatusBadRequest,
			body: `{"description":"SENTINEL Validation failed","errors":[{"field":"status","message":"SENTINEL It should be one of these values: 2,3,4,5","code":"invalid_value"},` +
				`{"field":"Bad Field","message":"SENTINEL","code":"x"},{"field":"cf_order","code":"datatype_mismatch"}]}`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation,
			message: "Freshdesk rejected the request (HTTP 400) [errors: cf_order=datatype_mismatch, status=invalid_value]",
		},
		{
			name: "authentication", status: http.StatusUnauthorized, body: `{"code":"invalid_credentials","message":"SENTINEL You have to be logged in to perform this action."}`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "Freshdesk rejected the request (HTTP 401) [invalid_credentials]",
		},
		{
			name: "forbidden", status: http.StatusForbidden, body: `{"code":"access_denied","message":"SENTINEL You are not authorized to perform this action."}`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthorization, message: "Freshdesk rejected the request (HTTP 403) [access_denied]",
		},
		{
			name: "not found", status: http.StatusNotFound, body: ``,
			branch: freshdesk.GetTicketBranchNotFound, kind: sdkgo.FailureNotFound, message: "Freshdesk found no such resource (HTTP 404)",
		},
		{
			name: "method not allowed", status: http.StatusMethodNotAllowed, body: `{"code":"method_not_allowed","message":"SENTINEL"}`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "Freshdesk rejected the request (HTTP 405) [method_not_allowed]",
		},
		{
			name: "plain text", status: http.StatusBadRequest, body: `SENTINEL SSL is required`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Freshdesk rejected the request (HTTP 400)",
		},
		{
			name: "reflected key", status: http.StatusBadRequest, body: `{"code":"` + testAPIKey + `","errors":[{"field":"email","code":"` + testAPIKey + `"}]}`,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Freshdesk rejected the request (HTTP 400) [errors: email]",
		},
		{
			name: "redirect", status: http.StatusFound, body: ``,
			branch: freshdesk.GetTicketBranchProviderRejected, kind: sdkgo.FailureProtocol,
			message: "Freshdesk redirected the request (HTTP 302); check that the domain is the freshdesk.com helpdesk domain",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://attacker.example.com/steal")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newFreshdeskDexContext("failure-"+test.name), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
		})
	}
}

func TestRetryableStatusesHonorRetryAfterForReads(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		kind       sdkgo.FailureKind
		delay      time.Duration
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryAfter: "34", kind: sdkgo.FailureRateLimit, delay: 34 * time.Second},
		{name: "unavailable", status: http.StatusServiceUnavailable, retryAfter: "4", kind: sdkgo.FailureAvailability, delay: 4 * time.Second},
		{name: "server error", status: http.StatusInternalServerError, kind: sdkgo.FailureAvailability},
		{name: "request timeout", status: http.StatusRequestTimeout, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, `{"code":"SENTINEL busy"}`)
			})
			_, err := sdkgo.RunQuery(newFreshdeskDexContext("retry-"+test.name), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, "SENTINEL")
			var retryAfter *dex.RetryAfterError
			if test.delay == 0 {
				require.False(t, errors.As(err, &retryAfter), "no Retry-After uses the Step retry policy")
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.delay, retryAfter.After)
		})
	}
}

func TestReadTransportFailuresRetry(t *testing.T) {
	_, err := sdkgo.RunQuery(newFreshdeskDexContext("transport"), newFreshdeskClient(t, closedLoopbackURL(t)).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	_, err = sdkgo.RunQuery(newFreshdeskDexContext("dropped"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedMalformedAndCredentialReflectingResponsesSelectInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		kind sdkgo.FailureKind
	}{
		{name: "oversized", body: `{"id":1,"description_text":"` + strings.Repeat("x", 4096) + `"}`, kind: sdkgo.FailureResponseTooLarge},
		{name: "reflects credential", body: `{"id":35436,"subject":"` + testAPIKey + `"}`, kind: sdkgo.FailureProtocol},
		{name: "malformed", body: `{"id":"not-a-number"}`, kind: sdkgo.FailureProtocol},
		{name: "another ticket", body: ticketBodyJSON(1, 2, nil), kind: sdkgo.FailureProtocol},
		{name: "array", body: `[]`, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.body)
			})
			client, err := freshdesk.New(freshdesk.Config{Domain: testDomain, MaxResponseBytes: 2048}, testCredentialProvider(), freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newFreshdeskDexContext("invalid-"+test.name), client.GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, freshdesk.GetTicketBranchInvalidResponse, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), testAPIKey)
		})
	}
}

func TestUnusableCredentialsSelectDefectBeforeAnyRequest(t *testing.T) {
	for name, credentials := range map[string]freshdesk.Credentials{
		"missing key":    {},
		"key with space": {APIKey: sdkgo.NewSecretString("two words")},
		"key with colon": {APIKey: sdkgo.NewSecretString("key:password")},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			client, err := freshdesk.New(freshdesk.Config{Domain: testDomain},
				sdkgo.StaticCredentialProvider[freshdesk.Credentials]{freshdeskConnection: credentials}, freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newFreshdeskDexContext("credentials-"+name), client.GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, freshdesk.GetTicketBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
			require.Zero(t, provider.requestCount())
		})
	}
}

func TestReceiptCarriesOnlyASafeRequestID(t *testing.T) {
	for requestID, expected := range map[string]string{"req-01.abc:9": "req-01.abc:9", "two words": "", strings.Repeat("a", 129): ""} {
		provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
			response.Header().Set("X-Request-Id", requestID)
			if index == 0 {
				writeJSON(t, response, http.StatusOK, ticketBodyJSON(35436, 2, nil))
				return
			}
			writeJSON(t, response, http.StatusOK, `[]`)
		})
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("request-id"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 35436})
		require.NoError(t, err)
		require.Equal(t, expected, result.Receipt.ProviderRequestID)
		require.Equal(t, "35436", result.Receipt.ProviderObjectID)
	}
}

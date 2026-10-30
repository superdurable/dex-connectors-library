// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewAcceptsOnlyABareSubdomain(t *testing.T) {
	for _, domain := range []string{"acme", "acme-support", "a1"} {
		_, err := gorgias.New(gorgias.Config{Domain: domain}, testCredentialProvider())
		require.NoError(t, err, domain)
	}
	for _, domain := range []string{
		"", "Acme", "https://acme.gorgias.com", "acme.gorgias.com", "acme/api", "acme.evil.example", "-acme", "acme-", "acme gorgias",
		"acme?x=1", "acme#fragment", "user@acme", strings.Repeat("a", 64),
	} {
		_, err := gorgias.New(gorgias.Config{Domain: domain}, testCredentialProvider())
		require.Error(t, err, domain)
		if domain != "" {
			require.Equal(t, "Gorgias domain must be lowercase letters, digits, and hyphens, such as acme for https://acme.gorgias.com, without https://, .gorgias.com, or a path",
				err.Error(), "the error never repeats the configured value")
		}
	}
	_, err := gorgias.New(gorgias.Config{Domain: testDomain, MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = gorgias.New(gorgias.Config{Domain: testDomain}, nil)
	require.Error(t, err)
	_, err = gorgias.New(gorgias.Config{Domain: testDomain}, testCredentialProvider(), gorgias.WithAPIBaseURL("http://acme.example.com/api"))
	require.Error(t, err, "a non-loopback base URL must use HTTPS")
}

func TestRequestsUseHTTPBasicWithTheEmailAndAPIKey(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("X-Request-Id", "req-0001")
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing"}, nil))
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("basic"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Equal(t, gorgias.GetTicketBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/api/tickets/5512", request.path)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(testEmail+":"+testAPIKey)), request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Accept"))
	require.Equal(t, "req-0001", result.Receipt.ProviderRequestID)
	require.Equal(t, map[string]string{"apiCallLimit": "3/40"}, result.Receipt.Metadata)
	require.Equal(t, "5512", result.Receipt.ProviderObjectID)
}

func TestProviderErrorsMapWithoutGorgiasMessageText(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{name: "validation", status: http.StatusBadRequest, body: `{"error":{"msg":"SENTINEL Invalid data","data":{"subject":["SENTINEL too long"],"Bad Field":["x"]}}}`,
			branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Gorgias rejected the request (HTTP 400) [fields: subject]"},
		{name: "token message", status: http.StatusConflict, body: `{"error":{"msg":"ticket_merged"}}`,
			branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureConflict, message: "Gorgias rejected the request (HTTP 409) [ticket_merged]"},
		{name: "authentication", status: http.StatusUnauthorized, body: `{"error":{"msg":"SENTINEL bad token"}}`,
			branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "Gorgias rejected the request (HTTP 401)"},
		{name: "authorization", status: http.StatusForbidden, branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "missing", status: http.StatusNotFound, branch: gorgias.GetTicketBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "merged redirect", status: http.StatusMovedPermanently, branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "echoed key", status: http.StatusBadRequest, body: `{"error":{"msg":"` + testAPIKey + `","data":{"x_` + testAPIKey + `":1}}}`,
			branch: gorgias.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Gorgias rejected the request (HTTP 400)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusMovedPermanently {
					response.Header().Set("Location", "https://evil.example/api/tickets/1")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newGorgiasDexContext("errors"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 1})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotNil(t, result.Failure)
			require.Equal(t, test.kind, result.Failure.Kind)
			if test.message != "" {
				require.Equal(t, test.message, result.Failure.Message)
			}
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
		})
	}
}

func TestRateLimitsAndOutagesRetryReadsAfterRetryAfter(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   sdkgo.FailureKind
	}{{http.StatusTooManyRequests, sdkgo.FailureRateLimit}, {http.StatusServiceUnavailable, sdkgo.FailureAvailability}} {
		provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			response.Header().Set("Retry-After", "7")
			writeJSON(t, response, test.status, `{"error":{"msg":"SENTINEL"}}`)
		})
		_, err := sdkgo.RunQuery(newGorgiasDexContext("retry"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 1})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "status %d enters the Execute retry policy", test.status)
		require.Equal(t, test.kind, retry.Failure.Kind)
		require.NotContains(t, retry.Failure.Message, "SENTINEL")
		var retryAfter *dex.RetryAfterError
		require.ErrorAs(t, err, &retryAfter)
		require.Equal(t, 7*time.Second, retryAfter.After)
	}
	client, err := gorgias.New(gorgias.Config{Domain: testDomain}, testCredentialProvider(), gorgias.WithAPIBaseURL(closedLoopbackURL(t)))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newGorgiasDexContext("refused"), client.GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 1})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedMalformedAndCredentialReflectingResponsesAreInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":  `{"id":`,
		"reflection": encodeJSON(t, map[string]any{"id": 1, "status": "open", "created_datetime": "2026-01-26T14:02:00", "subject": testAPIKey}),
		"oversized":  `{"id":1,"status":"open","created_datetime":"2026-01-26T14:02:00","subject":"` + strings.Repeat("x", 2048) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := gorgias.New(gorgias.Config{Domain: testDomain, MaxResponseBytes: 1024}, testCredentialProvider(), gorgias.WithAPIBaseURL(provider.URL+"/api"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newGorgiasDexContext("invalid"), client.GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 1})
			require.NoError(t, err)
			require.Equal(t, gorgias.GetTicketBranchInvalidResponse, result.Branch)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), testAPIKey)
		})
	}
}

func TestInvalidConnectionCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, credentials := range map[string]gorgias.Credentials{
		"display email":  {Email: "Agent <agent@acme.example.com>", APIKey: sdkgo.NewSecretString(testAPIKey)},
		"colon in email": {Email: "agent:x@acme.example.com", APIKey: sdkgo.NewSecretString(testAPIKey)},
		"spaced key":     {Email: testEmail, APIKey: sdkgo.NewSecretString("key with spaces")},
		"missing key":    {Email: testEmail},
	} {
		client, err := gorgias.New(gorgias.Config{Domain: testDomain},
			sdkgo.StaticCredentialProvider[gorgias.Credentials]{gorgiasConnection: credentials}, gorgias.WithAPIBaseURL(provider.URL+"/api"))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newGorgiasDexContext("credentials"), client.GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 1})
		require.NoError(t, err, name)
		require.Equal(t, gorgias.GetTicketBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	}
}

func TestTimestampsWithoutAnOffsetAreUTC(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, nil))
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("timestamps"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 1, 26, 14, 2, 0, 384938000, time.UTC), result.Value.Ticket.CreatedAt)
	require.Equal(t, time.Date(2026, 1, 27, 10, 5, 0, 0, time.UTC), *result.Value.Ticket.LastMessageAt)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func getEnvelope(t *testing.T, client *docusign.Client) (docusign.GetEnvelopeResult, error) {
	t.Helper()
	return sdkgo.RunQuery(newTestDexContext("get"), client.GetEnvelope(), testConnection, docusign.GetEnvelopeInput{EnvelopeID: testEnvelopeID})
}

func TestGetEnvelopeReadsTheDefaultAccountOnItsRegionalHost(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath: respondJSON(http.StatusOK, envelopeJSON(testEnvelopeID, "sent", "marker"))})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})

	result, err := getEnvelope(t, client)
	require.NoError(t, err)
	require.Equal(t, docusign.GetEnvelopeBranchFound, result.Branch)
	envelope := result.Value
	require.Equal(t, testEnvelopeID, envelope.EnvelopeID)
	require.Equal(t, docusign.EnvelopeStatusSent, envelope.Status)
	require.Equal(t, time.Date(2026, time.October, 4, 10, 0, 0, 123000000, time.UTC), *envelope.CreatedAt)
	require.Nil(t, envelope.CompletedAt)
	require.Equal(t, []docusign.EnvelopeCustomField{{Name: "dexIdempotencyKey", Value: "marker"}, {Name: "dexSigningRequestId", Value: "opp-123"}}, envelope.CustomFields)
	require.Equal(t, testEnvelopeID, result.Receipt.ProviderObjectID)

	requests := fake.recordedRequests()
	require.Len(t, requests, 2)
	require.Equal(t, "account.docusign.com", requests[0].host, "userinfo goes to the production account server")
	require.Equal(t, "/oauth/userinfo", requests[0].path)
	require.Equal(t, productionBaseHost, requests[1].host, "the API call goes to the default account's base URI")
	require.Equal(t, "custom_fields", requests[1].query.Get("include"))
	require.Equal(t, "Bearer "+sentinelAccessToken, requests[1].authorization)
	requireNoSecrets(t, result)

	_, err = getEnvelope(t, client)
	require.NoError(t, err)
	require.Len(t, fake.requestsTo(http.MethodGet, "/oauth/userinfo"), 1, "the account is cached for the access token")
}

func TestGetEnvelopeUsesTheConfiguredAccountAndTheDeveloperEnvironment(t *testing.T) {
	otherEnvelopePath := "/restapi/v2.1/accounts/" + otherAccountID + "/envelopes/" + testEnvelopeID
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"GET /oauth/userinfo": respondJSON(http.StatusOK, `{"accounts":[`+
			`{"account_id":"`+testAccountID+`","is_default":"true","base_uri":"https://demo.docusign.net"},`+
			`{"account_id":"`+otherAccountID+`","is_default":"false","base_uri":"https://demo.docusign.net/"}]}`),
		"GET " + otherEnvelopePath: respondJSON(http.StatusOK, envelopeJSON(testEnvelopeID, "completed", "marker")),
	})
	credentials := productionCredentials()
	credentials.AuthMethodID = docusign.DeveloperOAuthAuthMethodID
	client := newTestClient(t, fake, staticCredentials(credentials), docusign.Config{AccountID: "A4EC37D6-4444-5555-6666-143885C333AA"})

	result, err := getEnvelope(t, client)
	require.NoError(t, err)
	require.Equal(t, docusign.GetEnvelopeBranchFound, result.Branch)
	requests := fake.recordedRequests()
	require.Equal(t, "account-d.docusign.com", requests[0].host)
	require.Equal(t, "demo.docusign.net", requests[1].host)
	require.Equal(t, otherEnvelopePath, requests[1].path)
}

func TestGetEnvelopeRefusesAnAccountHostOutsideTheEnvironment(t *testing.T) {
	for name, test := range map[string]struct {
		authMethodID string
		baseURI      string
	}{
		"another domain":                    {docusign.ProductionOAuthAuthMethodID, "https://na3.docusign.net.attacker.example"},
		"plain HTTP":                        {docusign.ProductionOAuthAuthMethodID, "http://na3.docusign.net"},
		"a port":                            {docusign.ProductionOAuthAuthMethodID, "https://na3.docusign.net:8443"},
		"user information":                  {docusign.ProductionOAuthAuthMethodID, "https://user@na3.docusign.net"},
		"a path":                            {docusign.ProductionOAuthAuthMethodID, "https://na3.docusign.net/restapi"},
		"the demo host in production":       {docusign.ProductionOAuthAuthMethodID, "https://demo.docusign.net"},
		"a production host for a developer": {docusign.DeveloperOAuthAuthMethodID, "https://na3.docusign.net"},
		"two labels":                        {docusign.ProductionOAuthAuthMethodID, "https://a.b.docusign.net"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET /oauth/userinfo": respondJSON(http.StatusOK, userInfoJSON(testAccountID, test.baseURI))})
			credentials := productionCredentials()
			credentials.AuthMethodID = test.authMethodID
			result, err := getEnvelope(t, newTestClient(t, fake, staticCredentials(credentials), docusign.Config{}))
			require.NoError(t, err)
			require.Equal(t, docusign.GetEnvelopeBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Len(t, fake.recordedRequests(), 1, "the access token never reaches an unvalidated host")
		})
	}
}

func TestGetEnvelopeReportsAccountSelectionProblemsWithoutAnAPICall(t *testing.T) {
	for name, test := range map[string]struct {
		userInfo       string
		config         docusign.Config
		expectedBranch sdkgo.BranchID
	}{
		"configured account the user lacks": {userInfoJSON(testAccountID, "https://na3.docusign.net"), docusign.Config{AccountID: "00000000-0000-0000-0000-000000000000"}, docusign.GetEnvelopeBranchDefect},
		"no default account":                {`{"accounts":[{"account_id":"` + testAccountID + `","is_default":false,"base_uri":"https://na3.docusign.net"}]}`, docusign.Config{}, docusign.GetEnvelopeBranchDefect},
		"no accounts":                       {`{"accounts":[]}`, docusign.Config{}, docusign.GetEnvelopeBranchInvalidResponse},
		"not JSON":                          {`<html>`, docusign.Config{}, docusign.GetEnvelopeBranchInvalidResponse},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET /oauth/userinfo": respondJSON(http.StatusOK, test.userInfo)})
			result, err := getEnvelope(t, newTestClient(t, fake, staticCredentials(productionCredentials()), test.config))
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Len(t, fake.recordedRequests(), 1)
		})
	}
}

func TestGetEnvelopeRetriesRateLimitsAndOutagesWithDocuSignsDelay(t *testing.T) {
	resetAt := fixedNow.Add(17 * time.Minute)
	for name, test := range map[string]struct {
		status        int
		body          string
		header        map[string]string
		expectedKind  sdkgo.FailureKind
		expectedDelay time.Duration
	}{
		"hourly limit with reset":    {http.StatusBadRequest, docusignError("HOURLY_APIINVOCATION_LIMIT_EXCEEDED"), map[string]string{"X-RateLimit-Reset": strconv.FormatInt(resetAt.Unix(), 10)}, sdkgo.FailureRateLimit, 17 * time.Minute},
		"hourly limit without reset": {http.StatusBadRequest, docusignError("HOURLY_APIINVOCATION_LIMIT_EXCEEDED"), nil, sdkgo.FailureRateLimit, time.Hour},
		"burst limit":                {http.StatusTooManyRequests, docusignError("BURST_APIINVOCATION_LIMIT_EXCEEDED"), nil, sdkgo.FailureRateLimit, 30 * time.Second},
		"envelope polling limit":     {http.StatusBadRequest, docusignError("Hourly_Envelope_Polling_Limit_Exceeded"), nil, sdkgo.FailureRateLimit, time.Hour},
		"429 with Retry-After":       {http.StatusTooManyRequests, `{}`, map[string]string{"Retry-After": "12"}, sdkgo.FailureRateLimit, 12 * time.Second},
		"server error":               {http.StatusServiceUnavailable, docusignError("SERVICE_UNAVAILABLE"), nil, sdkgo.FailureAvailability, 0},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath: func(response http.ResponseWriter, _ *http.Request) {
				for headerName, value := range test.header {
					response.Header().Set(headerName, value)
				}
				writeJSON(response, test.status, test.body)
			}})
			_, err := getEnvelope(t, newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}))
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.expectedKind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), sentinelMessage)
			var retryAfter *dex.RetryAfterError
			if test.expectedDelay == 0 {
				require.False(t, errors.As(err, &retryAfter))
			} else {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.expectedDelay, retryAfter.After)
			}
		})
	}
}

func TestGetEnvelopeClassifiesConclusiveAnswers(t *testing.T) {
	for name, test := range map[string]struct {
		status         int
		body           string
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		"missing envelope":           {http.StatusBadRequest, docusignError("ENVELOPE_DOES_NOT_EXIST"), docusign.GetEnvelopeBranchNotFound, sdkgo.FailureNotFound},
		"404":                        {http.StatusNotFound, `{}`, docusign.GetEnvelopeBranchNotFound, sdkgo.FailureNotFound},
		"no access to the envelope":  {http.StatusBadRequest, docusignError("USER_NOT_ENVELOPE_SENDER_OR_RECIPIENT"), docusign.GetEnvelopeBranchProviderRejected, sdkgo.FailureAuthorization},
		"rejected token":             {http.StatusUnauthorized, docusignError("USER_AUTHENTICATION_FAILED"), docusign.GetEnvelopeBranchProviderRejected, sdkgo.FailureAuthentication},
		"malformed envelope":         {http.StatusOK, `{"envelopeId":"not-a-guid","status":"sent"}`, docusign.GetEnvelopeBranchInvalidResponse, sdkgo.FailureProtocol},
		"another envelope":           {http.StatusOK, envelopeJSON(otherEnvelopeID, "sent", "marker"), docusign.GetEnvelopeBranchInvalidResponse, sdkgo.FailureProtocol},
		"unparseable timestamp":      {http.StatusOK, `{"envelopeId":"` + testEnvelopeID + `","status":"sent","sentDateTime":"yesterday"}`, docusign.GetEnvelopeBranchInvalidResponse, sdkgo.FailureProtocol},
		"other request rejection":    {http.StatusBadRequest, docusignError("INVALID_REQUEST_PARAMETER"), docusign.GetEnvelopeBranchProviderRejected, sdkgo.FailureProviderRejection},
		"unauthorized without retry": {http.StatusForbidden, `not json`, docusign.GetEnvelopeBranchProviderRejected, sdkgo.FailureAuthorization},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath: respondJSON(test.status, test.body)})
			result, err := getEnvelope(t, newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{}))
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			requireNoSecrets(t, result)
		})
	}
}

func TestGetEnvelopeRefreshesOnceAfterARejectedToken(t *testing.T) {
	tokenRequests := 0
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST /oauth/token": func(response http.ResponseWriter, _ *http.Request) {
			tokenRequests++
			writeJSON(response, http.StatusOK, `{"access_token":"replacement-token","token_type":"Bearer","refresh_token":"rotated","expires_in":28800}`)
		},
		"GET " + envelopePath: func(response http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer replacement-token" {
				writeJSON(response, http.StatusUnauthorized, docusignError("USER_AUTHENTICATION_FAILED"))
				return
			}
			writeJSON(response, http.StatusOK, envelopeJSON(testEnvelopeID, "sent", "marker"))
		},
	})
	expiresAt := time.Now().Add(time.Hour)
	source := testsupport.NewRefreshingCredentialSource(productionCredentials(), &expiresAt)
	result, err := getEnvelope(t, newTestClient(t, fake, source, docusign.Config{}))
	require.NoError(t, err)
	require.Equal(t, docusign.GetEnvelopeBranchFound, result.Branch)
	require.Equal(t, 1, tokenRequests)
	refreshed, _ := source.Current()
	require.Equal(t, "rotated", refreshed.RefreshToken.Reveal())
}

func TestGetEnvelopeValidatesItsInputAndConnectionWithoutARequest(t *testing.T) {
	fake := newFakeDocuSign(t, nil)
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	result, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetEnvelope(), testConnection, docusign.GetEnvelopeInput{EnvelopeID: "../templates"})
	require.NoError(t, err)
	require.Equal(t, docusign.GetEnvelopeBranchDefect, result.Branch)

	credentials := productionCredentials()
	credentials.AuthMethodID = "unknown"
	result, err = getEnvelope(t, newTestClient(t, fake, staticCredentials(credentials), docusign.Config{}))
	require.NoError(t, err)
	require.Equal(t, docusign.GetEnvelopeBranchDefect, result.Branch)
	require.Empty(t, fake.recordedRequests())

	_, err = docusign.New(docusign.Config{AccountID: "12345"}, staticCredentials(productionCredentials()))
	require.ErrorContains(t, err, "accountId")
}

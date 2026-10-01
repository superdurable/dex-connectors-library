// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInvalidEndpointsAndLimits(t *testing.T) {
	credentials := staticTrelloCredentials()
	_, err := trello.New(trello.Config{Endpoint: "http://api.trello.com/1"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = trello.New(trello.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = trello.New(trello.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = trello.New(trello.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := trello.New(trello.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, "https://api.trello.com/1", trello.DefaultConfig().Endpoint)
}

func TestRequestsSendTheKeyAndTokenOnlyInTheAuthorizationHeader(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "Replace badge reader", members: true}))
	})
	client := newTrelloClient(t, provider.URL+"/1")

	result, err := sdkgo.RunQuery(newTrelloDexContext("base-path"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/1/cards/"+testCardID, request.path)
	require.Equal(t, expectedAuthorization, request.authorization)
	require.NotContains(t, request.rawQuery, testAPIKey, "the key never travels in the URL")
	require.NotContains(t, request.rawQuery, testToken, "the token never travels in the URL")
	require.NotContains(t, request.rawQuery, "key=")
	require.NotContains(t, request.rawQuery, "token=")
	require.Equal(t, testCardID, result.Receipt.ProviderObjectID)
	require.Equal(t, testRequestID, result.Receipt.ProviderRequestID, "Trello's atl-request-id header is kept")
}

func TestPlainTextFailuresNameTheStatusButNeverTrelloText(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		kind   sdkgo.FailureKind
	}{
		{http.StatusBadRequest, "invalid id SENTINEL", sdkgo.FailureValidation},
		{http.StatusUnauthorized, "invalid token SENTINEL", sdkgo.FailureAuthentication},
		{http.StatusForbidden, "SENTINEL forbidden", sdkgo.FailureAuthorization},
		{http.StatusConflict, "SENTINEL conflict", sdkgo.FailureConflict},
		{449, "SENTINEL Sub-Request Failed", sdkgo.FailureProviderRejection},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeText(t, response, test.status, test.body)
		})
		client := newTrelloClient(t, provider.URL)
		result, err := sdkgo.RunQuery(newTrelloDexContext("safe-failure"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
		require.NoError(t, err)
		require.Equal(t, trello.GetCardBranchProviderRejected, result.Branch)
		require.Equal(t, test.kind, result.Failure.Kind)
		require.Equal(t, fmt.Sprintf("Trello rejected the card with HTTP %d", test.status), result.Failure.Message)
		requireNoSentinel(t, result)
		require.Equal(t, 1, provider.requestCount(), "a token cannot be refreshed, so nothing is resent")
	}
}

func TestResponseThatReflectsACredentialIsNeverReturned(t *testing.T) {
	for name, reflected := range map[string]string{"token": testToken, "API key": testAPIKey} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "echo " + reflected}))
		})
		client := newTrelloClient(t, provider.URL)
		result, err := sdkgo.RunQuery(newTrelloDexContext("reflection"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
		require.NoError(t, err, name)
		require.Equal(t, trello.GetCardBranchInvalidResponse, result.Branch, name)
		requireNoSentinel(t, result)
	}
}

func TestRateLimitedReadWaitsForRetryAfterOrOneTrelloWindow(t *testing.T) {
	for name, test := range map[string]struct {
		retryAfter string
		delay      time.Duration
	}{
		"with Retry-After":    {retryAfter: "7", delay: 7 * time.Second},
		"without Retry-After": {delay: 10 * time.Second},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			if test.retryAfter != "" {
				response.Header().Set("Retry-After", test.retryAfter)
			}
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":"API_TOKEN_LIMIT_EXCEEDED","message":"Rate limit exceeded SENTINEL"}`)
		})
		client := newTrelloClient(t, provider.URL)
		_, err := sdkgo.RunQuery(newTrelloDexContext("rate-limit"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
		retry := requireRetry(t, err, sdkgo.FailureRateLimit)
		require.Equal(t, "Trello rate limited the card [API_TOKEN_LIMIT_EXCEEDED]", retry.Failure.Message, name)
		var retryAfter *dex.RetryAfterError
		require.ErrorAs(t, err, &retryAfter, name)
		require.Equal(t, test.delay, retryAfter.After, name)
	}
}

func TestServerErrorsAndLostResponsesRetryReads(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"500":           func(response http.ResponseWriter) { writeText(t, response, http.StatusInternalServerError, "SENTINEL") },
		"504":           func(response http.ResponseWriter) { writeText(t, response, http.StatusGatewayTimeout, "SENTINEL") },
		"lost response": func(response http.ResponseWriter) { dropConnection(t, response) },
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) { reply(response) })
		client := newTrelloClient(t, provider.URL)
		_, err := sdkgo.RunQuery(newTrelloDexContext("retry-read"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, name)
	}
}

func TestUnavailableOrUnsafeCredentialsSelectDefectWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, credentials := range map[string]trello.Credentials{
		"missing token":        {APIKey: sdkgo.NewSecretString(testAPIKey)},
		"missing key":          {Token: sdkgo.NewSecretString(testToken)},
		"header-unsafe token":  {APIKey: sdkgo.NewSecretString(testAPIKey), Token: sdkgo.NewSecretString("token\r\nX-Injected: 1")},
		"quote-breaking token": {APIKey: sdkgo.NewSecretString(testAPIKey), Token: sdkgo.NewSecretString(`token",oauth_token="other`)},
		"backslash in the key": {APIKey: sdkgo.NewSecretString(`key\`), Token: sdkgo.NewSecretString(testToken)},
	} {
		client, err := trello.New(trello.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[trello.Credentials]{trelloConnection: credentials})
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(newTrelloDexContext("no-credentials"), client.CreateCard(), trelloConnection, validCreateCardInput())
		require.NoError(t, err, name)
		require.Equal(t, trello.CreateCardBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
	}
	unknown, err := trello.New(trello.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[trello.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newTrelloDexContext("unknown-connection"), unknown.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchDefect, result.Branch)
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
	})
	client := newTrelloClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newTrelloDexContext("redirect"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestOversizedResponsesSelectInvalidResponse(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: strings.Repeat("x", 2048)}))
	})
	client, err := trello.New(trello.Config{Endpoint: provider.URL, MaxResponseBytes: 1024}, staticTrelloCredentials())
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newTrelloDexContext("oversized"), client.GetCard(), trelloConnection, trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

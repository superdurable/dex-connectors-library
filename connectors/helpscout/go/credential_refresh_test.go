// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// documentedTokenResponse is Help Scout's documented client credentials answer: no refresh token, two days.
const documentedTokenResponse = `{"token_type":"bearer","access_token":"access-2","expires_in":172800}`

func appCredentials(accessToken string) helpscout.Credentials {
	return helpscout.Credentials{
		AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"),
		AccessToken: sdkgo.NewSecretString(accessToken), WebhookSecret: sdkgo.NewSecretString(sentinelWebhookSecret),
	}
}

// clientCredentialsTokenRoute answers the documented form and rejects anything else.
func clientCredentialsTokenRoute(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != "client_credentials" ||
		request.PostForm.Get("client_id") != "app-id" || request.PostForm.Get("client_secret") != "app-secret" ||
		request.PostForm.Has("scope") || request.PostForm.Has("refresh_token") || request.Header.Get("Authorization") != "" {
		writeJSON(response, http.StatusBadRequest, `{"error":"invalid_request"}`)
		return
	}
	writeJSON(response, http.StatusOK, documentedTokenResponse)
}

func TestCredentialRefreshDriverObtainsATokenWithTheDocumentedClientCredentialsForm(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": clientCredentialsTokenRoute})
	driver := helpscout.NewCredentialRefreshDriver(fake.redirectingClient())
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[helpscout.Credentials]{Credentials: appCredentials(""), Now: fixedNow})
	require.NoError(t, err)
	require.Equal(t, "access-2", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "app-id", result.Credentials.AppID)
	require.Equal(t, "app-secret", result.Credentials.AppSecret.Reveal())
	require.Equal(t, sentinelWebhookSecret, result.Credentials.WebhookSecret.Reveal(), "the webhook secret survives the renewal")
	require.WithinDuration(t, time.Now().Add(48*time.Hour), result.ExpiresAt, time.Minute, "the expiry comes from expires_in")
	requests := fake.requestsTo(http.MethodPost, "/v2/oauth2/token")
	require.Len(t, requests, 1)
	require.Equal(t, "application/x-www-form-urlencoded", requests[0].contentType)
}

func TestCredentialRefreshDriverRenewsAnAbsentOrExpiringTokenAndKeepsOneWithoutExpiry(t *testing.T) {
	driver := helpscout.NewCredentialRefreshDriver(nil)
	inOneDay, inFourMinutes := fixedNow.Add(24*time.Hour), fixedNow.Add(4*time.Minute)
	for _, test := range []struct {
		name              string
		accessToken       string
		expiresAt         *time.Time
		isRefreshRequired bool
	}{
		{"no token yet", "", nil, true},
		{"token valid for a day", "access-1", &inOneDay, false},
		{"token inside the five-minute skew", "access-1", &inFourMinutes, true},
		{"token without a recorded expiry is kept until a 401", "access-1", nil, false},
	} {
		state := sdkgo.CredentialRefreshState[helpscout.Credentials]{Credentials: appCredentials(test.accessToken), ExpiresAt: test.expiresAt, Now: fixedNow}
		require.Equal(t, test.isRefreshRequired, driver.RefreshRequired(state), test.name)
	}
}

func TestCredentialRefreshDriverClassifiesTerminalAndRetryableFailures(t *testing.T) {
	for _, test := range []struct {
		status           int
		body             string
		isReauthRequired bool
	}{
		{http.StatusUnauthorized, `{"error":"invalid_client","error_description":"` + providerSecretMessage + `"}`, true},
		{http.StatusBadRequest, `{"error":"unauthorized_client"}`, true},
		{http.StatusBadGateway, `{"error":"invalid_client"}`, false},
		{http.StatusTooManyRequests, `{"error":"rate_limited"}`, false},
		{http.StatusOK, `{"token_type":"bearer","access_token":"x"}`, false},
	} {
		fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": respondJSON(test.status, test.body)})
		_, err := helpscout.NewCredentialRefreshDriver(fake.redirectingClient()).Refresh(context.Background(),
			sdkgo.CredentialRefreshState[helpscout.Credentials]{Credentials: appCredentials(""), Now: fixedNow})
		require.Error(t, err, test.status)
		require.Equal(t, test.isReauthRequired, sdkgo.IsReauthorizationRequired(err), test.status)
		require.NotContains(t, err.Error(), providerSecretMessage)
	}
	_, err := helpscout.NewCredentialRefreshDriver(nil).Refresh(context.Background(), sdkgo.CredentialRefreshState[helpscout.Credentials]{
		Credentials: helpscout.Credentials{AppID: "app-id"}, Now: fixedNow,
	})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "a missing App Secret is never sent")
}

// TestRefreshingConnectionObtainsAndStoresTheTokenOnFirstUse starts from the credentials Dex Web saves for
// this connector: an App ID, App Secret, and webhook secret, with no token and no expiry.
func TestRefreshingConnectionObtainsAndStoresTheTokenOnFirstUse(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/oauth2/token":             clientCredentialsTokenRoute,
		"GET /v2/conversations/501":         requireBearer("access-2", respondJSON(http.StatusOK, conversationJSON(501, "active", 123))),
		"GET /v2/conversations/501/threads": requireBearer("access-2", respondJSON(http.StatusOK, threadPageJSON(nil, false))),
	})
	credentials := testsupport.NewRefreshingCredentialSource(appCredentials(""), nil)
	client := newRefreshingClient(t, credentials, fake.redirectingClient())
	for range 2 {
		result, err := sdkgo.RunQuery(newStepContext("get"), client.GetConversation(), testConnection, helpscout.GetConversationInput{ConversationID: 501})
		require.NoError(t, err)
		require.Equal(t, helpscout.GetConversationBranchFound, result.Branch)
		requireSecretFree(t, result)
	}
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1, "the stored token is reused until it nears expiry")

	stored, expiresAt := credentials.Current()
	require.Equal(t, "access-2", stored.AccessToken.Reveal())
	require.Equal(t, "app-secret", stored.AppSecret.Reveal())
	require.Equal(t, sentinelWebhookSecret, stored.WebhookSecret.Reveal())
	require.NotNil(t, expiresAt)
	require.WithinDuration(t, time.Now().Add(48*time.Hour), *expiresAt, time.Minute)
}

func TestRefreshingConnectionObtainsANewTokenAfterHelpScoutAnswers401(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/oauth2/token":             clientCredentialsTokenRoute,
		"GET /v2/conversations/501":         requireBearer("access-2", respondJSON(http.StatusOK, conversationJSON(501, "active", 123))),
		"GET /v2/conversations/501/threads": requireBearer("access-2", respondJSON(http.StatusOK, threadPageJSON(nil, false))),
	})
	inOneDay := time.Now().Add(24 * time.Hour).UTC()
	credentials := testsupport.NewRefreshingCredentialSource(helpscout.Credentials{
		AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"), AccessToken: sdkgo.NewSecretString("access-1"),
	}, &inOneDay)
	result, err := sdkgo.RunQuery(newStepContext("get"), newRefreshingClient(t, credentials, fake.redirectingClient()).GetConversation(),
		testConnection, helpscout.GetConversationInput{ConversationID: 501})
	require.NoError(t, err)
	require.Equal(t, helpscout.GetConversationBranchFound, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501"), 2, "the rejected read is repeated once")
	stored, _ := credentials.Current()
	require.Equal(t, "access-2", stored.AccessToken.Reveal())
}

func TestARejectedAppNeedsNewCredentialsAndATokenOutageRetries(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		isRetry bool
	}{
		{"token outage", http.StatusServiceUnavailable, `{"error":"server_error"}`, true},
		{"deleted app", http.StatusUnauthorized, `{"error":"invalid_client"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": respondJSON(test.status, test.body)})
			credentials := testsupport.NewRefreshingCredentialSource(helpscout.Credentials{
				AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"),
			}, nil)
			result, err := sdkgo.RunQuery(newStepContext("search"), newRefreshingClient(t, credentials, fake.redirectingClient()).SearchConversations(),
				testConnection, helpscout.SearchConversationsInput{})
			if test.isRetry {
				retry := requireRetry(t, err, sdkgo.FailureAvailability)
				require.Contains(t, retry.Failure.Message, "obtained yet")
			} else {
				require.NoError(t, err)
				require.Equal(t, helpscout.SearchConversationsBranchDefect, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, "rejected the App ID or App Secret")
			}
			require.Empty(t, fake.requestsTo(http.MethodGet, "/v2/conversations"), "no API call without a usable token")
		})
	}
}

// TestWebhookEndpointRenewsAnExpiredTokenToReadTheWebhookSecret covers an idle connection: project storage
// refuses an expired record, so EndpointConfig.CredentialRefresh renews it before verifying.
func TestWebhookEndpointRenewsAnExpiredTokenToReadTheWebhookSecret(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": clientCredentialsTokenRoute})
	expiredAt := time.Now().Add(-time.Minute).UTC()
	credentials := testsupport.NewRefreshingCredentialSource(appCredentials("access-1"), &expiredAt)
	fixture := startWebhookFixture(t, newRefreshingClient(t, credentials, fake.redirectingClient()), helpscout.ConversationEventTriggerConfiguration{})
	body := conversationJSON(501, "active", 123)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(body))
	request.Header.Set("X-HelpScout-Event", helpscout.WebhookEventConversationCreated)
	request.Header.Set("X-HelpScout-Signature", helpScoutSignature(sentinelWebhookSecret, body))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "an idle connection keeps receiving after its access token expires")
	require.EqualValues(t, 501, fixture.receiveEvent(t).Payload.Conversation.ID)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1)
	stored, _ := credentials.Current()
	require.Equal(t, "access-2", stored.AccessToken.Reveal())
}

// TestWebhookEndpointOfAFreshConnectionVerifiesWithoutObtainingAToken proves a delivery needs only the
// webhook secret: a token outage cannot answer 503 to a connection that has no token yet.
func TestWebhookEndpointOfAFreshConnectionVerifiesWithoutObtainingAToken(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": respondJSON(http.StatusServiceUnavailable, `{"error":"server_error"}`)})
	credentials := testsupport.NewRefreshingCredentialSource(appCredentials(""), nil)
	fixture := startWebhookFixture(t, newRefreshingClient(t, credentials, fake.redirectingClient()), helpscout.ConversationEventTriggerConfiguration{})
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationCreated, conversationJSON(501, "active", 123), sentinelWebhookSecret))
	require.EqualValues(t, 501, fixture.receiveEvent(t).Payload.Conversation.ID)
	require.Empty(t, fake.recordedRequests(), "verification never waits for the token endpoint")
}

// requireBearer answers 401 unless the request carries accessToken.
func requireBearer(accessToken string, handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+accessToken {
			writeJSON(response, http.StatusUnauthorized, helpScoutErrorBody("Unauthorized", nil))
			return
		}
		handler(response, request)
	}
}

// newRefreshingClient uses the real clock, because a renewed expiry is compared with the wall-clock time.
func newRefreshingClient(
	t *testing.T, credentials *testsupport.RefreshingCredentialSource[helpscout.Credentials], httpClient *http.Client,
) *helpscout.Client {
	t.Helper()
	client, err := helpscout.New(helpscout.Config{}, credentials, helpscout.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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

// TestRenewingLocalConnectionObtainsAndStoresTheTokenOnFirstUse starts from the record Dex Web saves for
// this connector: an App ID, App Secret, and webhook secret, with no token and no expiry.
func TestRenewingLocalConnectionObtainsAndStoresTheTokenOnFirstUse(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/oauth2/token":             clientCredentialsTokenRoute,
		"GET /v2/conversations/501":         requireBearer("access-2", respondJSON(http.StatusOK, conversationJSON(501, "active", 123))),
		"GET /v2/conversations/501/threads": requireBearer("access-2", respondJSON(http.StatusOK, threadPageJSON(nil, false))),
	})
	configPath := writeConnectionFile(t, map[string]any{"app_id": "app-id", "app_secret": "app-secret", "webhook_secret": sentinelWebhookSecret}, nil)
	client := newRenewingClient(t, configPath, fake.redirectingClient())
	for range 2 {
		result, err := sdkgo.RunQuery(newStepContext("get"), client.GetConversation(), testConnection, helpscout.GetConversationInput{ConversationID: 501})
		require.NoError(t, err)
		require.Equal(t, helpscout.GetConversationBranchFound, result.Branch)
		requireSecretFree(t, result)
	}
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1, "the stored token is reused until it nears expiry")

	persisted := readConnectionRecord(t, configPath)
	require.Equal(t, "access-2", persisted.Credentials["access_token"])
	require.Equal(t, "app-secret", persisted.Credentials["app_secret"])
	require.Equal(t, sentinelWebhookSecret, persisted.Credentials["webhook_secret"])
	require.WithinDuration(t, time.Now().Add(48*time.Hour), persisted.CredentialExpiresAt, time.Minute)
}

func TestRenewingLocalConnectionObtainsANewTokenAfterHelpScoutAnswers401(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/oauth2/token":             clientCredentialsTokenRoute,
		"GET /v2/conversations/501":         requireBearer("access-2", respondJSON(http.StatusOK, conversationJSON(501, "active", 123))),
		"GET /v2/conversations/501/threads": requireBearer("access-2", respondJSON(http.StatusOK, threadPageJSON(nil, false))),
	})
	inOneDay := time.Now().Add(24 * time.Hour).UTC()
	configPath := writeConnectionFile(t, map[string]any{"app_id": "app-id", "app_secret": "app-secret", "access_token": "access-1"}, &inOneDay)
	result, err := sdkgo.RunQuery(newStepContext("get"), newRenewingClient(t, configPath, fake.redirectingClient()).GetConversation(),
		testConnection, helpscout.GetConversationInput{ConversationID: 501})
	require.NoError(t, err)
	require.Equal(t, helpscout.GetConversationBranchFound, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501"), 2, "the rejected read is repeated once")
	require.Equal(t, "access-2", readConnectionRecord(t, configPath).Credentials["access_token"])
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
			configPath := writeConnectionFile(t, map[string]any{"app_id": "app-id", "app_secret": "app-secret"}, nil)
			result, err := sdkgo.RunQuery(newStepContext("search"), newRenewingClient(t, configPath, fake.redirectingClient()).SearchConversations(),
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

// TestWebhookEndpointRenewsAnExpiredTokenToReadTheWebhookSecret covers an idle connection: the local
// provider refuses an expired record, so EndpointConfig.CredentialRefresh renews it before verifying.
func TestWebhookEndpointRenewsAnExpiredTokenToReadTheWebhookSecret(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": clientCredentialsTokenRoute})
	expiredAt := time.Now().Add(-time.Minute).UTC()
	configPath := writeConnectionFile(t, map[string]any{
		"app_id": "app-id", "app_secret": "app-secret", "access_token": "access-1", "webhook_secret": sentinelWebhookSecret,
	}, &expiredAt)
	fixture := startWebhookFixture(t, newRenewingClient(t, configPath, fake.redirectingClient()), helpscout.ConversationEventTriggerConfiguration{})
	body := conversationJSON(501, "active", 123)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(body))
	request.Header.Set("X-HelpScout-Event", helpscout.WebhookEventConversationCreated)
	request.Header.Set("X-HelpScout-Signature", helpScoutSignature(sentinelWebhookSecret, body))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "an idle connection keeps receiving after its access token expires")
	require.EqualValues(t, 501, fixture.receiveEvent(t).Payload.Conversation.ID)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1)
	require.Equal(t, "access-2", readConnectionRecord(t, configPath).Credentials["access_token"])
}

// TestWebhookEndpointOfAFreshConnectionVerifiesWithoutObtainingAToken proves a delivery needs only the
// webhook secret: a token outage cannot answer 503 to a connection that has no token yet.
func TestWebhookEndpointOfAFreshConnectionVerifiesWithoutObtainingAToken(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": respondJSON(http.StatusServiceUnavailable, `{"error":"server_error"}`)})
	configPath := writeConnectionFile(t, map[string]any{"app_id": "app-id", "app_secret": "app-secret", "webhook_secret": sentinelWebhookSecret}, nil)
	fixture := startWebhookFixture(t, newRenewingClient(t, configPath, fake.redirectingClient()), helpscout.ConversationEventTriggerConfiguration{})
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationCreated, conversationJSON(501, "active", 123), sentinelWebhookSecret))
	require.EqualValues(t, 501, fixture.receiveEvent(t).Payload.Conversation.ID)
	require.Empty(t, fake.recordedRequests(), "verification never waits for the token endpoint")
}

// TestGeneratedLocalConnectionCannotStoreTheToken pins a code generation limit: the generated
// NewLocalConnection gives an apiKey connector a provider without refresh, so a call retries unsent.
func TestGeneratedLocalConnectionCannotStoreTheToken(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/oauth2/token": clientCredentialsTokenRoute})
	configPath := writeConnectionFile(t, map[string]any{"app_id": "app-id", "app_secret": "app-secret"}, nil)
	store, err := localconfig.LoadFile(configPath)
	require.NoError(t, err)
	connection, err := helpscout.NewLocalConnection(store, testConnection.Name, helpscout.WithHTTPClient(fake.redirectingClient()))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newStepContext("search"), helpscout.ClientOfConnection(connection).SearchConversations(), testConnection,
		helpscout.SearchConversationsInput{})
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, retry.Failure.Message, "NewLocalRenewingConnection")
	require.Empty(t, fake.recordedRequests())
}

func TestDecodeResolvedCredentialsJSONAcceptsOnlyAnAccessToken(t *testing.T) {
	credentials, err := helpscout.DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"` + sentinelToken + `"}`))
	require.NoError(t, err)
	require.Equal(t, sentinelToken, credentials.AccessToken.Reveal())
	for _, contents := range []string{`{"access_token":"a","app_secret":"b"}`, `{"access_token":"has space"}`, `{}`, `[]`} {
		_, err := helpscout.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), "has space")
	}
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

// newRenewingClient uses the real clock, because localconfig requires a renewed expiry after wall-clock time.
func newRenewingClient(t *testing.T, configPath string, httpClient *http.Client) *helpscout.Client {
	t.Helper()
	store, err := localconfig.LoadFile(configPath)
	require.NoError(t, err)
	connection, err := helpscout.NewLocalRenewingConnection(store, testConnection.Name, helpscout.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return helpscout.ClientOfConnection(connection)
}

type connectionRecord struct {
	Credentials         map[string]string `json:"credentials"`
	CredentialExpiresAt time.Time         `json:"credentialExpiresAt"`
	ModuleVersion       string            `json:"moduleVersion"`
}

func readConnectionRecord(t *testing.T, configPath string) connectionRecord {
	t.Helper()
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var file struct {
		Connections []connectionRecord `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Len(t, file.Connections, 1)
	return file.Connections[0]
}

// writeConnectionFile writes the record Dex Web saves for this connector's connection.
func writeConnectionFile(t *testing.T, credentials map[string]any, expiresAt *time.Time) string {
	t.Helper()
	record := map[string]any{
		"connectorId": helpscout.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/helpscout",
		"moduleVersion": "v0.1.0", "provider": "helpscout", "connectionName": testConnection.Name,
		"configuration": map[string]any{}, "credentials": credentials,
	}
	if expiresAt != nil {
		record["credentialExpiresAt"] = expiresAt.Format(time.RFC3339Nano)
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

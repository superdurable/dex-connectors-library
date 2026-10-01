// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func oauthCredentials(accessToken string, refreshToken string) calendly.Credentials {
	return calendly.Credentials{
		AuthMethodID: calendly.CalendlyOAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(accessToken), RefreshToken: sdkgo.NewSecretString(refreshToken),
		WebhookSigningKey: sdkgo.NewSecretString(sentinelSigningKey),
	}
}

func TestCredentialRefreshDriverRotatesTheSingleUseRefreshTokenWithBasicClientAuthentication(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /oauth/token": func(response http.ResponseWriter, request *http.Request) {
		clientID, clientSecret, isBasic := request.BasicAuth()
		if !isBasic || clientID != "client-id" || clientSecret != "client-secret" || request.Host == "" {
			writeJSON(response, http.StatusUnauthorized, `{"error":"invalid_client","error_description":"x"}`)
			return
		}
		if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != "refresh_token" ||
			request.PostForm.Get("refresh_token") != "refresh-1" || request.PostForm.Has("client_secret") {
			writeJSON(response, http.StatusBadRequest, `{"error":"invalid_request"}`)
			return
		}
		writeJSON(response, http.StatusOK, fmt.Sprintf(`{"token_type":"Bearer","access_token":"access-2","refresh_token":"refresh-2","created_at":%d,"expires_in":7200,"owner":%q,"organization":%q,"scope":"users:read"}`,
			fixedNow.Unix(), testUserURI, testOrganizationURI))
	}})
	driver := calendly.NewCredentialRefreshDriver(fake.redirectingClient())
	state := sdkgo.CredentialRefreshState[calendly.Credentials]{Credentials: oauthCredentials("access-1", "refresh-1"), Now: fixedNow}
	require.True(t, driver.RefreshRequired(state), "an OAuth token without a recorded expiry is refreshed")
	expiresAt := fixedNow.Add(time.Hour)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[calendly.Credentials]{Credentials: state.Credentials, ExpiresAt: &expiresAt, Now: fixedNow}))

	result, err := driver.Refresh(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, "access-2", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "refresh-2", result.Credentials.RefreshToken.Reveal(), "the used refresh token is replaced")
	require.Equal(t, sentinelSigningKey, result.Credentials.WebhookSigningKey.Reveal(), "other credentials survive the refresh")
	require.Equal(t, calendly.CalendlyOAuthAuthMethodID, result.Credentials.AuthMethodID)
	require.WithinDuration(t, time.Now().Add(2*time.Hour), result.ExpiresAt, time.Minute)
	require.Len(t, fake.requestsTo(http.MethodPost, "/oauth/token"), 1)
}

func TestCredentialRefreshDriverRequiresReauthorizationForAReusedRefreshToken(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		fake := newFakeCalendly(t, map[string]http.HandlerFunc{
			"POST /oauth/token": respondJSON(status, `{"error":"invalid_grant","error_description":"`+providerSecretMessage+`"}`),
		})
		_, err := calendly.NewCredentialRefreshDriver(fake.redirectingClient()).Refresh(context.Background(),
			sdkgo.CredentialRefreshState[calendly.Credentials]{Credentials: oauthCredentials("access-1", "refresh-1"), Now: fixedNow})
		require.True(t, sdkgo.IsReauthorizationRequired(err), status)
		require.NotContains(t, err.Error(), providerSecretMessage)
	}
	unavailable := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /oauth/token": respondJSON(http.StatusTooManyRequests, `{"error":"temporarily_unavailable","error_description":"x"}`),
	})
	_, err := calendly.NewCredentialRefreshDriver(unavailable.redirectingClient()).Refresh(context.Background(),
		sdkgo.CredentialRefreshState[calendly.Credentials]{Credentials: oauthCredentials("access-1", "refresh-1"), Now: fixedNow})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "Calendly's eight-tokens-per-minute limit is retryable")
}

func TestCredentialRefreshDriverNeverRefreshesAPersonalAccessToken(t *testing.T) {
	driver := calendly.NewCredentialRefreshDriver(nil)
	state := sdkgo.CredentialRefreshState[calendly.Credentials]{Credentials: calendly.Credentials{
		AuthMethodID: calendly.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
	}, Now: fixedNow}
	require.False(t, driver.RefreshRequired(state))
	_, err := driver.Refresh(context.Background(), state)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

// TestOAuthConnectionRefreshesOnceAfterCalendlyRejectsTheAccessToken uses the local connection file that
// Dex Web writes, so the rotated tokens are persisted where the next call reads them.
func TestOAuthConnectionRefreshesOnceAfterCalendlyRejectsTheAccessToken(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /oauth/token": respondJSON(http.StatusOK, `{"token_type":"Bearer","access_token":"access-2","refresh_token":"refresh-2","expires_in":7200,"owner":"x","organization":"y"}`),
		"GET /scheduled_events/EVENT0001": func(response http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer access-2" {
				writeJSON(response, http.StatusUnauthorized, providerError("Unauthenticated", "The access token is invalid"))
				return
			}
			writeJSON(response, http.StatusOK, `{"resource":`+scheduledEventJSON("EVENT0001", "active", testWindowStart)+`}`)
		},
	})
	expiresAt := time.Now().Add(time.Hour).UTC()
	configPath := writeConnectionFile(t, map[string]any{
		"auth_method": calendly.CalendlyOAuthAuthMethodID, "oauth_client_id": "client-id", "oauth_client_secret": "client-secret",
		"access_token": "access-1", "refresh_token": "refresh-1",
	}, &expiresAt)
	result, err := getScheduledEventFromConnectionFile(t, configPath, fake.redirectingClient())
	require.NoError(t, err)
	require.Equal(t, calendly.GetScheduledEventBranchFound, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/oauth/token"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/scheduled_events/EVENT0001"), 2)

	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var persisted struct {
		Connections []struct {
			AuthMethodID string            `json:"authMethodId"`
			Credentials  map[string]string `json:"credentials"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &persisted))
	require.Len(t, persisted.Connections, 1)
	require.Equal(t, "refresh-2", persisted.Connections[0].Credentials["refresh_token"], "the rotated refresh token is persisted")
	require.Equal(t, "access-2", persisted.Connections[0].Credentials["access_token"])
	require.Equal(t, calendly.CalendlyOAuthAuthMethodID, persisted.Connections[0].AuthMethodID, "Dex Web's record members survive the refresh")
}

func TestPersonalAccessTokenRejectionIsNotRefreshed(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001": respondJSON(http.StatusUnauthorized, providerError("Unauthenticated", "revoked")),
	})
	configPath := writeConnectionFile(t, map[string]any{"auth_method": calendly.PersonalAccessTokenAuthMethodID, "access_token": sentinelToken}, nil)
	result, err := getScheduledEventFromConnectionFile(t, configPath, fake.redirectingClient())
	require.NoError(t, err)
	require.Equal(t, calendly.GetScheduledEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Len(t, fake.recordedRequests(), 1, "a personal access token has nothing to refresh")
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "reauthorization_required")
}

// getScheduledEventFromConnectionFile runs getScheduledEvent with the local file's refreshing provider.
func getScheduledEventFromConnectionFile(t *testing.T, configPath string, httpClient *http.Client) (calendly.GetScheduledEventResult, error) {
	t.Helper()
	client := newLocalFileClient(t, configPath, httpClient)
	return sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
		calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
}

// testCredentialFields mirrors the credentials object that Dex Web writes for this connector.
type testCredentialFields struct {
	AuthMethodID      string `json:"auth_method"`
	AccessToken       string `json:"access_token,omitempty"`
	OAuthClientID     string `json:"oauth_client_id,omitempty"`
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	WebhookSigningKey string `json:"webhook_signing_key,omitempty"`
}

func decodeTestCredentials(contents json.RawMessage) (calendly.Credentials, error) {
	var fields testCredentialFields
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return calendly.Credentials{}, err
	}
	return calendly.Credentials{
		AuthMethodID: fields.AuthMethodID, AccessToken: sdkgo.NewSecretString(fields.AccessToken), OAuthClientID: fields.OAuthClientID,
		OAuthClientSecret: sdkgo.NewSecretString(fields.OAuthClientSecret), RefreshToken: sdkgo.NewSecretString(fields.RefreshToken),
		WebhookSigningKey: sdkgo.NewSecretString(fields.WebhookSigningKey),
	}, nil
}

func encodeTestCredentials(credentials calendly.Credentials) (json.RawMessage, error) {
	return json.Marshal(testCredentialFields{
		AuthMethodID: credentials.AuthMethodID, AccessToken: credentials.AccessToken.Reveal(), OAuthClientID: credentials.OAuthClientID,
		OAuthClientSecret: credentials.OAuthClientSecret.Reveal(), RefreshToken: credentials.RefreshToken.Reveal(),
		WebhookSigningKey: credentials.WebhookSigningKey.Reveal(),
	})
}

// newLocalFileClient builds a client on the local file's refreshing provider with the real clock, because
// the provider requires a refreshed expiry after the wall-clock time.
func newLocalFileClient(t *testing.T, configPath string, httpClient *http.Client) *calendly.Client {
	t.Helper()
	store, err := localconfig.LoadFile(configPath)
	require.NoError(t, err)
	credentials := localconfig.NewRefreshingCredentialProvider(store, calendly.ConnectorID, testConnection.Name, decodeTestCredentials, encodeTestCredentials)
	client, err := calendly.New(calendly.Config{}, credentials, calendly.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}

func expiredOAuthConnectionFile(t *testing.T) string {
	t.Helper()
	expiredAt := time.Now().Add(-time.Minute).UTC()
	return writeConnectionFile(t, map[string]any{
		"auth_method": calendly.CalendlyOAuthAuthMethodID, "oauth_client_id": "client-id", "oauth_client_secret": "client-secret",
		"access_token": "access-1", "refresh_token": "refresh-1", "webhook_signing_key": sentinelSigningKey,
	}, &expiredAt)
}

func TestARetryableRefreshFailureRetriesAndAReusedGrantNeedsReauthorization(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		isRetry        bool
		expectedReason string
	}{
		{"token rate limit", http.StatusTooManyRequests, `{"error":"temporarily_unavailable","error_description":"x"}`, true, "refreshed yet"},
		{"token outage", http.StatusBadGateway, `{"error":"server_error"}`, true, "refreshed yet"},
		{"reused refresh token", http.StatusBadRequest, `{"error":"invalid_grant","error_description":"x"}`, false, "reauthorization"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /oauth/token": respondJSON(test.status, test.body)})
			client := newLocalFileClient(t, expiredOAuthConnectionFile(t), fake.redirectingClient())
			result, err := sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
				calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
				require.Contains(t, retry.Failure.Message, test.expectedReason)
			} else {
				require.NoError(t, err)
				require.Equal(t, calendly.GetScheduledEventBranchDefect, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, test.expectedReason)
			}
			require.Empty(t, fake.requestsTo(http.MethodGet, "/scheduled_events/EVENT0001"), "no API call without a usable token")
		})
	}
}

func TestCreateSchedulingLinkDoesNotRepeatAPostAfterARejectedOAuthToken(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /oauth/token":      respondJSON(http.StatusOK, `{"token_type":"Bearer","access_token":"access-2","refresh_token":"refresh-2","expires_in":7200}`),
		"POST /scheduling_links": respondJSON(http.StatusUnauthorized, providerError("Unauthenticated", "The access token is invalid")),
	})
	expiresAt := time.Now().Add(time.Hour).UTC()
	configPath := writeConnectionFile(t, map[string]any{
		"auth_method": calendly.CalendlyOAuthAuthMethodID, "oauth_client_id": "client-id", "oauth_client_secret": "client-secret",
		"access_token": "access-1", "refresh_token": "refresh-1",
	}, &expiresAt)
	result, err := createSchedulingLink(t, newLocalFileClient(t, configPath, fake.redirectingClient()), testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
	require.Empty(t, fake.requestsTo(http.MethodPost, "/oauth/token"), "the next Step refreshes; this one keeps its time budget")
}

func TestOAuthWebhookEndpointRefreshesAnExpiredTokenToReadTheSigningKey(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /oauth/token": respondJSON(http.StatusOK, `{"token_type":"Bearer","access_token":"access-2","refresh_token":"refresh-2","expires_in":7200}`),
	})
	client := newLocalFileClient(t, expiredOAuthConnectionFile(t), fake.redirectingClient())
	connection, err := calendly.NewConnection(client, testConnection)
	require.NoError(t, err)
	handler, err := connection.InviteeEventReceivedWebhookHandler()
	require.NoError(t, err)
	delivered := make(chan sdkgo.TriggerEvent[calendly.InviteeEvent], 1)
	runner := calendly.NewInviteeEventReceivedTrigger(calendly.InviteeEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: testConnection.Name, BindingName: "bookings",
		Target: sdkgo.TriggerTargetFunc[calendly.InviteeEvent](func(_ context.Context, event sdkgo.TriggerEvent[calendly.InviteeEvent]) error {
			delivered <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }() // The runner ends with ctx; only the delivery matters here.
	require.Eventually(t, func() bool { return handler.(interface{ RunningSourceCount() int }).RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)

	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE01", testEventTypeURI)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/calendly", strings.NewReader(body))
	request.Header.Set("Calendly-Webhook-Signature", calendlySignature(sentinelSigningKey, time.Now(), body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "an idle OAuth connection keeps receiving after its access token expires")
	require.Equal(t, "invitee.created:EVENT0001:INVITEE01", (<-delivered).ID)
	require.Len(t, fake.requestsTo(http.MethodPost, "/oauth/token"), 1)
}

func writeConnectionFile(t *testing.T, credentials map[string]any, expiresAt *time.Time) string {
	t.Helper()
	record := map[string]any{
		"connectorId": calendly.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/calendly",
		"moduleVersion": "v0.1.0", "provider": "calendly", "connectionName": testConnection.Name,
		"authMethodId": credentials["auth_method"], "configuration": map[string]any{}, "credentials": credentials,
	}
	if expiresAt != nil {
		record["credentialExpiresAt"] = expiresAt.Format(time.RFC3339Nano)
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	require.False(t, strings.Contains(string(contents), "\n"))
	return path
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// TestOAuthConnectionRefreshesOnceAfterCalendlyRejectsTheAccessToken stores the rotated tokens in the
// connection's credential source, where the next call reads them.
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
	credentials := unexpiredOAuthCredentials()
	result, err := getScheduledEventWithCredentials(t, credentials, fake.redirectingClient())
	require.NoError(t, err)
	require.Equal(t, calendly.GetScheduledEventBranchFound, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/oauth/token"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/scheduled_events/EVENT0001"), 2)

	stored, _ := credentials.Current()
	require.Equal(t, "refresh-2", stored.RefreshToken.Reveal(), "the rotated refresh token is stored")
	require.Equal(t, "access-2", stored.AccessToken.Reveal())
	require.Equal(t, calendly.CalendlyOAuthAuthMethodID, stored.AuthMethodID, "the refresh keeps the authorization method")
}

func TestPersonalAccessTokenRejectionIsNotRefreshed(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001": respondJSON(http.StatusUnauthorized, providerError("Unauthenticated", "revoked")),
	})
	credentials := testsupport.NewRefreshingCredentialSource(calendly.Credentials{
		AuthMethodID: calendly.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
	}, nil)
	result, err := getScheduledEventWithCredentials(t, credentials, fake.redirectingClient())
	require.NoError(t, err)
	require.Equal(t, calendly.GetScheduledEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Len(t, fake.recordedRequests(), 1, "a personal access token has nothing to refresh")
	require.False(t, credentials.IsReauthorizationRequired())
}

// getScheduledEventWithCredentials runs getScheduledEvent with credentials.
func getScheduledEventWithCredentials(
	t *testing.T, credentials *testsupport.RefreshingCredentialSource[calendly.Credentials], httpClient *http.Client,
) (calendly.GetScheduledEventResult, error) {
	t.Helper()
	client := newRefreshingClient(t, credentials, httpClient)
	return sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
		calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
}

// newRefreshingClient builds a client on a refreshing credential source with the real clock, because a
// refreshed expiry is compared with the wall-clock time.
func newRefreshingClient(
	t *testing.T, credentials *testsupport.RefreshingCredentialSource[calendly.Credentials], httpClient *http.Client,
) *calendly.Client {
	t.Helper()
	client, err := calendly.New(calendly.Config{}, credentials, calendly.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}

// unexpiredOAuthCredentials holds an OAuth token that expires in an hour.
func unexpiredOAuthCredentials() *testsupport.RefreshingCredentialSource[calendly.Credentials] {
	expiresAt := time.Now().Add(time.Hour).UTC()
	return testsupport.NewRefreshingCredentialSource(calendly.Credentials{
		AuthMethodID: calendly.CalendlyOAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("access-1"), RefreshToken: sdkgo.NewSecretString("refresh-1"),
	}, &expiresAt)
}

// expiredOAuthCredentials holds an OAuth token that expired a minute ago, with the webhook signing key.
func expiredOAuthCredentials() *testsupport.RefreshingCredentialSource[calendly.Credentials] {
	expiredAt := time.Now().Add(-time.Minute).UTC()
	return testsupport.NewRefreshingCredentialSource(oauthCredentials("access-1", "refresh-1"), &expiredAt)
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
			client := newRefreshingClient(t, expiredOAuthCredentials(), fake.redirectingClient())
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
	result, err := createSchedulingLink(t, newRefreshingClient(t, unexpiredOAuthCredentials(), fake.redirectingClient()), testEventTypeURI)
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
	client := newRefreshingClient(t, expiredOAuthCredentials(), fake.redirectingClient())
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

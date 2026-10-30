// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSendRequestRefreshesAnExpiredTokenBeforeTheRequestAndKeepsTheRotatedRefreshToken(t *testing.T) {
	session := newTestSession(t, nil, -time.Minute)

	first, err := session.send(t, "first", oauth2http.SendRequestInput{Path: "/me"})
	require.NoError(t, err)
	session.clock.advance(2 * time.Minute) // The 60-second replacement has expired too.
	second, err := session.send(t, "second", oauth2http.SendRequestInput{Path: "/me"})
	require.NoError(t, err)

	require.Equal(t, oauth2http.SendRequestBranchCompleted, first.Branch)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, second.Branch)
	requests := session.server.requests()
	require.Len(t, requests, 2)
	require.Equal(t, "Bearer access-token-2", requests[0].authorization, "the stored token had expired, so the request used the refreshed one")
	require.Equal(t, "Bearer access-token-3", requests[1].authorization)
	refreshes := session.server.refreshRequests()
	require.Len(t, refreshes, 2)
	require.Equal(t, "refresh-token-1", refreshes[0].Get("refresh_token"))
	require.Equal(t, "refresh-token-2", refreshes[1].Get("refresh_token"), "the second refresh used the rotated token the first returned")
	require.Equal(t, testClientID, refreshes[0].Get("client_id"))
	require.Equal(t, sentinelSecret, refreshes[0].Get("client_secret"), "client_secret_post is the default")
	stored, expiresAt := session.credentials.Current()
	require.Equal(t, "refresh-token-3", stored.RefreshToken.Reveal())
	require.True(t, expiresAt.Equal(session.clock.now().Add(time.Minute)), "the stored expiry follows expires_in from the refresh time")
	requireSecretFree(t, first, "access-token-2", "refresh-token-2")
}

func TestSendRequestPresentsTheClientWithHTTPBasicWhenTheConnectionAsksForIt(t *testing.T) {
	session := newTestSession(t, func(config *oauth2http.Config) {
		config.TokenEndpointAuthMethod = oauth2http.TokenEndpointAuthMethodClientSecretBasic
	}, -time.Minute)

	result, err := session.send(t, "basic", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
	refreshes := session.server.refreshRequests()
	require.Len(t, refreshes, 1)
	require.Empty(t, refreshes[0].Get("client_id"))
	require.Empty(t, refreshes[0].Get("client_secret"))
	require.Equal(t, []string{testClientID + ":" + sentinelSecret}, session.server.tokenBasicClients)
}

func TestSendRequestRenewsTheTokenOnceAfterA401AndResendsTheRequest(t *testing.T) {
	session := newTestSession(t, nil, 10*time.Minute)
	// The server revokes the token and, while it answers 401, the connection's clock passes the recorded
	// expiry, which is the evidence the credential provider needs to renew after a rejection.
	session.server.expireAccessToken("access-token-1")
	session.server.onUnauthorized = func() { session.clock.advance(11 * time.Minute) }

	result, err := session.send(t, "renewed", oauth2http.SendRequestInput{Method: "POST", Path: "/orders", Body: `{"item":"gear"}`})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
	requests := session.server.requests()
	require.Len(t, requests, 2, "one rejected request and one resend")
	require.Equal(t, "Bearer access-token-1", requests[0].authorization)
	require.Equal(t, "Bearer access-token-2", requests[1].authorization)
	require.Equal(t, requests[0].body, requests[1].body)
	require.Len(t, session.server.refreshRequests(), 1)
}

func TestSendRequestSelectsRejectedAfterOneRenewalWhenTheAPIKeepsRejectingTheToken(t *testing.T) {
	session := newTestSession(t, nil, 10*time.Minute)
	session.server.expireAccessToken("access-token-1")
	session.server.rejectsEveryToken = true
	session.server.onUnauthorized = func() { session.clock.advance(11 * time.Minute) }

	result, err := session.send(t, "always-rejected", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchRejected, result.Branch)
	require.Equal(t, http.StatusUnauthorized, result.Value.StatusCode)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Len(t, session.server.requests(), 2, "the request is resent at most once")
	require.Len(t, session.server.refreshRequests(), 1)
}

func TestSendRequestDoesNotRefreshAfter401WhenTheTokenHasNotExpired(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.expireAccessToken("access-token-1")

	result, err := session.send(t, "revoked", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchRejected, result.Branch)
	require.Equal(t, http.StatusUnauthorized, result.Value.StatusCode)
	require.Len(t, session.server.requests(), 1, "a 401 alone does not prove the token expired, so nothing is resent")
	require.Empty(t, session.server.refreshRequests())
}

func TestSendRequestSelectsRejectedWhenTheRefreshTokenWasAlreadyUsed(t *testing.T) {
	session := newTestSession(t, nil, -time.Minute)
	// Another replica used the stored refresh token, so the server no longer accepts it.
	_, err := (&http.Client{}).PostForm(session.server.URL+"/token", map[string][]string{"grant_type": {"refresh_token"}, "refresh_token": {"refresh-token-1"}})
	require.NoError(t, err)

	result, err := session.send(t, "used", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "must be renewed in Dex Web Connections")
	require.Zero(t, result.Value.StatusCode)
	require.Empty(t, session.server.requests(), "no API request is sent without a valid token")
	requireSecretFree(t, result, "refresh-token-1")
	require.NotContains(t, result.Failure.Message, "Token is not active", "the server's description is never repeated")
}

func TestSendRequestRetriesWhenTheTokenEndpointIsUnavailable(t *testing.T) {
	session := newTestSession(t, nil, -time.Minute)
	session.server.tokenStatus = http.StatusServiceUnavailable

	_, err := session.send(t, "unavailable", oauth2http.SendRequestInput{Path: "/me"})

	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Empty(t, session.server.requests())
	stored, _ := session.credentials.Current()
	require.Equal(t, "refresh-token-1", stored.RefreshToken.Reveal(), "a failed refresh keeps the stored material")

	session.server.mu.Lock()
	session.server.tokenStatus = 0
	session.server.mu.Unlock()
	result, err := session.send(t, "recovered", oauth2http.SendRequestInput{Path: "/me"})
	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
}

func TestSendRequestSelectsDefectWhenTheCredentialsAreUnusable(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	missing := &testSession{server: session.server}
	var err error
	missing.client, err = oauth2http.New(session.server.config(), sdkgo.StaticCredentialProvider[oauth2http.Credentials]{},
		oauth2http.WithHTTPClient(session.server.Client()))
	require.NoError(t, err)

	result, err := missing.send(t, "no-connection", oauth2http.SendRequestInput{Path: "/me"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, session.server.requests())
}

func TestRefreshRequiredFollowsTheExpiryAndWhetherARefreshTokenExists(t *testing.T) {
	driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: "http://identity.test/token"}, nil)
	require.NoError(t, err)
	now := testStart
	in := func(duration time.Duration) *time.Time { expiresAt := now.Add(duration); return &expiresAt }
	for _, test := range []struct {
		name      string
		access    string
		refresh   string
		expiresAt *time.Time
		expected  bool
	}{
		{"no access token", "", "refresh", in(time.Hour), true},
		{"hours left", "access", "refresh", in(2 * time.Hour), false},
		{"seconds left", "access", "refresh", in(time.Second), true},
		{"expired", "access", "refresh", in(-time.Minute), true},
		{"no recorded expiry but a refresh token", "access", "refresh", nil, true},
		{"no recorded expiry and no refresh token", "access", "", nil, false},
		{"expiring without a refresh token", "access", "", in(time.Second), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := sdkgo.CredentialRefreshState[oauth2http.Credentials]{
				Credentials: testCredentials(test.access, test.refresh), ExpiresAt: test.expiresAt, Now: now,
			}
			require.Equal(t, test.expected, driver.RefreshRequired(state))
		})
	}
}

func TestRefreshKeepsTheRefreshTokenWhenTheServerOmitsARotatedOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"access_token":"new-access","expires_in":300,"token_type":"bearer"}`)
	}))
	t.Cleanup(server.Close)
	driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: server.URL}, server.Client())
	require.NoError(t, err)

	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[oauth2http.Credentials]{
		Credentials: testCredentials("old-access", "kept-refresh"), Now: testStart,
	})

	require.NoError(t, err)
	require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "kept-refresh", result.Credentials.RefreshToken.Reveal())
	require.Equal(t, testClientID, result.Credentials.OAuthClientID)
	require.Equal(t, sentinelSecret, result.Credentials.OAuthClientSecret.Reveal())
	require.WithinDuration(t, time.Now().Add(5*time.Minute), result.ExpiresAt, 5*time.Second)
}

func TestRefreshRequiresReauthorizationForTerminalGrantErrorsAndMissingMaterial(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_client", "unauthorized_client"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(response, `{"error":"`+code+`","error_description":"secret detail"}`)
			}))
			t.Cleanup(server.Close)
			driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: server.URL}, server.Client())
			require.NoError(t, err)

			_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[oauth2http.Credentials]{
				Credentials: testCredentials("old", "refresh"), Now: testStart,
			})

			require.True(t, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "secret detail")
		})
	}
	driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: "http://127.0.0.1:1/token"}, nil)
	require.NoError(t, err)
	_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[oauth2http.Credentials]{
		Credentials: testCredentials("old", ""), Now: testStart,
	})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "without a refresh token nothing can renew the access token, and no request is made")
}

func TestRefreshTreatsServerFailuresAsRetryableEvenWithATerminalCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(response, `{"error":"invalid_grant"}`)
	}))
	t.Cleanup(server.Close)
	driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: server.URL}, server.Client())
	require.NoError(t, err)

	_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[oauth2http.Credentials]{
		Credentials: testCredentials("old", "refresh"), Now: testStart,
	})

	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err))
}

func TestNewCredentialRefreshDriverAcceptsHTTPAndHTTPSTokenEndpointsOnly(t *testing.T) {
	for _, accepted := range []string{"http://keycloak:8080/realms/app/protocol/openid-connect/token", "https://login.example.com/token?tenant=a"} {
		_, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: accepted}, nil)
		require.NoError(t, err, accepted)
	}
	for _, rejected := range []string{
		"", "/token", "ftp://login.example.com/token", "https://user:password@login.example.com/token",
		"https://login.example.com/token#fragment", "https://login.example.com/to ken", "https:///token",
	} {
		_, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: rejected}, nil)
		require.Error(t, err, rejected)
		require.NotContains(t, err.Error(), "password")
	}
}

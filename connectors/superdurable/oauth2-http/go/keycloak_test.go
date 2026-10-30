//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// keycloakClock is the wall clock plus an offset that a test moves forward to let a recorded expiry elapse
// without waiting for it.
type keycloakClock struct{ offset atomic.Int64 }

func (clock *keycloakClock) now() time.Time {
	return time.Now().Add(time.Duration(clock.offset.Load()))
}

// advancingCredentials moves the clock forward when the operation asks for a renewal after Keycloak
// answered 401, which is the moment a stored expiry that was wrong by minutes has been proven by the server.
type advancingCredentials struct {
	*testsupport.RefreshingCredentials[oauth2http.Credentials]
	clock   *keycloakClock
	advance time.Duration
}

func (credentials advancingCredentials) ResolveAfterRejection(
	ctx context.Context, call sdkgo.Call, driver sdkgo.CredentialRefreshDriver[oauth2http.Credentials],
) (oauth2http.Credentials, error) {
	credentials.clock.offset.Add(int64(credentials.advance))
	return credentials.RefreshingCredentials.ResolveAfterRejection(ctx, call, driver)
}

type keycloakSession struct {
	keycloak    *keycloakServer
	client      *oauth2http.Client
	credentials *testsupport.RefreshingCredentials[oauth2http.Credentials]
	clock       *keycloakClock
}

// newKeycloakSession stores grant with a recorded expiry of lifetime from now in a refreshing provider and
// opens the connector on the realm's endpoints, which are plain http.
func newKeycloakSession(t *testing.T, keycloak *keycloakServer, grant keycloakGrant, lifetime time.Duration, advanceOnUnauthorized time.Duration) *keycloakSession {
	t.Helper()
	clock := &keycloakClock{}
	expiresAt := clock.now().Add(lifetime)
	credentials := testsupport.NewRefreshingCredentials(oauth2http.Credentials{
		OAuthClientID: keycloakClientID, OAuthClientSecret: sdkgo.NewSecretString(keycloak.clientSecret),
		AccessToken: sdkgo.NewSecretString(grant.AccessToken), RefreshToken: sdkgo.NewSecretString(grant.RefreshToken),
	}, &expiresAt, clock.now)
	client, err := oauth2http.New(oauth2http.Config{
		AuthorizationEndpoint: keycloak.endpoints.Authorization, TokenEndpoint: keycloak.endpoints.Token,
		Scopes: []string{"openid", "profile"}, APIBaseURL: keycloak.baseURL,
	}, advancingCredentials{RefreshingCredentials: credentials, clock: clock, advance: advanceOnUnauthorized}, oauth2http.WithClock(clock.now))
	require.NoError(t, err)
	return &keycloakSession{keycloak: keycloak, client: client, credentials: credentials, clock: clock}
}

func (session *keycloakSession) userInfo(t *testing.T, step string) oauth2http.SendRequestResult {
	t.Helper()
	result, err := sendRequest(t, session.client, step, oauth2http.SendRequestInput{Path: session.keycloak.userInfoPath(t)})
	require.NoError(t, err)
	return result
}

func requireAliceProfile(t *testing.T, result oauth2http.SendRequestResult) {
	t.Helper()
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, http.StatusOK, result.Value.StatusCode)
	require.Contains(t, result.Value.ContentType, "application/json")
	require.Contains(t, result.Value.Body, `"preferred_username":"alice"`)
	require.Contains(t, result.Value.Body, `"email":"alice@example.test"`)
}

// TestKeycloakAuthorizationCodeFlowWithPKCEIssuesSixtySecondTokens checks the realm the other tests rely on:
// the code flow needs the PKCE verifier, and the access token lives for 60 seconds.
func TestKeycloakAuthorizationCodeFlowWithPKCEIssuesSixtySecondTokens(t *testing.T) {
	keycloak := requireKeycloak(t)

	code, verifier := keycloak.authorize(t)
	_, wrongVerifierStatus := keycloak.exchangeCode(t, code, verifier+"x")
	require.Equal(t, http.StatusBadRequest, wrongVerifierStatus, "Keycloak rejects a code redeemed with another verifier")
	code, _ = keycloak.authorize(t)
	_, missingVerifierStatus := keycloak.exchangeCode(t, code, "")
	require.Equal(t, http.StatusBadRequest, missingVerifierStatus, "the client requires PKCE")

	grant := keycloak.signIn(t)

	require.Equal(t, "Bearer", grant.TokenType)
	require.LessOrEqual(t, grant.ExpiresIn, 60)
	require.Greater(t, grant.ExpiresIn, 0)
	require.Contains(t, grant.Scope, "openid")
	require.Equal(t, http.StatusOK, keycloak.userInfoStatus(t, grant.AccessToken))
}

// TestKeycloakUserInfoThroughSendRequestRotatesTheRefreshTokenOnEveryRefresh sends the UserInfo request
// through the operation over plain http. The recorded expiry has elapsed before each call, so each call
// refreshes, and Keycloak invalidates a refresh token the moment it is used: the second call works only
// because the provider kept the rotated token.
func TestKeycloakUserInfoThroughSendRequestRotatesTheRefreshTokenOnEveryRefresh(t *testing.T) {
	keycloak := requireKeycloak(t)
	grant := keycloak.signIn(t)
	session := newKeycloakSession(t, keycloak, grant, time.Duration(grant.ExpiresIn)*time.Second, 0)

	session.clock.offset.Add(int64(2 * time.Minute))
	first := session.userInfo(t, "user-info-1")
	afterFirst, _ := session.credentials.Current()
	session.clock.offset.Add(int64(2 * time.Minute))
	second := session.userInfo(t, "user-info-2")
	afterSecond, _ := session.credentials.Current()

	requireAliceProfile(t, first)
	requireAliceProfile(t, second)
	require.Equal(t, 2, session.credentials.RefreshCount())
	require.NotEqual(t, grant.RefreshToken, afterFirst.RefreshToken.Reveal(), "the first refresh rotated the refresh token")
	require.NotEqual(t, afterFirst.RefreshToken.Reveal(), afterSecond.RefreshToken.Reveal())
	require.NotEqual(t, grant.AccessToken, afterFirst.AccessToken.Reveal())
	requireSecretFree(t, first, grant.AccessToken, grant.RefreshToken, afterFirst.AccessToken.Reveal(), keycloak.clientSecret)
}

// TestKeycloakRejectsAReplayedRefreshTokenAsReauthorizationRequired shows what a lost update means: once any
// holder used a refresh token, using it again is invalid_grant, which the driver reports as a connection that
// must be authorized again.
func TestKeycloakRejectsAReplayedRefreshTokenAsReauthorizationRequired(t *testing.T) {
	keycloak := requireKeycloak(t)
	grant := keycloak.signIn(t)
	driver, err := oauth2http.NewCredentialRefreshDriver(oauth2http.Config{TokenEndpoint: keycloak.endpoints.Token}, nil)
	require.NoError(t, err)
	state := sdkgo.CredentialRefreshState[oauth2http.Credentials]{
		Credentials: oauth2http.Credentials{
			OAuthClientID: keycloakClientID, OAuthClientSecret: sdkgo.NewSecretString(keycloak.clientSecret),
			AccessToken: sdkgo.NewSecretString(grant.AccessToken), RefreshToken: sdkgo.NewSecretString(grant.RefreshToken),
		},
		Now: time.Now(),
	}

	refreshed, err := driver.Refresh(context.Background(), state)
	require.NoError(t, err)
	require.NotEqual(t, grant.RefreshToken, refreshed.Credentials.RefreshToken.Reveal())
	_, replayErr := driver.Refresh(context.Background(), state)

	require.True(t, sdkgo.IsReauthorizationRequired(replayErr), "replaying %v", replayErr)
	require.NotContains(t, replayErr.Error(), grant.RefreshToken)
	require.NotContains(t, replayErr.Error(), keycloak.clientSecret)
}

// TestKeycloakExpiredAccessTokensAreRenewedByTheConnector waits past the 60-second lifetime once for two
// sessions. Keycloak rejects both expired tokens; the first session renews because its recorded expiry has
// elapsed, and the second discovers the expiry only from Keycloak's 401, after which its stored expiry is
// evidently wrong and the operation renews the token and sends the request again.
func TestKeycloakExpiredAccessTokensAreRenewedByTheConnector(t *testing.T) {
	keycloak := requireKeycloak(t)
	elapsedGrant, rejectedGrant := keycloak.signIn(t), keycloak.signIn(t)
	elapsed := newKeycloakSession(t, keycloak, elapsedGrant, time.Duration(elapsedGrant.ExpiresIn)*time.Second, 0)
	// The stored expiry is six minutes away, beyond the refresh skew, so nothing renews before the request.
	rejected := newKeycloakSession(t, keycloak, rejectedGrant, 6*time.Minute, 7*time.Minute)

	time.Sleep(time.Duration(max(elapsedGrant.ExpiresIn, rejectedGrant.ExpiresIn)+5) * time.Second) // The 60-second lifetime is the behavior under test.

	require.Equal(t, http.StatusUnauthorized, keycloak.userInfoStatus(t, elapsedGrant.AccessToken), "Keycloak rejects the expired token")
	require.Equal(t, http.StatusUnauthorized, keycloak.userInfoStatus(t, rejectedGrant.AccessToken))
	requireAliceProfile(t, elapsed.userInfo(t, "elapsed-expiry"))
	require.Equal(t, 1, elapsed.credentials.RefreshCount())
	requireAliceProfile(t, rejected.userInfo(t, "rejected-token"))
	require.Equal(t, 1, rejected.credentials.RefreshCount(), "the 401 proved the expiry; one renewal, then the request was sent again")
	renewed, _ := rejected.credentials.Current()
	require.NotEqual(t, rejectedGrant.AccessToken, renewed.AccessToken.Reveal())
}

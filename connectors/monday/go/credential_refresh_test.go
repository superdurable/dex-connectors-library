// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type credentialRoundTripFunc func(*http.Request) (*http.Response, error)

func (function credentialRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCredentialRefreshDriverExchangesTheRotatingRefreshTokenAsJSON(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	newAccessToken := jwtForTest(t, now.Add(time.Hour))
	client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, mondayOAuthTokenEndpoint, request.URL.String())
		require.Equal(t, "application/json", request.Header.Get("Content-Type"), "monday.com documents JSON token requests")
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]string{
			"grant_type": "refresh_token", "client_id": "client-id", "client_secret": "client-secret", "refresh_token": "existing-refresh",
		}, body)
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": newAccessToken, "refresh_token": "rotated-refresh", "token_type": "Bearer",
			"scope": "boards:read boards:write updates:write me:read",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest("old-access"), Now: now})
	require.NoError(t, err)
	require.Equal(t, newAccessToken, result.Credentials.AccessToken.Reveal())
	require.Equal(t, "rotated-refresh", result.Credentials.RefreshToken.Reveal())
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt, "monday.com sends no expires_in, so the JWT exp claim decides")
}

func TestCredentialRefreshDriverUsesANominalLifetimeWithoutAReadableExpiry(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusOK, map[string]any{"access_token": "opaque-access", "token_type": "Bearer"}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest("old-access"), Now: now})
	require.NoError(t, err)
	require.Equal(t, now.Add(nominalAccessTokenLifetime), result.ExpiresAt)
	require.Equal(t, "existing-refresh", result.Credentials.RefreshToken.Reveal(), "an omitted refresh token keeps the prior one")
}

func TestCredentialRefreshDriverClassifiesRejectionsWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name             string
		status           int
		body             map[string]any
		isReauthRequired bool
	}{
		{name: "revoked grant", status: 400, body: map[string]any{"error": "invalid_grant", "error_description": "SENTINEL"}, isReauthRequired: true},
		{name: "app disabled", status: 401, body: map[string]any{"error": "invalid_client", "error_description": "Api app is not active SENTINEL"}, isReauthRequired: true},
		{name: "missing scope", status: 200, body: map[string]any{"access_token": "a.b.c", "token_type": "Bearer", "scope": "boards:read"}, isReauthRequired: true},
		{name: "outage", status: 503, body: map[string]any{"error": "invalid_grant"}, isReauthRequired: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return tokenResponseForTest(t, test.status, test.body), nil
			})}
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest("old-access")})
			require.Error(t, err)
			require.Equal(t, test.isReauthRequired, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), "client-secret")
		})
	}
}

func TestRefreshRequiredReadsTheJWTExpiryWhenNoneIsRecorded(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	driver := NewCredentialRefreshDriver(nil)
	fresh := oauthCredentialsForTest(jwtForTest(t, now.Add(30*time.Minute)))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: fresh, Now: now}))
	nearExpiry := oauthCredentialsForTest(jwtForTest(t, now.Add(4*time.Minute)))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: nearExpiry, Now: now}))
	opaque := oauthCredentialsForTest("opaque-access")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: opaque, Now: now}), "an unreadable expiry refreshes")
	recorded := now.Add(time.Hour)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: opaque, ExpiresAt: &recorded, Now: now}))
	personal := Credentials{AuthMethodID: PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString("token")}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: personal, Now: now}), "a personal token never expires")
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: personal})
	require.Error(t, err)
}

// TestRejectedOAuthTokenIsRefreshedOnceAndTheRequestResent refreshes through a credential source that
// honors a rejection; the stored token has not reached its JWT expiry.
func TestRejectedOAuthTokenIsRefreshedOnceAndTheRequestResent(t *testing.T) {
	staleToken, freshToken := jwtForTest(t, time.Now().Add(time.Hour)), jwtForTest(t, time.Now().Add(2*time.Hour))
	var mutex sync.Mutex
	var apiAuthorizations []string
	tokenRequests := 0
	transport := credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		mutex.Lock()
		defer mutex.Unlock()
		if request.URL.String() == mondayOAuthTokenEndpoint {
			tokenRequests++
			return tokenResponseForTest(t, http.StatusOK, map[string]any{
				"access_token": freshToken, "refresh_token": "rotated-refresh", "token_type": "Bearer", "scope": "boards:read boards:write updates:write",
			}), nil
		}
		apiAuthorizations = append(apiAuthorizations, request.Header.Get("Authorization"))
		if request.Header.Get("Authorization") != freshToken {
			return tokenResponseForTest(t, http.StatusUnauthorized, map[string]any{"errors": []any{map[string]any{"message": "Not authenticated", "extensions": map[string]any{"code": "NOT_AUTHENTICATED"}}}}), nil
		}
		return tokenResponseForTest(t, http.StatusOK, map[string]any{"data": map[string]any{"items": []any{map[string]any{"id": "9876543210", "name": "Fire drill"}}}}), nil
	})
	source := testsupport.NewRefreshingCredentialSource(oauthCredentialsForTest(staleToken), nil)
	client, err := New(Config{}, source, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	connection, err := NewConnection(client, sdkgo.ConnectionRef{Provider: "monday", Name: "monday-oauth"})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newInternalDexContext(), connection.client.GetItem(), connection.reference, GetItemInput{ItemID: "9876543210"})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchFound, result.Branch)
	require.Equal(t, []string{staleToken, freshToken}, apiAuthorizations, "one rejection, one refresh, one resend")
	require.Equal(t, 1, tokenRequests)
	stored, expiresAt := source.Current()
	require.Equal(t, OAuthAuthMethodID, stored.AuthMethodID)
	require.Equal(t, freshToken, stored.AccessToken.Reveal())
	require.Equal(t, "rotated-refresh", stored.RefreshToken.Reveal(), "the rotated refresh token replaces the prior one")
	require.NotNil(t, expiresAt)
}

func oauthCredentialsForTest(accessToken string) Credentials {
	return Credentials{
		AuthMethodID: OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(accessToken), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
	}
}

// jwtForTest builds an unsigned JWT-shaped token whose payload carries exp; the connector never verifies it.
func jwtForTest(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": expiresAt.Unix(), "uid": 48202303})
	require.NoError(t, err)
	return "eyJhbGciOiJIUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".c2lnbmF0dXJl"
}

func tokenResponseForTest(t *testing.T, status int, body map[string]any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader(encoded)),
	}
}

// newInternalDexContext supplies the Step identity a Call needs outside a Worker.
func newInternalDexContext() *internalDexContext {
	return &internalDexContext{Context: context.Background()}
}

type internalDexContext struct{ context.Context }

func (*internalDexContext) FlowID() string                          { return "monday-flow" }
func (*internalDexContext) RunID() string                           { return "run" }
func (*internalDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (*internalDexContext) StepExecutionID() string                 { return strings.Repeat("s", 8) }
func (*internalDexContext) FromStepExecutionID() string             { return "" }
func (*internalDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*internalDexContext) Attempt() int32                          { return 1 }
func (*internalDexContext) HasTimerFired() bool                     { return false }
func (*internalDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*internalDexContext) WaitForMethodFailed() bool               { return false }
func (*internalDexContext) RecordEvent(string, any) error           { return nil }
func (*internalDexContext) RecordHeartbeat(any) error               { return nil }
func (*internalDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }
func (*internalDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*internalDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*internalDexContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }

var _ dex.Context = (*internalDexContext)(nil)

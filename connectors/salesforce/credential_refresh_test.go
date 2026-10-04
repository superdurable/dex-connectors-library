// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

var refreshTestNow = time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)

func oauthCredentials(authMethodID string) Credentials {
	return Credentials{
		AuthMethodID: authMethodID, OAuthClientID: "consumer-key",
		OAuthClientSecret: sdkgo.NewSecretString("consumer-secret"),
		AccessToken:       sdkgo.NewSecretString("old-session"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
		InstanceURL: "https://old.my.salesforce.com",
	}
}

func TestCredentialRefreshDriverRefreshesAtTheMethodLoginHostAndStoresTheInstanceURL(t *testing.T) {
	for _, test := range []struct {
		name            string
		authMethodID    string
		tokenEndpoint   string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "production keeps the refresh token", authMethodID: ProductionOAuthAuthMethodID, tokenEndpoint: "https://login.salesforce.com/services/oauth2/token", expectedRefresh: "existing-refresh"},
		{name: "blank method is production", authMethodID: "", tokenEndpoint: "https://login.salesforce.com/services/oauth2/token", expectedRefresh: "existing-refresh"},
		{name: "sandbox stores a rotated refresh token", authMethodID: SandboxOAuthAuthMethodID, tokenEndpoint: "https://test.salesforce.com/services/oauth2/token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, test.tokenEndpoint, request.URL.String())
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.Form.Get("grant_type"))
				require.Equal(t, "consumer-key", request.Form.Get("client_id"))
				require.Equal(t, "consumer-secret", request.Form.Get("client_secret"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				// Salesforce returns no expires_in, and a new refresh token only when rotation is enabled.
				response := map[string]any{
					"access_token": "new-session", "token_type": "Bearer", "scope": "refresh_token api",
					"instance_url": "https://example.my.salesforce.com", "id": "https://login.salesforce.com/id/00D/005",
					"issued_at": "1790000000000", "signature": "c2lnbmF0dXJl",
				}
				if test.rotatedRefresh != "" {
					response["refresh_token"] = test.rotatedRefresh
				}
				return tokenResponse(t, http.StatusOK, response), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return refreshTestNow }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentials(test.authMethodID), Now: refreshTestNow})
			require.NoError(t, err)
			require.Equal(t, "new-session", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, "https://example.my.salesforce.com", result.Credentials.InstanceURL)
			require.Equal(t, refreshTestNow.Add(nominalSessionLifetime), result.ExpiresAt)
			require.Equal(t, test.authMethodID, result.Credentials.AuthMethodID)
		})
	}
}

func TestCredentialRefreshDriverKeepsASessionUntilItIsRejectedOrIncomplete(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	session := oauthCredentials(ProductionOAuthAuthMethodID)
	withoutInstance := session
	withoutInstance.InstanceURL = ""
	farExpiry, nearExpiry := refreshTestNow.Add(time.Hour), refreshTestNow.Add(4*time.Minute)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: session, Now: refreshTestNow}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: session, ExpiresAt: &farExpiry, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: session, ExpiresAt: &nearExpiry, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withoutInstance, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{InstanceURL: session.InstanceURL}, Now: refreshTestNow}))
}

func TestCredentialRefreshDriverRequiresReauthorizationOnlyForTerminalFailures(t *testing.T) {
	for _, test := range []struct {
		name             string
		status           int
		body             map[string]any
		isReauthRequired bool
	}{
		{name: "expired refresh token", status: http.StatusBadRequest, body: map[string]any{"error": "invalid_grant", "error_description": "expired access/refresh token SENTINEL"}, isReauthRequired: true},
		{name: "inactive user", status: http.StatusBadRequest, body: map[string]any{"error": "inactive_user", "error_description": "SENTINEL"}, isReauthRequired: true},
		{name: "session without api scope", status: http.StatusOK, isReauthRequired: true, body: map[string]any{
			"access_token": "new-session", "token_type": "Bearer", "scope": "refresh_token id", "instance_url": "https://example.my.salesforce.com",
		}},
		{name: "outage reporting invalid_grant", status: http.StatusServiceUnavailable, body: map[string]any{"error": "invalid_grant"}},
		{name: "rate limited token endpoint", status: http.StatusBadRequest, body: map[string]any{"error": "rate_limit_exceeded", "error_description": "SENTINEL"}},
		{name: "plain HTTP instance URL", status: http.StatusOK, body: map[string]any{"access_token": "new-session", "token_type": "Bearer", "scope": "api", "instance_url": "http://example.my.salesforce.com"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return tokenResponse(t, test.status, test.body), nil
			})}
			credentials := oauthCredentials(ProductionOAuthAuthMethodID)
			credentials.InstanceURL = ""
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
			require.Error(t, err)
			require.Equal(t, test.isReauthRequired, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), "new-session")
		})
	}
}

func TestCredentialRefreshDriverMintsJWTBearerSessionsWithoutAScopeClaim(t *testing.T) {
	privateKey, privateKeyPEM := newTestRSAKey(t)
	for _, test := range []struct {
		environment JWTLoginEnvironment
		loginURL    string
	}{
		{environment: "", loginURL: "https://login.salesforce.com"},
		{environment: JWTLoginEnvironmentSandbox, loginURL: "https://test.salesforce.com"},
	} {
		t.Run(test.loginURL, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, test.loginURL+"/services/oauth2/token", request.URL.String())
				require.NoError(t, request.ParseForm())
				require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.Form.Get("grant_type"))
				require.Empty(t, request.Form.Get("client_secret"))
				claims := verifyAssertion(t, request.Form.Get("assertion"), &privateKey.PublicKey)
				require.Equal(t, "consumer-key", claims["iss"])
				require.Equal(t, "integration@example.com.prod", claims["sub"])
				require.Equal(t, test.loginURL, claims["aud"])
				require.NotContains(t, claims, "scope")
				lifetime := claims["exp"].(float64) - claims["iat"].(float64)
				require.Positive(t, lifetime)
				require.LessOrEqual(t, lifetime, float64(180))
				return tokenResponse(t, http.StatusOK, map[string]any{
					"access_token": "minted-session", "token_type": "Bearer", "scope": "api", "instance_url": "https://example.my.salesforce.com/",
				}), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return refreshTestNow }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
				AuthMethodID: JWTBearerAuthMethodID, OAuthClientID: "consumer-key", JWTUsername: "integration@example.com.prod",
				JWTPrivateKey: sdkgo.NewSecretString(privateKeyPEM), JWTLoginEnvironment: test.environment,
			}})
			require.NoError(t, err)
			require.Equal(t, "minted-session", result.Credentials.AccessToken.Reveal())
			require.Equal(t, "https://example.my.salesforce.com", result.Credentials.InstanceURL)
			require.Empty(t, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, refreshTestNow.Add(nominalSessionLifetime), result.ExpiresAt)
		})
	}
}

func TestCredentialRefreshDriverRejectsIncompleteJWTBearerMaterial(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("no token request may be sent")
		return nil, nil
	})}
	_, privateKeyPEM := newTestRSAKey(t)
	for name, credentials := range map[string]Credentials{
		"missing username": {AuthMethodID: JWTBearerAuthMethodID, OAuthClientID: "consumer-key", JWTPrivateKey: sdkgo.NewSecretString(privateKeyPEM)},
		"invalid key":      {AuthMethodID: JWTBearerAuthMethodID, OAuthClientID: "consumer-key", JWTUsername: "user", JWTPrivateKey: sdkgo.NewSecretString("-----BEGIN PRIVATE KEY-----\nSENTINEL\n-----END PRIVATE KEY-----")},
		"unknown method":   {AuthMethodID: "password", OAuthClientID: "consumer-key"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
			require.True(t, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func tokenResponse(t *testing.T, status int, body map[string]any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(contents))}
}

func newTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
}

func verifyAssertion(t *testing.T, assertion string, publicKey *rsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(assertion, ".")
	require.Len(t, parts, 3)
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	require.NoError(t, rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature))
	encodedClaims, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(encodedClaims, &claims))
	return claims
}

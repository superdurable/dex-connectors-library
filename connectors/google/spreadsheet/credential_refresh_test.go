// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

func TestCredentialRefreshDriverRefreshesOAuthAndHandlesRotation(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "preserves omitted refresh token", expectedRefresh: "existing-refresh"},
		{name: "persists rotated refresh token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, googleOAuthTokenEndpoint, request.URL.String())
				require.NoError(t, request.ParseForm())
				require.Equal(t, "client-id", request.Form.Get("client_id"))
				require.Equal(t, "client-secret", request.Form.Get("client_secret"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				response := map[string]any{
					"access_token": "new-access", "expires_in": 3600,
					"token_type": "Bearer", "scope": googleDriveFileScope,
				}
				if test.rotatedRefresh != "" {
					response["refresh_token"] = test.rotatedRefresh
				}
				return spreadsheetTokenResponse(t, http.StatusOK, response), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
				Credentials: Credentials{
					OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
					AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
				},
				Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
		})
	}
}

func TestCredentialRefreshDriverClassifiesInvalidGrantWithoutProviderText(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return spreadsheetTokenResponse(t, http.StatusBadRequest, map[string]any{
			"error": "invalid_grant", "error_description": "secret provider account detail",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider account detail")
}

func TestCredentialRefreshDriverRejectsMissingScope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return spreadsheetTokenResponse(t, http.StatusOK, map[string]any{
			"access_token": "new-access", "expires_in": 3600, "token_type": "Bearer",
			"scope": "https://www.googleapis.com/auth/spreadsheets.readonly",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "new-access")
}

func TestCredentialRefreshDriverUsesExpirySkew(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{})
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	credential := Credentials{AccessToken: sdkgo.NewSecretString("access-token")}
	insideSkew := now.Add(credentialRefreshSkew)
	beyondSkew := insideSkew.Add(time.Nanosecond)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &beyondSkew, Now: now}))
}

func spreadsheetTokenResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}

func TestCredentialRefreshDriverMintsWorkspaceDelegatedToken(t *testing.T) {
	serviceAccountJSON := serviceAccountKeyJSON(t)
	now := time.Date(2026, time.October, 9, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, googleOAuthTokenEndpoint, request.URL.String())
		require.NoError(t, request.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.Form.Get("grant_type"))
		require.Empty(t, request.Form.Get("client_secret"))
		parts := strings.Split(request.Form.Get("assertion"), ".")
		require.Len(t, parts, 3)
		claimsJSON, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, decodeErr)
		var claims map[string]any
		require.NoError(t, json.Unmarshal(claimsJSON, &claims))
		require.Equal(t, "sheets@project.iam.gserviceaccount.com", claims["iss"])
		require.Equal(t, "owner@example.com", claims["sub"])
		require.Equal(t, googleOAuthTokenEndpoint, claims["aud"])
		require.Equal(t, "https://www.googleapis.com/auth/spreadsheets https://www.googleapis.com/auth/drive.readonly", claims["scope"])
		require.Equal(t, float64(now.Add(time.Hour).Unix()), claims["exp"])
		return delegatedTokenResponse(t, map[string]any{"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer"}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID:      WorkspaceDomainDelegationAuthMethodID,
		ServiceAccountKey: sdkgo.NewSecretString(serviceAccountJSON), DelegatedUser: "owner@example.com",
	}})
	require.NoError(t, err)
	require.Equal(t, "delegated-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
}

func TestCredentialRefreshDriverRejectsADelegatedTokenWithoutTheSheetsScope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return delegatedTokenResponse(t, map[string]any{
			"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer",
			"scope": "https://www.googleapis.com/auth/drive.readonly",
		}), nil
	})}
	_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID:      WorkspaceDomainDelegationAuthMethodID,
		ServiceAccountKey: sdkgo.NewSecretString(serviceAccountKeyJSON(t)), DelegatedUser: "owner@example.com",
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestCredentialRefreshDriverRequiresReauthorizationForInvalidDelegation(t *testing.T) {
	serviceAccountJSON := serviceAccountKeyJSON(t)
	for name, credentials := range map[string]Credentials{
		"malformed key":        {ServiceAccountKey: sdkgo.NewSecretString("{not json"), DelegatedUser: "owner@example.com"},
		"incomplete key":       {ServiceAccountKey: sdkgo.NewSecretString(`{"client_email":"a@b.c"}`), DelegatedUser: "owner@example.com"},
		"display-name user":    {ServiceAccountKey: sdkgo.NewSecretString(serviceAccountJSON), DelegatedUser: "Owner <owner@example.com>"},
		"missing user":         {ServiceAccountKey: sdkgo.NewSecretString(serviceAccountJSON)},
		"plain HTTP token URI": {ServiceAccountKey: sdkgo.NewSecretString(strings.Replace(serviceAccountJSON, "https://oauth2", "http://oauth2", 1)), DelegatedUser: "owner@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid delegation must not reach the token endpoint")
				return nil, nil
			})}
			credentials.AuthMethodID = WorkspaceDomainDelegationAuthMethodID
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
			require.Error(t, err)
			require.True(t, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "PRIVATE KEY")
		})
	}
}

func TestCredentialRefreshDriverRequiresReauthorizationForAnUnknownMethod(t *testing.T) {
	_, err := NewCredentialRefreshDriver(nil).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
		Credentials: Credentials{AuthMethodID: "api-key"},
	})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestDecodeCredentialsKeepsConnectionsSavedBeforeDelegation(t *testing.T) {
	credentials, err := decodeCredentials(json.RawMessage(`{"oauth_client_id":"client.apps.googleusercontent.com",` +
		`"oauth_client_secret":"secret","access_token":"access","refresh_token":"refresh"}`))
	require.NoError(t, err)
	require.Equal(t, GoogleOAuthAuthMethodID, credentials.AuthMethodID)
	delegated, err := decodeCredentials(json.RawMessage(`{"auth_method":"workspace-domain-delegation",` +
		`"service_account_key":"{}","delegated_user":"owner@example.com"}`))
	require.NoError(t, err)
	require.Equal(t, WorkspaceDomainDelegationAuthMethodID, delegated.AuthMethodID)
	require.Empty(t, delegated.AccessToken.Reveal(), "the delegated access token is minted, never required")
}

func serviceAccountKeyJSON(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	contents, err := json.Marshal(map[string]string{
		"client_email": "sheets@project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})),
		"token_uri":    googleOAuthTokenEndpoint,
	})
	require.NoError(t, err)
	return string(contents)
}

func delegatedTokenResponse(t *testing.T, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(contents)))}
}

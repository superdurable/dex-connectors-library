// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

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
					"token_type": "Bearer", "scope": strings.Join(gmailOAuthScopes, " "),
				}
				if test.rotatedRefresh != "" {
					response["refresh_token"] = test.rotatedRefresh
				}
				return tokenResponse(t, http.StatusOK, response), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
				Credentials: Credentials{
					AuthMethodID: GoogleOAuthAuthMethodID, OAuthClientID: "client-id",
					OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
					AccessToken:       sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
					PrimaryEmail: "owner@example.com",
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
		return tokenResponse(t, http.StatusBadRequest, map[string]any{
			"error": "invalid_grant", "error_description": "secret provider account detail",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID: GoogleOAuthAuthMethodID, OAuthClientID: "client-id",
		OAuthClientSecret: sdkgo.NewSecretString("client-secret"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider account detail")
}

func TestCredentialRefreshDriverRejectsMissingScopes(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponse(t, http.StatusOK, map[string]any{
			"access_token": "new-access", "expires_in": 3600, "token_type": "Bearer",
			"scope": "https://www.googleapis.com/auth/gmail.readonly",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID: GoogleOAuthAuthMethodID, OAuthClientID: "client-id",
		OAuthClientSecret: sdkgo.NewSecretString("client-secret"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "new-access")
}

func TestCredentialRefreshDriverMintsWorkspaceDelegatedToken(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	serviceAccountJSON, err := json.Marshal(map[string]string{
		"client_email": "mailer@project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})),
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.NoError(t, request.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.Form.Get("grant_type"))
		parts := strings.Split(request.Form.Get("assertion"), ".")
		require.Len(t, parts, 3)
		claimsJSON, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, decodeErr)
		var claims map[string]any
		require.NoError(t, json.Unmarshal(claimsJSON, &claims))
		require.Equal(t, "mailer@project.iam.gserviceaccount.com", claims["iss"])
		require.Equal(t, "sender@example.com", claims["sub"])
		require.Equal(t, strings.Join(gmailOAuthScopes, " "), claims["scope"])
		return tokenResponse(t, http.StatusOK, map[string]any{
			"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID:      WorkspaceDomainDelegationAuthMethodID,
		ServiceAccountKey: sdkgo.NewSecretString(string(serviceAccountJSON)), DelegatedUser: "sender@example.com",
	}})
	require.NoError(t, err)
	require.Equal(t, "delegated-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "sender@example.com", result.Credentials.PrimaryEmail)
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
}

func TestDecodeResolvedCredentialsRejectsRenewalMaterial(t *testing.T) {
	contents, err := json.Marshal(map[string]string{
		"auth_method": "google-oauth", "access_token": "short-lived", "primary_email": "owner@example.com",
	})
	require.NoError(t, err)
	credentials, err := DecodeResolvedCredentialsJSON(contents)
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	contents, err = json.Marshal(map[string]string{
		"access_token": "short-lived", "primary_email": "owner@example.com", "refresh_token": "must-not-cross-broker",
	})
	require.NoError(t, err)
	_, err = DecodeResolvedCredentialsJSON(contents)
	require.Error(t, err)
}

func tokenResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCredentialRefreshDriverRefreshesOAuthAndHandlesRotation(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
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
				require.Equal(t, "refresh_token", request.Form.Get("grant_type"))
				require.Equal(t, "client-id", request.Form.Get("client_id"))
				require.Equal(t, "client-secret", request.Form.Get("client_secret"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				response := map[string]any{
					"access_token": "new-access", "expires_in": 3599,
					"token_type": "Bearer", "scope": strings.Join(calendarOAuthScopes, " "),
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
				},
				Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, now.Add(3599*time.Second), result.ExpiresAt)
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

func TestCredentialRefreshDriverRequiresEveryCalendarScope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponse(t, http.StatusOK, map[string]any{
			"access_token": "new-access", "expires_in": 3600, "token_type": "Bearer",
			"scope": "https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/calendar.calendarlist.readonly",
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
	serviceAccountJSON := newServiceAccountKeyJSON(t)
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://oauth2.googleapis.com/token", request.URL.String())
		require.NoError(t, request.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.Form.Get("grant_type"))
		parts := strings.Split(request.Form.Get("assertion"), ".")
		require.Len(t, parts, 3)
		claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		var claims map[string]any
		require.NoError(t, json.Unmarshal(claimsJSON, &claims))
		require.Equal(t, "scheduler@project.iam.gserviceaccount.com", claims["iss"])
		require.Equal(t, "organizer@example.com", claims["sub"])
		require.Equal(t, "https://oauth2.googleapis.com/token", claims["aud"])
		require.Equal(t, strings.Join(calendarOAuthScopes, " "), claims["scope"])
		require.Equal(t, float64(now.Unix()), claims["iat"])
		require.Equal(t, float64(now.Add(time.Hour).Unix()), claims["exp"])
		return tokenResponse(t, http.StatusOK, map[string]any{
			"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		AuthMethodID:      WorkspaceDomainDelegationAuthMethodID,
		ServiceAccountKey: sdkgo.NewSecretString(serviceAccountJSON), DelegatedUser: "organizer@example.com",
	}})
	require.NoError(t, err)
	require.Equal(t, "delegated-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "organizer@example.com", result.Credentials.DelegatedUser)
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
}

func TestCredentialRefreshDriverRejectsInvalidDelegationMaterialWithoutRequests(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid delegation material must not reach Google")
		return nil, nil
	})}
	driver := NewCredentialRefreshDriver(client)
	for _, credentials := range []Credentials{
		{AuthMethodID: WorkspaceDomainDelegationAuthMethodID, ServiceAccountKey: sdkgo.NewSecretString("{"), DelegatedUser: "organizer@example.com"},
		{AuthMethodID: WorkspaceDomainDelegationAuthMethodID, ServiceAccountKey: sdkgo.NewSecretString(newServiceAccountKeyJSON(t)), DelegatedUser: "Organizer <organizer@example.com>"},
		{AuthMethodID: WorkspaceDomainDelegationAuthMethodID, ServiceAccountKey: sdkgo.NewSecretString(`{"client_email":"a@b","private_key":"k","token_uri":"http://insecure.example.com/token"}`), DelegatedUser: "organizer@example.com"},
	} {
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
		require.Error(t, err)
		require.True(t, sdkgo.IsReauthorizationRequired(err))
	}
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{AuthMethodID: "api-key"}})
	require.ErrorContains(t, err, "not supported")
}

func TestCredentialRefreshDriverRefreshesMissingOrExpiringTokens(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{})
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	credential := Credentials{AccessToken: sdkgo.NewSecretString("access-token")}
	insideSkew := now.Add(5 * time.Minute)
	beyondSkew := insideSkew.Add(time.Second)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{}, ExpiresAt: &beyondSkew, Now: now}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, Now: now}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &beyondSkew, Now: now}))
}

func TestDecodeResolvedCredentialsRejectsRenewalMaterial(t *testing.T) {
	credentials, err := DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"workspace-domain-delegation","access_token":"short-lived"}`))
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	require.Equal(t, WorkspaceDomainDelegationAuthMethodID, credentials.AuthMethodID)
	_, err = DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"short-lived","refresh_token":"must-not-cross-broker"}`))
	require.Error(t, err)
	_, err = DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":""}`))
	require.Error(t, err)
}

func TestLocalCredentialsRoundTripOnlyTheSelectedMethod(t *testing.T) {
	encoded, err := EncodeCredentialsJSON(Credentials{
		AuthMethodID: GoogleOAuthAuthMethodID, OAuthClientID: "client-id",
		OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("access"),
		RefreshToken: sdkgo.NewSecretString("refresh"),
	})
	require.NoError(t, err)
	decoded, err := DecodeCredentialsJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, "refresh", decoded.RefreshToken.Reveal())
	_, err = EncodeCredentialsJSON(Credentials{AuthMethodID: GoogleOAuthAuthMethodID})
	require.Error(t, err)
}

func TestRefreshScopesMatchTheManifestScopes(t *testing.T) {
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Auth struct {
				Methods []struct {
					OAuth2 struct {
						Scopes []string `yaml:"scopes"`
					} `yaml:"oauth2"`
				} `yaml:"methods"`
			} `yaml:"auth"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, calendarOAuthScopes, manifest.Spec.Auth.Methods[0].OAuth2.Scopes)
}

func newServiceAccountKeyJSON(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	contents, err := json.Marshal(map[string]string{
		"client_email": "scheduler@project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})),
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	return string(contents)
}

func tokenResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}

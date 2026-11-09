// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshbooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type tokenRoundTrip func(*http.Request) (*http.Response, error)

func (send tokenRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return send(request)
}

func TestRefreshSendsRegisteredRedirectURIAndRotatesToken(t *testing.T) {
	const redirectURI = "https://dex.example/callback"
	client := &http.Client{Transport: tokenRoundTrip(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://api.freshbooks.com/auth/oauth/token", request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var parameters map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&parameters))
		require.Equal(t, redirectURI, parameters["redirect_uri"])
		require.Equal(t, "refresh_token", parameters["grant_type"])
		require.Equal(t, "old-refresh", parameters["refresh_token"])
		require.Equal(t, "client-id", parameters["client_id"])
		require.Equal(t, "client-secret", parameters["client_secret"])
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"next-access","refresh_token":"next-refresh","token_type":"Bearer","expires_in":3600,"scope":"user:profile:read user:invoices:read"}`))}, nil
	})}
	driver := NewCredentialRefreshDriver(client, redirectURI)
	state := sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("old-refresh"),
	}, Now: time.Now()}
	result, err := driver.Refresh(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, "next-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "next-refresh", result.Credentials.RefreshToken.Reveal())
}

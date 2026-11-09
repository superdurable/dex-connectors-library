// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestClientMintsAtConfiguredIdentityServiceEndpoint(t *testing.T) {
	const endpoint = "https://identity.example.com/connect/token"
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, endpoint, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "client_credentials", request.Form.Get("grant_type"))
		require.Equal(t, "sample-client-id", request.Form.Get("client_id"))
		require.Equal(t, "sample-client-secret", request.Form.Get("client_secret"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"next-access","token_type":"Bearer","expires_in":3600}`))}, nil
	})
	client := testClient(t, transport)
	state := sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret")}, Now: time.Now()}
	result, err := client.refreshDriver.Refresh(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, "next-access", result.Credentials.AccessToken.Reveal())
}

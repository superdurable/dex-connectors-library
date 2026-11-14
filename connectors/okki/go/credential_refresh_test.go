// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package okki

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

type transportFunc func(*http.Request) (*http.Response, error)

func (transport transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestCredentialRefreshRequestsCompanyScope(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://api-sandbox.xiaoman.cn/v1/oauth2/access_token", request.URL.String())
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "client_credentials", request.PostForm.Get("grant_type"))
		require.Equal(t, "company", request.PostForm.Get("scope"))
		require.Equal(t, "client123", request.PostForm.Get("client_id"))
		require.Equal(t, "secretKey", request.PostForm.Get("client_secret"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token_type":"Bearer","access_token":"jwt123"}`))}, nil
	})}
	driver := NewCredentialRefreshDriver(client)
	fixed := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	driver.now = func() time.Time { return fixed }
	state := sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{ClientID: "client123", ClientSecret: sdkgo.NewSecretString("secretKey")}, Now: fixed}
	result, err := driver.Refresh(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, 1, requests)
	require.Equal(t, "jwt123", result.Credentials.AccessToken.Reveal())
	require.Equal(t, fixed.Add(8*time.Hour), result.ExpiresAt)
}

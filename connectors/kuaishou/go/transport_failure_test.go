// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaishou

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type failingCredentialURLTransport struct{ requestedURLs []string }

func (transport *failingCredentialURLTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requestedURLs = append(transport.requestedURLs, request.URL.String())
	return nil, errors.New("dial tcp: connection refused")
}

// net/http wraps a transport error in *url.Error, whose text repeats the full URL.
func TestListUserVideosTransportFailureDoesNotLeakURLCredential(t *testing.T) {
	transport := &failingCredentialURLTransport{}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: "app123", AccessToken: sdkgo.NewSecretString("token456")}}
	client, err := New(Config{BaseURL: "https://provider.example.com"}, credentials, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListUserVideos(), testConnection, ListUserVideosInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotEmpty(t, transport.requestedURLs)
	require.Contains(t, transport.requestedURLs[0], "token456")
	require.NotContains(t, retry.Failure.Message, "token456")
	require.NotContains(t, err.Error(), "token456")
	require.NotContains(t, err.Error(), "provider.example.com")
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validConfig() oauth2http.Config {
	return oauth2http.Config{
		AuthorizationEndpoint: "http://keycloak:8080/realms/app/protocol/openid-connect/auth",
		TokenEndpoint:         "http://keycloak:8080/realms/app/protocol/openid-connect/token",
		Scopes:                []string{"openid", "profile"},
		APIBaseURL:            "http://keycloak:8080/",
	}
}

func staticCredentials() sdkgo.StaticCredentialProvider[oauth2http.Credentials] {
	return sdkgo.StaticCredentialProvider[oauth2http.Credentials]{testConnection: testCredentials("access", "refresh")}
}

func TestNewAcceptsPlainHTTPEndpointsThatTheConnectionOwnerEnters(t *testing.T) {
	_, err := oauth2http.New(validConfig(), staticCredentials())
	require.NoError(t, err)

	httpsConfig := validConfig()
	httpsConfig.AuthorizationEndpoint = "https://login.example.com/authorize"
	httpsConfig.TokenEndpoint = "https://login.example.com/token"
	httpsConfig.APIBaseURL = "https://api.example.com/v2/"
	_, err = oauth2http.New(httpsConfig, staticCredentials())
	require.NoError(t, err)
}

func TestNewRejectsInvalidSettingsBeforeAnyRequest(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*oauth2http.Config)
	}{
		{"missing authorization endpoint", func(config *oauth2http.Config) { config.AuthorizationEndpoint = "" }},
		{"authorization endpoint with another scheme", func(config *oauth2http.Config) { config.AuthorizationEndpoint = "ftp://login.example.com/authorize" }},
		{"missing token endpoint", func(config *oauth2http.Config) { config.TokenEndpoint = "" }},
		{"token endpoint with another scheme", func(config *oauth2http.Config) { config.TokenEndpoint = "ftp://login.example.com/token" }},
		{"token endpoint with user information", func(config *oauth2http.Config) { config.TokenEndpoint = "http://user:password@login.example.com/token" }},
		{"no scopes", func(config *oauth2http.Config) { config.Scopes = nil }},
		{"missing API base URL", func(config *oauth2http.Config) { config.APIBaseURL = "" }},
		{"API base URL with another scheme", func(config *oauth2http.Config) { config.APIBaseURL = "ws://api.example.com" }},
		{"API base URL with user information", func(config *oauth2http.Config) { config.APIBaseURL = "https://user:password@api.example.com" }},
		{"API base URL with a query", func(config *oauth2http.Config) { config.APIBaseURL = "https://api.example.com/v2?key=1" }},
		{"API base URL with an empty query", func(config *oauth2http.Config) { config.APIBaseURL = "https://api.example.com/v2?" }},
		{"API base URL with a fragment", func(config *oauth2http.Config) { config.APIBaseURL = "https://api.example.com/v2#x" }},
		{"API base URL with a space", func(config *oauth2http.Config) { config.APIBaseURL = "https://api.example.com/v 2" }},
		{"unknown client authentication method", func(config *oauth2http.Config) { config.TokenEndpointAuthMethod = "private_key_jwt" }},
		{"negative body limit", func(config *oauth2http.Config) { config.MaxBodyBytes = -1 }},
		{"request timeout under a second", func(config *oauth2http.Config) { config.RequestTimeout = 500 * time.Millisecond }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(&config)

			_, err := oauth2http.New(config, staticCredentials())

			require.Error(t, err)
			require.NotContains(t, err.Error(), "password")
		})
	}
	_, err := oauth2http.New(validConfig(), nil)
	require.Error(t, err, "a credential provider is required")
	_, err = oauth2http.New(validConfig(), staticCredentials(), nil)
	require.Error(t, err, "a nil option is a programming error")
}

func TestNewUsesTheManifestDefaults(t *testing.T) {
	defaults := oauth2http.DefaultConfig()

	require.Equal(t, oauth2http.TokenEndpointAuthMethodClientSecretPost, defaults.TokenEndpointAuthMethod)
	require.EqualValues(t, 1<<20, defaults.MaxBodyBytes)
	require.Equal(t, 12*time.Second, defaults.RequestTimeout)
}

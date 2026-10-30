// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Description string   `yaml:"description"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

type settingReference struct {
	FromSetting string `yaml:"fromSetting"`
}

type oauth2Manifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type        string          `yaml:"type"`
			Refreshable bool            `yaml:"refreshable"`
			Fields      []manifestField `yaml:"fields"`
			Guide       struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 struct {
				AuthorizationEndpoint  settingReference `yaml:"authorizationEndpoint"`
				TokenEndpoint          settingReference `yaml:"tokenEndpoint"`
				Scopes                 settingReference `yaml:"scopes"`
				ClientIDCredential     string           `yaml:"clientIDCredential"`
				ClientSecretCredential string           `yaml:"clientSecretCredential"`
				PKCE                   bool             `yaml:"pkce"`
				CredentialMappings     []struct {
					Credential string `yaml:"credential"`
					Source     string `yaml:"source"`
				} `yaml:"credentialMappings"`
			} `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

func readOAuth2Manifest(t *testing.T) oauth2Manifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest oauth2Manifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

func fieldsByName(fields []manifestField) map[string]manifestField {
	byName := map[string]manifestField{}
	for _, field := range fields {
		byName[field.Name] = field
	}
	return byName
}

// TestOAuthEndpointsAndScopesComeFromRequiredSettings keeps the OAuth declaration and the settings it names together.
func TestOAuthEndpointsAndScopesComeFromRequiredSettings(t *testing.T) {
	manifest := readOAuth2Manifest(t)
	settings := fieldsByName(manifest.Spec.Configuration.Fields)

	require.Equal(t, "oauth2", manifest.Spec.Auth.Type)
	require.True(t, manifest.Spec.Auth.Refreshable, "the connector refreshes its tokens")
	require.True(t, manifest.Spec.Auth.OAuth2.PKCE)
	for reference, expectedType := range map[settingReference]string{
		manifest.Spec.Auth.OAuth2.AuthorizationEndpoint: "url",
		manifest.Spec.Auth.OAuth2.TokenEndpoint:         "url",
		manifest.Spec.Auth.OAuth2.Scopes:                "stringList",
	} {
		field, found := settings[reference.FromSetting]
		require.True(t, found, "setting %q", reference.FromSetting)
		require.Equal(t, expectedType, field.Type)
		require.True(t, field.Required, "authorization cannot start without %s", field.Name)
		require.Nil(t, field.Default, "the connection owner enters %s", field.Name)
	}
}

// TestClientSecretAndTokensAreCredentialsNeverSettings keeps every secret out of the settings the platform stores in plain text.
func TestClientSecretAndTokensAreCredentialsNeverSettings(t *testing.T) {
	manifest := readOAuth2Manifest(t)
	credentials := fieldsByName(manifest.Spec.Auth.Fields)

	for _, setting := range manifest.Spec.Configuration.Fields {
		require.NotEqual(t, "secretString", setting.Type, setting.Name)
		for _, secretWord := range []string{"secret", "password", "accesstoken", "refreshtoken"} {
			require.NotContains(t, strings.ToLower(setting.Name), secretWord, "%s looks like a secret", setting.Name)
		}
		require.NotContains(t, credentials, setting.Name)
	}
	require.Equal(t, "oauth_client_id", manifest.Spec.Auth.OAuth2.ClientIDCredential)
	require.Equal(t, "string", credentials["oauth_client_id"].Type)
	require.Equal(t, "oauth_client_secret", manifest.Spec.Auth.OAuth2.ClientSecretCredential)
	for _, name := range []string{"oauth_client_secret", "access_token", "refresh_token"} {
		require.Equal(t, "secretString", credentials[name].Type, name)
	}
	require.True(t, credentials["oauth_client_secret"].Required)
	require.True(t, credentials["access_token"].Required)
	require.False(t, credentials["refresh_token"].Required, "a server may issue no refresh token")
	mapped := map[string]string{}
	for _, mapping := range manifest.Spec.Auth.OAuth2.CredentialMappings {
		mapped[mapping.Credential] = mapping.Source
	}
	require.Equal(t, map[string]string{"access_token": "access_token", "refresh_token": "refresh_token"}, mapped)
}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connections shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readOAuth2Manifest(t)
	require.Nil(t, manifest.Spec.Studio, "no Studio bundle: Dex Web renders the host-owned form")
	visibleFields := []string{}
	for _, field := range append(append([]manifestField(nil), manifest.Spec.Auth.Fields...), manifest.Spec.Configuration.Fields...) {
		visibleFields = append(visibleFields, field.Name)
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "a label is not guidance")
			require.Regexp(t, `Required|[Bb]lank`, field.Description, "the description states what blank means")
			if field.Type == "secretString" {
				require.Contains(t, field.Description, "Secret")
			}
			if field.Default != nil && len(field.Enum) == 0 {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default itself")
			}
			for _, value := range field.Enum {
				require.Contains(t, field.Description, value, "every choice is explained")
			}
		})
	}
	require.Equal(t, []string{
		"oauth_client_id", "oauth_client_secret", "access_token", "refresh_token",
		"authorizationEndpoint", "tokenEndpoint", "scopes", "tokenEndpointAuthMethod", "apiBaseUrl", "maxBodyBytes", "requestTimeout",
	}, visibleFields)
}

// TestAuthorizationGuideStartsAtTheServerSetupSection keeps the guide link and README anchor together.
func TestAuthorizationGuideStartsAtTheServerSetupSection(t *testing.T) {
	guide := readOAuth2Manifest(t).Spec.Auth.Guide
	startURL, err := url.Parse(guide.StartURL)
	require.NoError(t, err)
	require.Equal(t, "https", startURL.Scheme)
	require.Equal(t, "authorization-server-setup", startURL.Fragment)
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	require.Contains(t, string(readme), "\n## Authorization server setup\n")
	require.Len(t, guide.Steps, 4)
	for _, step := range guide.Steps {
		require.GreaterOrEqual(t, len(strings.Fields(step)), 15)
	}
}

func TestCredentialsAndConnectionsNeverRenderTheSecrets(t *testing.T) {
	credentials := testCredentials("SENTINEL-ACCESS-TOKEN", "SENTINEL-REFRESH-TOKEN")
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		for _, secret := range []string{sentinelSecret, "SENTINEL-ACCESS-TOKEN", "SENTINEL-REFRESH-TOKEN"} {
			require.NotContains(t, rendered, secret)
		}
	}
	client, err := oauth2http.New(validConfig(), sdkgo.StaticCredentialProvider[oauth2http.Credentials]{testConnection: credentials})
	require.NoError(t, err)
	connection, err := oauth2http.NewConnection(client, testConnection)
	require.NoError(t, err)
	require.Equal(t, "oauth2http.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err = connection.MarshalJSON()
	require.Error(t, err)
}

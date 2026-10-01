// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
}

type zoomManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type   string          `yaml:"type"`
			Fields []manifestField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 struct {
				AuthorizationEndpoint  string   `yaml:"authorizationEndpoint"`
				TokenEndpoint          string   `yaml:"tokenEndpoint"`
				ClientIDCredential     string   `yaml:"clientIDCredential"`
				ClientSecretCredential string   `yaml:"clientSecretCredential"`
				Scopes                 []string `yaml:"scopes"`
				PKCE                   bool     `yaml:"pkce"`
				CredentialMappings     []struct {
					Credential string `yaml:"credential"`
					Source     string `yaml:"source"`
				} `yaml:"credentialMappings"`
			} `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

func loadZoomManifest(t *testing.T) zoomManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest zoomManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Dex Web exchanges the code at the manifest endpoint and the driver refreshes at its own constant.
func TestManifestOAuthMatchesTheRefreshDriver(t *testing.T) {
	oauth := loadZoomManifest(t).Spec.Auth.OAuth2
	require.Equal(t, "https://zoom.us/oauth/authorize", oauth.AuthorizationEndpoint)
	require.Equal(t, zoomOAuthTokenEndpoint, oauth.TokenEndpoint)
	require.Equal(t, zoomRequiredOAuthScopes, oauth.Scopes)
	require.True(t, oauth.PKCE)
	require.Equal(t, "oauth_client_id", oauth.ClientIDCredential)
	require.Equal(t, "oauth_client_secret", oauth.ClientSecretCredential)
	mappings := map[string]string{}
	for _, mapping := range oauth.CredentialMappings {
		mappings[mapping.Credential] = mapping.Source
	}
	require.Equal(t, map[string]string{"access_token": "access_token", "refresh_token": "refresh_token"}, mappings)
}

// Dex Web renders these descriptions and the guide as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadZoomManifest(t)
	require.Nil(t, manifest.Spec.Studio, "no field needs a picker, so the connector ships no Studio bundle")
	defaults := DefaultConfig()
	defaultByField := map[string]any{"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes)}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.False(t, field.Required, "%s has a default", field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}

	auth := manifest.Spec.Auth
	require.Equal(t, "oauth2", auth.Type)
	require.Equal(t, "https://marketplace.zoom.us/", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, "\n")
	for _, instruction := range []string{
		"Develop > Build an app > General app", "User-managed", "OAuth Information", "OAuth redirect URL",
		"Redirect URI shown below", "OAuth allow lists", "127.0.0.1 rather than localhost", "Scopes", "Add Scopes > Meeting",
		"App Credentials", "only members of the developer's Zoom account",
	} {
		require.Contains(t, guide, instruction)
	}
	for _, scope := range zoomRequiredOAuthScopes {
		require.Contains(t, guide, scope, "the guide names every requested scope")
	}
	fieldNames := []string{}
	for _, field := range auth.Fields {
		fieldNames = append(fieldNames, field.Name)
		isSecret := field.Name != "oauth_client_id"
		require.Equal(t, isSecret, field.Type == "secretString", "%s secrecy", field.Name)
		require.Equal(t, isSecret, strings.HasPrefix(field.Description, "Secret "), "%s states secrecy", field.Name)
		if field.Name == "access_token" || field.Name == "refresh_token" {
			require.Contains(t, field.Description, "automatically", "%s is an authorization output", field.Name)
			require.Contains(t, field.Description, "never enter it", "%s is never entered", field.Name)
			continue
		}
		require.Contains(t, field.Description, "https://marketplace.zoom.us/", "%s names where to start", field.Name)
		require.Contains(t, field.Description, "App Credentials", "%s names the page", field.Name)
		require.Contains(t, field.Description, "blank", "%s explains blank", field.Name)
	}
	require.Equal(t, []string{"oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, fieldNames)
}

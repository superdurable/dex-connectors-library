// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type          string          `yaml:"type"`
			DefaultMethod string          `yaml:"defaultMethod"`
			Methods       []authMethod    `yaml:"methods"`
			Fields        []guidanceField `yaml:"fields"`
		} `yaml:"auth"`
		Studio     any   `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Authorization string `yaml:"authorization"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

type authMethod struct {
	ID          string          `yaml:"id"`
	Type        string          `yaml:"type"`
	Recommended bool            `yaml:"recommended"`
	Fields      []guidanceField `yaml:"fields"`
	Guide       struct {
		StartURL string   `yaml:"startURL"`
		Steps    []string `yaml:"steps"`
	} `yaml:"guide"`
	OAuth2 *struct {
		AuthorizationEndpoint string              `yaml:"authorizationEndpoint"`
		TokenEndpoint         string              `yaml:"tokenEndpoint"`
		Scopes                []string            `yaml:"scopes"`
		PKCE                  bool                `yaml:"pkce"`
		CredentialMappings    []map[string]string `yaml:"credentialMappings"`
	} `yaml:"oauth2"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

var mondayURLPattern = regexp.MustCompile(`https://monday\.com/?`)

// oauthOutputFields are produced by Dex Web's OAuth exchange; every other credential is user-supplied.
var oauthOutputFields = map[string]bool{"access_token": true, "refresh_token": true}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	var names []string
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
	}
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"maxResponseBytes", "api_token", "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, names,
		"a new visible field needs its own guidance review")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			} else if field.Name != "maxResponseBytes" {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
			switch {
			case oauthOutputFields[field.Name]:
				require.Contains(t, field.Description, "never enter it manually", "OAuth outputs are not manual inputs")
				require.True(t, field.Name == "refresh_token" || strings.Contains(field.Description, "JWT"))
			case field.Name == "api_token":
				require.Regexp(t, mondayURLPattern, field.Description)
				for _, path := range []string{"profile picture", "Developers", "API token > Show", "Administration > Connections > Personal API token", "Regenerate"} {
					require.Contains(t, field.Description, path)
				}
			case field.Name == "oauth_client_id" || field.Name == "oauth_client_secret":
				require.Contains(t, field.Description, "Basic Information > App credentials")
			}
		})
	}
}

func TestAuthorizationGuidesCoverTokenAndOAuthSetup(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Empty(t, manifest.Spec.Auth.Type, "each method declares its own type")
	require.Equal(t, "personal-api-token", manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)

	personal := manifest.Spec.Auth.Methods[0]
	require.Equal(t, "personal-api-token", personal.ID)
	require.Equal(t, "apiKey", personal.Type)
	require.True(t, personal.Recommended)
	require.Nil(t, personal.OAuth2)
	require.Equal(t, "https://monday.com/", personal.Guide.StartURL)
	personalGuide := strings.Join(personal.Guide.Steps, " ")
	for _, instruction := range []string{"profile picture", "Developers", "Developer Center", "API token > Show", "Administration > Connections > Personal API token", "Regenerate", "permissions"} {
		require.Contains(t, personalGuide, instruction)
	}

	oauth := manifest.Spec.Auth.Methods[1]
	require.Equal(t, "monday-oauth", oauth.ID)
	require.Equal(t, "oauth2", oauth.Type)
	require.NotNil(t, oauth.OAuth2)
	require.Equal(t, "https://auth.monday.com/oauth2/authorize", oauth.OAuth2.AuthorizationEndpoint)
	require.Equal(t, "https://auth.monday.com/oauth_ms/oauth/token", oauth.OAuth2.TokenEndpoint, "the OAuth 2.1 endpoint issues refresh tokens")
	require.True(t, oauth.OAuth2.PKCE, "monday.com's OAuth 2.1 flow requires S256 PKCE")
	require.Equal(t, []string{"boards:read", "boards:write", "updates:write"}, oauth.OAuth2.Scopes)
	require.Equal(t, []map[string]string{{"credential": "access_token", "source": "access_token"}, {"credential": "refresh_token", "source": "refresh_token"}},
		oauth.OAuth2.CredentialMappings)
	oauthGuide := strings.Join(oauth.Guide.Steps, " ")
	for _, instruction := range []string{"Developer Center", "OAuth & Permissions", "boards:read", "boards:write", "updates:write",
		"Redirect URI shown below", "New OAuth Flow", "PKCE", "Basic Information > App credentials", "Client ID", "Client secret", "installed"} {
		require.Contains(t, oauthGuide, instruction)
	}
}

func TestEveryOperationNeedsAnAuthorizedConnection(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Len(t, manifest.Spec.Operations, 5)
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
	}
}

// TestManifestDeclaresNoStudioBundleOrTriggers records that GET-only Studio commands cannot call POST-only GraphQL.
func TestManifestDeclaresNoStudioBundleOrTriggers(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

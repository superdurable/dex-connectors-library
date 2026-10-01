// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"fmt"
	"os"
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
			Methods       []guidanceAuth  `yaml:"methods"`
			Fields        []guidanceField `yaml:"fields"`
		} `yaml:"auth"`
		Studio     any   `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Kind          string `yaml:"kind"`
			Authorization string `yaml:"authorization"`
			Execution     struct {
				Durability string `yaml:"durability"`
				Retry      struct {
					TotalDuration string `yaml:"totalDuration"`
				} `yaml:"retry"`
			} `yaml:"execution"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

type guidanceAuth struct {
	ID            string          `yaml:"id"`
	Type          string          `yaml:"type"`
	Recommended   bool            `yaml:"recommended"`
	Fields        []guidanceField `yaml:"fields"`
	Configuration struct {
		Fields []guidanceField `yaml:"fields"`
	} `yaml:"configuration"`
	Guide struct {
		StartURL string   `yaml:"startURL"`
		Steps    []string `yaml:"steps"`
	} `yaml:"guide"`
	OAuth2 *struct {
		AuthorizationEndpoint  string              `yaml:"authorizationEndpoint"`
		TokenEndpoint          string              `yaml:"tokenEndpoint"`
		ClientIDCredential     string              `yaml:"clientIDCredential"`
		ClientSecretCredential string              `yaml:"clientSecretCredential"`
		Scopes                 []string            `yaml:"scopes"`
		PKCE                   bool                `yaml:"pkce"`
		CredentialMappings     []map[string]string `yaml:"credentialMappings"`
	} `yaml:"oauth2"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

// derivedCredentialFields are produced by Dex Web's OAuth exchange or by the refresh driver, never typed.
var derivedCredentialFields = map[string]bool{"access_token": true, "refresh_token": true}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	var names []string
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
		visibleFields = append(visibleFields, method.Configuration.Fields...)
	}
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{
		"maxResponseBytes", "client_id", "client_secret", "access_token",
		"client_id", "client_secret", "access_token", "refresh_token", "organisation",
	}, names, "a new visible field needs its own guidance review")

	for index, field := range visibleFields {
		t.Run(fmt.Sprintf("%d-%s", index, field.Name), func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 150, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			switch {
			case field.Type == "secretString":
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			case field.Name != "maxResponseBytes":
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
			switch {
			case derivedCredentialFields[field.Name]:
				require.Contains(t, field.Description, "never enter it manually", "derived tokens are not manual inputs")
				if field.Name == "access_token" {
					require.Contains(t, field.Description, "30-minute")
				} else {
					require.Contains(t, field.Description, "60 days")
				}
			case field.Name == "client_id" || field.Name == "client_secret":
				require.Contains(t, field.Description, "https://developer.xero.com/app/manage")
				require.Contains(t, field.Description, "Configuration")
			case field.Name == "organisation":
				require.Contains(t, field.Description, "https://go.xero.com")
				require.Contains(t, field.Description, "tenant ID")
				require.Contains(t, field.Description, "https://api.xero.com/connections")
			}
		})
	}
}

func TestAuthorizationGuidesCoverCustomConnectionAndOAuthSetup(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Empty(t, manifest.Spec.Auth.Type, "each method declares its own type")
	require.Equal(t, CustomConnectionAuthMethodID, manifest.Spec.Auth.DefaultMethod, "the method Dex Web can complete today comes first")
	require.Len(t, manifest.Spec.Auth.Methods, 2)

	custom := manifest.Spec.Auth.Methods[0]
	require.Equal(t, CustomConnectionAuthMethodID, custom.ID)
	require.Equal(t, "apiKey", custom.Type, "Dex Web saves the client ID and secret as a plain credential form")
	require.True(t, custom.Recommended)
	require.Nil(t, custom.OAuth2)
	require.Equal(t, "https://developer.xero.com/app/manage", custom.Guide.StartURL)
	customGuide := strings.Join(custom.Guide.Steps, " ")
	for _, instruction := range append([]string{"New app", "Custom connection", "authorising user", "Connect", "subscription",
		"Demo Company", "Configuration", "Client ID", "Generate a secret", "https://apps.xero.com/connected"}, accountingScopes...) {
		require.Contains(t, customGuide, instruction)
	}
	require.False(t, findField(custom.Fields, "access_token").Required, "Dex mints the token; the form never requires it")

	oauth := manifest.Spec.Auth.Methods[1]
	require.Equal(t, OAuthAuthMethodID, oauth.ID)
	require.Equal(t, "oauth2", oauth.Type)
	require.NotNil(t, oauth.OAuth2)
	require.Equal(t, "https://login.xero.com/identity/connect/authorize", oauth.OAuth2.AuthorizationEndpoint)
	require.Equal(t, xeroTokenEndpoint, oauth.OAuth2.TokenEndpoint)
	require.Equal(t, append([]string{"offline_access"}, accountingScopes...), oauth.OAuth2.Scopes, "the driver checks the same accounting scopes")
	require.False(t, oauth.OAuth2.PKCE, "a Xero web app uses its client secret; Xero documents PKCE only for public apps")
	require.Equal(t, "client_id", oauth.OAuth2.ClientIDCredential)
	require.Equal(t, "client_secret", oauth.OAuth2.ClientSecretCredential)
	require.Equal(t, []map[string]string{{"credential": "access_token", "source": "access_token"}, {"credential": "refresh_token", "source": "refresh_token"}},
		oauth.OAuth2.CredentialMappings)
	oauthGuide := strings.Join(oauth.Guide.Steps, " ")
	for _, instruction := range []string{"Web app", "http://localhost", "http://127.0.0.1", "Redirect URI shown below", "Redirect URIs",
		"Generate a secret", "offline_access", "organisation", "60 days", "https://apps.xero.com/connected"} {
		require.Contains(t, oauthGuide, instruction)
	}
}

func TestOperationsNeedAnAuthorizedConnectionAndWritesStayInsideTheKeyLifetime(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Len(t, manifest.Spec.Operations, 6)
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
		require.Equal(t, "async", operation.Execution.Durability, "%s: duplicates are safe, so Dex's async dispatch is kept", operation.Name)
		if operation.Kind == "mutation" {
			require.Equal(t, "4m", operation.Execution.Retry.TotalDuration, "%s: Xero keeps an Idempotency-Key for six minutes", operation.Name)
		}
	}
}

// TestManifestDeclaresNoStudioBundleOrTriggers records that /connections returns an array, which Studio
// setup commands reject, and that Xero webhooks need an intent-to-receive handshake.
func TestManifestDeclaresNoStudioBundleOrTriggers(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
}

func findField(fields []guidanceField, name string) guidanceField {
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	return guidanceField{}
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

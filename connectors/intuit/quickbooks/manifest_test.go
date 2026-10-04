// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

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
			Type        string          `yaml:"type"`
			Refreshable bool            `yaml:"refreshable"`
			Fields      []guidanceField `yaml:"fields"`
			Guide       struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 struct {
				Protocol                string              `yaml:"protocol"`
				AuthorizationEndpoint   string              `yaml:"authorizationEndpoint"`
				TokenEndpoint           string              `yaml:"tokenEndpoint"`
				AuthorizationParameters map[string]string   `yaml:"authorizationParameters"`
				ClientIDCredential      string              `yaml:"clientIDCredential"`
				ClientSecretCredential  string              `yaml:"clientSecretCredential"`
				Scopes                  []string            `yaml:"scopes"`
				PKCE                    bool                `yaml:"pkce"`
				CredentialMappings      []map[string]string `yaml:"credentialMappings"`
			} `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio     any   `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Kind          string `yaml:"kind"`
			Idempotency   string `yaml:"idempotency"`
			Authorization string `yaml:"authorization"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Enum        []string `yaml:"enum"`
	Required    bool     `yaml:"required"`
	Default     any      `yaml:"default"`
	Description string   `yaml:"description"`
}

// derivedCredentialFields are produced by Dex Web's OAuth exchange or by the refresh driver, never typed.
var derivedCredentialFields = map[string]bool{"access_token": true, "refresh_token": true, "id_token": true}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"environment", "realmId", "maxResponseBytes", "client_id", "client_secret", "access_token", "refresh_token", "id_token"},
		names, "a new visible field needs its own guidance review")

	for index, field := range visibleFields {
		t.Run(fmt.Sprintf("%d-%s", index, field.Name), func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 150, "a label-length description is not guidance")
			if field.Default != nil && len(field.Enum) == 0 {
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
			case field.Name == "client_id" || field.Name == "client_secret":
				require.Contains(t, field.Description, "https://developer.intuit.com")
				require.Contains(t, field.Description, "Keys and credentials")
			case field.Name == "environment":
				require.Equal(t, []string{string(EnvironmentProduction), string(EnvironmentSandbox)}, field.Enum)
				require.Contains(t, field.Description, "https://"+productionAPIHost)
				require.Contains(t, field.Description, "https://"+sandboxAPIHost)
			case field.Name == "realmId":
				require.False(t, field.Required, "the ID token names the company; realmId is only a fallback")
				require.Contains(t, field.Description, "Settings > Subscriptions and billing")
				require.Contains(t, field.Description, "https://developer.intuit.com/app/developer/playground")
			}
		})
	}
	require.Contains(t, findField(manifest.Spec.Auth.Fields, "access_token").Description, "one-hour")
	require.Contains(t, findField(manifest.Spec.Auth.Fields, "refresh_token").Description, "100 days")
	require.False(t, findField(manifest.Spec.Auth.Fields, "id_token").Required, "a connection without realmid can still use the realmId fallback")
}

func TestAuthorizationGuideCoversIntuitAppSetupAndTheCompanyID(t *testing.T) {
	manifest := readGuidanceManifest(t)
	auth := manifest.Spec.Auth
	require.Equal(t, "oauth2", auth.Type)
	require.True(t, auth.Refreshable, "Intuit access tokens last one hour")
	require.Equal(t, "https://developer.intuit.com", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"My Hub > App dashboard", "Settings > Redirect URIs", "Redirect URI shown below", "http://localhost", "http://127.0.0.1",
		"Keys and credentials", "Show credentials", "Production Key questionnaire", "sandbox", accountingScope, "openid", "realmId",
		"100 days", "five years",
	} {
		require.Contains(t, guide, instruction)
	}
	oauth := auth.OAuth2
	require.Equal(t, "oauth2", oauth.Protocol)
	require.Equal(t, "https://appcenter.intuit.com/connect/oauth2", oauth.AuthorizationEndpoint)
	require.Equal(t, intuitTokenEndpoint, oauth.TokenEndpoint)
	require.Equal(t, []string{accountingScope, "openid"}, oauth.Scopes, "openid makes Intuit return the ID token that names the company")
	require.Equal(t, map[string]string{"claims": `{"id_token":{"realmId":null}}`}, oauth.AuthorizationParameters)
	require.False(t, oauth.PKCE, "Intuit documents no PKCE; the app authenticates with its client secret")
	require.Equal(t, "client_id", oauth.ClientIDCredential)
	require.Equal(t, "client_secret", oauth.ClientSecretCredential)
	require.Equal(t, []map[string]string{
		{"credential": "access_token", "source": "access_token"}, {"credential": "refresh_token", "source": "refresh_token"},
		{"credential": "id_token", "source": "id_token"},
	}, oauth.CredentialMappings)
}

func TestEveryOperationNeedsAnAuthorizedConnectionAndEveryWriteIsKeyed(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Len(t, manifest.Spec.Operations, 7)
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
		if operation.Kind == "mutation" {
			require.Equal(t, "required", operation.Idempotency, "%s sends the Step's requestid", operation.Name)
		} else {
			require.Equal(t, "none", operation.Idempotency, operation.Name)
		}
	}
}

// No Triggers: one QuickBooks webhook request can batch several companies' changes; webhooktrigger decodes one.
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

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Refreshable   bool   `yaml:"refreshable"`
			MethodLabel   string `yaml:"methodLabel"`
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID          string          `yaml:"id"`
				Description string          `yaml:"description"`
				Fields      []guidanceField `yaml:"fields"`
				Guide       struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				OAuth2 struct {
					AuthorizationEndpoint   string            `yaml:"authorizationEndpoint"`
					TokenEndpoint           string            `yaml:"tokenEndpoint"`
					AuthorizationParameters map[string]string `yaml:"authorizationParameters"`
					Scopes                  []string          `yaml:"scopes"`
					PKCE                    bool              `yaml:"pkce"`
					CredentialMappings      []struct {
						Credential string `yaml:"credential"`
						Source     string `yaml:"source"`
					} `yaml:"credentialMappings"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		require.Len(t, method.Fields, 5, method.ID)
		visibleFields = append(visibleFields, method.Fields...)
	}
	var names []string
	for _, field := range visibleFields[:6] {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"maxResponseBytes", "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token", "api_domain"}, names,
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
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret") || strings.HasPrefix(field.Description, "Maximum"))
			}
			if field.Name == "access_token" || field.Name == "refresh_token" || field.Name == "api_domain" {
				require.Contains(t, field.Description, "never enter it manually", "OAuth outputs are not manual inputs")
			}
		})
	}
	require.Nil(t, manifest.Spec.Studio, "no Studio unit: see the README's Pickers section")
}

func TestEveryDataCenterMethodAuthorizesAtItsOwnZohoAccountsServer(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.True(t, auth.Refreshable)
	require.Equal(t, "Data center", auth.MethodLabel)
	require.Equal(t, crm.USDataCenterAuthMethodID, auth.DefaultMethod)
	dataCenters := crm.DataCenters()
	require.Len(t, auth.Methods, len(dataCenters))
	for index, method := range auth.Methods {
		dataCenter := dataCenters[index]
		t.Run(method.ID, func(t *testing.T) {
			require.Equal(t, dataCenter.AuthMethodID, method.ID)
			require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/auth", method.OAuth2.AuthorizationEndpoint)
			require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/token", method.OAuth2.TokenEndpoint)
			require.Equal(t, []string{"ZohoCRM.modules.READ", "ZohoCRM.modules.CREATE", "ZohoCRM.modules.UPDATE", "ZohoCRM.coql.READ", "ZohoCRM.settings.fields.READ"},
				method.OAuth2.Scopes, "no DELETE scope: the connector never deletes")
			require.Equal(t, map[string]string{"access_type": "offline", "prompt": "consent"}, method.OAuth2.AuthorizationParameters)
			require.False(t, method.OAuth2.PKCE, "Zoho documents no PKCE for server-based clients")
			var mappings []string
			for _, mapping := range method.OAuth2.CredentialMappings {
				mappings = append(mappings, mapping.Credential+"="+mapping.Source)
			}
			require.Equal(t, []string{"access_token=access_token", "refresh_token=refresh_token", "api_domain=api_domain"}, mappings)

			consoleURL := strings.Replace(dataCenter.AccountsURL, "https://accounts.", "https://api-console.", 1)
			require.Equal(t, consoleURL, method.Guide.StartURL)
			require.Contains(t, method.Description, dataCenter.ProductionAPIDomain())
			require.Contains(t, method.Description, dataCenter.AccountsURL)
			guide := strings.Join(method.Guide.Steps, " ")
			for _, text := range append([]string{consoleURL, dataCenter.AccountsURL, "Server-based Applications", "Redirect URI", "Authorized Redirect URIs",
				"Client Secret tab", "Settings tab", "consent", "sandbox", "revoke"}, method.OAuth2.Scopes...) {
				require.Contains(t, guide, text)
			}
			for _, other := range dataCenters {
				if other.AuthMethodID != dataCenter.AuthMethodID {
					require.NotContains(t, guide+method.Description, other.AccountsURL+" ", "a method names only its own data center")
					require.NotContains(t, method.Description, other.ProductionAPIDomain()+" ")
				}
			}
		})
	}
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

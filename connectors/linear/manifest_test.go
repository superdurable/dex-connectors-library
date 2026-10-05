// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}

type manifestAuthMethod struct {
	ID     string          `yaml:"id"`
	Type   string          `yaml:"type"`
	Fields []manifestField `yaml:"fields"`
	Guide  struct {
		StartURL string   `yaml:"startURL"`
		Steps    []string `yaml:"steps"`
	} `yaml:"guide"`
	OAuth2 *struct {
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
}

type linearManifest struct {
	Metadata struct {
		Company string `yaml:"company"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Refreshable   bool                 `yaml:"refreshable"`
			DefaultMethod string               `yaml:"defaultMethod"`
			Methods       []manifestAuthMethod `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					Method     string            `yaml:"method"`
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
					Headers    map[string]string `yaml:"fixedHeaders"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
					} `yaml:"credential"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
				Outputs     []struct {
					Name string `yaml:"name"`
				} `yaml:"outputs"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func readLinearManifest(t *testing.T) linearManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest linearManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connections shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readLinearManifest(t)
	require.Equal(t, "Linear", manifest.Metadata.Company)
	require.Equal(t, "v0.21.0", manifest.Metadata.Version)
	require.True(t, manifest.Spec.Auth.Refreshable, "OAuth access tokens expire after 24 hours")
	require.Equal(t, linear.PersonalAPIKeyAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	visibleFields := map[string][]string{}
	for _, method := range manifest.Spec.Auth.Methods {
		mapped := map[string]bool{}
		if method.OAuth2 != nil {
			for _, mapping := range method.OAuth2.CredentialMappings {
				mapped[mapping.Credential] = true
			}
		}
		for _, field := range method.Fields {
			if mapped[field.Name] {
				require.Contains(t, field.Description, "never enter it manually", "%s is produced by OAuth", field.Name)
				continue
			}
			visibleFields[method.ID] = append(visibleFields[method.ID], field.Name)
			require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "%s/%s: a label is not guidance", method.ID, field.Name)
			require.Contains(t, field.Description, "https://linear.app/settings/", "%s/%s names where to start", method.ID, field.Name)
			if field.Type == "secretString" {
				require.Contains(t, field.Description, "Secret", "%s/%s says it is secret", method.ID, field.Name)
			}
			require.True(t, field.Required || strings.Contains(field.Description, "Blank"), "%s/%s says what blank means", method.ID, field.Name)
		}
		require.Len(t, method.Guide.Steps, 4, method.ID)
		for _, step := range method.Guide.Steps {
			require.GreaterOrEqual(t, len(strings.Fields(step)), 15, method.ID)
		}
	}
	require.Equal(t, map[string][]string{
		linear.PersonalAPIKeyAuthMethodID: {"api_key", "webhook_signing_secret"},
		linear.OAuthAuthMethodID:          {"oauth_client_id", "oauth_client_secret", "webhook_signing_secret"},
	}, visibleFields)

	visibleConfiguration := []string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		visibleConfiguration = append(visibleConfiguration, field.Name)
		require.False(t, field.Required, field.Name)
		require.Contains(t, field.Description, "Blank uses the default", field.Name)
		require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default itself")
	}
	require.Equal(t, []string{"maxResponseBytes", "webhookMaxBodyBytes", "webhookSignatureTolerance"}, visibleConfiguration)
	defaults := linear.DefaultConfig()
	require.EqualValues(t, 4<<20, defaults.MaxResponseBytes)
	require.EqualValues(t, 1<<20, defaults.WebhookMaxBodyBytes)
	require.Equal(t, time.Minute, defaults.WebhookSignatureTolerance, "Linear recommends one minute")
}

func TestAuthorizationGuidesAndOAuthEndpoints(t *testing.T) {
	for _, method := range readLinearManifest(t).Spec.Auth.Methods {
		guide := strings.Join(method.Guide.Steps, " ")
		startURL, err := url.Parse(method.Guide.StartURL)
		require.NoError(t, err)
		require.Equal(t, "https", startURL.Scheme)
		require.Equal(t, "linear.app", startURL.Host)
		require.Contains(t, guide, "webhook_signing_secret")
		if method.OAuth2 == nil {
			require.Contains(t, guide, "Read and Write")
			require.Contains(t, guide, "lin_api_")
			continue
		}
		require.Equal(t, "https://linear.app/oauth/authorize", method.OAuth2.AuthorizationEndpoint)
		require.Equal(t, "https://api.linear.app/oauth/token", method.OAuth2.TokenEndpoint)
		require.Equal(t, []string{"write"}, method.OAuth2.Scopes, "one scope avoids Linear's comma-separated scope parameter; read is always granted")
		require.Equal(t, map[string]string{"prompt": "consent"}, method.OAuth2.AuthorizationParameters)
		require.True(t, method.OAuth2.PKCE)
		require.Contains(t, guide, "Redirect URI")
		require.Contains(t, guide, "write scope")
	}
}

func TestStudioTeamCommandReadsLinearOverGraphQLGet(t *testing.T) {
	studio := readLinearManifest(t).Spec.Studio
	require.Len(t, studio.Commands, 1)
	command := studio.Commands[0]
	require.Equal(t, "listTeams", command.ID)
	require.Equal(t, "GET", command.Request.Method)
	require.Equal(t, "https://api.linear.app/graphql", command.Request.URL)
	require.Equal(t, "access_token", command.Request.Credential.Field, "a personal API key travels without Bearer, which Studio commands cannot send")
	require.Equal(t, "bearer", command.Request.Credential.Scheme)
	require.Equal(t, map[string]string{"apollo-require-preflight": "true"}, command.Request.Headers, "Linear blocks a GraphQL GET without it as CSRF")
	require.True(t, strings.HasPrefix(command.Request.FixedQuery["query"], "query LinearTeamPicker {"))
	require.NotContains(t, command.Request.FixedQuery["query"], "mutation")
	require.Len(t, studio.Units, 1)
	require.Equal(t, linear.UIUnitTeamPicker, studio.Units[0].ID)
	require.Contains(t, studio.Units[0].Description, "manual entry")
	outputs := []string{}
	for _, output := range studio.Units[0].Outputs {
		outputs = append(outputs, output.Name)
	}
	require.Equal(t, []string{linear.UITeamPickerPortTeamID, linear.UITeamPickerPortTeamKey, linear.UITeamPickerPortTeamName}, outputs)
}

func TestCredentialsAndConnectionsNeverRenderSecrets(t *testing.T) {
	credentials := linear.Credentials{
		AuthMethodID: linear.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken), APIKey: sdkgo.NewSecretString(testAPIKey),
		RefreshToken: sdkgo.NewSecretString(testAccessToken), OAuthClientSecret: sdkgo.NewSecretString(testAccessToken),
		WebhookSigningSecret: sdkgo.NewSecretString(testSigningSecret),
	}
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		require.NotContains(t, rendered, testAccessToken)
		require.NotContains(t, rendered, testAPIKey)
		require.NotContains(t, rendered, testSigningSecret)
	}
	connection, err := linear.NewConnection(newAPIKeyClient(t, "http://127.0.0.1:1"), linearConnection)
	require.NoError(t, err)
	require.Equal(t, "linear.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err = connection.MarshalJSON()
	require.Error(t, err)
}

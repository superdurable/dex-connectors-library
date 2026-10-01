// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
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
	ID          string          `yaml:"id"`
	Type        string          `yaml:"type"`
	Recommended bool            `yaml:"recommended"`
	Fields      []manifestField `yaml:"fields"`
	Guide       struct {
		StartURL string   `yaml:"startURL"`
		Steps    []string `yaml:"steps"`
	} `yaml:"guide"`
	OAuth2 *struct {
		AuthorizationEndpoint string   `yaml:"authorizationEndpoint"`
		TokenEndpoint         string   `yaml:"tokenEndpoint"`
		Scopes                []string `yaml:"scopes"`
		PKCE                  bool     `yaml:"pkce"`
		CredentialMappings    []struct {
			Credential string `yaml:"credential"`
			Source     string `yaml:"source"`
		} `yaml:"credentialMappings"`
	} `yaml:"oauth2"`
}

type calendlyManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string               `yaml:"defaultMethod"`
			Methods       []manifestAuthMethod `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					URL        string `yaml:"url"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
					} `yaml:"credential"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func readCalendlyManifest(t *testing.T) calendlyManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest calendlyManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// requiredScopes are every scope an operation, the picker, or createWebhookSubscription needs.
var requiredScopes = []string{"users:read", "event_types:read", "scheduled_events:write", "scheduling_links:write", "webhooks:write"}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connectors shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readCalendlyManifest(t)
	require.Equal(t, calendly.PersonalAccessTokenAuthMethodID, manifest.Spec.Auth.DefaultMethod, "the most common setup is the default")
	require.Len(t, manifest.Spec.Auth.Methods, 2)
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
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
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
		calendly.PersonalAccessTokenAuthMethodID: {"access_token", "webhook_signing_key"},
		calendly.CalendlyOAuthAuthMethodID:       {"oauth_client_id", "oauth_client_secret", "webhook_signing_key"},
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
	defaults := calendly.DefaultConfig()
	require.EqualValues(t, 1<<20, defaults.MaxResponseBytes)
	require.EqualValues(t, 1<<20, defaults.WebhookMaxBodyBytes)
	require.Equal(t, 3*time.Minute, defaults.WebhookSignatureTolerance, "Calendly's documented three-minute tolerance")
}

func TestAuthorizationGuidesRequestTheScopesTheOperationsNeed(t *testing.T) {
	for _, method := range readCalendlyManifest(t).Spec.Auth.Methods {
		guide := strings.Join(method.Guide.Steps, " ")
		for _, scope := range requiredScopes {
			require.Contains(t, guide, scope, "%s guide grants %s", method.ID, scope)
		}
		startURL, err := url.Parse(method.Guide.StartURL)
		require.NoError(t, err)
		require.Equal(t, "https", startURL.Scheme)
		if method.OAuth2 == nil {
			require.Equal(t, "calendly.com", startURL.Host)
			continue
		}
		require.Equal(t, requiredScopes, method.OAuth2.Scopes)
		require.Equal(t, "https://auth.calendly.com/oauth/authorize", method.OAuth2.AuthorizationEndpoint)
		require.Equal(t, "https://auth.calendly.com/oauth/token", method.OAuth2.TokenEndpoint)
		require.True(t, method.OAuth2.PKCE, "Calendly asks every app to use PKCE with S256")
		require.Contains(t, guide, "Redirect URI")
	}
}

func TestStudioCommandsReadOnlyTheFixedCalendlyAPIHost(t *testing.T) {
	studio := readCalendlyManifest(t).Spec.Studio
	require.Len(t, studio.Commands, 2)
	for _, command := range studio.Commands {
		target, err := url.Parse(command.Request.URL)
		require.NoError(t, err)
		require.Equal(t, "api.calendly.com", target.Host, command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field, "both methods store their bearer token in access_token")
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
	}
	require.Len(t, studio.Units, 1)
	require.Contains(t, studio.Units[0].Description, "event type URI")
}

func TestCredentialsAndConnectionsNeverRenderSecrets(t *testing.T) {
	credentials := calendly.Credentials{
		AuthMethodID: calendly.CalendlyOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		RefreshToken: sdkgo.NewSecretString(sentinelToken), OAuthClientSecret: sdkgo.NewSecretString(sentinelToken),
		WebhookSigningKey: sdkgo.NewSecretString(sentinelSigningKey),
	}
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		require.NotContains(t, rendered, sentinelToken)
		require.NotContains(t, rendered, sentinelSigningKey)
	}
	connection, err := calendly.NewConnection(newTestClient(t, nil, personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{}), testConnection)
	require.NoError(t, err)
	require.Equal(t, "calendly.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err = connection.MarshalJSON()
	require.Error(t, err)
}

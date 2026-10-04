// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
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

type helpScoutManifest struct {
	Metadata struct {
		Company string `yaml:"company"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
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
			OAuth2 *struct{} `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					Method     string `yaml:"method"`
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
		Operations []struct {
			Name      string `yaml:"name"`
			Execution struct {
				Durability string `yaml:"durability"`
			} `yaml:"execution"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

func readHelpScoutManifest(t *testing.T) helpScoutManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest helpScoutManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connections shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readHelpScoutManifest(t)
	require.Equal(t, "Help Scout", manifest.Metadata.Company, "the company directory is helpscout")
	auth := manifest.Spec.Auth
	require.Equal(t, "apiKey", auth.Type)
	visibleFields := []string{}
	for _, field := range auth.Fields {
		visibleFields = append(visibleFields, field.Name)
		if field.Name == "access_token" {
			require.False(t, field.Required)
			require.Contains(t, field.Description, "never enter it manually", "the connector obtains the token")
			require.Contains(t, field.Description, "leave it blank")
			continue
		}
		require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "%s: a label is not guidance", field.Name)
		require.Contains(t, field.Description, "https://", "%s names where to start", field.Name)
		if field.Type == "secretString" {
			require.Contains(t, field.Description, "Secret", "%s says it is secret", field.Name)
		}
		require.True(t, field.Required || strings.Contains(field.Description, "Blank"), "%s says what blank means", field.Name)
	}
	require.Equal(t, []string{"app_id", "app_secret", "access_token", "webhook_secret"}, visibleFields)
	require.Len(t, auth.Guide.Steps, 4)
	for _, step := range auth.Guide.Steps {
		require.GreaterOrEqual(t, len(strings.Fields(step)), 15, step)
	}

	visibleConfiguration := []string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		visibleConfiguration = append(visibleConfiguration, field.Name)
		require.False(t, field.Required, field.Name)
		require.Contains(t, field.Description, "Blank uses the default", field.Name)
		require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default itself")
	}
	require.Equal(t, []string{"maxResponseBytes", "webhookMaxBodyBytes"}, visibleConfiguration)
	require.EqualValues(t, 4<<20, helpscout.DefaultConfig().MaxResponseBytes)
	require.EqualValues(t, 4<<20, helpscout.DefaultConfig().WebhookMaxBodyBytes)
}

func TestTheGuideSetsUpTheClientCredentialsFlowWithoutOAuthConsent(t *testing.T) {
	manifest := readHelpScoutManifest(t)
	auth := manifest.Spec.Auth
	require.Nil(t, auth.OAuth2, "no authorization-code method until Dex Web accepts scope-less providers")
	require.NotContains(t, manifest.Spec.Studio.Setup.BackendCapabilities, "oauth.connection.manage")
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, phrase := range []string{"My Apps", "Create My App", "Redirection URL", "placeholder", "App ID", "App Secret", "no scopes",
		"Leave access_token blank", "https://api.helpscout.net/v2/oauth2/token", "two-day"} {
		require.Contains(t, guide, phrase)
	}
	require.NotContains(t, guide, "Redirect URI", "Dex Web's OAuth redirect plays no part")
	startURL, err := url.Parse(auth.Guide.StartURL)
	require.NoError(t, err)
	require.Equal(t, "secure.helpscout.net", startURL.Host)
}

func TestStudioCommandReadsOnlyTheFixedHelpScoutAPIHost(t *testing.T) {
	studio := readHelpScoutManifest(t).Spec.Studio
	require.Len(t, studio.Commands, 1)
	command := studio.Commands[0]
	require.Equal(t, "listMailboxes", command.ID)
	require.Equal(t, "GET", command.Request.Method)
	require.Equal(t, "https://api.helpscout.net/v2/mailboxes", command.Request.URL)
	require.Equal(t, "access_token", command.Request.Credential.Field)
	require.Equal(t, "bearer", command.Request.Credential.Scheme)
	require.Len(t, studio.Units, 1)
	require.Equal(t, helpscout.UIUnitMailboxPicker, studio.Units[0].ID)
	require.Contains(t, studio.Units[0].Description, "inbox ID")
}

func TestOnlyTheReplyRunsWithSyncDurability(t *testing.T) {
	durabilities := map[string]string{}
	for _, operation := range readHelpScoutManifest(t).Spec.Operations {
		durabilities[operation.Name] = operation.Execution.Durability
	}
	require.Equal(t, map[string]string{
		"searchConversations": "async", "getConversation": "async", "replyToConversation": "sync",
		"updateConversation": "async", "findCustomerByEmail": "async",
	}, durabilities)
}

func TestCredentialsAndConnectionsNeverRenderSecrets(t *testing.T) {
	credentials := appCredentials(sentinelToken)
	credentials.AppSecret = sdkgo.NewSecretString(sentinelToken)
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		require.NotContains(t, rendered, sentinelToken)
		require.NotContains(t, rendered, sentinelWebhookSecret)
	}
	connection, err := helpscout.NewConnection(newTestClient(t, nil, staticCredentials(sentinelWebhookSecret), helpscout.Config{}), testConnection)
	require.NoError(t, err)
	require.Equal(t, "helpscout.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err = connection.MarshalJSON()
	require.Error(t, err)
}

func TestConversationStatusesAreHelpScoutsOwn(t *testing.T) {
	require.Equal(t, []helpscout.ConversationStatus{"active", "pending", "closed", "spam"}, helpscout.ConversationStatuses())
}

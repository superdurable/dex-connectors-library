// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connectors page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type    string          `yaml:"type"`
			Methods []any           `yaml:"methods"`
			Fields  []guidanceField `yaml:"fields"`
			Guide   struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 any `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio struct {
			Commands []any `yaml:"commands"`
			Units    []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
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
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"maxResponseBytes", "webhookMaxBodyBytes", "api_token", "webhook_secret"}, names,
		"a new visible field needs its own guidance review")
	providerPages := map[string]string{"api_token": "https://app.clickup.com/settings/apps", "webhook_secret": "https://developer.clickup.com/reference/createwebhook"}
	formats := map[string]string{"api_token": "starts with pk_", "webhook_secret": "opaque string"}
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
			}
			page, isProviderSourced := providerPages[field.Name]
			if !isProviderSourced {
				return
			}
			require.Contains(t, field.Description, page, "provider values name the ClickUp page to start from")
			require.Contains(t, field.Description, formats[field.Name])
			require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			require.Contains(t, field.Description, "stored only in this credential field")
		})
	}
}

func TestPersonalTokenGuideCoversTheTokenWebhookSecretAndRevocation(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods, "one personal-token method; see the README for why ClickUp OAuth is not declared")
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://app.clickup.com/settings/apps", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"Settings > Apps", "API Token", "Generate", "Regenerate", "pk_", "POST https://api.clickup.com/api/v2/team/{team_id}/webhook",
		"taskStatusUpdated", "webhook.secret", "webhook_secret", "DELETE https://api.clickup.com/api/v2/webhook/{webhook_id}",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestStudioDeclaresNoProviderCommands records why the bundle has no provider-backed pickers.
func TestStudioDeclaresNoProviderCommands(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	require.Empty(t, studio.Commands, "Studio commands cannot send ClickUp's raw Authorization header; see the README")
	require.Len(t, studio.Units, 1)
	require.Equal(t, "taskEventPicker", studio.Units[0].ID)
	require.Contains(t, studio.Units[0].Description, "selecting none accepts every task event")
	_, err := os.Stat("ui/package-lock.json")
	require.NoError(t, err, "a manifest that declares Studio units ships a ui bundle")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

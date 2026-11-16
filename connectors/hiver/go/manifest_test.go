// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

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
			Type        string          `yaml:"type"`
			Refreshable bool            `yaml:"refreshable"`
			Methods     []any           `yaml:"methods"`
			Fields      []guidanceField `yaml:"fields"`
			Guide       struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 any `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []struct {
				ID         string `yaml:"id"`
				Capability string `yaml:"capability"`
				Request    struct {
					Method     string              `yaml:"method"`
					URL        string              `yaml:"url"`
					Credential map[string]string   `yaml:"credential"`
					FixedQuery map[string]string   `yaml:"fixedQuery"`
					Parameters []map[string]string `yaml:"parameters"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
				Outputs     []struct {
					Name string `yaml:"name"`
					Type string `yaml:"type"`
				} `yaml:"outputs"`
			} `yaml:"units"`
		} `yaml:"studio"`
		Triggers []struct {
			Name              string `yaml:"name"`
			EventType         string `yaml:"eventType"`
			ConfigurationType string `yaml:"configurationType"`
			Description       string `yaml:"description"`
		} `yaml:"triggers"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

var hiverURLPattern = regexp.MustCompile(`https://(v2\.hiverhq\.com|developer\.hiverhq\.com|help\.hiverhq\.com|api2\.hiverhq\.com)/[^\s;,)]*`)

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"maxResponseBytes", "requestIntervalMilliseconds", "pollInterval", "pollPagesPerPoll", "api_key"}, names, "a new visible field needs its own guidance review")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
			}
			if field.Type != "secretString" {
				return
			}
			require.True(t, field.Required)
			require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			require.Regexp(t, hiverURLPattern, field.Description, "the key names the Hiver page to start from")
			for _, instruction := range []string{"Admin Panel > Integrations > Developer APIs", "bearer token", "opaque string", "without spaces"} {
				require.Contains(t, field.Description, instruction)
			}
		})
	}
	interval := visibleFields[1]
	require.Contains(t, interval.Description, "milliseconds")
	require.Contains(t, interval.Description, "one request per second")
	require.Regexp(t, hiverURLPattern, interval.Description, "the provider limit names Hiver's documentation")
	pollInterval, pollPages := visibleFields[2], visibleFields[3]
	for _, instruction := range []string{"Go duration", "at least 1s", "5000 requests per day", "pollPagesPerPoll"} {
		require.Contains(t, pollInterval.Description, instruction)
	}
	require.Regexp(t, hiverURLPattern, pollInterval.Description)
	for _, instruction := range []string{"Pages of 100 conversations", "from 2 to 5", "first page", "sweep", "daily request limit"} {
		require.Contains(t, pollPages.Description, instruction)
	}
}

func TestAPIKeyGuideCoversWhereTheKeyIsAndHowToRevokeIt(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.False(t, manifest.Spec.Auth.Refreshable, "Hiver API keys do not expire")
	require.Empty(t, manifest.Spec.Auth.Methods)
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://v2.hiverhq.com/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"Hiver admin", "Admin Panel > Integrations > Developer APIs", "Create an API key", "Create and generate API key",
		"toggle", "copy icon", "Pro and Elite", "one request per second", "5000 requests per day", "delete the app's API key",
		"https://help.hiverhq.com/hiver-api/hiver-api",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestManifestDeclaresTheInboxPickerAndConversationReceivedTrigger checks the read-only setup command and
// the Trigger the inbox picker configures.
func TestManifestDeclaresTheInboxPickerAndConversationReceivedTrigger(t *testing.T) {
	manifest := readGuidanceManifest(t)
	studio := manifest.Spec.Studio
	require.Equal(t, []string{"use.configuration.write", "hiver.inboxes-list"}, studio.Setup.BackendCapabilities)
	require.Len(t, studio.Commands, 1)
	command := studio.Commands[0]
	require.Equal(t, "listInboxes", command.ID)
	require.Equal(t, "hiver.inboxes-list", command.Capability)
	require.Equal(t, "GET", command.Request.Method, "setup commands only read")
	require.Equal(t, "https://api2.hiverhq.com/v1/inboxes", command.Request.URL)
	require.Equal(t, map[string]string{"field": "api_key", "scheme": "bearer"}, command.Request.Credential)
	require.Equal(t, map[string]string{"limit": "100"}, command.Request.FixedQuery)
	require.Equal(t, []map[string]string{{"name": "next_page", "location": "query", "target": "next_page"}}, command.Request.Parameters)
	require.Len(t, studio.Units, 1)
	require.Equal(t, "inboxPicker", studio.Units[0].ID)
	require.Contains(t, studio.Units[0].Description, "https://api2.hiverhq.com/v1/inboxes")
	require.Len(t, studio.Units[0].Outputs, 1)
	require.Equal(t, "inboxId", studio.Units[0].Outputs[0].Name)
	require.Equal(t, "string", studio.Units[0].Outputs[0].Type, "Hiver IDs are kept as strings")

	require.Len(t, manifest.Spec.Triggers, 1)
	trigger := manifest.Spec.Triggers[0]
	require.Equal(t, "conversationReceived", trigger.Name)
	require.Equal(t, "ConversationReceivedEvent", trigger.EventType)
	require.Equal(t, "ConversationReceivedTriggerConfiguration", trigger.ConfigurationType)
	require.Contains(t, trigger.Description, "never delivered")
	_, err := os.Stat("../ui/src/units.tsx")
	require.NoError(t, err, "the picker ships in the connector UI bundle")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

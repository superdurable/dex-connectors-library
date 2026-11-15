// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

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
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []any `yaml:"commands"`
			Units    []struct {
				ID                  string   `yaml:"id"`
				Description         string   `yaml:"description"`
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"units"`
		} `yaml:"studio"`
		Triggers []struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
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

var reamazeURLPattern = regexp.MustCompile(`https://(www\.reamaze\.com|[a-z{}]+\.reamaze\.io)/[^\s;,)]*|https://acme\.reamaze\.io`)

// providerSourcedFields are the values a user copies from Re:amaze, with the format each must name.
var providerSourcedFields = map[string]string{
	"brand":     "lowercase letters, digits, and hyphens",
	"email":     "one bare address",
	"api_token": "opaque string of letters and digits",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"brand", "maxResponseBytes", "pollInterval", "email", "api_token"}, names, "a new visible field needs its own guidance review")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
			}
			format, isProviderSourced := providerSourcedFields[field.Name]
			if !isProviderSourced {
				return
			}
			require.True(t, field.Required)
			require.Regexp(t, reamazeURLPattern, field.Description, "provider values name the Re:amaze page to start from")
			require.Contains(t, field.Description, format)
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "Generate New Token")
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
		})
	}
}

func TestAPITokenGuideCoversWhereTheTokenIsAndHowToRevokeIt(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods)
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://www.reamaze.com/login", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"staff user", "Settings", "API Token under Developer", "Generate New Token", "HTTP Basic", "{email}:{api_token}",
		"https://acme.reamaze.io", "Settings > Brands", "revoke",
	} {
		require.Contains(t, guide, instruction)
	}
}

// Studio commands support neither per-brand hosts nor HTTP Basic, so the channel is a guided entry, not a picker.
func TestManifestDeclaresTheChannelEntryWithoutProviderCommands(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Empty(t, manifest.Spec.Studio.Commands)
	require.Equal(t, []string{"use.configuration.write"}, manifest.Spec.Studio.Setup.BackendCapabilities)
	require.Len(t, manifest.Spec.Studio.Units, 1)
	unit := manifest.Spec.Studio.Units[0]
	require.Equal(t, "channelSlugEntry", unit.ID)
	require.Empty(t, unit.BackendCapabilities)
	require.Regexp(t, reamazeURLPattern, unit.Description)
	for _, guidance := range []string{"slug", "such as support", "Settings > Channels", "blank", "every channel"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Len(t, manifest.Spec.Triggers, 1)
	require.Equal(t, "conversationCreated", manifest.Spec.Triggers[0].Name)
	require.Contains(t, manifest.Spec.Triggers[0].Description, "baseline")
	_, err := os.Stat("../ui/package-lock.json")
	require.NoError(t, err)
}

func TestPollIntervalGuidanceNamesTheSharedRateLimit(t *testing.T) {
	for _, field := range readGuidanceManifest(t).Spec.Configuration.Fields {
		if field.Name != "pollInterval" {
			continue
		}
		require.Equal(t, "duration", field.Type)
		for _, guidance := range []string{"Non-secret", "at least 10s", "per-minute", "operation Steps"} {
			require.Contains(t, field.Description, guidance)
		}
		return
	}
	t.Fatal("pollInterval is not declared")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

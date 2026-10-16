// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

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
			Type   string          `yaml:"type"`
			Fields []guidanceField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
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
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "bot_token"}, names, "a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			}
		})
	}
	token := visibleFields[2]
	require.True(t, token.Required)
	for _, text := range []string{"https://discord.com/developers/applications", "Bot > Reset Token", "without the Bot prefix"} {
		require.Contains(t, token.Description, text)
	}
	require.Nil(t, manifest.Spec.Studio, "the connector ships no Studio bundle; channel IDs come from Flow input")
}

func TestTheAuthorizationGuideNamesThePermissionsAndChannelIDs(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "apiKey", auth.Type, "a bot token is a static credential; Discord bot tokens do not expire")
	require.Equal(t, "https://discord.com/developers/applications", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"New Application", "Reset Token", "shows once", "Message Content Intent", "OAuth2 > URL Generator", "bot scope",
		"View Channels", "Read Message History", "Send Messages", "Add Reactions", "Developer Mode", "Copy Channel ID",
	} {
		require.Contains(t, guide, text)
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

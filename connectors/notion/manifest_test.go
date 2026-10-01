// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

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
		Studio *struct{} `yaml:"studio"`
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "api_token"}, names, "a new visible field needs its own guidance review")
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
	for _, text := range []string{"https://app.notion.com/developers/connections", "Internal connections", "Configuration", "API token", "ntn_"} {
		require.Contains(t, token.Description, text)
	}
}

func TestTheAuthorizationGuideNamesThePortalCapabilitiesAndSharing(t *testing.T) {
	manifest := readGuidanceManifest(t)
	auth := manifest.Spec.Auth
	require.Equal(t, "apiKey", auth.Type, "Dex Web cannot complete Notion's scope-less OAuth exchange")
	require.Equal(t, "https://app.notion.com/developers/connections", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"Workspace Owner", "Internal connections", "Create a new connection", "Read content", "Update content", "Insert content",
		"Configuration tab", "Refresh", "Content access", "••• menu > Connections > + Add connection", "not found",
	} {
		require.Contains(t, guide, text)
	}
	require.Nil(t, manifest.Spec.Studio, "Notion offers no GET listing for a picker, so the connector ships no Studio UI")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

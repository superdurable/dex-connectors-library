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
		Studio *struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					Method       string            `yaml:"method"`
					URL          string            `yaml:"url"`
					FixedHeaders map[string]string `yaml:"fixedHeaders"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "pollInterval", "pollPageSize", "api_token"}, names, "a new visible field needs its own guidance review")
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
	for _, field := range visibleFields[2:4] {
		require.Contains(t, field.Description, "databaseRowChanged", "poll settings name the Trigger they tune")
	}
	require.Contains(t, visibleFields[2].Description, "at least 5s")
	require.Contains(t, visibleFields[3].Description, "maximum query page size")
	token := visibleFields[4]
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
}

// Notion lists data sources only through POST search, so the picker resolves a pasted database link with GET.
func TestTheDataSourcePickerResolvesThroughReadOnlyNotionCommands(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	require.NotNil(t, studio)
	urls := map[string]string{}
	for _, command := range studio.Commands {
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, map[string]string{"Notion-Version": "2026-03-11"}, command.Request.FixedHeaders)
		urls[command.ID] = command.Request.URL
	}
	require.Equal(t, map[string]string{
		"getDatabase":   "https://api.notion.com/v1/databases/{databaseId}",
		"getDataSource": "https://api.notion.com/v1/data_sources/{dataSourceId}",
	}, urls)
	require.Len(t, studio.Units, 1)
	require.Equal(t, "dataSourcePicker", studio.Units[0].ID)
	for _, text := range []string{"https://app.notion.com", "Copy link", "Manage data sources", "Copy data source ID", "blank"} {
		require.Contains(t, studio.Units[0].Description, text)
	}
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

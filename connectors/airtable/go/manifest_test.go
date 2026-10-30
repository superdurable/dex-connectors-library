// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

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
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []struct {
				ID         string `yaml:"id"`
				Capability string `yaml:"capability"`
				Request    struct {
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "personal_access_token"}, names, "a new visible field needs its own guidance review")
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
	for _, text := range []string{"https://airtable.com/create/tokens", "Create new token", "starts with pat"} {
		require.Contains(t, token.Description, text)
	}
}

func TestTheAuthorizationGuideNamesTheScopesAndBaseAccess(t *testing.T) {
	manifest := readGuidanceManifest(t)
	auth := manifest.Spec.Auth
	require.Equal(t, "apiKey", auth.Type, "Dex Web's code exchange cannot send the HTTP Basic client credentials Airtable OAuth requires")
	require.Equal(t, "https://airtable.com/create/tokens", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"Create new token", "data.records:read", "data.records:write", "schema.bases:read", "Add a base",
		"Create token", "shows once", "Regenerating", "editor access",
	} {
		require.Contains(t, guide, text)
	}
}

func TestStudioCommandsAreBearerReadsOfAirtablesMetadataAPI(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	require.Len(t, studio.Commands, 2)
	expected := map[string]string{
		"listBases":  "https://api.airtable.com/v0/meta/bases",
		"listTables": "https://api.airtable.com/v0/meta/bases/{baseId}/tables",
	}
	for _, command := range studio.Commands {
		require.Equal(t, expected[command.ID], command.Request.URL, command.ID)
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, "personal_access_token", command.Request.Credential.Field)
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
		require.Contains(t, studio.Setup.BackendCapabilities, command.Capability)
	}
	require.NotContains(t, studio.Setup.BackendCapabilities, "oauth.connection.manage", "the connection has no OAuth flow to manage")
	for _, unit := range studio.Units {
		require.GreaterOrEqual(t, len(unit.Description), 100, unit.ID)
		require.Contains(t, unit.Description, "stores", "a picker says what it saves")
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

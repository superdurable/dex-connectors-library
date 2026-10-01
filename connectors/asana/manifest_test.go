// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

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
			Type    string          `yaml:"type"`
			Methods []any           `yaml:"methods"`
			Fields  []guidanceField `yaml:"fields"`
			Guide   struct {
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
					Method     string            `yaml:"method"`
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
					} `yaml:"credential"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID                  string   `yaml:"id"`
				Description         string   `yaml:"description"`
				BackendCapabilities []string `yaml:"backendCapabilities"`
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "access_token"}, names, "a new visible field needs its own guidance review")
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
	token := manifest.Spec.Auth.Fields[0]
	require.True(t, token.Required)
	require.Contains(t, token.Description, "https://app.asana.com/0/my-apps")
	require.Contains(t, token.Description, "Personal access tokens > Create new token")
	require.Contains(t, token.Description, "opaque", "Asana documents no token prefix, so none is promised")
}

func TestPersonalAccessTokenIsTheOnlyMethodAndItsGuideNamesTheConsolePath(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "apiKey", auth.Type)
	require.Empty(t, auth.Methods, "Dex Web cli-v1.1.0 cannot save an API-key method of a multi-method manifest")
	require.Equal(t, "https://app.asana.com/0/my-apps", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{"Personal access tokens", "Create new token", "API terms", "shows once", "does not expire", "delete the token", "every workspace"} {
		require.Contains(t, guide, text)
	}
}

func TestStudioCommandsAreReadOnlyAndPinnedToTheAsanaAPI(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	require.Len(t, studio.Commands, 3)
	capabilities := map[string]bool{}
	for _, capability := range studio.Setup.BackendCapabilities {
		capabilities[capability] = true
	}
	require.False(t, capabilities["oauth.connection.manage"], "the connection has no OAuth method")
	for _, command := range studio.Commands {
		require.Equal(t, "GET", command.Request.Method, command.ID)
		require.True(t, strings.HasPrefix(command.Request.URL, "https://app.asana.com/api/1.0/"), command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field, command.ID)
		require.Equal(t, "bearer", command.Request.Credential.Scheme, command.ID)
		require.Equal(t, "100", command.Request.FixedQuery["limit"], command.ID)
		require.Equal(t, "name", command.Request.FixedQuery["opt_fields"], "a picker reads only gid and name")
		require.True(t, capabilities[command.Capability], command.ID)
	}
	require.Len(t, studio.Units, 2)
	for _, unit := range studio.Units {
		require.GreaterOrEqual(t, len(unit.Description), 120, unit.ID)
		require.Contains(t, unit.Description, "manual entry", unit.ID)
		for _, capability := range unit.BackendCapabilities {
			require.True(t, capabilities[capability], unit.ID)
		}
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

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

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
		Studio map[string]any `yaml:"studio"`
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "api_key", "token"}, names, "a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.True(t, field.Required)
			}
		})
	}
	apiKey, token := manifest.Spec.Auth.Fields[0], manifest.Spec.Auth.Fields[1]
	require.Contains(t, apiKey.Description, "https://trello.com/apps/admin")
	require.Contains(t, apiKey.Description, "Trello Auth tab")
	require.Contains(t, apiKey.Description, "Generate a new API Key")
	require.Contains(t, apiKey.Description, "documents no key format", "Trello documents no key format, so none is promised")
	require.Contains(t, token.Description, "https://trello.com/1/authorize?expiration=never&scope=read,write&response_type=token&key=")
	require.Contains(t, token.Description, "Allow")
	require.Contains(t, token.Description, "https://trello.com/u/your-username/account")
}

func TestKeyAndTokenIsTheOnlyMethodAndItsGuideNamesThePortalPath(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "apiKey", auth.Type)
	require.Empty(t, auth.Methods, "Trello's OAuth 1.0 three-legged flow does not fit the platform's OAuth 2.0 authorization")
	require.Equal(t, "https://trello.com/apps/admin", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"New", "Joint Development Agreement", "Trello Auth tab", "Generate a new API Key", "scope=read,write",
		"expiration=never", "expiration=30days", "Allow", "Applications", "Revoke", "HTTP 401", "every board the member can open",
	} {
		require.Contains(t, guide, text)
	}
}

// TestNoStudioPickerIsDeclared records why board and list IDs are guided input: Trello answers both list
// endpoints with a top-level JSON array, which Dex Web rejects, and a Studio command cannot send Trello's
// key and token.
func TestNoStudioPickerIsDeclared(t *testing.T) {
	require.Empty(t, readGuidanceManifest(t).Spec.Studio)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err), "a connector without Studio units ships no UI bundle")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

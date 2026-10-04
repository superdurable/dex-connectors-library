// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

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
		Studio   any   `yaml:"studio"`
		Triggers []any `yaml:"triggers"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

var gorgiasURLPattern = regexp.MustCompile(`https://(docs\.gorgias\.com|\{domain\}\.gorgias\.com)/[^\s;,)]*`)

// providerSourcedFields are the values a user copies from Gorgias, with the format each must name.
var providerSourcedFields = map[string]string{
	"domain":  "lowercase letters, digits, and hyphens",
	"email":   "agent@example.com",
	"api_key": "opaque string of letters and digits",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"domain", "maxResponseBytes", "email", "api_key"}, names, "a new visible field needs its own guidance review")

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
			require.Regexp(t, gorgiasURLPattern, field.Description, "provider values name the Gorgias page to start from")
			require.Contains(t, field.Description, format)
			require.Contains(t, field.Description, "REST API")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "Create API Key")
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
		})
	}
}

func TestAPIKeyGuideCoversWhereTheKeyIsAndHowToRevokeIt(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.False(t, manifest.Spec.Auth.Refreshable, "a static API key never refreshes")
	require.Empty(t, manifest.Spec.Auth.Methods, "OAuth2 needs per-account endpoints a manifest cannot declare")
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://www.gorgias.com/login", manifest.Spec.Auth.Guide.StartURL, "setup starts where the user signs in")
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"login page", "https://{domain}.gorgias.com", "account owner or an admin", "Settings", "Account > REST API", "Base API URL",
		"Username", "Create API Key", "shown once", "reset the API key", "role", "OAuth2",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestManifestDeclaresNoStudioBundleOrTrigger records that Studio cannot reach a per-account host with HTTP Basic, and Gorgias webhooks are unsigned.
func TestManifestDeclaresNoStudioBundleOrTrigger(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

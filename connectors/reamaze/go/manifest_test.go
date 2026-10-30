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
	require.Equal(t, []string{"brand", "maxResponseBytes", "email", "api_token"}, names, "a new visible field needs its own guidance review")

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

// Studio commands support neither per-brand hosts nor HTTP Basic, and Re:amaze documents no event webhook.
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

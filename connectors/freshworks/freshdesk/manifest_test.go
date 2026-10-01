// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

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

var freshdeskURLPattern = regexp.MustCompile(`https://(login\.freshworks\.com|support\.freshdesk\.com|\{domain\}\.freshdesk\.com)/[^\s;,)]*`)

// providerSourcedFields are the values a user copies from Freshdesk, with the format each must name.
var providerSourcedFields = map[string]string{
	"domain":  "lowercase letters, digits, and hyphens",
	"api_key": "opaque string of letters and digits",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"domain", "maxResponseBytes", "api_key"}, names, "a new visible field needs its own guidance review")

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
			require.Regexp(t, freshdeskURLPattern, field.Description, "provider values name the Freshdesk page to start from")
			require.Contains(t, field.Description, format)
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "Profile Settings")
				require.Contains(t, field.Description, "X as the password")
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
		})
	}
}

func TestAPIKeyGuideCoversWhereTheKeyIsAndHowToRevokeIt(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods)
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://login.freshworks.com/email-login/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"https://{domain}.freshdesk.com", "profile picture", "Profile Settings", "View API key", "captcha",
		"verified agent", "Sprout and Free", "custom support domain", "reset the API key", "agent's role",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestManifestDeclaresNoStudioBundle records that Studio commands can reach neither a per-helpdesk
// Freshdesk host nor send the HTTP Basic credential an API key needs, so there is no picker.
func TestManifestDeclaresNoStudioBundle(t *testing.T) {
	require.Nil(t, readGuidanceManifest(t).Spec.Studio)
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

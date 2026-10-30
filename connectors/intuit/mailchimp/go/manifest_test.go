// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

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
	Metadata struct {
		Company string `yaml:"company"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
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

var mailchimpURLPattern = regexp.MustCompile(`https://(login\.mailchimp\.com|us1\.admin\.mailchimp\.com)/[^\s;,)]*`)

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"maxResponseBytes", "api_key"}, names, "a new visible field needs its own guidance review; the data center is derived, never typed")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
				return
			}
			require.Equal(t, "secretString", field.Type)
			require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			require.Regexp(t, mailchimpURLPattern, field.Description, "provider values name the Mailchimp page to start from")
			for _, instruction := range []string{"Profile", "Extras", "API keys", "Create A Key", "Generate Key", "-us6", "us6.api.mailchimp.com", "Bearer", "June 22, 2026"} {
				require.Contains(t, field.Description, instruction)
			}
		})
	}
}

func TestAPIKeyGuideCoversWhereTheKeyIsItsExpiryAndHowToRevokeIt(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "Intuit", manifest.Metadata.Company, "Mailchimp is an Intuit company, so it lives under connectors/intuit")
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods)
	require.Nil(t, manifest.Spec.Auth.OAuth2, "OAuth is deferred; see the README")
	require.Equal(t, "https://us1.admin.mailchimp.com/account/api/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"role", "403", "admin", "Profile", "Extras", "API keys", "Create A Key", "Generate Key", "Copy Key to Clipboard",
		"-us6", "only once", "June 22, 2026", "one year", "Revoke", "REVOKE", "cannot be reactivated",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestManifestDeclaresNoStudioBundleAndNoTriggers records that Studio cannot reach per-data-center hosts.
func TestManifestDeclaresNoStudioBundleAndNoTriggers(t *testing.T) {
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

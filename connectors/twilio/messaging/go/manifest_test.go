// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

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
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID            string          `yaml:"id"`
				Description   string          `yaml:"description"`
				Recommended   bool            `yaml:"recommended"`
				Fields        []guidanceField `yaml:"fields"`
				Configuration struct {
					Fields []guidanceField `yaml:"fields"`
				} `yaml:"configuration"`
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"methods"`
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

var consoleURLPattern = regexp.MustCompile(`https://console\.twilio\.com[^\s;,)]*`)

// providerSourcedFields are the values a user copies from the Twilio Console, with the format each must name.
var providerSourcedFields = map[string]string{
	"accountSid":     "AC followed by 32 hexadecimal characters",
	"defaultSender":  "E.164",
	"auth_token":     "32 hexadecimal characters",
	"api_key_sid":    "SK followed by 32 hexadecimal characters",
	"api_key_secret": "32 characters",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
		visibleFields = append(visibleFields, method.Configuration.Fields...)
	}
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"accountSid", "defaultSender", "endpoint", "maxResponseBytes", "auth_token", "api_key_sid", "api_key_secret"}, names,
		"a new visible field needs its own guidance review")

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
			require.Regexp(t, consoleURLPattern, field.Description, "provider values name the Console page to start from")
			require.Contains(t, field.Description, format)
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			} else {
				require.Regexp(t, `(?i)non-secret|not secret`, field.Description)
			}
		})
	}
}

func TestAuthenticationMethodsExplainSetupAndRevocation(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "auth-token", manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	for _, method := range manifest.Spec.Auth.Methods {
		require.Regexp(t, `^https://console\.twilio\.com/`, method.Guide.StartURL, method.ID)
		require.GreaterOrEqual(t, len(method.Guide.Steps), 3, method.ID)
		require.NotEmpty(t, method.Description, method.ID)
		require.Empty(t, method.Configuration.Fields, "%s: Dex Web cli-v1.1.0 omits method configuration fields from the setup form", method.ID)
		guide := strings.Join(method.Guide.Steps, " ")
		require.Contains(t, guide, "accountSid", "%s: both methods need the Account SID", method.ID)
		require.Regexp(t, `(?i)rotate|revoke`, guide, method.ID)
	}
	require.Equal(t, "api-key", manifest.Spec.Auth.Methods[1].ID)
	require.True(t, manifest.Spec.Auth.Methods[1].Recommended)
}

// TestManifestDeclaresNoStudioBundle records that Studio commands cannot send the HTTP Basic credential Twilio requires.
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

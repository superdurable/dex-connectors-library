// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

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
		Studio   any `yaml:"studio"`
		Triggers any `yaml:"triggers"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

var providerSettingsURLPattern = regexp.MustCompile(`https://(www\.fastmail\.help|support\.apple\.com|help\.yahoo\.com|app\.fastmail\.com|account\.apple\.com|login\.yahoo\.com)(/[^\s;,)]*)?`)

// userSuppliedFields are the values a user copies from a provider page, with the format each must name.
var userSuppliedFields = map[string]string{
	"imapHost": "imap.example.com", "smtpHost": "smtp.example.com", "imapPort": "1 through 65535", "smtpPort": "1 through 65535",
	"fromAddress": "support@example.com", "fromName": "Acme Support", "username": "support@example.com",
	"password": "app password", "smtp_username": "complete address", "smtp_password": "relay",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"imapHost", "imapPort", "imapSecurity", "smtpHost", "smtpPort", "smtpSecurity", "fromAddress", "fromName",
		"username", "password", "smtp_username", "smtp_password"}, names, "a new visible field needs its own guidance review")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil && field.Type != "enum" {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if field.Required {
				require.Contains(t, field.Description, "Blank is invalid")
			} else {
				require.Contains(t, field.Description, "Blank", "optional fields explain what blank means")
			}
			format, isUserSupplied := userSuppliedFields[field.Name]
			if !isUserSupplied {
				require.Equal(t, "enum", field.Type)
				require.Contains(t, field.Description, "never")
				return
			}
			require.Contains(t, field.Description, format)
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "Dex keeps it out of Flow state")
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
			if field.Name == "imapHost" || field.Name == "password" {
				require.Len(t, providerSettingsURLPattern.FindAllString(field.Description, -1), 3, "name each verified provider's page")
			}
		})
	}
}

func TestPasswordGuideCoversAppPasswordsRevocationAndUnsupportedProviders(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods)
	require.Nil(t, manifest.Spec.Auth.OAuth2, "XOAUTH2 is not offered in this release")
	require.Equal(t, "https://www.fastmail.help/hc/en-us/articles/1500000278342", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"imap.fastmail.com 993", "smtp.mail.me.com 587 STARTTLS", "imap.mail.yahoo.com 993",
		"Manage app passwords and access", "App-Specific Passwords", "two-factor authentication", "Create app password",
		"smtp_username", "delete the app password", "Gmail connector", "Microsoft 365",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestManifestDeclaresNoStudioBundleOrTrigger records that Studio commands are HTTPS-only, so no IMAP
// mailbox picker can exist, and that the SDK has no durable poll cursor for a messageReceived Trigger.
func TestManifestDeclaresNoStudioBundleOrTrigger(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Nil(t, manifest.Spec.Triggers)
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

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Description string   `yaml:"description"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

type webhookManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Fields []manifestField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

func readWebhookManifest(t *testing.T) webhookManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest webhookManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connectors shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readWebhookManifest(t)
	require.Nil(t, manifest.Spec.Studio, "no Studio bundle: Dex Web renders the host-owned form")
	visibleFields := []string{}
	for _, field := range append(append([]manifestField(nil), manifest.Spec.Auth.Fields...), manifest.Spec.Configuration.Fields...) {
		visibleFields = append(visibleFields, field.Name)
	}
	require.Equal(t, []string{
		"signing_secret", "verification", "signatureHeader", "signaturePrefix", "signatureEncoding", "timestampToleranceSeconds",
		"tokenHeader", "eventIdHeader", "eventIdPointer", "maxBodyBytes", "forwardedHeaders", "deliveryUrl",
	}, visibleFields)

	secret := manifest.Spec.Auth.Fields[0]
	require.Equal(t, "secretString", secret.Type)
	require.True(t, secret.Required)
	for _, scheme := range []string{"hmacSha256", "standardWebhooks", "sharedToken", "whsec_", "sendEvent"} {
		require.Contains(t, secret.Description, scheme, "the secret explains each scheme's value")
	}
	for _, field := range manifest.Spec.Configuration.Fields {
		t.Run(field.Name, func(t *testing.T) {
			require.False(t, field.Required, "every configuration field has a default or a blank meaning")
			require.Contains(t, field.Description, "Blank", "the description states what blank means")
			require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "a label is not guidance")
			if field.Default != nil && len(field.Enum) == 0 {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default itself")
			}
			for _, value := range field.Enum {
				require.Contains(t, field.Description, value, "every choice is explained")
			}
		})
	}

	defaults := webhook.DefaultConfig()
	require.Equal(t, webhook.VerificationSchemeHmacSha256, defaults.VerificationScheme)
	require.Equal(t, "X-Signature-256", defaults.SignatureHeader)
	require.Equal(t, webhook.SignatureEncodingHex, defaults.SignatureEncoding)
	require.EqualValues(t, 300, defaults.TimestampToleranceSeconds)
	require.Equal(t, "X-Webhook-Token", defaults.TokenHeader)
	require.EqualValues(t, 1<<20, defaults.MaxBodyBytes)
	require.Empty(t, defaults.DeliveryURL, "a receive-only connection needs no delivery URL")
}

// TestAuthorizationGuideStartsAtTheSenderSetupSection keeps the guide link and README anchor together.
func TestAuthorizationGuideStartsAtTheSenderSetupSection(t *testing.T) {
	guide := readWebhookManifest(t).Spec.Auth.Guide
	startURL, err := url.Parse(guide.StartURL)
	require.NoError(t, err)
	require.Equal(t, "https", startURL.Scheme)
	require.Equal(t, "sender-setup", startURL.Fragment)
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	require.Contains(t, string(readme), "\n## Sender setup\n")
	require.Len(t, guide.Steps, 4)
	for _, step := range guide.Steps {
		require.GreaterOrEqual(t, len(strings.Fields(step)), 15)
	}
}

func TestCredentialsAndConnectionsNeverRenderTheSecret(t *testing.T) {
	credentials := webhook.Credentials{SigningSecret: sdkgo.NewSecretString(sentinelSecret)}
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		require.NotContains(t, rendered, sentinelSecret)
	}
	connection := newTestConnection(t, webhook.Config{}, sentinelSecret)
	require.Equal(t, "webhook.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err := connection.MarshalJSON()
	require.Error(t, err)
}

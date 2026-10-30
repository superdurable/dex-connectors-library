// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

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
		Studio     map[string]any `yaml:"studio"`
		Triggers   []any          `yaml:"triggers"`
		Operations []struct {
			Name string `yaml:"name"`
		} `yaml:"operations"`
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
	require.Equal(t, []string{
		"region", "endpoint", "defaultQueueUrl", "maxMessagesPerReceive", "receiveWaitSeconds", "maxResponseBytes",
		"access_key_id", "secret_access_key", "session_token",
	}, names, "a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 150, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, " "+fmt.Sprint(field.Default)+" ", "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			}
		})
	}
	fields := map[string]guidanceField{}
	for _, field := range visibleFields {
		fields[field.Name] = field
	}
	require.Contains(t, fields["region"].Description, "https://console.aws.amazon.com/sqs/v3/home")
	require.Contains(t, fields["endpoint"].Description, "ElasticMQ")
	require.Contains(t, fields["defaultQueueUrl"].Description, "Details panel")
	require.Contains(t, fields["receiveWaitSeconds"].Description, "waitTimeSeconds to 0", "zero is only reachable from the operation input")
	require.Contains(t, fields["access_key_id"].Description, "Security credentials > Access keys > Create access key")
	require.Contains(t, fields["secret_access_key"].Description, "Retrieve access keys")
	require.Contains(t, fields["session_token"].Description, "ASIA")
	require.False(t, fields["session_token"].Required, "long-term keys need no session token")
	require.True(t, fields["access_key_id"].Required)
	require.True(t, fields["secret_access_key"].Required)
}

func TestAccessKeyIsTheOnlyMethodAndItsGuideCoversPermissionsAndQueueSetup(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "apiKey", auth.Type)
	require.Empty(t, auth.Methods, "IAM roles, instance profiles, and SSO are outside this release")
	require.Equal(t, "https://console.aws.amazon.com/iam", auth.Guide.StartURL)
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"Create user", "sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility", "kms:Decrypt",
		"Create access key", "Retrieve access keys", "Details panel", "Visibility timeout", "Dead-letter queue",
		"ElasticMQ", "Deactivate", "IAM roles",
	} {
		require.Contains(t, guide, text)
	}
}

// TestNoStudioPickerOrTriggerIsDeclared records two release decisions the README explains.
func TestNoStudioPickerOrTriggerIsDeclared(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Empty(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	var operations []string
	for _, operation := range manifest.Spec.Operations {
		operations = append(operations, operation.Name)
	}
	require.Equal(t, []string{"receiveMessages", "deleteMessage", "changeMessageVisibility"}, operations)
	_, err := os.Stat("../ui")
	require.True(t, os.IsNotExist(err), "a connector without Studio units ships no UI bundle")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

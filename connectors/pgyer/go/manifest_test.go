// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pgyer_test

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

type visibleField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}
type connectionManifest struct {
	Spec struct {
		Configuration struct {
			Fields []visibleField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Fields []visibleField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Operations []struct {
			Name            string `yaml:"name"`
			ConfigurationUI any    `yaml:"configurationUI"`
		} `yaml:"operations"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

func TestConnectionGuidanceCoversEveryVisibleField(t *testing.T) {
	body, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest connectionManifest
	require.NoError(t, yaml.Unmarshal(body, &manifest))
	fields := append(manifest.Spec.Configuration.Fields, manifest.Spec.Auth.Fields...)
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", field.Name)
		require.NotEmpty(t, field.Description)
	}
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "api_key"}, names)
	require.Equal(t, "secretString", fields[3].Type)
	require.Contains(t, fields[3].Description, "https://www.pgyer.com/account/api")
	require.Equal(t, "https://www.pgyer.com", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, text := range []string{"Account Center", "API Info", "real-name verification", "1098"} {
		require.Contains(t, guide, text)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "listUploadedApps", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

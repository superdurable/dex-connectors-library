// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zentao_test

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
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "token"}, names)
	require.Equal(t, "secretString", fields[3].Type)
	require.Contains(t, fields[3].Description, "https://www.zentao.net/book/api/1397.html")
	require.Equal(t, "https://www.zentao.net/book/api/1397.html", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, text := range []string{"16.5", "account and password", "api.php/v1/tokens"} {
		require.Contains(t, guide, text)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "listProducts", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

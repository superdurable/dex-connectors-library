// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConnectionGuidanceCoversEveryVisibleField(t *testing.T) {
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Configuration struct {
				Fields []struct{ Name, Description string } `yaml:"fields"`
			} `yaml:"configuration"`
			Auth struct {
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				Fields []struct{ Name, Type, Description string } `yaml:"fields"`
			} `yaml:"auth"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, "https://system.netsuite.com/", manifest.Spec.Auth.Guide.StartURL)
	require.Len(t, manifest.Spec.Auth.Guide.Steps, 4)
	configurationFields := map[string]string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		configurationFields[field.Name] = field.Description
		require.Contains(t, field.Description, "Blank", field.Name)
	}
	require.ElementsMatch(t, []string{"baseUrl", "authorizationEndpoint", "tokenEndpoint", "timeout", "maxResponseBytes"}, guidanceFieldNames(configurationFields))
	for _, name := range []string{"baseUrl", "authorizationEndpoint", "tokenEndpoint"} {
		require.Contains(t, configurationFields[name], "https://", name)
	}
	authorizationFields := map[string]string{}
	for _, field := range manifest.Spec.Auth.Fields {
		authorizationFields[field.Name] = field.Description
		require.True(t, strings.Contains(field.Description, "Blank") || strings.Contains(field.Description, "blank"), field.Name)
		if field.Type == "secretString" {
			require.Contains(t, field.Description, "Secret", field.Name)
		}
	}
	require.ElementsMatch(t, []string{"oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, guidanceFieldNames(authorizationFields))
	require.Contains(t, strings.Join(manifest.Spec.Auth.Guide.Steps, " "), "rest_webservices")
}

func guidanceFieldNames(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

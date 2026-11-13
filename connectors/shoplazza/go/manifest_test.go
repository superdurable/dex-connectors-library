// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shoplazza_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConnectionGuidanceCoversVisibleFields(t *testing.T) {
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Configuration struct {
				Fields []struct {
					Name        string `yaml:"name"`
					Description string `yaml:"description"`
				} `yaml:"fields"`
			} `yaml:"configuration"`
			Auth struct {
				Fields []struct {
					Name        string `yaml:"name"`
					Type        string `yaml:"type"`
					Description string `yaml:"description"`
				} `yaml:"fields"`
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"auth"`
			Operations []struct {
				Name            string `yaml:"name"`
				ConfigurationUI any    `yaml:"configurationUI"`
			} `yaml:"operations"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	names := []string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		names = append(names, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank")
	}
	for _, field := range manifest.Spec.Auth.Fields {
		names = append(names, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank")
		require.Equal(t, "secretString", field.Type)
	}
	require.Equal(t, []string{"shopDomain", "timeout", "maxResponseBytes", "access_token"}, names)
	require.Equal(t, "https://www.shoplazza.cn/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, expected := range []string{"Manage Private Apps", "read_shop", "generated Access Token", "myshoplaza.com"} {
		require.Contains(t, guide, expected)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "getShop", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
}

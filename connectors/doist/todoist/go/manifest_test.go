// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

func TestConnectionFormGuidanceAndSecretSafeDefaults(t *testing.T) {
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Configuration struct {
				Fields []struct {
					Name, Description string
					Default           any
				}
			}
			Auth struct {
				Type   string
				Fields []struct {
					Name, Type, Description string
					Required                bool
				}
				Guide struct {
					StartURL string `yaml:"startURL"`
					Steps    []string
				}
			}
		}
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Equal(t, "https://app.todoist.com/app/settings/integrations/developer", manifest.Spec.Auth.Guide.StartURL)
	require.Len(t, manifest.Spec.Auth.Guide.Steps, 4)
	require.Len(t, manifest.Spec.Auth.Fields, 1)
	field := manifest.Spec.Auth.Fields[0]
	require.Equal(t, "api_token", field.Name)
	require.Equal(t, "secretString", field.Type)
	require.True(t, field.Required)
	for _, instruction := range []string{"https://app.todoist.com/", "Settings > Integrations > Developer", "without a Bearer prefix", "blank"} {
		require.Contains(t, field.Description, instruction)
	}
	require.Len(t, manifest.Spec.Configuration.Fields, 3)
	for index, name := range []string{"baseUrl", "timeout", "maxResponseBytes"} {
		configuration := manifest.Spec.Configuration.Fields[index]
		require.Equal(t, name, configuration.Name)
		require.NotEmpty(t, configuration.Description)
		require.NotNil(t, configuration.Default)
	}
	defaults := withConfigDefaults(Config{})
	require.NoError(t, defaults.Validate())
	encoded, err := json.Marshal(defaults)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "api_token")
	_, err = json.Marshal(Credentials{APIToken: sdkgo.NewSecretString("manifest-test-secret")})
	require.Error(t, err)
}

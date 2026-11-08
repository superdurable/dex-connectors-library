// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConnectionFieldsExplainSourcesAndSafeDefaults(t *testing.T) {
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Configuration struct {
				Fields []struct {
					Name, Type, Description string
					Default                 any
				}
			} `yaml:"configuration"`
			Auth struct {
				Type  string `yaml:"type"`
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				Fields []struct{ Name, Type, Description string } `yaml:"fields"`
			} `yaml:"auth"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Equal(t, "https://www.odoo.com/documentation/19.0/developer/reference/external_api.html", manifest.Spec.Auth.Guide.StartURL)
	require.Len(t, manifest.Spec.Auth.Guide.Steps, 3)
	require.Len(t, manifest.Spec.Configuration.Fields, 4)
	configFields := map[string]string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		configFields[field.Name] = field.Description
		require.Contains(t, field.Description, "Blank", field.Name)
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.ElementsMatch(t, []string{"baseUrl", "timeout", "maxResponseBytes", "database"}, fieldNames(configFields))
	require.Contains(t, configFields["baseUrl"], "HTTPS")
	require.Contains(t, configFields["database"], "X-Odoo-Database")
	require.Contains(t, configFields["timeout"], "duration")
	require.Contains(t, configFields["maxResponseBytes"], "bytes")
	require.Len(t, manifest.Spec.Auth.Fields, 1)
	authFields := map[string]string{}
	for _, field := range manifest.Spec.Auth.Fields {
		authFields[field.Name] = field.Type
		require.True(t, strings.Contains(field.Description, "Blank") || strings.Contains(field.Description, "blank"), field.Name)
		require.Contains(t, field.Description, "Account Security > New API Key", field.Name)
	}
	require.Equal(t, map[string]string{"apiKey": "secretString"}, authFields)
}

func fieldNames(fields map[string]string) []string {
	result := make([]string, 0, len(fields))
	for name := range fields {
		result = append(result, name)
	}
	return result
}

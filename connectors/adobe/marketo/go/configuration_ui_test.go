// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMarketoConnectionFieldsGuideEveryVisibleValue(t *testing.T) {
	data, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest struct {
		Spec struct {
			Configuration struct {
				Fields []struct {
					Name        string `yaml:"name"`
					Description string `yaml:"description"`
					Default     any    `yaml:"default"`
				} `yaml:"fields"`
			} `yaml:"configuration"`
			Auth struct {
				Fields []struct {
					Name        string `yaml:"name"`
					Description string `yaml:"description"`
					Type        string `yaml:"type"`
				} `yaml:"fields"`
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"auth"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))
	var configurationNames []string
	for _, field := range manifest.Spec.Configuration.Fields {
		configurationNames = append(configurationNames, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", field.Name)
		if field.Name == "restEndpoint" || field.Name == "tokenEndpoint" {
			require.Contains(t, field.Description, "https://app.marketo.com/", field.Name)
		}
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"restEndpoint", "tokenEndpoint", "timeout", "maxResponseBytes"}, configurationNames)
	var authNames []string
	for _, field := range manifest.Spec.Auth.Fields {
		authNames = append(authNames, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", field.Name)
		if field.Type == "secretString" {
			require.Contains(t, field.Description, "Secret", field.Name)
		}
	}
	require.Equal(t, []string{"client_id", "client_secret", "access_token"}, authNames)
	require.Equal(t, "https://app.marketo.com/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{"Admin > Users & Roles > Roles", "Read-Write Lead", "Admin > Integration > LaunchPoint", "Admin > Integration > Web Services"} {
		require.Contains(t, guide, instruction)
	}
}

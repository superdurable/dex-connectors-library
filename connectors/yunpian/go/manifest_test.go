// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunpian_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

type visibleField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
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
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest connectionManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	fields := append(manifest.Spec.Configuration.Fields, manifest.Spec.Auth.Fields...)
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", field.Name)
		require.Greater(t, len(field.Description), 100, field.Name)
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "apiKey"}, names)
	require.Equal(t, "secretString", fields[3].Type)
	require.Contains(t, fields[3].Description, "https://www.yunpian.com")
	require.Contains(t, fields[3].Description, "Subaccount Management")
	require.Contains(t, fields[3].Description, "secret")
	require.Equal(t, "https://www.yunpian.com", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, value := range []string{"Account Settings > Subaccount Management", "System Settings > IP Whitelist", "Rotate or revoke", "https://us.yunpian.com"} {
		require.Contains(t, guide, value)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "getAccountBalance", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

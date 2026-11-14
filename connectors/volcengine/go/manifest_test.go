// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package volcengine_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
		require.Greater(t, len(field.Description), 110, field.Name)
		if field.Default != nil && field.Name != "service" {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "region", "service", "access_key_id", "secret_access_key"}, names)
	require.Equal(t, "secretString", fields[5].Type)
	require.Equal(t, "secretString", fields[6].Type)
	for _, field := range fields[5:] {
		require.Contains(t, field.Description, "https://console.volcengine.com")
		require.Contains(t, field.Description, "访问控制")
		require.Contains(t, field.Description, "Dex stores it only as a credential")
	}
	require.Equal(t, "https://console.volcengine.com", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, text := range []string{"访问控制", "用户管理", "新建密钥", "Access Key", "API Key", "X-Date", "HMAC-SHA256", "rotate"} {
		require.Contains(t, guide, text)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "describeRegions", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

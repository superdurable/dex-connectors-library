// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caiyun_test

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
		if field.Default != nil && len(fmt.Sprint(field.Default)) > 1 {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"baseUrl", "timeout", "maxResponseBytes", "app_key", "app_secret"}, names)
	for _, field := range fields[3:] {
		require.Equal(t, "secretString", field.Type)
		require.Contains(t, field.Description, "https://platform.caiyunapp.com")
		require.Contains(t, field.Description, "访问控制")
	}
	require.Equal(t, "https://platform.caiyunapp.com", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, text := range []string{"API 凭证管理", "App Key", "App Secret", "HMAC-SHA256", "rotate", "calls"} {
		require.Contains(t, guide, text)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "getCurrentWeather", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

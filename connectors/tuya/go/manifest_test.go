// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tuya_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

type visibleField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Default     any      `yaml:"default"`
	Description string   `yaml:"description"`
	Enum        []string `yaml:"enum"`
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

func TestConnectionGuidanceCoversVisibleFields(t *testing.T) {
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
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"endpoint", "timeout", "maxResponseBytes", "client_id", "client_secret"}, names)
	require.Equal(t, "enum", fields[0].Type)
	require.Len(t, fields[0].Enum, 7)
	require.Equal(t, "secretString", fields[3].Type)
	require.Equal(t, "secretString", fields[4].Type)
	require.Contains(t, fields[4].Description, "never sent to Tuya")
	require.Equal(t, "https://developer.tuya.com/en/docs/iot/manage-projects?id=Ka49p0n8vkzm6", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, value := range []string{"Cloud > Development > My Cloud Projects", "Authorization Key", "IoT Core", "Devices > All Devices", "client_secret", "token", "Rotate"} {
		require.Contains(t, guide, value)
	}
	require.Len(t, manifest.Spec.Operations, 1)
	require.Equal(t, "getDeviceDetails", manifest.Spec.Operations[0].Name)
	require.Nil(t, manifest.Spec.Operations[0].ConfigurationUI)
	require.Nil(t, manifest.Spec.Studio)
}

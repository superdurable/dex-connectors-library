// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mongodb_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type connectionField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}

type connectionManifest struct {
	Spec struct {
		Configuration struct {
			Fields []connectionField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Fields []connectionField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Studio *struct{} `yaml:"studio"`
	} `yaml:"spec"`
}

func TestConnectionFormGuidesEveryVisibleField(t *testing.T) {
	content, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest connectionManifest
	require.NoError(t, yaml.Unmarshal(content, &manifest))
	fields := append(manifest.Spec.Configuration.Fields, manifest.Spec.Auth.Fields...)
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", field.Name)
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), field.Name)
		}
	}
	require.Equal(t, []string{"database", "timeout", "maxDocuments", "maxResponseBytes", "connection_uri"}, names)
	require.Contains(t, fields[0].Description, "https://cloud.mongodb.com")
	require.Equal(t, "secretString", fields[4].Type)
	require.Contains(t, fields[4].Description, "Secret")
	require.Contains(t, fields[4].Description, "https://cloud.mongodb.com")
	require.Equal(t, "https://cloud.mongodb.com/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{"Database Access", "Network Access", "Connect", "Browse Collections", "read permission", "rotate"} {
		require.Contains(t, guide, instruction)
	}
	require.Nil(t, manifest.Spec.Studio, "native database protocol has no HTTPS setup units")
}

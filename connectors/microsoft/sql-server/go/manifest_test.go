// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"gopkg.in/yaml.v3"
)

// connectionManifest is the part of connector.yaml that Dex Web renders in Connections.
type connectionManifest struct {
	Metadata struct {
		Company string `yaml:"company"`
	} `yaml:"metadata"`
	Spec struct {
		Configuration struct {
			Fields []connectionManifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type    string                    `yaml:"type"`
			Methods []any                     `yaml:"methods"`
			Fields  []connectionManifestField `yaml:"fields"`
			Guide   struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Studio   *struct{} `yaml:"studio"`
		Triggers []any     `yaml:"triggers"`
	} `yaml:"spec"`
}

type connectionManifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Description string   `yaml:"description"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

func readConnectionManifest(t *testing.T) connectionManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest connectionManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

func TestConnectionFormShowsGuidanceForEveryVisibleField(t *testing.T) {
	manifest := readConnectionManifest(t)
	fields := append(append([]connectionManifestField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	names := make([]string, len(fields))
	for index, field := range fields {
		names[index] = field.Name
	}
	require.Equal(t, []string{
		"host", "port", "database", "user", "encrypt", "connectTimeout", "statementTimeout", "maxRows", "maxResponseBytes", "password",
	}, names, "the complete Dex Web Connections form")
	providerValues := map[string]bool{"host": true, "port": true, "database": true, "user": true, "password": true}
	for _, field := range fields {
		t.Run(field.Name, func(t *testing.T) {
			require.Contains(t, strings.ToLower(field.Description), "blank", "states what leaving the field blank means")
			if providerValues[field.Name] {
				require.Contains(t, field.Description, "https://", "names a provider URL to start from")
			}
			if field.Name == "password" {
				require.Equal(t, "secretString", field.Type)
				require.True(t, field.Required)
				require.Contains(t, field.Description, "Secret")
				return
			}
			require.NotEqual(t, "secretString", field.Type)
			if providerValues[field.Name] {
				require.Contains(t, field.Description, "Not secret")
			}
			for _, option := range field.Enum {
				require.Contains(t, field.Description, option, "every enum option is explained")
			}
			if field.Default != nil && field.Type != "enum" {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default below the field")
			}
		})
	}
}

func TestConnectionDefaultsVerifyTLSAndMatchTheGeneratedConfig(t *testing.T) {
	manifest := readConnectionManifest(t)
	defaults := map[string]any{}
	for _, field := range manifest.Spec.Configuration.Fields {
		defaults[field.Name] = field.Default
		if field.Name == "encrypt" {
			require.Equal(t, []string{"mandatory", "strict", "trust-server-certificate", "disable"}, field.Enum, "Microsoft's optional mode is not offered")
			require.Contains(t, field.Description, "SQLSERVER_SSL_CA", "the private CA bundle is discoverable from the form")
			require.Contains(t, field.Description, "impostor server", "skipping verification is an explicit, explained choice")
		}
	}
	config := sqlserver.DefaultConfig()
	require.Equal(t, "mandatory", defaults["encrypt"])
	require.Equal(t, sqlserver.EncryptMandatory, config.Encrypt)
	require.Equal(t, 1433, defaults["port"])
	require.Equal(t, int64(1433), config.Port)
	require.Nil(t, defaults["host"], "the server location has no default")
}

func TestAuthorizationGuideCreatesALeastPrivilegeUser(t *testing.T) {
	manifest := readConnectionManifest(t)
	require.Equal(t, "Microsoft", manifest.Metadata.Company, "the company directory is connectors/microsoft")
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods, "SQL authentication is the only method")
	require.True(t, strings.HasPrefix(manifest.Spec.Auth.Guide.StartURL, "https://learn.microsoft.com/"))
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, "\n")
	for _, instruction := range []string{
		"CREATE USER dex_app WITH PASSWORD", "CREATE LOGIN", "GRANT SELECT, INSERT", "ALTER USER", "ALTER LOGIN dex_app DISABLE",
		"Support only Microsoft Entra authentication", "firewall rule", "not a permission boundary",
	} {
		require.Contains(t, guide, instruction)
	}
	require.Nil(t, manifest.Spec.Studio, "no Studio units: setup commands cannot reach a database")
	require.Empty(t, manifest.Spec.Triggers)
}

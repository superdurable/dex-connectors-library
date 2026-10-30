// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string `yaml:"defaultMethod"`
			Refreshable   bool   `yaml:"refreshable"`
			Methods       []struct {
				ID            string          `yaml:"id"`
				Type          string          `yaml:"type"`
				Description   string          `yaml:"description"`
				Recommended   bool            `yaml:"recommended"`
				Fields        []guidanceField `yaml:"fields"`
				Configuration struct {
					Fields []guidanceField `yaml:"fields"`
				} `yaml:"configuration"`
				Guide struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio any `yaml:"studio"`
	} `yaml:"spec"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
		visibleFields = append(visibleFields, method.Configuration.Fields...)
	}
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{
		"accountIdentifier", "warehouse", "role", "database", "schema", "statementTimeoutSeconds", "maxRows", "maxResponseBytes",
		"user", "private_key", "programmatic_access_token",
	}, names, "a new visible field needs its own guidance review")
	providerSourced := map[string]bool{
		"accountIdentifier": true, "warehouse": true, "role": true, "database": true, "schema": true,
		"user": true, "private_key": true, "programmatic_access_token": true,
	}
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !providerSourced[field.Name] {
				return
			}
			require.Contains(t, field.Description, "https://", "provider values name the page to start from")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.True(t, field.Required)
			} else {
				require.Contains(t, field.Description, "Not secret")
			}
		})
	}
}

func TestAuthenticationMethodsExplainSetupRotationAndRevocation(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, snowflake.KeyPairAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	require.False(t, manifest.Spec.Auth.Refreshable, "a key-pair JWT is signed per request and a token is static, so nothing refreshes")
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	keyPair, token := manifest.Spec.Auth.Methods[0], manifest.Spec.Auth.Methods[1]
	require.Equal(t, snowflake.KeyPairAuthMethodID, keyPair.ID)
	require.True(t, keyPair.Recommended)
	require.Equal(t, snowflake.ProgrammaticAccessTokenAuthMethodID, token.ID)
	for _, method := range manifest.Spec.Auth.Methods {
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://docs.snowflake.com/"), method.ID)
		require.GreaterOrEqual(t, len(method.Guide.Steps), 4, method.ID)
		require.Empty(t, method.Configuration.Fields, "%s: Dex Web omits method configuration fields from the setup form", method.ID)
		require.Regexp(t, `(?i)rotate`, strings.Join(method.Guide.Steps, " "), method.ID)
		require.Regexp(t, `(?i)revoke`, strings.Join(method.Guide.Steps, " "), method.ID)
	}
	keyPairGuide := strings.Join(keyPair.Guide.Steps, " ")
	for _, instruction := range []string{"openssl genrsa 2048", "-nocrypt", "RSA_PUBLIC_KEY", "RSA_PUBLIC_KEY_2", "TYPE = SERVICE"} {
		require.Contains(t, keyPairGuide, instruction)
	}
	tokenGuide := strings.Join(token.Guide.Steps, " ")
	for _, instruction := range []string{"network policy", "Generate new token", "One specific role", "REMOVE PROGRAMMATIC ACCESS TOKEN"} {
		require.Contains(t, tokenGuide, instruction)
	}
}

func TestConnectionDefaultsMatchTheGeneratedConfig(t *testing.T) {
	manifest := readGuidanceManifest(t)
	defaults := map[string]any{}
	for _, field := range manifest.Spec.Configuration.Fields {
		defaults[field.Name] = field.Default
	}
	config := snowflake.DefaultConfig()
	require.Equal(t, 3600, defaults["statementTimeoutSeconds"])
	require.Equal(t, int64(3600), config.StatementTimeoutSeconds)
	require.Equal(t, 1000, defaults["maxRows"])
	require.Equal(t, int64(1000), config.MaxRows)
	require.Nil(t, defaults["accountIdentifier"], "the account has no default")
	require.Nil(t, defaults["warehouse"], "blank uses the user's DEFAULT_WAREHOUSE")
}

// TestManifestDeclaresNoStudioBundle records that a per-account host cannot be a Studio command's fixed host.
func TestManifestDeclaresNoStudioBundle(t *testing.T) {
	require.Nil(t, readGuidanceManifest(t).Spec.Studio)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

type salesforceManifest struct {
	Spec struct {
		Provider      string `yaml:"provider"`
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID     string          `yaml:"id"`
				Type   string          `yaml:"type"`
				Fields []manifestField `yaml:"fields"`
				Guide  struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				OAuth2 *struct {
					AuthorizationEndpoint string   `yaml:"authorizationEndpoint"`
					TokenEndpoint         string   `yaml:"tokenEndpoint"`
					Scopes                []string `yaml:"scopes"`
					PKCE                  bool     `yaml:"pkce"`
					CredentialMappings    []struct {
						Credential string `yaml:"credential"`
						Source     string `yaml:"source"`
					} `yaml:"credentialMappings"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []any `yaml:"commands"`
			Units    []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func loadSalesforceManifest(t *testing.T) salesforceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest salesforceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Dex Web performs authorization with these endpoints, and the refresh driver must use the same login host.
func TestOAuthMethodsMatchTheRefreshDriver(t *testing.T) {
	manifest := loadSalesforceManifest(t)
	require.Equal(t, "salesforce", manifest.Spec.Provider)
	require.Equal(t, ProductionOAuthAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	loginURLs := map[string]string{ProductionOAuthAuthMethodID: productionLoginURL, SandboxOAuthAuthMethodID: sandboxLoginURL}
	methodIDs := []string{}
	for _, method := range manifest.Spec.Auth.Methods {
		methodIDs = append(methodIDs, method.ID)
		if method.ID == JWTBearerAuthMethodID {
			require.Equal(t, "serviceAccount", method.Type)
			require.Nil(t, method.OAuth2)
			continue
		}
		require.Equal(t, "oauth2", method.Type)
		require.Equal(t, loginURLs[method.ID]+"/services/oauth2/authorize", method.OAuth2.AuthorizationEndpoint)
		require.Equal(t, loginURLs[method.ID]+oauthTokenPath, method.OAuth2.TokenEndpoint)
		require.Equal(t, loginURLs[method.ID], method.Guide.StartURL)
		require.Equal(t, []string{"api", "refresh_token"}, method.OAuth2.Scopes)
		require.True(t, method.OAuth2.PKCE)
		mappings := map[string]string{}
		for _, mapping := range method.OAuth2.CredentialMappings {
			mappings[mapping.Credential] = mapping.Source
		}
		require.Equal(t, map[string]string{"access_token": "access_token", "refresh_token": "refresh_token", "instance_url": instanceURLField}, mappings)
	}
	require.Equal(t, []string{ProductionOAuthAuthMethodID, SandboxOAuthAuthMethodID, JWTBearerAuthMethodID}, methodIDs)
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadSalesforceManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"apiVersion": defaults.APIVersion, "maxResponseBytes": int(defaults.MaxResponseBytes), "queryBatchSize": int(defaults.QueryBatchSize),
	}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}
	for _, method := range manifest.Spec.Auth.Methods {
		guide := strings.Join(method.Guide.Steps, "\n")
		require.Contains(t, guide, "Setup > Apps > External Client Apps > External Client App Manager", method.ID)
		require.Contains(t, guide, "Consumer Key and Secret", method.ID)
		for _, field := range method.Fields {
			isAuthorizationOutput := field.Name == "access_token" || field.Name == "refresh_token" || field.Name == "instance_url"
			if isAuthorizationOutput {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				require.Contains(t, field.Description, "never", "%s/%s is never entered", method.ID, field.Name)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			isSecret := field.Name == "oauth_client_secret" || field.Name == "jwt_private_key"
			require.Equal(t, isSecret, field.Type == "secretString", "%s/%s secrecy", method.ID, field.Name)
			require.Equal(t, isSecret, strings.HasPrefix(field.Description, "Secret "), "%s/%s states secrecy", method.ID, field.Name)
		}
		if method.Type == "oauth2" {
			require.Contains(t, guide, "Redirect URI shown by Dex Web", method.ID)
			require.Contains(t, guide, "(api)", method.ID)
			require.Contains(t, guide, "refresh_token", method.ID)
			require.Contains(t, guide, "PKCE", method.ID)
		} else {
			require.Contains(t, guide, "certificate", method.ID)
			require.Contains(t, guide, "pre-authorized", method.ID)
		}
	}
}

// Studio commands use fixed hosts, and every Salesforce REST request needs the org's own instance URL.
func TestStudioUnitsNeedNoProviderCommand(t *testing.T) {
	manifest := loadSalesforceManifest(t)
	require.Empty(t, manifest.Spec.Studio.Commands)
	units := map[string]string{}
	for _, unit := range manifest.Spec.Studio.Units {
		units[unit.ID] = unit.Description
	}
	require.Contains(t, units[UIUnitSObjectPicker], "Setup > Object Manager")
	require.Contains(t, units[UIUnitFieldNameInput], "Fields & Relationships")
	for _, description := range units {
		require.Contains(t, description, "blank")
	}
}

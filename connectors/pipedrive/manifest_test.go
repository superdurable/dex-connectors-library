// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}

type pipedriveManifest struct {
	Spec struct {
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
					CredentialMappings    []struct {
						Credential string `yaml:"credential"`
						Source     string `yaml:"source"`
					} `yaml:"credentialMappings"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					URL        string `yaml:"url"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
						Header string `yaml:"header"`
					} `yaml:"credential"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func loadPipedriveManifest(t *testing.T) pipedriveManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest pipedriveManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Dex Web authorizes with the manifest endpoints; the refresh driver must use the same token endpoint and domain field.
func TestOAuthMethodMatchesTheRefreshDriver(t *testing.T) {
	manifest := loadPipedriveManifest(t)
	require.Equal(t, APITokenAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	methodIDs := []string{}
	for _, method := range manifest.Spec.Auth.Methods {
		methodIDs = append(methodIDs, method.ID)
		if method.ID != OAuthAuthMethodID {
			require.Nil(t, method.OAuth2)
			continue
		}
		require.Equal(t, "https://oauth.pipedrive.com/oauth/authorize", method.OAuth2.AuthorizationEndpoint)
		require.Equal(t, pipedriveOAuthTokenEndpoint, method.OAuth2.TokenEndpoint)
		for _, scope := range requiredOAuthScopes {
			require.Contains(t, method.OAuth2.Scopes, scope)
		}
		mappings := map[string]string{}
		for _, mapping := range method.OAuth2.CredentialMappings {
			mappings[mapping.Credential] = mapping.Source
		}
		require.Equal(t, map[string]string{"access_token": "access_token", "refresh_token": "refresh_token", "api_domain": apiDomainResponseField}, mappings)
	}
	require.Equal(t, []string{APITokenAuthMethodID, OAuthAuthMethodID}, methodIDs)
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadPipedriveManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes)}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}
	for _, method := range manifest.Spec.Auth.Methods {
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://app.pipedrive.com/"), method.ID)
		guide := strings.Join(method.Guide.Steps, "\n")
		for _, field := range method.Fields {
			isAuthorizationOutput := field.Name == "access_token" || field.Name == "refresh_token" || field.Name == "api_domain"
			if isAuthorizationOutput {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				require.Contains(t, field.Description, "never", "%s/%s is never entered", method.ID, field.Name)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			isSecret := field.Type == "secretString"
			require.Equal(t, isSecret, strings.HasPrefix(field.Description, "Secret "), "%s/%s states secrecy", method.ID, field.Name)
		}
		if method.ID == OAuthAuthMethodID {
			require.Contains(t, guide, "Redirect URI shown by Dex Web", method.ID)
			for _, scope := range []string{"deals:full", "contacts:full", "base"} {
				require.Contains(t, guide, scope, method.ID)
			}
		} else {
			require.Contains(t, guide, "Personal preferences > API")
		}
	}
}

// Studio commands reach a fixed host, so pickers use the shared API host with the Personal API token only.
func TestStudioCommandsSendOnlyTheAPITokenToPipedrivesSharedHost(t *testing.T) {
	manifest := loadPipedriveManifest(t)
	require.NotEmpty(t, manifest.Spec.Studio.Commands)
	for _, command := range manifest.Spec.Studio.Commands {
		require.True(t, strings.HasPrefix(command.Request.URL, "https://api.pipedrive.com/"), command.ID)
		require.Equal(t, "api_token", command.Request.Credential.Field, command.ID)
		require.Equal(t, "header", command.Request.Credential.Scheme, command.ID)
		require.Equal(t, "x-api-token", command.Request.Credential.Header, command.ID)
	}
	units := map[string]string{}
	for _, unit := range manifest.Spec.Studio.Units {
		units[unit.ID] = unit.Description
	}
	require.Len(t, units, 3)
	for unitID, description := range units {
		require.Contains(t, description, "Personal API token", unitID)
		require.Contains(t, description, "when the list", unitID)
	}
	require.Contains(t, units[UIUnitOwnerPicker], "blank")
	require.Contains(t, units[UIUnitCustomFieldPicker], "blank")
}

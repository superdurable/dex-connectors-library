// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms

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

type formsManifest struct {
	Spec struct {
		Provider      string `yaml:"provider"`
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Methods []struct {
				ID     string          `yaml:"id"`
				Type   string          `yaml:"type"`
				Fields []manifestField `yaml:"fields"`
				Guide  struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				OAuth2 struct {
					Scopes     []string `yaml:"scopes"`
					UserScopes []string `yaml:"userScopes"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID         string `yaml:"id"`
				Capability string `yaml:"capability"`
				Request    struct {
					Method     string            `yaml:"method"`
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func loadFormsManifest(t *testing.T) formsManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest formsManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Google reports alias grants under canonical URIs, and Dex Web matches granted scopes literally.
func TestOAuthScopesAreCanonicalAndMatchTheRefreshDriver(t *testing.T) {
	manifest := loadFormsManifest(t)
	require.Equal(t, "google", manifest.Spec.Provider)
	methodScopes := map[string][]string{}
	delegationGuide := ""
	for _, method := range manifest.Spec.Auth.Methods {
		methodScopes[method.ID] = method.OAuth2.Scopes
		require.Empty(t, method.OAuth2.UserScopes)
		if method.ID == WorkspaceDomainDelegationAuthMethodID {
			delegationGuide = strings.Join(method.Guide.Steps, "\n")
		}
	}
	require.Equal(t, formsOAuthScopes, methodScopes[GoogleOAuthAuthMethodID])
	for _, scope := range formsOAuthScopes {
		require.True(t, strings.HasPrefix(scope, "https://www.googleapis.com/auth/"), scope)
		require.Contains(t, delegationGuide, strings.TrimPrefix(scope, "https://www.googleapis.com/auth/"))
	}
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadFormsManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes), "responsePageSize": int(defaults.ResponsePageSize),
	}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}
	for _, method := range manifest.Spec.Auth.Methods {
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://console.cloud.google.com/"), method.ID)
		guide := strings.Join(method.Guide.Steps, "\n")
		require.Contains(t, guide, "Google Forms API", method.ID)
		require.Contains(t, guide, "Google Drive API", method.ID)
		for _, field := range method.Fields {
			isDerived := field.Name == "access_token" || field.Name == "refresh_token"
			if isDerived {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			require.Equal(t, field.Name == "oauth_client_secret" || field.Name == "service_account_key", field.Type == "secretString", "%s/%s secrecy", method.ID, field.Name)
		}
		if method.Type == "oauth2" {
			require.Contains(t, guide, "Redirect URI", method.ID)
			require.Contains(t, guide, "test users", method.ID)
			require.Contains(t, guide, "restricted scope", method.ID)
		}
	}
	require.Len(t, manifest.Spec.Studio.Units, 1)
	require.Contains(t, manifest.Spec.Studio.Units[0].Description, "blank")
}

// The Forms API has no form listing, so the picker lists form files through Google Drive.
func TestFormPickerCommandListsOnlyNonTrashedFormsReadOnly(t *testing.T) {
	manifest := loadFormsManifest(t)
	require.Len(t, manifest.Spec.Studio.Commands, 1)
	command := manifest.Spec.Studio.Commands[0]
	require.Equal(t, "listForms", command.ID)
	require.Equal(t, "google.forms.forms-list", command.Capability)
	require.Equal(t, "GET", command.Request.Method)
	require.Equal(t, "https://www.googleapis.com/drive/v3/files", command.Request.URL)
	require.Equal(t, "mimeType = 'application/vnd.google-apps.form' and trashed = false", command.Request.FixedQuery["q"])
	require.Equal(t, "nextPageToken,files(id,name)", command.Request.FixedQuery["fields"])
	require.Equal(t, "allDrives", command.Request.FixedQuery["corpora"])
	require.Equal(t, "true", command.Request.FixedQuery["supportsAllDrives"])
	require.Equal(t, "true", command.Request.FixedQuery["includeItemsFromAllDrives"])
}

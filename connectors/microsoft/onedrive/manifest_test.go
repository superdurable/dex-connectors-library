// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

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

type onedriveManifest struct {
	Spec struct {
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
					AuthorizationEndpoint string   `yaml:"authorizationEndpoint"`
					TokenEndpoint         string   `yaml:"tokenEndpoint"`
					Scopes                []string `yaml:"scopes"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
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

func loadManifest(t *testing.T) onedriveManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest onedriveManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Microsoft returns short Graph scope names, and Dex Web compares returned scopes literally.
func TestDelegatedScopesAreShortCanonicalNamesSentAgainOnRefresh(t *testing.T) {
	manifest := loadManifest(t)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	delegated := manifest.Spec.Auth.Methods[0]
	require.Equal(t, MicrosoftOAuthAuthMethodID, delegated.ID)
	require.Equal(t, delegatedScopes, delegated.OAuth2.Scopes)
	require.Contains(t, delegated.OAuth2.Scopes, "offline_access")
	for _, scope := range delegated.OAuth2.Scopes {
		require.NotContains(t, scope, "https://", scope)
		require.NotContains(t, []string{"openid", "profile", "email"}, scope)
	}
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", delegated.OAuth2.AuthorizationEndpoint)
	require.Equal(t, microsoftOrganizationsTokenEndpoint, delegated.OAuth2.TokenEndpoint)
	require.Equal(t, MicrosoftAppOnlyAuthMethodID, manifest.Spec.Auth.Methods[1].ID)
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes), "maxTextBytes": int(defaults.MaxTextBytes),
		"maxUploadBytes": int(defaults.MaxUploadBytes), "searchPageSize": int(defaults.SearchPageSize),
	}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}
	for _, method := range manifest.Spec.Auth.Methods {
		require.Equal(t, "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade", method.Guide.StartURL, method.ID)
		guide := strings.Join(method.Guide.Steps, "\n")
		for _, field := range method.Fields {
			if field.Name == "access_token" || field.Name == "refresh_token" {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			require.Equal(t, strings.HasSuffix(field.Name, "client_secret"), field.Type == "secretString", "%s/%s secrecy", method.ID, field.Name)
		}
		if method.Type == "oauth2" {
			for _, required := range []string{"Redirect URI", "multitenant", "AADSTS50194", "Files.ReadWrite.All", "Sites.Read.All", "offline_access", "admin consent"} {
				require.Contains(t, guide, required, method.ID)
			}
			continue
		}
		for _, required := range []string{"Sites.Selected", "Grant admin consent", "/permissions", "drive ID"} {
			require.Contains(t, guide, required, method.ID)
		}
	}
}

func TestStudioCommandsAreFixedGraphReadsWithBoundedFields(t *testing.T) {
	manifest := loadManifest(t)
	commandIDs := []string{}
	for _, command := range manifest.Spec.Studio.Commands {
		commandIDs = append(commandIDs, command.ID)
		require.True(t, strings.HasPrefix(command.Request.URL, DefaultConfig().Endpoint+graphAPIVersionPath+"/"), command.ID)
		require.NotEmpty(t, command.Request.FixedQuery["$select"], command.ID)
		require.NotContains(t, command.Request.FixedQuery["$select"], "downloadUrl", command.ID)
	}
	require.Equal(t, []string{"searchSites", "getMyDrive", "listSiteDrives", "listRootFolders", "listFolderChildren"}, commandIDs)
	for _, unit := range manifest.Spec.Studio.Units {
		require.Contains(t, unit.Description, "blank means", unit.ID)
		require.Contains(t, unit.Description, "paste", unit.ID)
	}
}

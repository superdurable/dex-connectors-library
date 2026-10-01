// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

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
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}

type entraIDManifest struct {
	Spec struct {
		Provider      string `yaml:"provider"`
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID          string          `yaml:"id"`
				Type        string          `yaml:"type"`
				Recommended bool            `yaml:"recommended"`
				Fields      []manifestField `yaml:"fields"`
				Guide       struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				OAuth2 *struct {
					AuthorizationEndpoint string   `yaml:"authorizationEndpoint"`
					TokenEndpoint         string   `yaml:"tokenEndpoint"`
					Scopes                []string `yaml:"scopes"`
					PKCE                  bool     `yaml:"pkce"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio     any   `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Authorization string `yaml:"authorization"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

func loadManifest(t *testing.T) entraIDManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest entraIDManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Microsoft returns short-form Graph scopes, and Dex Web matches granted scopes literally.
func TestBothMethodsUseTheSameLeastPrivilegedGraphPermissions(t *testing.T) {
	manifest := loadManifest(t)
	require.Equal(t, "microsoft", manifest.Spec.Provider)
	require.Equal(t, EntraAppOnlyAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	appOnly, oauth := manifest.Spec.Auth.Methods[0], manifest.Spec.Auth.Methods[1]
	require.Equal(t, EntraAppOnlyAuthMethodID, appOnly.ID)
	require.True(t, appOnly.Recommended)
	require.Nil(t, appOnly.OAuth2)
	require.Equal(t, MicrosoftOAuthAuthMethodID, oauth.ID)
	require.NotNil(t, oauth.OAuth2)
	require.Equal(t, append([]string{"offline_access"}, delegatedGraphScopes...), oauth.OAuth2.Scopes)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", oauth.OAuth2.AuthorizationEndpoint)
	require.Equal(t, organizationsTokenEndpoint, oauth.OAuth2.TokenEndpoint)
	require.True(t, oauth.OAuth2.PKCE)
	for _, method := range manifest.Spec.Auth.Methods {
		guide := strings.Join(method.Guide.Steps, "\n")
		for _, permission := range delegatedGraphScopes {
			require.Contains(t, guide, permission, "%s names every permission to grant", method.ID)
		}
		for _, broader := range []string{"User.ReadWrite.All", "Directory.ReadWrite.All", "Group.ReadWrite.All"} {
			require.NotContains(t, guide, broader, "%s never asks for a broader permission", method.ID)
		}
		require.Contains(t, guide, "admin consent")
		require.Contains(t, guide, "Privileged")
	}
	require.Contains(t, strings.Join(oauth.Guide.Steps, "\n"), "AADSTS50194")
	require.Contains(t, strings.Join(oauth.Guide.Steps, "\n"), "Redirect URI shown by Dex Web")
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := loadManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"maxResponseBytes": int(defaults.MaxResponseBytes), "listUsersPageSize": int(defaults.ListUsersPageSize),
		"creationKeyAttribute": string(defaults.CreationKeyAttribute),
	}
	visible := []string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		visible = append(visible, field.Name)
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
		require.Contains(t, strings.ToLower(field.Description), "blank", "%s explains blank", field.Name)
	}
	for _, method := range manifest.Spec.Auth.Methods {
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://entra.microsoft.com/"), method.ID)
		for _, field := range method.Fields {
			visible = append(visible, method.ID+"/"+field.Name)
			require.GreaterOrEqual(t, len(field.Description), 120, "%s/%s: a label-length description is not guidance", method.ID, field.Name)
			require.Equal(t, field.Type == "secretString", strings.HasPrefix(field.Description, "Secret"), "%s/%s says whether it is secret first", method.ID, field.Name)
			if field.Name == "access_token" || field.Name == "refresh_token" {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				require.Regexp(t, "never (enter|paste)", field.Description)
				continue
			}
			require.Contains(t, field.Description, "https://entra.microsoft.com", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			require.True(t, field.Required, "%s/%s", method.ID, field.Name)
		}
	}
	require.Equal(t, []string{
		"maxResponseBytes", "listUsersPageSize", "creationKeyAttribute",
		"entra-app-only/tenant_id", "entra-app-only/client_id", "entra-app-only/client_secret", "entra-app-only/access_token",
		"microsoft-oauth/client_id", "microsoft-oauth/client_secret", "microsoft-oauth/access_token", "microsoft-oauth/refresh_token",
	}, visible, "a new visible field needs its own guidance review")
}

// The default app-only method has no token Dex Web can refresh for a picker, so the connector ships none.
func TestManifestDeclaresNoStudioBundleOrTriggers(t *testing.T) {
	manifest := loadManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
	require.Len(t, manifest.Spec.Operations, 8)
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

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

type workspaceAdminManifest struct {
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
					Scopes                  []string          `yaml:"scopes"`
					UserScopes              []string          `yaml:"userScopes"`
					AuthorizationParameters map[string]string `yaml:"authorizationParameters"`
					PKCE                    bool              `yaml:"pkce"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio     any   `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Kind          string `yaml:"kind"`
			Authorization string `yaml:"authorization"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

func loadManifest(t *testing.T) workspaceAdminManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest workspaceAdminManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Google reports alias grants under canonical URIs, and Dex Web matches granted scopes literally.
func TestBothMethodsRequestExactlyTheTwoCanonicalDirectoryScopes(t *testing.T) {
	manifest := loadManifest(t)
	require.Equal(t, "google", manifest.Spec.Provider)
	require.Equal(t, []string{
		"https://www.googleapis.com/auth/admin.directory.user",
		"https://www.googleapis.com/auth/admin.directory.group.member",
	}, directoryOAuthScopes)
	for _, method := range manifest.Spec.Auth.Methods {
		guide := strings.Join(method.Guide.Steps, "\n")
		for _, scope := range directoryOAuthScopes {
			require.Contains(t, guide, strings.TrimPrefix(scope, "https://www.googleapis.com/auth/"), "%s names every scope to authorize", method.ID)
		}
		require.NotContains(t, guide, "admin.directory.group,", "the broader group scope is never requested")
		if method.OAuth2 != nil {
			require.Equal(t, directoryOAuthScopes, method.OAuth2.Scopes)
			require.Empty(t, method.OAuth2.UserScopes)
			require.True(t, method.OAuth2.PKCE)
			require.Equal(t, map[string]string{"access_type": "offline", "prompt": "consent"}, method.OAuth2.AuthorizationParameters)
		}
	}
}

func TestDomainWideDelegationIsTheRecommendedDefault(t *testing.T) {
	manifest := loadManifest(t)
	require.Equal(t, WorkspaceDomainDelegationAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	delegation, oauth := manifest.Spec.Auth.Methods[0], manifest.Spec.Auth.Methods[1]
	require.Equal(t, WorkspaceDomainDelegationAuthMethodID, delegation.ID)
	require.Equal(t, "serviceAccount", delegation.Type)
	require.True(t, delegation.Recommended)
	require.Nil(t, delegation.OAuth2)
	require.Equal(t, GoogleOAuthAuthMethodID, oauth.ID)
	require.Equal(t, "oauth2", oauth.Type)
	require.False(t, oauth.Recommended)
	delegationGuide := strings.Join(delegation.Guide.Steps, "\n")
	for _, instruction := range []string{"Admin SDK API", "Show advanced settings", "Client ID", "Keys > Add key > Create new key > JSON",
		"https://admin.google.com/ac/owl/domainwidedelegation", "Manage Domain Wide Delegation", "super administrator",
		"User Management Admin", "Groups Admin", "24 hours"} {
		require.Contains(t, delegationGuide, instruction)
	}
	oauthGuide := strings.Join(oauth.Guide.Steps, "\n")
	for _, instruction := range []string{"Admin SDK API", "Internal audience", "Redirect URI shown by Dex Web", "client secret",
		"User Management Admin", "Groups Admin", "test users", "seven days"} {
		require.Contains(t, oauthGuide, instruction)
	}
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := loadManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes), "listUsersPageSize": int(defaults.ListUsersPageSize),
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
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://console.cloud.google.com/"), method.ID)
		for _, field := range method.Fields {
			visible = append(visible, method.ID+"/"+field.Name)
			require.GreaterOrEqual(t, len(field.Description), 120, "%s/%s: a label-length description is not guidance", method.ID, field.Name)
			require.Equal(t, field.Type == "secretString", strings.HasPrefix(field.Description, "Secret"), "%s/%s says whether it is secret first", method.ID, field.Name)
			isAuthorizationOutput := field.Name == "access_token" || field.Name == "refresh_token"
			if isAuthorizationOutput {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				require.Regexp(t, "never (enter|paste)", field.Description)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			require.True(t, field.Required, "%s/%s", method.ID, field.Name)
		}
	}
	require.Equal(t, []string{
		"endpoint", "maxResponseBytes", "listUsersPageSize",
		"workspace-domain-delegation/service_account_key", "workspace-domain-delegation/delegated_user", "workspace-domain-delegation/access_token",
		"google-oauth/oauth_client_id", "google-oauth/oauth_client_secret", "google-oauth/access_token", "google-oauth/refresh_token",
	}, visible, "a new visible field needs its own guidance review")
}

// No Studio command can list org units or groups within the two Directory scopes, so the connector ships no picker or bundle.
func TestManifestDeclaresNoStudioBundleOrTriggers(t *testing.T) {
	manifest := loadManifest(t)
	require.Nil(t, manifest.Spec.Studio)
	require.Empty(t, manifest.Spec.Triggers)
	_, err := os.Stat("ui")
	require.True(t, os.IsNotExist(err))
	require.Len(t, manifest.Spec.Operations, 7)
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
	}
}

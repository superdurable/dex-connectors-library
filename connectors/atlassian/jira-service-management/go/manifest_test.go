// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Fields []guidanceField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 struct {
				AuthorizationParameters map[string]string `yaml:"authorizationParameters"`
				Scopes                  []string          `yaml:"scopes"`
				PKCE                    bool              `yaml:"pkce"`
			} `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					Method     string            `yaml:"method"`
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
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

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"cloudId", "endpoint", "maxResponseBytes", "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, names,
		"a new visible field needs its own guidance review")
	descriptions := map[string]string{}
	for _, field := range visibleFields {
		descriptions[field.Name] = field.Description
		require.GreaterOrEqual(t, len(field.Description), 120, "%s: a label-length description is not guidance", field.Name)
		if field.Default != nil {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s: Dex Web already shows the default", field.Name)
		}
		require.Contains(t, strings.ToLower(field.Description), "blank", "%s explains what blank means", field.Name)
		if field.Type == "secretString" {
			require.True(t, strings.HasPrefix(field.Description, "Secret"), "%s says it is secret first", field.Name)
		}
	}
	require.Contains(t, descriptions["cloudId"], "Non-secret")
	require.Contains(t, descriptions["cloudId"], "https://<your-site>.atlassian.net/_edge/tenant_info")
	require.Contains(t, descriptions["oauth_client_id"], "https://developer.atlassian.com/console/myapps/")
	require.Contains(t, descriptions["oauth_client_secret"], "https://developer.atlassian.com/console/myapps/")
	require.Contains(t, descriptions["access_token"], "never enter it manually")
}

func TestOAuthSetupNamesTheAppPageCallbackScopesAndConsent(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "https://developer.atlassian.com/console/myapps/", auth.Guide.StartURL)
	require.Equal(t, []string{
		"read:servicedesk-request", "write:servicedesk-request", "manage:servicedesk-customer", "read:jira-work", "write:jira-work", "offline_access",
	}, auth.OAuth2.Scopes)
	require.Equal(t, map[string]string{"audience": "api.atlassian.com", "prompt": "consent"}, auth.OAuth2.AuthorizationParameters)
	require.False(t, auth.OAuth2.PKCE, "Atlassian 3LO documents no PKCE")
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"OAuth 2.0 integration", "Jira Service Management API", "Jira API", "Callback URL", "Redirect URI", "read:servicedesk-request",
		"write:servicedesk-request", "manage:servicedesk-customer", "read:jira-work", "write:jira-work", "offline_access", "agent",
		"consent", "Enable sharing", "90 days",
	} {
		require.Contains(t, guide, text)
	}
}

func TestStudioCommandsAreReadOnlyAndPinnedToTheAtlassianGateway(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	var commandIDs []string
	for _, command := range studio.Commands {
		commandIDs = append(commandIDs, command.ID)
		require.Equal(t, "GET", command.Request.Method, command.ID)
		require.True(t, strings.HasPrefix(command.Request.URL, "https://api.atlassian.com/"), command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field, command.ID)
		require.Equal(t, "bearer", command.Request.Credential.Scheme, command.ID)
	}
	require.Equal(t, []string{"listAccessibleSites", "listServiceDesks", "listRequestTypes"}, commandIDs)
	var unitIDs []string
	for _, unit := range studio.Units {
		unitIDs = append(unitIDs, unit.ID)
		require.GreaterOrEqual(t, len(unit.Description), 120, unit.ID)
	}
	require.Equal(t, []string{"sitePicker", "serviceDeskPicker", "requestTypePicker"}, unitIDs)
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

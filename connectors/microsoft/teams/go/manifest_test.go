// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

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
				AuthorizationEndpoint   string            `yaml:"authorizationEndpoint"`
				TokenEndpoint           string            `yaml:"tokenEndpoint"`
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
					Parameters []struct {
						Name     string `yaml:"name"`
						Location string `yaml:"location"`
						Target   string `yaml:"target"`
					} `yaml:"parameters"`
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
	require.Equal(t, []string{"endpoint", "maxResponseBytes", "maxMessageBytes", "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, names,
		"a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.NotContains(t, field.Description, "Q~", "never write a client secret sample")
			}
		})
	}
	descriptions := map[string]string{}
	for _, field := range visibleFields {
		descriptions[field.Name] = field.Description
	}
	require.Contains(t, descriptions["oauth_client_id"], "https://entra.microsoft.com")
	require.Contains(t, descriptions["oauth_client_id"], "Non-secret")
	require.Contains(t, descriptions["oauth_client_secret"], "Certificates & secrets")
	require.Contains(t, descriptions["oauth_client_secret"], "not the Secret ID")
	require.Contains(t, descriptions["access_token"], "never enter it manually")
	require.Contains(t, descriptions["maxMessageBytes"], "80 KB")
}

func TestOAuthSetupNamesTheAppRegistrationPermissionsAndAdminConsent(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", auth.OAuth2.AuthorizationEndpoint)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/token", auth.OAuth2.TokenEndpoint)
	require.Equal(t, []string{
		"Team.ReadBasic.All", "Channel.ReadBasic.All", "ChannelMessage.Send", "ChannelMessage.Read.All",
		"ChatMessage.Send", "Chat.ReadBasic", "offline_access",
	}, auth.OAuth2.Scopes, "short canonical names match the scope string Microsoft returns")
	require.True(t, auth.OAuth2.PKCE, "Microsoft recommends PKCE for confidential clients too")
	require.Empty(t, auth.OAuth2.AuthorizationParameters)
	require.True(t, strings.HasPrefix(auth.Guide.StartURL, "https://entra.microsoft.com/"))
	guide := strings.Join(auth.Guide.Steps, " ")
	for _, text := range []string{
		"App registrations", "multitenant", "AADSTS50194", "Redirect URI", "Web", "Application (client) ID", "Client secrets",
		"Delegated permissions", "administrator consent", "Grant admin consent", "Consent on behalf of your organization",
		"Need admin approval", "work or school account", "Personal Microsoft accounts are not supported",
	} {
		require.Contains(t, guide, text)
	}
	for _, scope := range auth.OAuth2.Scopes {
		require.Contains(t, guide, scope, "the guide names every requested scope")
	}
	for _, scope := range []string{"openid", "profile", "email"} {
		require.NotContains(t, auth.OAuth2.Scopes, scope)
	}
}

func TestStudioCommandsAreReadOnlyAndPinnedToMicrosoftGraph(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	commandURLs := map[string]string{}
	for _, command := range studio.Commands {
		require.Equal(t, "GET", command.Request.Method, command.ID)
		require.True(t, strings.HasPrefix(command.Request.URL, "https://graph.microsoft.com/v1.0/"), command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field, command.ID)
		require.Equal(t, "bearer", command.Request.Credential.Scheme, command.ID)
		commandURLs[command.ID] = command.Request.URL
	}
	require.Equal(t, map[string]string{
		"listJoinedTeams": "https://graph.microsoft.com/v1.0/me/joinedTeams",
		"listChannels":    "https://graph.microsoft.com/v1.0/teams/{teamId}/channels",
		"listChats":       "https://graph.microsoft.com/v1.0/me/chats",
	}, commandURLs)
	require.Equal(t, map[string]string{"$select": "id,displayName,membershipType"}, studio.Commands[1].Request.FixedQuery)
	require.Equal(t, map[string]string{"$expand": "members", "$top": "50"}, studio.Commands[2].Request.FixedQuery)
	var unitIDs []string
	for _, unit := range studio.Units {
		require.GreaterOrEqual(t, len(unit.Description), 120, unit.ID)
		unitIDs = append(unitIDs, unit.ID)
	}
	require.Equal(t, []string{"teamPicker", "channelPicker", "chatPicker"}, unitIDs)
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

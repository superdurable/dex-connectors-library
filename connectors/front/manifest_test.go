// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex/sdk-go/dex"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type        string          `yaml:"type"`
			Refreshable bool            `yaml:"refreshable"`
			Methods     []any           `yaml:"methods"`
			Fields      []guidanceField `yaml:"fields"`
			Guide       struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
			OAuth2 any `yaml:"oauth2"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID         string `yaml:"id"`
				Capability string `yaml:"capability"`
				Request    struct {
					Method     string `yaml:"method"`
					URL        string `yaml:"url"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
					} `yaml:"credential"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
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
	require.Equal(t, []string{"maxResponseBytes", "api_token"}, names, "a new visible field needs its own guidance review")
	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
			}
		})
	}
	token := manifest.Spec.Auth.Fields[0]
	require.Equal(t, "secretString", token.Type)
	require.True(t, strings.HasPrefix(token.Description, "Secret"), "secret fields say so first")
	for _, guidance := range []string{"https://app.frontapp.com/", "Settings > Developers > API Tokens", "Create API token", "eyJ",
		"stored only in this credential field", "https://api2.frontapp.com", "without a restart"} {
		require.Contains(t, token.Description, guidance)
	}
}

func TestAPITokenGuideCoversCreationPermissionsAndRevocation(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.False(t, manifest.Spec.Auth.Refreshable, "Front API tokens do not expire")
	require.Empty(t, manifest.Spec.Auth.Methods, "one API-token method; see the README for why Front OAuth is not declared")
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://app.frontapp.com/", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"company admin", "Settings > Developers", "API Tokens", "Create API token", "Access resources", "Shared resources",
		"All shared workspaces", "Global resources", "Private resources", "conversations, messages, comments, contacts, inboxes, tags, and teammates",
		"write on conversations and comments", "send on messages", "Delete",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestPickerCommandsKeepTheTokenOnFrontsAPIHost pins each Studio command to its read-only list endpoint.
func TestPickerCommandsKeepTheTokenOnFrontsAPIHost(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	urls := map[string]string{}
	for _, command := range studio.Commands {
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, "api_token", command.Request.Credential.Field)
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
		urls[command.ID+" "+command.Capability] = command.Request.URL
	}
	require.Equal(t, map[string]string{
		"listInboxes front.inboxes-list":     "https://api2.frontapp.com/inboxes",
		"listTeammates front.teammates-list": "https://api2.frontapp.com/teammates",
		"listTags front.tags-list":           "https://api2.frontapp.com/tags",
	}, urls)
	tags := studio.Commands[2]
	require.Equal(t, map[string]string{"limit": "100"}, tags.Request.FixedQuery)
	require.Len(t, tags.Request.Parameters, 1)
	require.Equal(t, "page_token", tags.Request.Parameters[0].Target)
	units := map[string]string{}
	for _, unit := range studio.Units {
		units[unit.ID] = unit.Description
	}
	require.Contains(t, units[front.UIUnitInboxPicker], "stores the stable inbox ID")
	require.Contains(t, units[front.UIUnitTeammatePicker], "stores the stable teammate ID")
	require.Contains(t, units[front.UIUnitTagPicker], "stores the stable tag ID")
	_, err := os.Stat("ui/package-lock.json")
	require.NoError(t, err, "a manifest that declares Studio units ships a ui bundle")
}

func TestOnlyTheReplyRunsWithSyncDurability(t *testing.T) {
	require.Equal(t, dex.StepDurabilitySync, front.ReplyToConversationDefinition.StepDefaults.ExecuteDurability)
	for _, durability := range []dex.StepDurability{
		front.SearchConversationsDefinition.StepDefaults.ExecuteDurability,
		front.GetConversationDefinition.StepDefaults.ExecuteDurability,
		front.UpdateConversationDefinition.StepDefaults.ExecuteDurability,
		front.FindContactByEmailDefinition.StepDefaults.ExecuteDurability,
	} {
		require.Equal(t, dex.StepDurabilityAsync, durability)
	}
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

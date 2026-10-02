// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

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

type manifestCommand struct {
	ID         string `yaml:"id"`
	Capability string `yaml:"capability"`
	Request    struct {
		URL        string            `yaml:"url"`
		FixedQuery map[string]string `yaml:"fixedQuery"`
	} `yaml:"request"`
}

type excelManifest struct {
	Spec struct {
		Provider      string `yaml:"provider"`
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Methods []struct {
				ID     string          `yaml:"id"`
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
			Commands []manifestCommand `yaml:"commands"`
		} `yaml:"studio"`
		Operations []struct {
			Name      string `yaml:"name"`
			Execution struct {
				Durability string `yaml:"durability"`
			} `yaml:"execution"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

func loadExcelManifest(t *testing.T) excelManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest excelManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Dex Web matches granted scopes literally, and Microsoft returns Graph permissions in short form.
func TestOAuthUsesTheOrganizationsEndpointAndShortGraphScopes(t *testing.T) {
	manifest := loadExcelManifest(t)
	require.Equal(t, "microsoft", manifest.Spec.Provider)
	require.Len(t, manifest.Spec.Auth.Methods, 1, "Microsoft documents no application permission for the workbook API")
	method := manifest.Spec.Auth.Methods[0]
	require.Equal(t, OAuthAuthMethodID, method.ID)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", method.OAuth2.AuthorizationEndpoint)
	require.Equal(t, microsoftTokenEndpoint, method.OAuth2.TokenEndpoint)
	require.Equal(t, append([]string{"offline_access"}, graphPermissions...), method.OAuth2.Scopes)
	require.Equal(t, strings.Join(method.OAuth2.Scopes, " "), refreshScope)
	for _, scope := range method.OAuth2.Scopes {
		require.NotContains(t, scope, "https://", "scopes use the short form Microsoft returns")
	}
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadExcelManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{"maxResponseBytes": int(defaults.MaxResponseBytes), "maxCells": int(defaults.MaxCells)}
	require.Len(t, manifest.Spec.Configuration.Fields, len(defaultByField))
	for _, field := range manifest.Spec.Configuration.Fields {
		require.Equal(t, defaultByField[field.Name], field.Default, field.Name)
		require.NotContains(t, field.Description, fmt.Sprint(field.Default), "%s repeats its displayed default", field.Name)
		require.Contains(t, field.Description, "only", "%s explains when an override is useful", field.Name)
	}
	method := manifest.Spec.Auth.Methods[0]
	require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://entra.microsoft.com/"))
	guide := strings.Join(method.Guide.Steps, "\n")
	for _, required := range []string{"Multiple Entra ID tenants", "AADSTS50194", "Redirect URI", "http://localhost", "Files.ReadWrite.All", "offline_access", "Grant admin consent", "New client secret"} {
		require.Contains(t, guide, required)
	}
	for _, field := range method.Fields {
		if field.Name == "access_token" || field.Name == "refresh_token" {
			require.Contains(t, field.Description, "automatically", "%s is an authorization output", field.Name)
			continue
		}
		require.Contains(t, field.Description, "https://entra.microsoft.com", field.Name)
		require.Contains(t, field.Description, "blank", field.Name)
		require.Equal(t, field.Name == "oauth_client_secret", field.Type == "secretString", field.Name)
	}
}

func TestCellLimitDescriptionMatchesTheEnforcedCeiling(t *testing.T) {
	for _, field := range loadExcelManifest(t).Spec.Configuration.Fields {
		if field.Name == "maxCells" {
			require.Equal(t, 100_000, MaximumCellsLimit)
			require.Contains(t, field.Description, "no larger than 100,000")
			return
		}
	}
	t.Fatal("maxCells is not declared")
}

// Studio commands run on a fixed host, and Dex Web path parameters accept only unreserved characters.
func TestPickerCommandsStayOnGraphAndBoundTheirResponses(t *testing.T) {
	commands := loadExcelManifest(t).Spec.Studio.Commands
	require.Len(t, commands, 4)
	for _, command := range commands {
		require.True(t, strings.HasPrefix(command.Request.URL, graphBaseURL+"/"), command.ID)
		require.NotEmpty(t, command.Request.FixedQuery["$select"], command.ID)
		require.True(t, strings.HasPrefix(command.Capability, "microsoft.excel."), command.ID)
	}
	require.Equal(t, graphBaseURL+"/drives/b!{driveKey}/items/{workbookId}/workbook/worksheets", commands[2].Request.URL)
	require.Equal(t, graphBaseURL+"/drives/b!{driveKey}/items/{workbookId}/workbook/tables", commands[3].Request.URL)
}

// appendTableRows has no provider idempotency key, so only sync durability keeps Dex from dispatching it twice.
func TestOnlyAppendTableRowsRunsWithSyncDurability(t *testing.T) {
	durabilityByOperation := map[string]string{}
	for _, operation := range loadExcelManifest(t).Spec.Operations {
		durabilityByOperation[operation.Name] = operation.Execution.Durability
	}
	require.Equal(t, map[string]string{
		"getTableRows": "async", "getValues": "async", "updateValues": "async", "appendTableRows": "sync",
	}, durabilityByOperation)
}

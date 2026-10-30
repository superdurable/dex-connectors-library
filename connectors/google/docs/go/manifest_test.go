// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

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
	ID      string `yaml:"id"`
	Request struct {
		URL        string            `yaml:"url"`
		FixedQuery map[string]string `yaml:"fixedQuery"`
	} `yaml:"request"`
}

type docsManifest struct {
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

func loadDocsManifest(t *testing.T) docsManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest docsManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Google reports alias grants under canonical URIs, and Dex Web matches granted scopes literally.
func TestOAuthScopesAreCanonicalAndMatchTheRefreshDriver(t *testing.T) {
	manifest := loadDocsManifest(t)
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
	require.Equal(t, docsOAuthScopes, methodScopes[GoogleOAuthAuthMethodID])
	for _, scope := range docsOAuthScopes {
		require.True(t, strings.HasPrefix(scope, "https://www.googleapis.com/auth/"), scope)
		require.Contains(t, delegationGuide, strings.TrimPrefix(scope, "https://www.googleapis.com/auth/"))
	}
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadDocsManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"docsEndpoint": defaults.DocsEndpoint, "driveEndpoint": defaults.DriveEndpoint,
		"maxResponseBytes": int(defaults.MaxResponseBytes), "maxTextBytes": int(defaults.MaxTextBytes),
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
		require.Contains(t, guide, "Google Docs API", method.ID)
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
}

func TestTextLimitDescriptionMatchesTheEnforcedCeiling(t *testing.T) {
	manifest := loadDocsManifest(t)
	for _, field := range manifest.Spec.Configuration.Fields {
		if field.Name == "maxTextBytes" {
			require.Equal(t, 4<<20, maxTextBytesLimit)
			require.Contains(t, field.Description, "4 MiB")
			return
		}
	}
	t.Fatal("maxTextBytes is not declared")
}

func TestPickerCommandsListOnlyNonTrashedDocumentsAndFoldersInEveryDrive(t *testing.T) {
	manifest := loadDocsManifest(t)
	commands := map[string]manifestCommand{}
	for _, command := range manifest.Spec.Studio.Commands {
		commands[command.ID] = command
	}
	require.Len(t, commands, 2)
	require.Equal(t, "mimeType = '"+googleDocumentMimeType+"' and trashed = false", commands["listDocuments"].Request.FixedQuery["q"])
	require.Equal(t, "modifiedTime desc", commands["listDocuments"].Request.FixedQuery["orderBy"])
	require.Equal(t, "mimeType = 'application/vnd.google-apps.folder' and trashed = false", commands["listFolders"].Request.FixedQuery["q"])
	for id, command := range commands {
		require.Equal(t, DefaultConfig().DriveEndpoint+driveAPIPath+"/files", command.Request.URL, id)
		require.Equal(t, "allDrives", command.Request.FixedQuery["corpora"], id)
		require.Equal(t, "true", command.Request.FixedQuery["supportsAllDrives"], id)
		require.Equal(t, "true", command.Request.FixedQuery["includeItemsFromAllDrives"], id)
	}
}

// createDocument has no provider idempotency key, so only sync durability keeps Dex from dispatching it twice.
func TestOnlyCreateDocumentRunsWithSyncDurability(t *testing.T) {
	manifest := loadDocsManifest(t)
	durabilityByOperation := map[string]string{}
	for _, operation := range manifest.Spec.Operations {
		durabilityByOperation[operation.Name] = operation.Execution.Durability
	}
	require.Equal(t, map[string]string{
		"getDocumentText": "async", "createDocument": "sync", "replaceDocumentText": "async", "appendText": "async",
	}, durabilityByOperation)
}

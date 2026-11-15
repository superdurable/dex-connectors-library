// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

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

type driveManifest struct {
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
		Triggers []struct {
			Name              string `yaml:"name"`
			EventType         string `yaml:"eventType"`
			ConfigurationType string `yaml:"configurationType"`
		} `yaml:"triggers"`
		Studio struct {
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
				Outputs     []struct {
					Name string `yaml:"name"`
				} `yaml:"outputs"`
			} `yaml:"units"`
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
				} `yaml:"request"`
			} `yaml:"commands"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func loadDriveManifest(t *testing.T) driveManifest {
	t.Helper()
	contents, err := os.ReadFile("../connector.yaml")
	require.NoError(t, err)
	var manifest driveManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// Google reports alias grants under canonical URIs, and Dex Web matches granted scopes literally.
func TestOAuthScopesAreCanonicalAndMatchTheRefreshDriver(t *testing.T) {
	manifest := loadDriveManifest(t)
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
	require.Equal(t, driveOAuthScopes, methodScopes[GoogleOAuthAuthMethodID])
	for _, scope := range driveOAuthScopes {
		require.True(t, strings.HasPrefix(scope, "https://www.googleapis.com/auth/drive."), scope)
		require.Contains(t, delegationGuide, strings.TrimPrefix(scope, "https://www.googleapis.com/auth/"))
	}
}

// Dex Web renders these descriptions and guides as the only setup instructions.
func TestConfigurationSurfaceGuidesEveryVisibleField(t *testing.T) {
	manifest := loadDriveManifest(t)
	defaults := DefaultConfig()
	defaultByField := map[string]any{
		"endpoint": defaults.Endpoint, "maxResponseBytes": int(defaults.MaxResponseBytes), "maxTextBytes": int(defaults.MaxTextBytes),
		"maxUploadBytes": int(defaults.MaxUploadBytes), "searchPageSize": int(defaults.SearchPageSize),
		"pollInterval": defaults.PollInterval.String(), "pollPageSize": int(defaults.PollPageSize),
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
		require.Contains(t, guide, "Google Drive API", method.ID)
		for _, field := range method.Fields {
			isDerived := field.Name == "access_token" || field.Name == "refresh_token"
			if isDerived {
				require.Contains(t, field.Description, "automatically", "%s/%s is an authorization output", method.ID, field.Name)
				continue
			}
			require.Contains(t, field.Description, "https://", "%s/%s names where to start", method.ID, field.Name)
			require.Contains(t, field.Description, "blank", "%s/%s explains blank", method.ID, field.Name)
			require.Equal(t, strings.HasPrefix(field.Name, "oauth_client_secret") || field.Name == "service_account_key", field.Type == "secretString", "%s/%s secrecy", method.ID, field.Name)
		}
		if method.Type == "oauth2" {
			require.Contains(t, guide, "Redirect URI", method.ID)
			require.Contains(t, guide, "test users", method.ID)
			require.Contains(t, guide, "restricted scope", method.ID)
		}
	}
}

func TestFolderPickerCommandListsOnlyNonTrashedFolders(t *testing.T) {
	manifest := loadDriveManifest(t)
	require.Len(t, manifest.Spec.Studio.Commands, 1)
	command := manifest.Spec.Studio.Commands[0]
	require.Equal(t, "listFolders", command.ID)
	require.Equal(t, DefaultConfig().Endpoint+driveAPIPath+"/files", command.Request.URL)
	require.Equal(t, "mimeType = 'application/vnd.google-apps.folder' and trashed = false", command.Request.FixedQuery["q"])
	require.Equal(t, "allDrives", command.Request.FixedQuery["corpora"])
	require.Equal(t, "true", command.Request.FixedQuery["supportsAllDrives"])
	require.Equal(t, "true", command.Request.FixedQuery["includeItemsFromAllDrives"])
}

// The three file Triggers share one event and configuration shape, so one feed serves them all.
func TestFileTriggersShareTheChangeFeedShapesAndGuidedUnits(t *testing.T) {
	manifest := loadDriveManifest(t)
	var triggerNames []string
	for _, trigger := range manifest.Spec.Triggers {
		triggerNames = append(triggerNames, trigger.Name)
		require.Equal(t, "FileEvent", trigger.EventType, trigger.Name)
		require.Equal(t, "FileTriggerConfiguration", trigger.ConfigurationType, trigger.Name)
	}
	require.Equal(t, []string{fileCreatedTriggerName, fileUpdatedTriggerName, fileTrashedTriggerName}, triggerNames)
	unitOutputs := map[string][]string{}
	for _, unit := range manifest.Spec.Studio.Units {
		require.Contains(t, unit.Description, "blank", unit.ID)
		for _, output := range unit.Outputs {
			unitOutputs[unit.ID] = append(unitOutputs[unit.ID], output.Name)
		}
		if unit.ID == UIUnitMimeTypePicker {
			require.Contains(t, unit.Description, "https://developers.google.com/workspace/drive/api/guides/mime-types")
		}
	}
	require.Equal(t, []string{UIFolderPickerPortFolderID, UIFolderPickerPortFolderName}, unitOutputs[UIUnitFolderPicker])
	require.Equal(t, []string{UIMimeTypePickerPortMimeType}, unitOutputs[UIUnitMimeTypePicker])
}

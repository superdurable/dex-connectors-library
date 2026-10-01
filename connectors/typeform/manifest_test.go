// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Description string   `yaml:"description"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

type typeformManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID     string          `yaml:"id"`
				Type   string          `yaml:"type"`
				Fields []manifestField `yaml:"fields"`
				Guide  struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"methods"`
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
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID          string `yaml:"id"`
				Description string `yaml:"description"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

func readTypeformManifest(t *testing.T) typeformManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest typeformManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// requiredScopes are every scope an operation, the picker, or upsertWebhook needs.
var requiredScopes = []string{"forms:read", "responses:read", "webhooks:write"}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connectors shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readTypeformManifest(t)
	require.Equal(t, typeform.PersonalAccessTokenAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 1)
	method := manifest.Spec.Auth.Methods[0]
	require.Equal(t, "apiKey", method.Type)
	visibleFields := []string{}
	for _, field := range method.Fields {
		visibleFields = append(visibleFields, field.Name)
		require.Equal(t, "secretString", field.Type, field.Name)
		require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "%s: a label is not guidance", field.Name)
		require.Contains(t, field.Description, "https://", "%s names where to start", field.Name)
		require.Contains(t, field.Description, "Secret", "%s says it is secret", field.Name)
		require.True(t, field.Required || strings.Contains(field.Description, "Blank"), "%s says what blank means", field.Name)
	}
	require.Equal(t, []string{"access_token", "webhook_secret"}, visibleFields)
	require.Len(t, method.Guide.Steps, 4)
	for _, step := range method.Guide.Steps {
		require.GreaterOrEqual(t, len(strings.Fields(step)), 15)
	}
	guide := strings.Join(method.Guide.Steps, " ")
	for _, scope := range requiredScopes {
		require.Contains(t, guide, scope)
		require.Contains(t, method.Fields[0].Description, scope)
	}
	startURL, err := url.Parse(method.Guide.StartURL)
	require.NoError(t, err)
	require.Equal(t, "https://admin.typeform.com/user/tokens", startURL.String())

	visibleConfiguration := []string{}
	for _, field := range manifest.Spec.Configuration.Fields {
		visibleConfiguration = append(visibleConfiguration, field.Name)
		require.False(t, field.Required, field.Name)
		require.Contains(t, field.Description, "Blank uses the default", field.Name)
		require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, field.Name)
		if field.Type == "integer" {
			require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web shows the default itself")
		}
	}
	require.Equal(t, []string{"dataCenter", "maxResponseBytes", "webhookMaxBodyBytes"}, visibleConfiguration)
	defaults := typeform.DefaultConfig()
	require.Equal(t, typeform.DataCenterUs, defaults.DataCenter)
	require.EqualValues(t, 1<<20, defaults.MaxResponseBytes)
	require.EqualValues(t, 1<<20, defaults.WebhookMaxBodyBytes)
}

// TestDataCenterGuidanceNamesEachHost keeps the enum description aligned with the hosts the client uses.
func TestDataCenterGuidanceNamesEachHost(t *testing.T) {
	field := readTypeformManifest(t).Spec.Configuration.Fields[0]
	require.Equal(t, []string{"us", "eu", "newEu"}, field.Enum)
	for value, host := range map[string]string{"us": "https://api.typeform.com", "eu": "https://api.eu.typeform.com", "newEu": "https://api.typeform.eu"} {
		require.Contains(t, field.Description, value+" is "+host)
	}
	require.Contains(t, field.Description, "returns none instead of an error", "the silent wrong-region failure is explained")
}

func TestStudioCommandsReadOnlyTheFixedTypeformFormListHosts(t *testing.T) {
	studio := readTypeformManifest(t).Spec.Studio
	hosts := []string{}
	for _, command := range studio.Commands {
		target, err := url.Parse(command.Request.URL)
		require.NoError(t, err)
		hosts = append(hosts, target.Host)
		require.Equal(t, "/forms", target.Path, command.ID)
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, "typeform.forms-list", command.Capability)
		require.Equal(t, "access_token", command.Request.Credential.Field)
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
	}
	require.Equal(t, []string{"api.typeform.com", "api.typeform.eu"}, hosts, "api.eu.typeform.com accounts list forms on api.typeform.com")
	require.Len(t, studio.Units, 1)
	require.Equal(t, typeform.UIUnitFormPicker, studio.Units[0].ID)
	require.Contains(t, studio.Units[0].Description, "stores the form ID")
}

func TestCredentialsAndConnectionsNeverRenderSecrets(t *testing.T) {
	credentials := typeform.Credentials{
		AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		WebhookSecret: sdkgo.NewSecretString(sentinelSecret),
	}
	for _, rendered := range []string{fmt.Sprint(credentials), fmt.Sprintf("%+v", credentials), fmt.Sprintf("%#v", credentials)} {
		require.NotContains(t, rendered, sentinelToken)
		require.NotContains(t, rendered, sentinelSecret)
	}
	connection, err := typeform.NewConnection(newTestClient(t, nil, personalAccessTokenCredentials(sentinelSecret), typeform.Config{}), testConnection)
	require.NoError(t, err)
	require.Equal(t, "typeform.Connection{[REDACTED]}", fmt.Sprint(connection))
	_, err = connection.MarshalJSON()
	require.Error(t, err)
}

func TestNewRejectsAnUnknownDataCenter(t *testing.T) {
	_, err := typeform.New(typeform.Config{DataCenter: "asia"}, personalAccessTokenCredentials(""))
	require.ErrorContains(t, err, "dataCenter")
}

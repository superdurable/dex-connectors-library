// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connections page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			MethodLabel   string `yaml:"methodLabel"`
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
				ID          string          `yaml:"id"`
				DisplayName string          `yaml:"displayName"`
				Description string          `yaml:"description"`
				Fields      []guidanceField `yaml:"fields"`
				Guide       struct {
					StartURL string   `yaml:"startURL"`
					Steps    []string `yaml:"steps"`
				} `yaml:"guide"`
				OAuth2 struct {
					AuthorizationEndpoint   string            `yaml:"authorizationEndpoint"`
					TokenEndpoint           string            `yaml:"tokenEndpoint"`
					AuthorizationParameters map[string]string `yaml:"authorizationParameters"`
					Scopes                  []string          `yaml:"scopes"`
					PKCE                    bool              `yaml:"pkce"`
					CredentialMappings      []struct {
						Credential string `yaml:"credential"`
						Source     string `yaml:"source"`
					} `yaml:"credentialMappings"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
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

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
	StudioUnit  *struct {
		Unit string `yaml:"unit"`
		Port string `yaml:"port"`
	} `yaml:"studioUnit"`
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		require.Len(t, method.Fields, 4, method.ID)
		visibleFields = append(visibleFields, method.Fields...)
	}
	var names []string
	for _, field := range visibleFields[:6] {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"orgId", "maxResponseBytes", "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"}, names,
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
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret") || strings.HasPrefix(field.Description, "Maximum"))
			}
		})
	}
	orgID := manifest.Spec.Configuration.Fields[0]
	require.False(t, orgID.Required, "Dex Web renders the picker only for a field the first save can leave blank")
	require.Equal(t, "organizationPicker", orgID.StudioUnit.Unit)
	require.Equal(t, "orgId", orgID.StudioUnit.Port)
	for _, text := range []string{"orgId header", "organization picker", "consent screen", "OAUTH_ORG_MISMATCH", "Setup > Developer Space > API", "refuses to start", "2389290"} {
		require.Contains(t, orgID.Description, text)
	}
	for _, field := range visibleFields[2:6] {
		if field.Name == "access_token" || field.Name == "refresh_token" {
			require.Contains(t, field.Description, "never enter it manually", "OAuth outputs are not manual inputs")
		}
	}
}

func TestEveryDataCenterMethodAuthorizesAtItsOwnZohoAccountsServer(t *testing.T) {
	auth := readGuidanceManifest(t).Spec.Auth
	require.Equal(t, "Data center", auth.MethodLabel)
	require.Equal(t, desk.USDataCenterAuthMethodID, auth.DefaultMethod)
	dataCenters := desk.DataCenters()
	require.Len(t, auth.Methods, len(dataCenters))
	for index, method := range auth.Methods {
		dataCenter := dataCenters[index]
		t.Run(method.ID, func(t *testing.T) {
			require.Equal(t, dataCenter.AuthMethodID, method.ID)
			require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/auth", method.OAuth2.AuthorizationEndpoint)
			require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/token", method.OAuth2.TokenEndpoint)
			require.Equal(t, []string{"Desk.tickets.READ", "Desk.tickets.CREATE", "Desk.tickets.UPDATE", "Desk.search.READ", "Desk.basic.READ"}, method.OAuth2.Scopes)
			require.Equal(t, map[string]string{"access_type": "offline", "prompt": "consent"}, method.OAuth2.AuthorizationParameters)
			require.False(t, method.OAuth2.PKCE, "Zoho documents no PKCE for server-based clients")
			require.Len(t, method.OAuth2.CredentialMappings, 2, "api_domain is not retained: Zoho Desk is served from desk hosts, not api_domain")

			consoleURL := strings.Replace(dataCenter.AccountsURL, "https://accounts.", "https://api-console.", 1)
			require.Equal(t, consoleURL, method.Guide.StartURL)
			require.Contains(t, method.Description, dataCenter.DeskURL)
			require.Contains(t, method.Description, dataCenter.AccountsURL)
			guide := strings.Join(method.Guide.Steps, " ")
			for _, text := range []string{consoleURL, dataCenter.AccountsURL, "Server-based Applications", "Redirect URI", "Authorized Redirect URIs",
				"Client Secret tab", "Settings tab", "Desk.search.READ", "Desk.basic.READ", "consent", "orgId", "revoke"} {
				require.Contains(t, guide, text)
			}
			for _, other := range dataCenters {
				if other.AuthMethodID != dataCenter.AuthMethodID {
					require.NotContains(t, guide+method.Description, other.AccountsURL+" ", "a method names only its own data center")
				}
			}
		})
	}
}

func TestStudioCommandsListOrganizationsOnlyOnEachDataCentersOwnHost(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	dataCenters := desk.DataCenters()
	require.Len(t, studio.Commands, len(dataCenters))
	for index, command := range studio.Commands {
		dataCenter := dataCenters[index]
		require.Equal(t, dataCenter.StudioOrganizationsCommandID, command.ID)
		require.Equal(t, "zohodesk.organizations-list", command.Capability)
		require.Equal(t, "GET", command.Request.Method, command.ID)
		require.Equal(t, dataCenter.DeskURL+"/api/v1/organizations", command.Request.URL, command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field, command.ID)
		require.Equal(t, "bearer", command.Request.Credential.Scheme, command.ID)
		parsed, err := url.Parse(command.Request.URL)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(parsed.Host, "desk."), command.ID)
	}
	require.Contains(t, studio.Setup.BackendCapabilities, "zohodesk.organizations-list")
	require.Len(t, studio.Units, 1)
	require.GreaterOrEqual(t, len(studio.Units[0].Description), 120)
	require.Contains(t, studio.Units[0].Description, "own data center")

	bundleProvider, err := os.ReadFile("ui/src/provider.ts")
	require.NoError(t, err)
	for _, dataCenter := range dataCenters {
		require.Contains(t, string(bundleProvider), fmt.Sprintf(`%q: %q`, dataCenter.AuthMethodID, dataCenter.StudioOrganizationsCommandID),
			"the bundle runs only the command of the connection's data center")
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

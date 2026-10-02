// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders and the runtime relies on.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			DefaultMethod string         `yaml:"defaultMethod"`
			Methods       []guidanceAuth `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID         string `yaml:"id"`
				Capability string `yaml:"capability"`
				Request    struct {
					Method     string            `yaml:"method"`
					URL        string            `yaml:"url"`
					Credential map[string]string `yaml:"credential"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
				} `yaml:"request"`
			} `yaml:"commands"`
			Units []struct {
				ID      string `yaml:"id"`
				Outputs []struct {
					Name string `yaml:"name"`
				} `yaml:"outputs"`
			} `yaml:"units"`
		} `yaml:"studio"`
		Triggers   []any `yaml:"triggers"`
		Operations []struct {
			Name          string `yaml:"name"`
			Kind          string `yaml:"kind"`
			Authorization string `yaml:"authorization"`
			Branches      []struct {
				ID       string `yaml:"id"`
				Optional bool   `yaml:"optional"`
			} `yaml:"branches"`
			Execution struct {
				Durability           string `yaml:"durability"`
				ExecuteMethodTimeout string `yaml:"executeMethodTimeout"`
				HeartbeatTimeout     string `yaml:"heartbeatTimeout"`
			} `yaml:"execution"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

type guidanceAuth struct {
	ID            string          `yaml:"id"`
	Type          string          `yaml:"type"`
	Recommended   bool            `yaml:"recommended"`
	Fields        []guidanceField `yaml:"fields"`
	Configuration struct {
		Fields []guidanceField `yaml:"fields"`
	} `yaml:"configuration"`
	Guide struct {
		StartURL string   `yaml:"startURL"`
		Steps    []string `yaml:"steps"`
	} `yaml:"guide"`
	OAuth2 *struct {
		AuthorizationEndpoint string   `yaml:"authorizationEndpoint"`
		TokenEndpoint         string   `yaml:"tokenEndpoint"`
		Scopes                []string `yaml:"scopes"`
		PKCE                  bool     `yaml:"pkce"`
	} `yaml:"oauth2"`
}

type guidanceField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

// derivedCredentialFields are produced by Dex Web's OAuth exchange or by the refresh driver, never typed.
var derivedCredentialFields = map[string]bool{"access_token": true, "refresh_token": true}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	fields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	for _, method := range manifest.Spec.Auth.Methods {
		fields = append(fields, method.Fields...)
		fields = append(fields, method.Configuration.Fields...)
	}
	for _, field := range fields {
		description := field.Description
		require.GreaterOrEqual(t, len(description), 120, "field %s needs complete guidance", field.Name)
		require.True(t, strings.Contains(description, "Blank") || strings.Contains(description, "blank"), "field %s explains blank", field.Name)
		if field.Type == "secretString" {
			require.Contains(t, description, "Secret", "field %s says it is secret", field.Name)
		}
		if derivedCredentialFields[field.Name] {
			require.Contains(t, description, "never enter it manually", "field %s is an output", field.Name)
			continue
		}
		if field.Name != "maxResponseBytes" {
			require.Contains(t, description, "https://", "field %s names where to start", field.Name)
		}
		if field.Default != nil {
			require.NotContains(t, description, "4194304", "Dex Web shows the default itself")
		}
	}
}

func TestDelegatedOAuthUsesTheOrganizationsEndpointsAndLeastMailScopes(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, MicrosoftOAuthAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	delegated := manifest.Spec.Auth.Methods[0]
	require.Equal(t, MicrosoftOAuthAuthMethodID, delegated.ID)
	require.True(t, delegated.Recommended)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", delegated.OAuth2.AuthorizationEndpoint)
	require.Equal(t, delegatedTokenEndpoint, delegated.OAuth2.TokenEndpoint, "Dex Web and the refresh driver use one token endpoint")
	require.Equal(t, []string{"offline_access", "Mail.ReadWrite", "Mail.Send"}, delegated.OAuth2.Scopes,
		"short canonical names match Microsoft's returned scope string; offline_access yields the refresh token")
	require.Equal(t, delegatedRefreshScope, strings.Join(delegated.OAuth2.Scopes, " "))
	require.True(t, delegated.OAuth2.PKCE)
	guide := strings.Join(delegated.Guide.Steps, "\n")
	for _, required := range []string{"Multitenant", "AADSTS50194", "Redirect URI", "Mail.ReadWrite", "Mail.Send", "offline_access",
		"Grant admin consent", "Certificates & secrets", "Personal Microsoft accounts are not supported"} {
		require.Contains(t, guide, required)
	}
	require.True(t, strings.HasPrefix(delegated.Guide.StartURL, "https://entra.microsoft.com/"))
}

func TestAppOnlyGuideLimitsTheAppToOneMailbox(t *testing.T) {
	manifest := readGuidanceManifest(t)
	appOnly := manifest.Spec.Auth.Methods[1]
	require.Equal(t, AppOnlyAuthMethodID, appOnly.ID)
	require.Equal(t, "apiKey", appOnly.Type)
	require.Nil(t, appOnly.OAuth2, "the client credentials grant needs no consent screen")
	require.Equal(t, []string{"mailbox"}, []string{appOnly.Configuration.Fields[0].Name})
	require.True(t, appOnly.Configuration.Fields[0].Required)
	guide := strings.Join(appOnly.Guide.Steps, "\n")
	for _, required := range []string{"RBAC for Applications", "New-ServicePrincipal", "New-ManagementScope", "New-ManagementRoleAssignment",
		"Application Mail.ReadWrite", "Application Mail.Send", "Test-ServicePrincipalAuthorization", "every mailbox in the tenant",
		"https://graph.microsoft.com/.default", "https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac"} {
		require.Contains(t, guide, required)
	}
}

func TestStudioCommandsReadOnlyGraphMailFolders(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Len(t, manifest.Spec.Studio.Commands, 4)
	for _, command := range manifest.Spec.Studio.Commands {
		require.Equal(t, "GET", command.Request.Method)
		target, err := url.Parse(command.Request.URL)
		require.NoError(t, err)
		require.Equal(t, "https", target.Scheme)
		require.Equal(t, graphHost, target.Host, "a command never reaches another host")
		require.True(t, strings.HasPrefix(target.Path, graphAPIVersion+"/"))
		require.Contains(t, target.Path, "mailFolders")
		require.Equal(t, map[string]string{"field": "access_token", "scheme": "bearer"}, command.Request.Credential)
		require.Equal(t, "id,displayName,childFolderCount", command.Request.FixedQuery["$select"])
		require.Equal(t, "outlookmail.mail-folders-list", command.Capability)
	}
	require.Equal(t, "mailFolderPicker", manifest.Spec.Studio.Units[0].ID)
	require.Equal(t, UIMailFolderPickerPortFolderID, manifest.Spec.Studio.Units[0].Outputs[0].Name)
}

func TestOnlyTheSendsRunWithSyncDurabilityAndNoTriggersAreDeclared(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Empty(t, manifest.Spec.Triggers, "Graph change notifications need a validationToken handshake the SDK does not offer")
	for _, operation := range manifest.Spec.Operations {
		require.Equal(t, "required", operation.Authorization, operation.Name)
		required := 0
		for _, branch := range operation.Branches {
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, "%s has exactly one happy-path branch", operation.Name)
		switch operation.Name {
		case "sendMessage", "replyToMessage":
			require.Equal(t, "sync", operation.Execution.Durability, operation.Name)
			require.Equal(t, operation.Execution.ExecuteMethodTimeout, operation.Execution.HeartbeatTimeout,
				"a lost Worker is detected within one Execute timeout")
		default:
			require.Equal(t, "async", operation.Execution.Durability, operation.Name)
		}
	}
}

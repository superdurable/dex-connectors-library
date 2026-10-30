// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex/sdk-go/dex"
	"gopkg.in/yaml.v3"
)

type manifestField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
}

type docusignManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Refreshable   bool   `yaml:"refreshable"`
			DefaultMethod string `yaml:"defaultMethod"`
			Methods       []struct {
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
					PKCE                  bool     `yaml:"pkce"`
					CredentialMappings    []struct {
						Credential string `yaml:"credential"`
					} `yaml:"credentialMappings"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
	} `yaml:"spec"`
}

func readManifest(t *testing.T) docusignManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest docusignManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// TestConnectionFormFieldsCarryGuidance enumerates every field Dex Web Connections shows for this connector.
func TestConnectionFormFieldsCarryGuidance(t *testing.T) {
	manifest := readManifest(t)
	require.True(t, manifest.Spec.Auth.Refreshable)
	require.Equal(t, docusign.ProductionOAuthAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	visibleFields := map[string][]string{}
	for _, method := range manifest.Spec.Auth.Methods {
		mapped := map[string]bool{}
		for _, mapping := range method.OAuth2.CredentialMappings {
			mapped[mapping.Credential] = true
		}
		for _, field := range method.Fields {
			if mapped[field.Name] {
				require.Contains(t, field.Description, "never enter it manually", "%s is produced by OAuth", field.Name)
				continue
			}
			visibleFields[method.ID] = append(visibleFields[method.ID], field.Name)
			requireFieldGuidance(t, method.ID+"/"+field.Name, field)
		}
		require.GreaterOrEqual(t, len(method.Guide.Steps), 4, method.ID)
		for _, step := range method.Guide.Steps {
			require.GreaterOrEqual(t, len(strings.Fields(step)), 15, method.ID)
		}
		require.Contains(t, strings.Join(method.Guide.Steps, " "), "Redirect URI that Dex Web shows")
	}
	require.Equal(t, map[string][]string{
		docusign.ProductionOAuthAuthMethodID: {"oauth_client_id", "oauth_client_secret", "connect_hmac_key"},
		docusign.DeveloperOAuthAuthMethodID:  {"oauth_client_id", "oauth_client_secret", "connect_hmac_key"},
	}, visibleFields)
	for _, field := range manifest.Spec.Configuration.Fields {
		requireFieldGuidance(t, field.Name, field)
	}
}

func requireFieldGuidance(t *testing.T, name string, field manifestField) {
	t.Helper()
	require.GreaterOrEqual(t, len(strings.Fields(field.Description)), 25, "%s: a label is not guidance", name)
	if field.Type == "secretString" {
		require.Contains(t, field.Description, "Secret", "%s says it is secret", name)
	}
	if field.Default == nil {
		require.Contains(t, field.Description, "https://", "%s names where to start", name)
	} else {
		require.NotContains(t, field.Description, ")", "%s does not repeat its parenthesized default", name)
	}
	require.True(t, field.Required || strings.Contains(field.Description, "Blank"), "%s says what blank means", name)
}

// TestOAuthEndpointsMatchTheEnvironmentTheConnectorCalls keeps the manifest and the client in step.
func TestOAuthEndpointsMatchTheEnvironmentTheConnectorCalls(t *testing.T) {
	manifest := readManifest(t)
	accountServers := map[string]string{
		docusign.ProductionOAuthAuthMethodID: "https://account.docusign.com",
		docusign.DeveloperOAuthAuthMethodID:  "https://account-d.docusign.com",
	}
	require.Len(t, manifest.Spec.Auth.Methods, len(accountServers))
	for _, method := range manifest.Spec.Auth.Methods {
		server := accountServers[method.ID]
		require.Equal(t, server+"/oauth/auth", method.OAuth2.AuthorizationEndpoint)
		require.Equal(t, server+"/oauth/token", method.OAuth2.TokenEndpoint)
		require.Equal(t, []string{"signature", "extended"}, method.OAuth2.Scopes, "no impersonation: the connector has no JWT grant")
		require.False(t, method.OAuth2.PKCE, "the confidential client authenticates every exchange with its secret key")
		require.True(t, strings.HasPrefix(method.Guide.StartURL, "https://apps"), method.ID)
	}
}

// TestOperationExecutionDefaultsMatchTheirIdempotency keeps the only non-idempotent create on sync.
func TestOperationExecutionDefaultsMatchTheirIdempotency(t *testing.T) {
	require.Equal(t, dex.StepDurabilitySync, docusign.CreateEnvelopeFromTemplateDefinition.StepDefaults.ExecuteDurability)
	for _, durability := range []dex.StepDurability{
		docusign.GetEnvelopeDefinition.StepDefaults.ExecuteDurability, docusign.ListEnvelopeRecipientsDefinition.StepDefaults.ExecuteDurability,
		docusign.VoidEnvelopeDefinition.StepDefaults.ExecuteDurability, docusign.DownloadCombinedDocumentDefinition.StepDefaults.ExecuteDurability,
	} {
		require.Equal(t, dex.StepDurabilityAsync, durability)
	}
	require.Greater(t, docusign.DownloadCombinedDocumentDefinition.StepDefaults.ExecuteMethodTimeout,
		docusign.GetEnvelopeDefinition.StepDefaults.ExecuteMethodTimeout)
}

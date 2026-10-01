// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"gopkg.in/yaml.v3"
)

// guidanceManifest is the part of connector.yaml that Dex Web renders on the Connectors page.
type guidanceManifest struct {
	Spec struct {
		Configuration struct {
			Fields []guidanceField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type    string          `yaml:"type"`
			Methods []any           `yaml:"methods"`
			Fields  []guidanceField `yaml:"fields"`
			Guide   struct {
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
					FixedHeaders map[string]string `yaml:"fixedHeaders"`
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

var intercomURLPattern = regexp.MustCompile(`https://app(\.eu|\.au)?\.intercom\.com/[^\s;,)]*`)

// providerSourcedFields are the values a user copies from Intercom, with the format each must name.
var providerSourcedFields = map[string]string{
	"region":        "app.eu.intercom.com",
	"access_token":  "opaque string",
	"client_secret": "opaque string",
}

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append(append([]guidanceField(nil), manifest.Spec.Configuration.Fields...), manifest.Spec.Auth.Fields...)
	var names []string
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"region", "maxResponseBytes", "webhookMaxBodyBytes", "access_token", "client_secret"}, names,
		"a new visible field needs its own guidance review")

	for _, field := range visibleFields {
		t.Run(field.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 120, "a label-length description is not guidance")
			if field.Default != nil {
				defaultText := fmt.Sprint(field.Default)
				for _, phrase := range []string{defaultText + " bytes", "defaults to " + defaultText, "Blank uses " + defaultText, "(" + defaultText + ")"} {
					require.NotContains(t, field.Description, phrase, "Dex Web already shows the default in parentheses")
				}
				if field.Type == "integer" {
					require.NotContains(t, field.Description, defaultText)
				}
			}
			if !field.Required {
				require.Contains(t, strings.ToLower(field.Description), "blank", "optional fields explain what blank means")
			}
			format, isProviderSourced := providerSourcedFields[field.Name]
			if !isProviderSourced {
				return
			}
			require.Regexp(t, intercomURLPattern, field.Description, "provider values name the Intercom page to start from")
			require.Contains(t, field.Description, format)
			if field.Type == "secretString" {
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
				require.Contains(t, field.Description, "stored only in this credential field")
			} else {
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
		})
	}
}

func TestAccessTokenGuideCoversTheAppTokenSecretWebhooksAndRevocation(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, "apiKey", manifest.Spec.Auth.Type)
	require.Empty(t, manifest.Spec.Auth.Methods, "one access-token method; see the README for why Intercom OAuth cannot be declared")
	require.Nil(t, manifest.Spec.Auth.OAuth2)
	require.Equal(t, "https://app.intercom.com/a/apps/_/developer-hub", manifest.Spec.Auth.Guide.StartURL)
	guide := strings.Join(manifest.Spec.Auth.Guide.Steps, " ")
	for _, instruction := range []string{
		"app.eu.intercom.com", "app.au.intercom.com", "region field", "API Version", intercom.APIVersion,
		"Configure > Authentication", "Access Token", "Configure > Basic Information", "Client secret", "Configure > Webhooks",
		"HEAD request", "conversation.user.created", "Test & Publish > Your Workspaces", "Regenerate token", "Uninstall app",
	} {
		require.Contains(t, guide, instruction)
	}
}

// TestAdminPickerCommandsPinEveryRegionalHost keeps the token on Intercom's hosts and the version header current.
func TestAdminPickerCommandsPinEveryRegionalHost(t *testing.T) {
	studio := readGuidanceManifest(t).Spec.Studio
	urls := map[string]string{}
	for _, command := range studio.Commands {
		require.Equal(t, "intercom.admins-list", command.Capability)
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, "access_token", command.Request.Credential.Field)
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
		require.Equal(t, map[string]string{"Intercom-Version": intercom.APIVersion}, command.Request.FixedHeaders)
		urls[command.ID] = command.Request.URL
	}
	require.Equal(t, map[string]string{
		"listAdmins": "https://api.intercom.io/admins", "listAdminsEU": "https://api.eu.intercom.io/admins", "listAdminsAU": "https://api.au.intercom.io/admins",
	}, urls)
	units := map[string]string{}
	for _, unit := range studio.Units {
		units[unit.ID] = unit.Description
	}
	require.Contains(t, units[intercom.UIUnitAdminPicker], "stores the chosen admin's numeric ID")
	require.Contains(t, units[intercom.UIUnitConversationTopicPicker], "selecting none accepts every supported conversation topic")
	_, err := os.Stat("ui/package-lock.json")
	require.NoError(t, err, "a manifest that declares Studio units ships a ui bundle")
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

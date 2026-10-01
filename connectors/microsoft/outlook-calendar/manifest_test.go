// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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
			DefaultMethod string         `yaml:"defaultMethod"`
			Methods       []guidanceAuth `yaml:"methods"`
		} `yaml:"auth"`
		Studio struct {
			Commands []struct {
				ID      string `yaml:"id"`
				Request struct {
					URL        string            `yaml:"url"`
					FixedQuery map[string]string `yaml:"fixedQuery"`
					Credential struct {
						Field  string `yaml:"field"`
						Scheme string `yaml:"scheme"`
					} `yaml:"credential"`
				} `yaml:"request"`
			} `yaml:"commands"`
		} `yaml:"studio"`
		Triggers []any `yaml:"triggers"`
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

// entraClientSecretShape matches the detectable Entra secret format, which no file may contain.
var entraClientSecretShape = regexp.MustCompile(`[A-Za-z0-9_.~-]{3}[0-9]Q~[A-Za-z0-9_.~-]{30,}`)

func TestEveryVisibleConnectionFieldGuidesTheUser(t *testing.T) {
	manifest := readGuidanceManifest(t)
	visibleFields := append([]guidanceField(nil), manifest.Spec.Configuration.Fields...)
	var names []string
	for _, method := range manifest.Spec.Auth.Methods {
		visibleFields = append(visibleFields, method.Fields...)
		visibleFields = append(visibleFields, method.Configuration.Fields...)
	}
	for _, field := range visibleFields {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{
		"maxResponseBytes", "client_id", "client_secret", "access_token", "refresh_token",
		"client_id", "client_secret", "access_token", "tenantId", "mailbox",
	}, names, "a new visible field needs its own guidance review")
	for index, field := range visibleFields {
		t.Run(fmt.Sprintf("%d-%s", index, field.Name), func(t *testing.T) {
			require.GreaterOrEqual(t, len(field.Description), 150, "a label-length description is not guidance")
			if field.Default != nil {
				require.NotContains(t, field.Description, fmt.Sprint(field.Default), "Dex Web already shows the default in parentheses")
			}
			require.Contains(t, strings.ToLower(field.Description), "blank", "every field explains what blank means")
			switch {
			case field.Type == "secretString":
				require.True(t, strings.HasPrefix(field.Description, "Secret"), "secret fields say so first")
			case field.Name != "maxResponseBytes":
				require.True(t, strings.HasPrefix(field.Description, "Non-secret"), "non-secret fields say so first")
			}
			switch field.Name {
			case "access_token", "refresh_token":
				require.Contains(t, field.Description, "never enter it manually", "derived tokens are not manual inputs")
			case "client_id", "client_secret", "tenantId":
				require.Contains(t, field.Description, "https://entra.microsoft.com > Entra ID > App registrations")
			case "mailbox":
				require.Contains(t, field.Description, "https://admin.microsoft.com")
				require.Contains(t, field.Description, "/users/{mailbox}")
			}
		})
	}
	require.False(t, findGuidanceField(manifest.Spec.Auth.Methods[1].Fields, "access_token").Required, "the app-only token is requested by the application")
}

func TestAuthorizationGuidesCoverMultiTenantOAuthAndScopedAppOnlySetup(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Equal(t, outlookcalendar.MicrosoftOAuthAuthMethodID, manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)

	delegated := manifest.Spec.Auth.Methods[0]
	require.Equal(t, outlookcalendar.MicrosoftOAuthAuthMethodID, delegated.ID)
	require.True(t, delegated.Recommended)
	require.NotNil(t, delegated.OAuth2)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize", delegated.OAuth2.AuthorizationEndpoint)
	require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/token", delegated.OAuth2.TokenEndpoint)
	require.Equal(t, []string{"offline_access", "Calendars.ReadWrite"}, delegated.OAuth2.Scopes,
		"short Graph permission names, as the returned scope lists them; no openid, profile, or email")
	require.True(t, delegated.OAuth2.PKCE)
	delegatedGuide := strings.Join(delegated.Guide.Steps, " ")
	for _, instruction := range []string{"Accounts in any organizational directory (multitenant)", "AADSTS50194", "Redirect URI shown below",
		"http://localhost", "Delegated permissions", "Calendars.ReadWrite", "offline_access", "Value once", "Application (client) ID"} {
		require.Contains(t, delegatedGuide, instruction)
	}

	appOnly := manifest.Spec.Auth.Methods[1]
	require.Equal(t, outlookcalendar.AppOnlyAuthMethodID, appOnly.ID)
	require.Equal(t, "apiKey", appOnly.Type, "Dex Web saves the client ID and secret as a plain credential form")
	require.Nil(t, appOnly.OAuth2)
	appOnlyGuide := strings.Join(appOnly.Guide.Steps, " ")
	for _, instruction := range []string{"Directory (tenant) ID", "New-ServicePrincipal", "New-ManagementScope", "New-ManagementRoleAssignment",
		"Application Calendars.ReadWrite", "Test-ServicePrincipalAuthorization", "grants combine", "Grant admin consent", "every mailbox",
		"Application access policies", "leave access_token blank"} {
		require.Contains(t, appOnlyGuide, instruction)
	}
	require.Empty(t, manifest.Spec.Triggers, "change notifications need the validationToken handshake that webhooktrigger lacks")
}

func TestStudioCommandsAreBoundedReadsOfTheConnectionsOwnCalendars(t *testing.T) {
	manifest := readGuidanceManifest(t)
	require.Len(t, manifest.Spec.Studio.Commands, 2)
	for _, command := range manifest.Spec.Studio.Commands {
		require.True(t, strings.HasPrefix(command.Request.URL, "https://graph.microsoft.com/v1.0/"), command.ID)
		require.Equal(t, "access_token", command.Request.Credential.Field)
		require.Equal(t, "bearer", command.Request.Credential.Scheme)
		require.Equal(t, "id,name,canEdit,isDefaultCalendar,owner", command.Request.FixedQuery["$select"], "responses stay bounded")
	}
	require.Equal(t, "https://graph.microsoft.com/v1.0/me/calendars", manifest.Spec.Studio.Commands[0].Request.URL)
	require.Equal(t, "https://graph.microsoft.com/v1.0/users/{mailbox}/calendars", manifest.Spec.Studio.Commands[1].Request.URL)
}

func TestNoFileHoldsSomethingShapedLikeAnEntraClientSecret(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents, err := os.ReadFile(entry.Name())
		require.NoError(t, err)
		require.False(t, entraClientSecretShape.Match(contents), entry.Name())
	}
}

type createdEventTarget struct {
	dex.StepDefaultsNoWaitFor[outlookcalendar.CreateEventResult]
}

func (createdEventTarget) Execute(dex.Context, outlookcalendar.CreateEventResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateEventFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := outlookcalendar.NewConnection(newCalendarClient(t, "http://127.0.0.1:1"), calendarConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "calendar", GroupLabel: "Calendar", Explanation: "Create an event."}
	mapToInput := func(string) outlookcalendar.CreateEventInput { return outlookcalendar.CreateEventInput{} }
	require.NotPanics(t, func() {
		outlookcalendar.NewCreateEventStep(outlookcalendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: calendarConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdEventTarget{}),
		})
	})
	require.Panics(t, func() {
		outlookcalendar.NewCreateEventStep(outlookcalendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection,
			MapToOperationInput: mapToInput, ProviderRejected: sdkgo.GoTo(createdEventTarget{}),
		})
	})
	require.Panics(t, func() {
		outlookcalendar.NewCreateEventStep(outlookcalendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdEventTarget{}),
		})
	})
}

func TestEveryOperationKeepsOneRequiredBranchNoUncertaintyAndAsyncDurability(t *testing.T) {
	type operationShape struct {
		branches []sdkgo.BranchDefinition
		defaults sdkgo.StepDefaults
	}
	for operation, shape := range map[string]operationShape{
		"listEvents":    {outlookcalendar.ListEventsDefinition.Branches, outlookcalendar.ListEventsDefinition.StepDefaults},
		"getEvent":      {outlookcalendar.GetEventDefinition.Branches, outlookcalendar.GetEventDefinition.StepDefaults},
		"createEvent":   {outlookcalendar.CreateEventDefinition.Branches, outlookcalendar.CreateEventDefinition.StepDefaults},
		"updateEvent":   {outlookcalendar.UpdateEventDefinition.Branches, outlookcalendar.UpdateEventDefinition.StepDefaults},
		"queryFreeBusy": {outlookcalendar.QueryFreeBusyDefinition.Branches, outlookcalendar.QueryFreeBusyDefinition.StepDefaults},
	} {
		required := 0
		for _, branch := range shape.branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, "%s retries ambiguous outcomes safely instead", operation)
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, operation)
		require.Equal(t, dex.StepDurabilityAsync, shape.defaults.ExecuteDurability, "%s is duplicate-safe, so Dex's async dispatch is kept", operation)
	}
}

func TestCalendarConnectionCannotBeSerialized(t *testing.T) {
	connection, err := outlookcalendar.NewConnection(newCalendarClient(t, "http://127.0.0.1:1"), calendarConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "outlookcalendar.Connection{[REDACTED]}", fmt.Sprint(connection))
}

func findGuidanceField(fields []guidanceField, name string) guidanceField {
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	return guidanceField{}
}

func readGuidanceManifest(t *testing.T) guidanceManifest {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest guidanceManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

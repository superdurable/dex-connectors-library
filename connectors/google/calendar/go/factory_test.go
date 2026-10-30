// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"gopkg.in/yaml.v3"
)

const googleScopeURIPrefix = "https://www.googleapis.com/auth/"

type createdEventTarget struct {
	dex.StepDefaultsNoWaitFor[calendar.CreateEventResult]
}

func (createdEventTarget) Execute(dex.Context, calendar.CreateEventResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateEventFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := calendar.NewConnection(newCalendarClient(t, "http://127.0.0.1:1"), calendarConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "calendar", GroupLabel: "Calendar", Explanation: "Create an event."}
	mapToInput := func(string) calendar.CreateEventInput { return calendar.CreateEventInput{} }
	require.NotPanics(t, func() {
		calendar.NewCreateEventStep(calendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: calendarConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdEventTarget{}),
		})
	})
	require.Panics(t, func() {
		calendar.NewCreateEventStep(calendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: calendarConnection.Name,
			MapToOperationInput: mapToInput, Conflict: sdkgo.GoTo(createdEventTarget{}),
		})
	})
	require.Panics(t, func() {
		calendar.NewCreateEventStep(calendar.CreateEventStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdEventTarget{}),
		})
	})
}

func TestEveryOperationKeepsOneRequiredBranchAndNoUncertainty(t *testing.T) {
	definitions := map[string][]sdkgo.BranchDefinition{
		"listEvents":    calendar.ListEventsDefinition.Branches,
		"getEvent":      calendar.GetEventDefinition.Branches,
		"createEvent":   calendar.CreateEventDefinition.Branches,
		"updateEvent":   calendar.UpdateEventDefinition.Branches,
		"queryFreeBusy": calendar.QueryFreeBusyDefinition.Branches,
	}
	for operation, branches := range definitions {
		required := 0
		for _, branch := range branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, "%s retries ambiguous outcomes safely instead", operation)
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, operation)
	}
	for _, definition := range []sdkgo.StepDefaults{
		calendar.ListEventsDefinition.StepDefaults, calendar.CreateEventDefinition.StepDefaults, calendar.QueryFreeBusyDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, definition.ExecuteDurability)
	}
}

func TestCalendarConnectionCannotBeSerialized(t *testing.T) {
	connection, err := calendar.NewConnection(newCalendarClient(t, "http://127.0.0.1:1"), calendarConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "calendar-token")
	require.Equal(t, "calendar.Connection{[REDACTED]}", fmt.Sprint(connection))
}

type calendarOAuthManifest struct {
	Spec struct {
		Provider string `yaml:"provider"`
		Auth     struct {
			Methods []struct {
				ID     string `yaml:"id"`
				OAuth2 struct {
					Scopes     []string `yaml:"scopes"`
					UserScopes []string `yaml:"userScopes"`
				} `yaml:"oauth2"`
				Guide struct {
					Steps []string `yaml:"steps"`
				} `yaml:"guide"`
			} `yaml:"methods"`
		} `yaml:"auth"`
	} `yaml:"spec"`
}

// Google reports alias grants under canonical URIs, and Dex Web matches granted scopes literally.
func TestOAuthScopesAreCanonicalNarrowAndDelegatedIdentically(t *testing.T) {
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest calendarOAuthManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, "google", manifest.Spec.Provider)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	oauthMethod, delegationMethod := manifest.Spec.Auth.Methods[0], manifest.Spec.Auth.Methods[1]
	require.Equal(t, calendar.GoogleOAuthAuthMethodID, oauthMethod.ID)
	require.Equal(t, calendar.WorkspaceDomainDelegationAuthMethodID, delegationMethod.ID)
	require.Empty(t, oauthMethod.OAuth2.UserScopes)
	require.Equal(t, []string{
		googleScopeURIPrefix + "calendar.events",
		googleScopeURIPrefix + "calendar.events.freebusy",
		googleScopeURIPrefix + "calendar.calendarlist.readonly",
	}, oauthMethod.OAuth2.Scopes)
	for _, scope := range oauthMethod.OAuth2.Scopes {
		require.NotEqual(t, googleScopeURIPrefix+"calendar", scope, "the full calendar scope is broader than every operation needs")
		shortName := strings.TrimPrefix(scope, googleScopeURIPrefix)
		require.True(t, containsText(oauthMethod.Guide.Steps, shortName), "the OAuth guide names %s", shortName)
		require.True(t, containsText(delegationMethod.Guide.Steps, shortName), "the delegation guide names %s", shortName)
	}
}

func containsText(values []string, text string) bool {
	for _, value := range values {
		if strings.Contains(value, text) {
			return true
		}
	}
	return false
}

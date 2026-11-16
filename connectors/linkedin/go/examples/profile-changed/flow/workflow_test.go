// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package profilechanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent() sdkgo.TriggerEvent[linkedin.ProfileChangedEvent] {
	return sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]{
		ID: "linkedin-profile-0123456789abcdef0123456789abcdef",
		Payload: linkedin.ProfileChangedEvent{
			Subject:       "782bbtaQ",
			Previous:      linkedin.ProfileClaims{Subject: "782bbtaQ", Name: "Ada Lovelace"},
			Current:       linkedin.ProfileClaims{Subject: "782bbtaQ", Name: "Ada King"},
			ChangedFields: []linkedin.ProfileField{linkedin.ProfileFieldName},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent()
	require.Equal(t, "linkedin-profile-change-linkedin-profile-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryProfileChange(event))
	require.Equal(t, linkedin.GetAuthenticatedProfileInput{}, MapToGetAuthenticatedProfileInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent())))
	missingID := MapToFlowInput(sampleTriggerEvent())
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingSubject := MapToFlowInput(sampleTriggerEvent())
	missingSubject.Change.Subject = ""
	require.ErrorContains(t, validateInput(missingSubject), "change.subject")
	noFields := MapToFlowInput(sampleTriggerEvent())
	noFields.Change.ChangedFields = nil
	require.ErrorContains(t, validateInput(noFields), "change.changedFields")
}

func TestTriggerBindingHasNoVisibleField(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, linkedin.ProfileChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, ProfileChangedTriggerBinding, binding.BindingName)
	require.Nil(t, binding.ConfigurationUI, "the binding watches the connected member's own profile; nothing is picked")
	require.Contains(t, binding.Definition.Description, "connected member's own")
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(linkedin.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordProfileChangeStepType, dex.GetFinalStepType[Input](recordProfileChange{}))
	require.Equal(t, completeProfileChangeStepType, dex.GetFinalStepType[linkedin.GetAuthenticatedProfileResult](completeProfileChange{}))
	wait, err := recordProfileChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := linkedin.New(linkedin.Config{}, sdkgo.StaticCredentialProvider[linkedin.Credentials]{})
	require.NoError(t, err)
	connection, err := linkedin.NewConnection(client, sdkgo.ConnectionRef{Provider: "linkedin", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}

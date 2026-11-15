// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package memberchangeintake

import (
	"testing"

	"github.com/stretchr/testify/require"
	mailchimp "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent() sdkgo.TriggerEvent[mailchimp.MemberChangedEvent] {
	return sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]{
		ID: "mailchimp-0123456789abcdef0123456789abcdef",
		Payload: mailchimp.MemberChangedEvent{
			AudienceID: "57afe96172",
			Member:     mailchimp.Member{SubscriberHash: mailchimp.SubscriberHash("ben@example.com"), EmailAddress: "ben@example.com", Status: mailchimp.MemberStatusSubscribed, ListID: "57afe96172"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent()
	require.Equal(t, "mailchimp-member-change-mailchimp-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryMemberChange(event))
	require.Equal(t, mailchimp.GetMemberInput{ListID: "57afe96172", EmailAddress: "ben@example.com"}, MapToGetMemberInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent())))
	missingID := MapToFlowInput(sampleTriggerEvent())
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingAddress := MapToFlowInput(sampleTriggerEvent())
	missingAddress.Change.Member.EmailAddress = ""
	require.ErrorContains(t, validateInput(missingAddress), "change.member.emailAddress")
}

func TestTriggerBindingShowsGuidedAudienceAndStatusUnits(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, mailchimp.MemberChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, MemberChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 2, "the audience and the statuses are the only visible Trigger fields")
	audience, statuses := binding.ConfigurationUI.Units[0], binding.ConfigurationUI.Units[1]
	require.Equal(t, mailchimp.UIUnitAudienceIDEntry, audience.UnitID)
	require.True(t, audience.Required, "a binding without an audience cannot poll")
	for _, guidance := range []string{"57afe96172", "Audience > More options > Audience settings", "baseline", "Restart the Worker"} {
		require.Contains(t, audience.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: mailchimp.UIAudienceIDEntryPortAudienceID, JSONPointer: "/audienceId"}}, audience.Bindings)
	require.Equal(t, mailchimp.UIUnitMemberStatusPicker, statuses.UnitID)
	require.False(t, statuses.Required)
	require.Contains(t, statuses.Description, "Select none")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: mailchimp.UIMemberStatusPickerPortStatuses, JSONPointer: "/statuses"}}, statuses.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(mailchimp.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordMemberChangeStepType, dex.GetFinalStepType[Input](recordMemberChange{}))
	require.Equal(t, completeMemberChangeStepType, dex.GetFinalStepType[mailchimp.GetMemberResult](completeMemberChange{}))
	wait, err := recordMemberChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := mailchimp.New(mailchimp.Config{}, sdkgo.StaticCredentialProvider[mailchimp.Credentials]{})
	require.NoError(t, err)
	connection, err := mailchimp.NewConnection(client, sdkgo.ConnectionRef{Provider: "mailchimp", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}

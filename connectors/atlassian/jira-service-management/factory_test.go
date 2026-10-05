// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdTicketTarget struct {
	dex.StepDefaultsNoWaitFor[jiraservicemanagement.CreateTicketResult]
}

func (createdTicketTarget) Execute(dex.Context, jiraservicemanagement.CreateTicketResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateTicketFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := jiraservicemanagement.NewConnection(newTestClient(t, "http://127.0.0.1:1"), jsmConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "jsm", GroupLabel: "Jira Service Management", Explanation: "Raise a request."}
	mapToInput := func(string) jiraservicemanagement.CreateTicketInput { return jiraservicemanagement.CreateTicketInput{} }
	require.NotPanics(t, func() {
		jiraservicemanagement.NewCreateTicketStep(jiraservicemanagement.CreateTicketStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: jsmConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdTicketTarget{}),
		})
	})
	require.Panics(t, func() {
		jiraservicemanagement.NewCreateTicketStep(jiraservicemanagement.CreateTicketStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: jsmConnection.Name,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(createdTicketTarget{}),
		})
	})
	require.Panics(t, func() {
		jiraservicemanagement.NewCreateTicketStep(jiraservicemanagement.CreateTicketStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdTicketTarget{}),
		})
	})
}

// TestOnlyUnkeyedWritesAreUncertainAndSync records the duplicate-dispatch decision per operation.
func TestOnlyUnkeyedWritesAreUncertainAndSync(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
	}{
		{"findCustomerByEmail", jiraservicemanagement.FindCustomerByEmailDefinition.Branches, jiraservicemanagement.FindCustomerByEmailDefinition.StepDefaults, false},
		{"searchTickets", jiraservicemanagement.SearchTicketsDefinition.Branches, jiraservicemanagement.SearchTicketsDefinition.StepDefaults, false},
		{"getTicket", jiraservicemanagement.GetTicketDefinition.Branches, jiraservicemanagement.GetTicketDefinition.StepDefaults, false},
		{"createTicket", jiraservicemanagement.CreateTicketDefinition.Branches, jiraservicemanagement.CreateTicketDefinition.StepDefaults, true},
		{"updateTicket", jiraservicemanagement.UpdateTicketDefinition.Branches, jiraservicemanagement.UpdateTicketDefinition.StepDefaults, false},
		{"transitionTicket", jiraservicemanagement.TransitionTicketDefinition.Branches, jiraservicemanagement.TransitionTicketDefinition.StepDefaults, false},
		{"addComment", jiraservicemanagement.AddCommentDefinition.Branches, jiraservicemanagement.AddCommentDefinition.StepDefaults, true},
	} {
		required, hasUncertain := 0, false
		for _, branch := range test.branches {
			if branch.ID == sdkgo.UncertainBranchID {
				hasUncertain = true
			}
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.hasUncertain, hasUncertain, test.operation)
		expectedDurability := dex.StepDurabilityAsync
		if test.hasUncertain {
			expectedDurability = dex.StepDurabilitySync
		}
		require.Equal(t, expectedDurability, test.defaults.ExecuteDurability, "%s: an async fallback attempt would resend an unkeyed write", test.operation)
	}
}

func TestConnectionCannotBeSerialized(t *testing.T) {
	connection, err := jiraservicemanagement.NewConnection(newTestClient(t, "http://127.0.0.1:1"), jsmConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "jiraservicemanagement.Connection{[REDACTED]}", fmt.Sprint(connection))
}

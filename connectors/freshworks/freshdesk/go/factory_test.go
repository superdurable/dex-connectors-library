// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "freshdesk", GroupLabel: "Freshdesk", Explanation: "Call Freshdesk."}
	require.NotPanics(t, func() {
		freshdesk.NewSearchTicketsStep(freshdesk.SearchTicketsStepConfig[string]{
			StepType: "SearchTickets", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(tag string) freshdesk.SearchTicketsInput {
				return freshdesk.SearchTicketsInput{Tags: []string{tag}}
			},
			Searched: sdkgo.GoTo(completeTarget[freshdesk.SearchTicketsResult]{}),
		})
		freshdesk.NewGetTicketStep(freshdesk.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(id int64) freshdesk.GetTicketInput { return freshdesk.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[freshdesk.GetTicketResult]{}),
		})
		freshdesk.NewCreateTicketStep(freshdesk.CreateTicketStepConfig[string]{
			StepType: "OpenTicket", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(string) freshdesk.CreateTicketInput { return validCreateTicketInput() },
			Created:             sdkgo.GoTo(completeTarget[freshdesk.CreateTicketResult]{}),
			Uncertain:           sdkgo.GoTo(completeTarget[freshdesk.CreateTicketResult]{}),
		})
		freshdesk.NewUpdateTicketStep(freshdesk.UpdateTicketStepConfig[int64]{
			StepType: "Prioritize", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(id int64) freshdesk.UpdateTicketInput {
				return freshdesk.UpdateTicketInput{TicketID: id, Priority: freshdesk.TicketPriorityHigh}
			},
			Updated: sdkgo.GoTo(completeTarget[freshdesk.UpdateTicketResult]{}),
		})
		freshdesk.NewAddNoteStep(freshdesk.AddNoteStepConfig[int64]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(id int64) freshdesk.AddNoteInput { return freshdesk.AddNoteInput{TicketID: id, Body: "Triaged."} },
			Added:               sdkgo.GoTo(completeTarget[freshdesk.AddNoteResult]{}),
		})
	})
	require.Panics(t, func() {
		freshdesk.NewAddNoteStep(freshdesk.AddNoteStepConfig[int64]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: freshdeskConnection.Name,
			MapToOperationInput: func(id int64) freshdesk.AddNoteInput { return freshdesk.AddNoteInput{TicketID: id, Body: "Triaged."} },
			Uncertain:           sdkgo.GoTo(completeTarget[freshdesk.AddNoteResult]{}),
		})
	}, "added is the required branch")
	require.Panics(t, func() {
		freshdesk.NewGetTicketStep(freshdesk.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id int64) freshdesk.GetTicketInput { return freshdesk.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[freshdesk.GetTicketResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(freshdesk.SearchTicketsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(freshdesk.GetTicketDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(freshdesk.CreateTicketDefinition.Branches), "Freshdesk has no idempotency key, so an unconfirmed create is uncertain")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(freshdesk.UpdateTicketDefinition.Branches), "an absolute-value update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"added": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(freshdesk.AddNoteDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{
		freshdesk.SearchTicketsDefinition.StepDefaults, freshdesk.GetTicketDefinition.StepDefaults, freshdesk.UpdateTicketDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute update is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "two 12-second requests fit inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "a one-minute Retry-After fits in the window")
	}
	for _, defaults := range []sdkgo.StepDefaults{freshdesk.CreateTicketDefinition.StepDefaults, freshdesk.AddNoteDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilitySync, defaults.ExecuteDurability, "async would dispatch a second request after seven seconds")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIKey)
	credentials := freshdesk.Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIKey)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestFreshdeskEnumsKeepFreshdesksIntegers(t *testing.T) {
	require.Equal(t, []freshdesk.TicketStatus{2, 3, 4, 5}, freshdesk.FixedTicketStatuses())
	require.Equal(t, []freshdesk.TicketPriority{1, 2, 3, 4}, freshdesk.TicketPriorities())
	require.Equal(t, freshdesk.TicketStatus(6), freshdesk.TicketStatusWaitingOnCustomer)
	require.Equal(t, freshdesk.TicketStatus(7), freshdesk.TicketStatusWaitingOnThirdParty)
	encoded, err := json.Marshal(freshdesk.Ticket{Status: freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityUrgent})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":2`)
	require.Contains(t, string(encoded), `"priority":4`)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) freshdesk.Connection {
	t.Helper()
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := freshdesk.NewConnection(client, freshdeskConnection)
	require.NoError(t, err)
	return connection
}

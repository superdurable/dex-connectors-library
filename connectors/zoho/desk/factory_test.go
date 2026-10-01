// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "zoho-desk", GroupLabel: "Zoho Desk", Explanation: "Call Zoho Desk."}
	require.NotPanics(t, func() {
		desk.NewSearchTicketsStep(desk.SearchTicketsStepConfig[string]{
			StepType: "SearchTickets", Annotations: annotations, Connection: connection, ConnectionName: deskConnection.Name,
			MapToOperationInput: func(email string) desk.SearchTicketsInput { return desk.SearchTicketsInput{ContactEmail: email} },
			Searched:            sdkgo.GoTo(completeTarget[desk.SearchTicketsResult]{}),
		})
		desk.NewGetTicketStep(desk.GetTicketStepConfig[string]{
			StepType: "ReadTicket", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) desk.GetTicketInput { return desk.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[desk.GetTicketResult]{}),
		})
		desk.NewCreateTicketStep(desk.CreateTicketStepConfig[string]{
			StepType: "OpenTicket", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) desk.CreateTicketInput { return validCreateTicketInput() },
			Created:             sdkgo.GoTo(completeTarget[desk.CreateTicketResult]{}),
			Uncertain:           sdkgo.GoTo(completeTarget[desk.CreateTicketResult]{}),
		})
		desk.NewUpdateTicketStep(desk.UpdateTicketStepConfig[string]{
			StepType: "Prioritize", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) desk.UpdateTicketInput {
				return desk.UpdateTicketInput{TicketID: id, Priority: desk.TicketPriorityHigh}
			},
			Updated: sdkgo.GoTo(completeTarget[desk.UpdateTicketResult]{}),
		})
		desk.NewAddCommentStep(desk.AddCommentStepConfig[string]{
			StepType: "AddComment", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) desk.AddCommentInput { return desk.AddCommentInput{TicketID: id, Content: "Triaged."} },
			Added:               sdkgo.GoTo(completeTarget[desk.AddCommentResult]{}),
		})
	})
	require.Panics(t, func() {
		desk.NewAddCommentStep(desk.AddCommentStepConfig[string]{
			StepType: "AddComment", Connection: connection,
			MapToOperationInput: func(id string) desk.AddCommentInput { return desk.AddCommentInput{TicketID: id, Content: "Triaged."} },
			Uncertain:           sdkgo.GoTo(completeTarget[desk.AddCommentResult]{}),
		})
	}, "added is the required branch")
	require.Panics(t, func() {
		desk.NewGetTicketStep(desk.GetTicketStepConfig[string]{
			StepType: "ReadTicket", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) desk.GetTicketInput { return desk.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[desk.GetTicketResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(desk.SearchTicketsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(desk.GetTicketDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(desk.CreateTicketDefinition.Branches), "Zoho Desk has no idempotency key, so an unconfirmed create is uncertain")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(desk.UpdateTicketDefinition.Branches), "an absolute-value update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"added": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(desk.AddCommentDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{
		desk.SearchTicketsDefinition.StepDefaults, desk.GetTicketDefinition.StepDefaults, desk.UpdateTicketDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute update is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "the 25-second operation deadline fits inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
	for _, defaults := range []sdkgo.StepDefaults{desk.CreateTicketDefinition.StepDefaults, desk.AddCommentDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilitySync, defaults.ExecuteDurability, "async would dispatch a second request after seven seconds")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := testCredentials(desk.USDataCenterAuthMethodID)
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	require.NotContains(t, rendered, "zoho-client-secret")
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestZohoDeskVocabulariesKeepZohoDesksOwnNames(t *testing.T) {
	require.Equal(t, []desk.TicketStatus{"Open", "On Hold", "Escalated", "Closed"}, desk.DefaultTicketStatuses())
	require.Equal(t, []desk.TicketStatusType{"Open", "On Hold", "Closed"}, desk.TicketStatusTypes())
	require.Equal(t, []desk.TicketPriority{"High", "Medium", "Low"}, desk.DefaultTicketPriorities())
	require.True(t, desk.Ticket{StatusType: desk.TicketStatusTypeClosed}.IsClosed())
	require.False(t, desk.Ticket{Status: "Closed", StatusType: desk.TicketStatusTypeOpen}.IsClosed(), "a status named Closed can be of type Open")
	encoded, err := json.Marshal(desk.Ticket{Status: "Waiting for Customer", Priority: "Urgent"})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":"Waiting for Customer"`)
	require.Contains(t, string(encoded), `"priority":"Urgent"`)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) desk.Connection {
	t.Helper()
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := desk.NewConnection(client, deskConnection)
	require.NoError(t, err)
	return connection
}

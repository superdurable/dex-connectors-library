// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
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
	annotations := sdkgo.StepAnnotations{GroupID: "gorgias", GroupLabel: "Gorgias", Explanation: "Call Gorgias."}
	require.NotPanics(t, func() {
		gorgias.NewSearchTicketsStep(gorgias.SearchTicketsStepConfig[int64]{
			StepType: "SearchTickets", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(id int64) gorgias.SearchTicketsInput { return gorgias.SearchTicketsInput{RequesterID: id} },
			Searched:            sdkgo.GoTo(completeTarget[gorgias.SearchTicketsResult]{}),
		})
		gorgias.NewGetTicketStep(gorgias.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(id int64) gorgias.GetTicketInput { return gorgias.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[gorgias.GetTicketResult]{}),
		})
		gorgias.NewFindCustomerByEmailStep(gorgias.FindCustomerByEmailStepConfig[string]{
			StepType: "FindCustomer", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(email string) gorgias.FindCustomerByEmailInput {
				return gorgias.FindCustomerByEmailInput{Email: email}
			},
			Found: sdkgo.GoTo(completeTarget[gorgias.FindCustomerByEmailResult]{}),
		})
		gorgias.NewCreateTicketStep(gorgias.CreateTicketStepConfig[string]{
			StepType: "OpenTicket", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(string) gorgias.CreateTicketInput { return validCreateTicketInput() },
			Created:             sdkgo.GoTo(completeTarget[gorgias.CreateTicketResult]{}),
		})
		gorgias.NewUpdateTicketStep(gorgias.UpdateTicketStepConfig[int64]{
			StepType: "Prioritize", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(id int64) gorgias.UpdateTicketInput {
				return gorgias.UpdateTicketInput{TicketID: id, Priority: gorgias.TicketPriorityHigh}
			},
			Updated: sdkgo.GoTo(completeTarget[gorgias.UpdateTicketResult]{}),
		})
		gorgias.NewAddNoteStep(gorgias.AddNoteStepConfig[int64]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(id int64) gorgias.AddNoteInput { return gorgias.AddNoteInput{TicketID: id, Body: "Triaged."} },
			Added:               sdkgo.GoTo(completeTarget[gorgias.AddNoteResult]{}),
		})
	})
	require.Panics(t, func() {
		gorgias.NewAddNoteStep(gorgias.AddNoteStepConfig[int64]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: gorgiasConnection.Name,
			MapToOperationInput: func(id int64) gorgias.AddNoteInput { return gorgias.AddNoteInput{TicketID: id, Body: "Triaged."} },
			Uncertain:           sdkgo.GoTo(completeTarget[gorgias.AddNoteResult]{}),
		})
	}, "added is the required branch")
	require.Panics(t, func() {
		gorgias.NewGetTicketStep(gorgias.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id int64) gorgias.GetTicketInput { return gorgias.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[gorgias.GetTicketResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

// TestBranchNamesMatchTheSiblingDesks pins the names a process relies on to swap Zendesk, Freshdesk, and Gorgias.
func TestBranchNamesMatchTheSiblingDesks(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(gorgias.SearchTicketsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(gorgias.GetTicketDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(gorgias.FindCustomerByEmailDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(gorgias.CreateTicketDefinition.Branches), "Gorgias has no idempotency key, so an unconfirmed create can be uncertain")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(gorgias.UpdateTicketDefinition.Branches), "an absolute-value update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"added": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(gorgias.AddNoteDefinition.Branches))
}

func TestDefinitionsDeclareDurabilityAndTimeouts(t *testing.T) {
	for _, defaults := range []sdkgo.StepDefaults{
		gorgias.SearchTicketsDefinition.StepDefaults, gorgias.GetTicketDefinition.StepDefaults, gorgias.FindCustomerByEmailDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "two 8-second requests fit inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "a one-minute Retry-After fits in the window")
	}
	update := gorgias.UpdateTicketDefinition.StepDefaults
	require.Equal(t, dex.StepDurabilityAsync, update.ExecuteDurability, "a duplicate dispatch of an absolute update is harmless")
	require.Equal(t, 45*time.Second, update.ExecuteMethodTimeout, "a read, two tag writes, and a field write fit inside it")
	for _, defaults := range []sdkgo.StepDefaults{gorgias.CreateTicketDefinition.StepDefaults, gorgias.AddNoteDefinition.StepDefaults} {
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
	credentials := gorgias.Credentials{Email: testEmail, APIKey: sdkgo.NewSecretString(testAPIKey)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIKey)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestGorgiasEnumsKeepGorgiasStrings(t *testing.T) {
	require.Equal(t, []gorgias.TicketStatus{"open", "closed"}, gorgias.TicketStatuses())
	require.Equal(t, []gorgias.TicketPriority{"low", "normal", "high", "critical"}, gorgias.TicketPriorities())
	encoded, err := json.Marshal(gorgias.Ticket{Status: gorgias.TicketStatusClosed, Priority: gorgias.TicketPriorityCritical})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":"closed"`)
	require.Contains(t, string(encoded), `"priority":"critical"`)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) gorgias.Connection {
	t.Helper()
	client, err := gorgias.New(gorgias.Config{Domain: testDomain}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := gorgias.NewConnection(client, gorgiasConnection)
	require.NoError(t, err)
	return connection
}

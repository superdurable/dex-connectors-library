// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
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
	annotations := sdkgo.StepAnnotations{GroupID: "zendesk", GroupLabel: "Zendesk Support", Explanation: "Call Zendesk."}
	require.NotPanics(t, func() {
		support.NewSearchTicketsStep(support.SearchTicketsStepConfig[string]{
			StepType: "SearchTickets", Annotations: annotations, Connection: connection, ConnectionName: zendeskConnection.Name,
			MapToOperationInput: func(email string) support.SearchTicketsInput {
				return support.SearchTicketsInput{RequesterEmail: email}
			},
			Searched: sdkgo.GoTo(completeTarget[support.SearchTicketsResult]{}),
		})
		support.NewGetTicketStep(support.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id int64) support.GetTicketInput { return support.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[support.GetTicketResult]{}),
		})
		support.NewCreateTicketStep(support.CreateTicketStepConfig[string]{
			StepType: "OpenTicket", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) support.CreateTicketInput { return validCreateTicketInput() },
			Created:             sdkgo.GoTo(completeTarget[support.CreateTicketResult]{}),
			ProviderRejected:    sdkgo.GoTo(completeTarget[support.CreateTicketResult]{}),
			InvalidResponse:     sdkgo.GoTo(completeTarget[support.CreateTicketResult]{}),
			Defect:              sdkgo.GoTo(completeTarget[support.CreateTicketResult]{}),
		})
		support.NewUpdateTicketStep(support.UpdateTicketStepConfig[int64]{
			StepType: "FollowUp", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id int64) support.UpdateTicketInput {
				return support.UpdateTicketInput{TicketID: id, Status: "open"}
			},
			Updated: sdkgo.GoTo(completeTarget[support.UpdateTicketResult]{}),
		})
	})
	require.Panics(t, func() {
		support.NewCreateTicketStep(support.CreateTicketStepConfig[string]{
			StepType: "OpenTicket", Connection: connection,
			MapToOperationInput: func(string) support.CreateTicketInput { return validCreateTicketInput() },
			ProviderRejected:    sdkgo.GoTo(completeTarget[support.CreateTicketResult]{}),
		})
	}, "created is the required branch")
	require.Panics(t, func() {
		support.NewGetTicketStep(support.GetTicketStepConfig[int64]{
			StepType: "ReadTicket", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id int64) support.GetTicketInput { return support.GetTicketInput{TicketID: id} },
			Found:               sdkgo.GoTo(completeTarget[support.GetTicketResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(support.SearchTicketsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(support.GetTicketDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(support.CreateTicketDefinition.Branches), "the Idempotency-Key makes every dispatch failure retryable, so there is no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "conflict": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(support.UpdateTicketDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{
		support.SearchTicketsDefinition.StepDefaults, support.GetTicketDefinition.StepDefaults,
		support.CreateTicketDefinition.StepDefaults, support.UpdateTicketDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "three 9-second requests of updateTicket fit inside it")
		require.Greater(t, defaults.ExecuteRetry.TotalDuration, time.Minute, "a one-minute Zendesk Retry-After must fit in the window")
		require.Less(t, defaults.ExecuteRetry.TotalDuration, 2*time.Hour, "Zendesk keeps an Idempotency-Key for two hours")
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIToken)
	credentials := support.Credentials{Email: testEmail, APIToken: sdkgo.NewSecretString(testAPIToken)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIToken)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) support.Connection {
	t.Helper()
	client, err := support.New(support.Config{Subdomain: testSubdomain}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := support.NewConnection(client, zendeskConnection)
	require.NoError(t, err)
	return connection
}

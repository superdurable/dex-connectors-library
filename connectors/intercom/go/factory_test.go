// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
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
	annotations := sdkgo.StepAnnotations{GroupID: "intercom", GroupLabel: "Intercom", Explanation: "Call Intercom."}
	require.NotPanics(t, func() {
		intercom.NewSearchConversationsStep(intercom.SearchConversationsStepConfig[string]{
			StepType: "SearchConversations", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(email string) intercom.SearchConversationsInput {
				return intercom.SearchConversationsInput{ContactEmail: email}
			},
			Searched: sdkgo.GoTo(completeTarget[intercom.SearchConversationsResult]{}),
		})
		intercom.NewGetConversationStep(intercom.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(id string) intercom.GetConversationInput {
				return intercom.GetConversationInput{ConversationID: id}
			},
			Found: sdkgo.GoTo(completeTarget[intercom.GetConversationResult]{}),
		})
		intercom.NewReplyToConversationStep(intercom.ReplyToConversationStepConfig[string]{
			StepType: "Reply", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(string) intercom.ReplyToConversationInput { return validReplyInput() },
			Replied:             sdkgo.GoTo(completeTarget[intercom.ReplyToConversationResult]{}),
			Uncertain:           sdkgo.GoTo(completeTarget[intercom.ReplyToConversationResult]{}),
		})
		intercom.NewUpdateConversationStateStep(intercom.UpdateConversationStateStepConfig[string]{
			StepType: "Close", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(id string) intercom.UpdateConversationStateInput {
				return intercom.UpdateConversationStateInput{ConversationID: id, AdminID: testAdminID, State: intercom.ConversationStateClosed}
			},
			Updated: sdkgo.GoTo(completeTarget[intercom.UpdateConversationStateResult]{}),
		})
		intercom.NewFindContactByEmailStep(intercom.FindContactByEmailStepConfig[string]{
			StepType: "FindContact", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(email string) intercom.FindContactByEmailInput {
				return intercom.FindContactByEmailInput{Email: email}
			},
			Found: sdkgo.GoTo(completeTarget[intercom.FindContactByEmailResult]{}),
		})
	})
	require.Panics(t, func() {
		intercom.NewReplyToConversationStep(intercom.ReplyToConversationStepConfig[string]{
			StepType: "Reply", Annotations: annotations, Connection: connection, ConnectionName: intercomConnection.Name,
			MapToOperationInput: func(string) intercom.ReplyToConversationInput { return validReplyInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[intercom.ReplyToConversationResult]{}),
		})
	}, "replied is the required branch")
	require.Panics(t, func() {
		intercom.NewGetConversationStep(intercom.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) intercom.GetConversationInput {
				return intercom.GetConversationInput{ConversationID: id}
			},
			Found: sdkgo.GoTo(completeTarget[intercom.GetConversationResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(intercom.SearchConversationsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(intercom.GetConversationDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"replied": false, "notFound": true, "providerRejected": true, "uncertain": true, "invalidResponse": true, "defect": true},
		branchOptionality(intercom.ReplyToConversationDefinition.Branches), "Intercom has no idempotency key, so an unconfirmed reply can be uncertain")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(intercom.UpdateConversationStateDefinition.Branches), "a state change is safe to repeat, so it is never uncertain")
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(intercom.FindContactByEmailDefinition.Branches))
	require.Equal(t, dex.StepDurabilitySync, intercom.ReplyToConversationDefinition.StepDefaults.ExecuteDurability,
		"sync durability keeps Dex from dispatching a second reply while the first is in flight")
	for _, defaults := range []sdkgo.StepDefaults{
		intercom.SearchConversationsDefinition.StepDefaults, intercom.GetConversationDefinition.StepDefaults,
		intercom.UpdateConversationStateDefinition.StepDefaults, intercom.FindContactByEmailDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
	}
	for _, defaults := range []sdkgo.StepDefaults{
		intercom.SearchConversationsDefinition.StepDefaults, intercom.GetConversationDefinition.StepDefaults, intercom.ReplyToConversationDefinition.StepDefaults,
		intercom.UpdateConversationStateDefinition.StepDefaults, intercom.FindContactByEmailDefinition.StepDefaults,
	} {
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "three 9-second requests fit inside it")
		require.Greater(t, defaults.ExecuteRetry.TotalDuration, time.Minute, "a one-minute rate-limit wait must fit in the window")
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := intercom.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken), ClientSecret: sdkgo.NewSecretString(testClientSecret)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	require.NotContains(t, rendered, testClientSecret)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestConversationStatesAreIntercomsOwnVocabulary(t *testing.T) {
	require.Equal(t, []intercom.ConversationState{"open", "closed", "snoozed"}, intercom.ConversationStates())
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) intercom.Connection {
	t.Helper()
	client, err := intercom.New(intercom.Config{}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, intercomConnection)
	require.NoError(t, err)
	return connection
}

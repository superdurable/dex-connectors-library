// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
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
	annotations := sdkgo.StepAnnotations{GroupID: "reamaze", GroupLabel: "Re:amaze", Explanation: "Call Re:amaze."}
	require.NotPanics(t, func() {
		reamaze.NewSearchConversationsStep(reamaze.SearchConversationsStepConfig[string]{
			StepType: "SearchConversations", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(tag string) reamaze.SearchConversationsInput {
				return reamaze.SearchConversationsInput{Tags: []string{tag}}
			},
			Searched: sdkgo.GoTo(completeTarget[reamaze.SearchConversationsResult]{}),
		})
		reamaze.NewGetConversationStep(reamaze.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(slug string) reamaze.GetConversationInput {
				return reamaze.GetConversationInput{ConversationID: slug}
			},
			Found: sdkgo.GoTo(completeTarget[reamaze.GetConversationResult]{}),
		})
		reamaze.NewCreateConversationStep(reamaze.CreateConversationStepConfig[string]{
			StepType: "OpenConversation", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(string) reamaze.CreateConversationInput { return validCreateConversationInput() },
			Created:             sdkgo.GoTo(completeTarget[reamaze.CreateConversationResult]{}),
		})
		reamaze.NewUpdateConversationStep(reamaze.UpdateConversationStepConfig[string]{
			StepType: "Reopen", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(slug string) reamaze.UpdateConversationInput {
				return reamaze.UpdateConversationInput{ConversationID: slug, AddTags: []string{"vip"}}
			},
			Updated: sdkgo.GoTo(completeTarget[reamaze.UpdateConversationResult]{}),
		})
		reamaze.NewReplyToConversationStep(reamaze.ReplyToConversationStepConfig[string]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(slug string) reamaze.ReplyToConversationInput {
				return reamaze.ReplyToConversationInput{ConversationID: slug, Text: "Triaged.", IsInternalNote: true}
			},
			Replied: sdkgo.GoTo(completeTarget[reamaze.ReplyToConversationResult]{}),
		})
		reamaze.NewFindContactByEmailStep(reamaze.FindContactByEmailStepConfig[string]{
			StepType: "FindContact", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(email string) reamaze.FindContactByEmailInput {
				return reamaze.FindContactByEmailInput{Email: email}
			},
			Found: sdkgo.GoTo(completeTarget[reamaze.FindContactByEmailResult]{}),
		})
	})
	require.Panics(t, func() {
		reamaze.NewReplyToConversationStep(reamaze.ReplyToConversationStepConfig[string]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: reamazeConnection.Name,
			MapToOperationInput: func(slug string) reamaze.ReplyToConversationInput {
				return reamaze.ReplyToConversationInput{ConversationID: slug, Text: "Triaged."}
			},
			Uncertain: sdkgo.GoTo(completeTarget[reamaze.ReplyToConversationResult]{}),
		})
	}, "replied is the required branch")
	require.Panics(t, func() {
		reamaze.NewGetConversationStep(reamaze.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(slug string) reamaze.GetConversationInput {
				return reamaze.GetConversationInput{ConversationID: slug}
			},
			Found: sdkgo.GoTo(completeTarget[reamaze.GetConversationResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	queryBranches := map[sdkgo.BranchID]bool{"providerRejected": true, "invalidResponse": true, "defect": true}
	require.Equal(t, withBranches(queryBranches, map[sdkgo.BranchID]bool{"searched": false}), branchOptionality(reamaze.SearchConversationsDefinition.Branches))
	require.Equal(t, withBranches(queryBranches, map[sdkgo.BranchID]bool{"found": false, "notFound": true}), branchOptionality(reamaze.GetConversationDefinition.Branches))
	require.Equal(t, withBranches(queryBranches, map[sdkgo.BranchID]bool{"found": false, "notFound": true}), branchOptionality(reamaze.FindContactByEmailDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(reamaze.CreateConversationDefinition.Branches), "Re:amaze has no idempotency key, so an unreconciled create is uncertain")
	require.Equal(t, withBranches(queryBranches, map[sdkgo.BranchID]bool{"updated": false, "notFound": true}),
		branchOptionality(reamaze.UpdateConversationDefinition.Branches), "an absolute-value update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"replied": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(reamaze.ReplyToConversationDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{
		reamaze.SearchConversationsDefinition.StepDefaults, reamaze.GetConversationDefinition.StepDefaults,
		reamaze.FindContactByEmailDefinition.StepDefaults, reamaze.UpdateConversationDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute update is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "two 12-second requests fit inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
	for _, defaults := range []sdkgo.StepDefaults{reamaze.CreateConversationDefinition.StepDefaults, reamaze.ReplyToConversationDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilitySync, defaults.ExecuteDurability, "async would dispatch a second request after seven seconds")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.MaximumAttempts, int32(2), "a retry is what reconciles an unconfirmed send")
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIToken)
	credentials := reamaze.Credentials{Email: testEmail, APIToken: sdkgo.NewSecretString(testAPIToken)}
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials), testAPIToken)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestReamazeEnumsKeepReamazesIntegers(t *testing.T) {
	statuses := reamaze.ConversationStatuses()
	require.Len(t, statuses, 10)
	for index, status := range statuses {
		require.Equal(t, reamaze.ConversationStatus(index), status)
	}
	require.Equal(t, reamaze.ConversationStatus(5), reamaze.ConversationStatusOnHold)
	require.Equal(t, reamaze.MessageVisibility(1), reamaze.MessageVisibilityInternalNote)
	encoded, err := json.Marshal(reamaze.Conversation{ID: "a", Status: reamaze.ConversationStatusOpen})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":0`, "Open (0) is never omitted")
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func withBranches(base map[sdkgo.BranchID]bool, extra map[sdkgo.BranchID]bool) map[sdkgo.BranchID]bool {
	combined := map[sdkgo.BranchID]bool{}
	for id, isOptional := range base {
		combined[id] = isOptional
	}
	for id, isOptional := range extra {
		combined[id] = isOptional
	}
	return combined
}

func newTestConnection(t *testing.T) reamaze.Connection {
	t.Helper()
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := reamaze.NewConnection(client, reamazeConnection)
	require.NoError(t, err)
	return connection
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
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
	annotations := sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "Call Hiver."}
	require.NotPanics(t, func() {
		hiver.NewListInboxesStep(hiver.ListInboxesStepConfig[string]{
			StepType: "ListInboxes", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(token string) hiver.ListInboxesInput { return hiver.ListInboxesInput{PageToken: token} },
			Listed:              sdkgo.GoTo(completeTarget[hiver.ListInboxesResult]{}),
		})
		hiver.NewListConversationsStep(hiver.ListConversationsStepConfig[string]{
			StepType: "ListConversations", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(inboxID string) hiver.ListConversationsInput {
				return hiver.ListConversationsInput{InboxID: inboxID}
			},
			Listed: sdkgo.GoTo(completeTarget[hiver.ListConversationsResult]{}),
		})
		hiver.NewGetConversationStep(hiver.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(id string) hiver.GetConversationInput {
				return hiver.GetConversationInput{InboxID: "105902", ConversationID: id}
			},
			Found: sdkgo.GoTo(completeTarget[hiver.GetConversationResult]{}),
		})
		hiver.NewUpdateConversationStep(hiver.UpdateConversationStepConfig[string]{
			StepType: "CloseConversation", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(id string) hiver.UpdateConversationInput {
				return hiver.UpdateConversationInput{InboxID: "105902", ConversationID: id, Status: hiver.ConversationStatusClosed}
			},
			Updated: sdkgo.GoTo(completeTarget[hiver.UpdateConversationResult]{}),
		})
		hiver.NewAddNoteStep(hiver.AddNoteStepConfig[string]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(id string) hiver.AddNoteInput {
				return hiver.AddNoteInput{InboxID: "105902", ConversationID: id, Content: "Triaged."}
			},
			Added: sdkgo.GoTo(completeTarget[hiver.AddNoteResult]{}),
		})
		hiver.NewCreateSharedDraftStep(hiver.CreateSharedDraftStepConfig[string]{
			StepType: "DraftReply", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(id string) hiver.CreateSharedDraftInput {
				return hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: id, Body: "Thanks."}
			},
			Created: sdkgo.GoTo(completeTarget[hiver.CreateSharedDraftResult]{}),
		})
	})
	require.Panics(t, func() {
		hiver.NewAddNoteStep(hiver.AddNoteStepConfig[string]{
			StepType: "AddNote", Annotations: annotations, Connection: connection, ConnectionName: hiverConnection.Name,
			MapToOperationInput: func(id string) hiver.AddNoteInput { return hiver.AddNoteInput{ConversationID: id} },
			Uncertain:           sdkgo.GoTo(completeTarget[hiver.AddNoteResult]{}),
		})
	}, "added is the required branch")
	require.Panics(t, func() {
		hiver.NewGetConversationStep(hiver.GetConversationStepConfig[string]{
			StepType: "ReadConversation", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) hiver.GetConversationInput { return hiver.GetConversationInput{ConversationID: id} },
			Found:               sdkgo.GoTo(completeTarget[hiver.GetConversationResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(hiver.ListInboxesDefinition.Branches))
	readBranches := map[sdkgo.BranchID]bool{"notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true}
	require.Equal(t, withRequired(readBranches, "listed"), branchOptionality(hiver.ListConversationsDefinition.Branches))
	require.Equal(t, withRequired(readBranches, "found"), branchOptionality(hiver.GetConversationDefinition.Branches))
	require.Equal(t, withRequired(readBranches, "updated"), branchOptionality(hiver.UpdateConversationDefinition.Branches),
		"an absolute update is safe to repeat, so it has no uncertain branch")
	singleDispatchBranches := map[sdkgo.BranchID]bool{"notFound": true, "providerRejected": true, "uncertain": true, "defect": true}
	require.Equal(t, withRequired(singleDispatchBranches, "added"), branchOptionality(hiver.AddNoteDefinition.Branches))
	require.Equal(t, withRequired(singleDispatchBranches, "created"), branchOptionality(hiver.CreateSharedDraftDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{
		hiver.ListInboxesDefinition.StepDefaults, hiver.ListConversationsDefinition.StepDefaults, hiver.GetConversationDefinition.StepDefaults,
		hiver.UpdateConversationDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute update is harmless")
		require.GreaterOrEqual(t, defaults.ExecuteMethodTimeout, 30*time.Second)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
	require.Equal(t, 60*time.Second, hiver.UpdateConversationDefinition.StepDefaults.ExecuteMethodTimeout,
		"up to eight requests one second apart fit inside it")
	for _, defaults := range []sdkgo.StepDefaults{hiver.AddNoteDefinition.StepDefaults, hiver.CreateSharedDraftDefinition.StepDefaults} {
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
	credentials := hiver.Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIKey)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for name, config := range map[string]hiver.Config{
		"negative response limit": {MaxResponseBytes: -1},
		"negative interval":       {RequestIntervalMilliseconds: -1},
		"interval above a minute": {RequestIntervalMilliseconds: 60001},
	} {
		_, err := hiver.New(config, testCredentialProvider())
		require.Error(t, err, name)
	}
	_, err := hiver.New(hiver.Config{}, nil)
	require.Error(t, err)
	_, err = hiver.New(hiver.Config{}, testCredentialProvider(), hiver.WithAPIBaseURL("http://hiver.example.com/v1"))
	require.Error(t, err, "a non-loopback base URL must use HTTPS")
	require.Equal(t, hiver.Config{MaxResponseBytes: 4 << 20, RequestIntervalMilliseconds: 1000}, hiver.DefaultConfig())
}

func TestConversationStatusesKeepHiversVocabulary(t *testing.T) {
	require.Equal(t, []hiver.ConversationStatus{"open", "pending", "closed"}, hiver.ConversationStatuses())
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func withRequired(optionalBranches map[sdkgo.BranchID]bool, required sdkgo.BranchID) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{required: false}
	for branch, isOptional := range optionalBranches {
		branches[branch] = isOptional
	}
	return branches
}

func newTestConnection(t *testing.T) hiver.Connection {
	t.Helper()
	client, err := hiver.New(hiver.Config{}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := hiver.NewConnection(client, hiverConnection)
	require.NoError(t, err)
	return connection
}

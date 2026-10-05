// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type pageTarget struct {
	dex.StepDefaultsNoWaitFor[pipedrive.SearchObjectsResult]
}

func (pageTarget) Execute(dex.Context, pipedrive.SearchObjectsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type objectTarget struct {
	dex.StepDefaultsNoWaitFor[pipedrive.GetObjectResult]
}

func (objectTarget) Execute(dex.Context, pipedrive.GetObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type writtenObjectTarget struct {
	dex.StepDefaultsNoWaitFor[pipedrive.CreateObjectResult]
}

func (writtenObjectTarget) Execute(dex.Context, pipedrive.CreateObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type upsertTarget struct {
	dex.StepDefaultsNoWaitFor[pipedrive.UpsertObjectResult]
}

func (upsertTarget) Execute(dex.Context, pipedrive.UpsertObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	name := pipedriveConnection.Name
	annotations := sdkgo.StepAnnotations{GroupID: "pipedrive", GroupLabel: "Pipedrive", Explanation: "Call Pipedrive."}
	require.NotPanics(t, func() {
		pipedrive.NewSearchObjectsStep(pipedrive.SearchObjectsStepConfig[string]{
			StepType: "Search", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.SearchObjectsInput { return pipedrive.SearchObjectsInput{} },
			Searched:            sdkgo.GoTo(pageTarget{}),
		})
		pipedrive.NewListObjectsStep(pipedrive.ListObjectsStepConfig[string]{
			StepType: "List", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.ListObjectsInput { return pipedrive.ListObjectsInput{} },
			Listed:              sdkgo.GoTo(pageTarget{}),
		})
		pipedrive.NewGetObjectStep(pipedrive.GetObjectStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.GetObjectInput { return pipedrive.GetObjectInput{} },
			Found:               sdkgo.GoTo(objectTarget{}),
		})
		pipedrive.NewCreateObjectStep(pipedrive.CreateObjectStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.CreateObjectInput { return pipedrive.CreateObjectInput{} },
			Created:             sdkgo.GoTo(writtenObjectTarget{}),
		})
		pipedrive.NewUpdateObjectStep(pipedrive.UpdateObjectStepConfig[string]{
			StepType: "Update", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.UpdateObjectInput { return pipedrive.UpdateObjectInput{} },
			Updated:             sdkgo.GoTo(writtenObjectTarget{}),
		})
		pipedrive.NewUpsertObjectStep(pipedrive.UpsertObjectStepConfig[string]{
			StepType: "Upsert", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.UpsertObjectInput { return pipedrive.UpsertObjectInput{} },
			Upserted:            sdkgo.GoTo(upsertTarget{}),
		})
	})
	require.Panics(t, func() {
		pipedrive.NewCreateObjectStep(pipedrive.CreateObjectStepConfig[string]{
			StepType: "Create", Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) pipedrive.CreateObjectInput { return pipedrive.CreateObjectInput{} },
			Uncertain:           sdkgo.GoTo(writtenObjectTarget{}),
		})
	}, "the created branch is required")
	require.Panics(t, func() {
		pipedrive.NewGetObjectStep(pipedrive.GetObjectStepConfig[string]{
			StepType: "Get", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(string) pipedrive.GetObjectInput { return pipedrive.GetObjectInput{} },
			Found:               sdkgo.GoTo(objectTarget{}),
		})
	}, "the static connection name must match the runtime connection")
}

// Pipedrive has no idempotency key: only creates run sync and declare uncertain; the PATCH update is repeatable.
func TestOnlyCreatingMutationsRunSyncWithAnUncertainBranch(t *testing.T) {
	for _, definition := range []sdkgo.MutationDefinition{pipedrive.CreateObjectDefinition, pipedrive.UpsertObjectDefinition} {
		require.Equal(t, dex.StepDurabilitySync, definition.StepDefaults.ExecuteDurability, definition.Operation.OperationID)
		require.Contains(t, branchIDs(definition.Branches), sdkgo.UncertainBranchID, definition.Operation.OperationID)
	}
	require.Equal(t, dex.StepDurabilityAsync, pipedrive.UpdateObjectDefinition.StepDefaults.ExecuteDurability)
	require.NotContains(t, branchIDs(pipedrive.UpdateObjectDefinition.Branches), sdkgo.UncertainBranchID)
	for _, definition := range []sdkgo.QueryDefinition{pipedrive.SearchObjectsDefinition, pipedrive.GetObjectDefinition, pipedrive.ListObjectsDefinition} {
		require.Equal(t, dex.StepDurabilityAsync, definition.StepDefaults.ExecuteDurability, definition.Operation.OperationID)
	}
}

func TestConnectionAndCredentialsNeverRenderSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	credentials := oauthCredentials("https://acme.pipedrive.com")
	credentials.APIToken = sdkgo.NewSecretString(testAPIToken)
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	for _, secret := range []string{testAPIToken, testAccessToken, "client-secret", "stored-refresh-token"} {
		require.NotContains(t, rendered, secret)
	}
}

func newTestConnection(t *testing.T) pipedrive.Connection {
	t.Helper()
	connection, err := pipedrive.NewConnection(newAPITokenClient(t, "http://127.0.0.1:1"), pipedriveConnection)
	require.NoError(t, err)
	return connection
}

func branchIDs(branches []sdkgo.BranchDefinition) []sdkgo.BranchID {
	ids := make([]sdkgo.BranchID, 0, len(branches))
	for _, branch := range branches {
		ids = append(ids, branch.ID)
	}
	return ids
}

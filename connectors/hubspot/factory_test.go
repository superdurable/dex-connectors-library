// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type searchTarget struct {
	dex.StepDefaultsNoWaitFor[hubspot.SearchObjectsResult]
}

func (searchTarget) Execute(dex.Context, hubspot.SearchObjectsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type getTarget struct {
	dex.StepDefaultsNoWaitFor[hubspot.GetObjectResult]
}

func (getTarget) Execute(dex.Context, hubspot.GetObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type upsertTarget struct {
	dex.StepDefaultsNoWaitFor[hubspot.UpsertObjectResult]
}

func (upsertTarget) Execute(dex.Context, hubspot.UpsertObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type updateTarget struct {
	dex.StepDefaultsNoWaitFor[hubspot.UpdateObjectResult]
}

func (updateTarget) Execute(dex.Context, hubspot.UpdateObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "hubspot", GroupLabel: "HubSpot", Explanation: "Call HubSpot."}
	require.NotPanics(t, func() {
		hubspot.NewSearchObjectsStep(hubspot.SearchObjectsStepConfig[string]{
			StepType: "Search", Annotations: annotations, Connection: connection, ConnectionName: hubspotConnection.Name,
			MapToOperationInput: func(string) hubspot.SearchObjectsInput {
				return hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeContacts}
			},
			Searched: sdkgo.GoTo(searchTarget{}),
		})
		hubspot.NewGetObjectStep(hubspot.GetObjectStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: hubspotConnection.Name,
			MapToOperationInput: func(string) hubspot.GetObjectInput { return hubspot.GetObjectInput{} },
			Found:               sdkgo.GoTo(getTarget{}),
		})
		hubspot.NewUpsertObjectStep(hubspot.UpsertObjectStepConfig[string]{
			StepType: "Upsert", Annotations: annotations, Connection: connection, ConnectionName: hubspotConnection.Name,
			MapToOperationInput: func(string) hubspot.UpsertObjectInput { return hubspot.UpsertObjectInput{} },
			Upserted:            sdkgo.GoTo(upsertTarget{}),
		})
		hubspot.NewUpdateObjectStep(hubspot.UpdateObjectStepConfig[string]{
			StepType: "Update", Annotations: annotations, Connection: connection, ConnectionName: hubspotConnection.Name,
			MapToOperationInput: func(string) hubspot.UpdateObjectInput { return hubspot.UpdateObjectInput{} },
			Updated:             sdkgo.GoTo(updateTarget{}),
		})
	})
	require.Panics(t, func() {
		hubspot.NewUpsertObjectStep(hubspot.UpsertObjectStepConfig[string]{
			StepType: "Upsert", Connection: connection,
			MapToOperationInput: func(string) hubspot.UpsertObjectInput { return hubspot.UpsertObjectInput{} },
			Conflict:            sdkgo.GoTo(upsertTarget{}),
		})
	}, "the upserted branch is required")
	require.Panics(t, func() {
		hubspot.NewGetObjectStep(hubspot.GetObjectStepConfig[string]{
			StepType: "Get", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(string) hubspot.GetObjectInput { return hubspot.GetObjectInput{} },
			Found:               sdkgo.GoTo(getTarget{}),
		})
	}, "the static connection name must match the runtime connection")
}

func TestMutationsDeclareNoUncertainBranchBecauseBothWritesAreIdempotent(t *testing.T) {
	for _, definition := range []sdkgo.MutationDefinition{hubspot.UpsertObjectDefinition, hubspot.UpdateObjectDefinition} {
		for _, branch := range definition.Branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, definition.Operation.OperationID)
		}
		require.Equal(t, dex.StepDurabilityAsync, definition.StepDefaults.ExecuteDurability, definition.Operation.OperationID)
	}
}

func TestConnectionAndCredentialsNeverRenderSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testAccessToken)
	credentials := hubspot.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-secret")}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	require.NotContains(t, rendered, "refresh-secret")
}

func TestNewRejectsUnsafeEndpointAndMissingDependencies(t *testing.T) {
	provider := sdkgo.StaticCredentialProvider[hubspot.Credentials]{}
	for _, endpoint := range []string{"http://api.hubapi.com", "https://user:secret@api.hubapi.com", "https://api.hubapi.com?token=x"} {
		_, err := hubspot.New(hubspot.Config{Endpoint: endpoint}, provider)
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), "secret")
	}
	_, err := hubspot.New(hubspot.Config{}, nil)
	require.Error(t, err)
	_, err = hubspot.New(hubspot.Config{}, provider, nil)
	require.Error(t, err)
	client, err := hubspot.New(hubspot.Config{}, provider)
	require.NoError(t, err, "a blank endpoint uses HubSpot's public API host")
	require.NotNil(t, client)
}

func newTestConnection(t *testing.T) hubspot.Connection {
	t.Helper()
	client := newStaticTokenClient(t, "http://127.0.0.1:1", hubspot.Config{})
	connection, err := hubspot.NewConnection(client, hubspotConnection)
	require.NoError(t, err)
	return connection
}

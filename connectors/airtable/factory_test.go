// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type listTarget struct {
	dex.StepDefaultsNoWaitFor[airtable.ListRecordsResult]
}

func (listTarget) Execute(dex.Context, airtable.ListRecordsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type getTarget struct {
	dex.StepDefaultsNoWaitFor[airtable.GetRecordResult]
}

func (getTarget) Execute(dex.Context, airtable.GetRecordResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type upsertTarget struct {
	dex.StepDefaultsNoWaitFor[airtable.UpsertRecordsResult]
}

func (upsertTarget) Execute(dex.Context, airtable.UpsertRecordsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type updateTarget struct {
	dex.StepDefaultsNoWaitFor[airtable.UpdateRecordsResult]
}

func (updateTarget) Execute(dex.Context, airtable.UpdateRecordsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "airtable", GroupLabel: "Airtable", Explanation: "Call Airtable."}
	require.NotPanics(t, func() {
		airtable.NewListRecordsStep(airtable.ListRecordsStepConfig[string]{
			StepType: "List", Annotations: annotations, Connection: connection, ConnectionName: airtableConnection.Name,
			MapToOperationInput: func(string) airtable.ListRecordsInput { return airtable.ListRecordsInput{} },
			Listed:              sdkgo.GoTo(listTarget{}),
		})
		airtable.NewGetRecordStep(airtable.GetRecordStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: airtableConnection.Name,
			MapToOperationInput: func(string) airtable.GetRecordInput { return airtable.GetRecordInput{} },
			Found:               sdkgo.GoTo(getTarget{}),
		})
		airtable.NewUpsertRecordsStep(airtable.UpsertRecordsStepConfig[string]{
			StepType: "Upsert", Annotations: annotations, Connection: connection, ConnectionName: airtableConnection.Name,
			MapToOperationInput: func(string) airtable.UpsertRecordsInput { return airtable.UpsertRecordsInput{} },
			Upserted:            sdkgo.GoTo(upsertTarget{}),
		})
		airtable.NewUpdateRecordsStep(airtable.UpdateRecordsStepConfig[string]{
			StepType: "Update", Annotations: annotations, Connection: connection, ConnectionName: airtableConnection.Name,
			MapToOperationInput: func(string) airtable.UpdateRecordsInput { return airtable.UpdateRecordsInput{} },
			Updated:             sdkgo.GoTo(updateTarget{}),
		})
	})
	require.Panics(t, func() {
		airtable.NewUpsertRecordsStep(airtable.UpsertRecordsStepConfig[string]{
			StepType: "Upsert", Annotations: annotations, Connection: connection, ConnectionName: airtableConnection.Name,
			MapToOperationInput: func(string) airtable.UpsertRecordsInput { return airtable.UpsertRecordsInput{} },
			ProviderRejected:    sdkgo.GoTo(upsertTarget{}),
		})
	}, "the upserted branch is required")
	require.Panics(t, func() {
		airtable.NewGetRecordStep(airtable.GetRecordStepConfig[string]{
			StepType: "Get", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(string) airtable.GetRecordInput { return airtable.GetRecordInput{} },
			Found:               sdkgo.GoTo(getTarget{}),
		})
	}, "the static connection name must match the runtime connection")
}

func TestWritesDeclareNoUncertainBranchAndUpsertNeverRunsTwoAttemptsAtOnce(t *testing.T) {
	for _, definition := range []sdkgo.MutationDefinition{airtable.UpsertRecordsDefinition, airtable.UpdateRecordsDefinition} {
		for _, branch := range definition.Branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, definition.Operation.OperationID)
		}
	}
	require.Equal(t, dex.StepDurabilitySync, airtable.UpsertRecordsDefinition.StepDefaults.ExecuteDurability,
		"Airtable enforces no unique merge key, so a concurrent async fallback attempt could create a second record")
	require.Equal(t, dex.StepDurabilityAsync, airtable.UpdateRecordsDefinition.StepDefaults.ExecuteDurability,
		"absolute values make concurrent duplicate updates converge")
	for _, definition := range []sdkgo.QueryDefinition{airtable.ListRecordsDefinition, airtable.GetRecordDefinition} {
		require.Equal(t, dex.StepDurabilityAsync, definition.StepDefaults.ExecuteDurability, definition.Operation.OperationID)
	}
	for _, retry := range []*dex.RetryPolicy{
		airtable.ListRecordsDefinition.StepDefaults.ExecuteRetry, airtable.UpsertRecordsDefinition.StepDefaults.ExecuteRetry,
	} {
		require.GreaterOrEqual(t, retry.TotalDuration, 3*retry.MaximumInterval, "the window fits several of Airtable's 30-second rate-limit waits")
	}
}

func TestConnectionAndCredentialsNeverRenderSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	credentials := airtable.Credentials{PersonalAccessToken: sdkgo.NewSecretString(testAccessToken)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
}

func TestNewRejectsUnsafeEndpointsAndMissingDependencies(t *testing.T) {
	provider := sdkgo.StaticCredentialProvider[airtable.Credentials]{}
	for _, endpoint := range []string{"http://api.airtable.com", "https://user:secret@api.airtable.com", "https://api.airtable.com?token=x", "api.airtable.com"} {
		_, err := airtable.New(airtable.Config{Endpoint: endpoint}, provider)
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), "secret")
	}
	_, err := airtable.New(airtable.Config{}, nil)
	require.Error(t, err)
	_, err = airtable.New(airtable.Config{}, provider, nil)
	require.Error(t, err)
	_, err = airtable.New(airtable.Config{MaxResponseBytes: -1}, provider)
	require.Error(t, err)
	client, err := airtable.New(airtable.Config{}, provider)
	require.NoError(t, err, "a blank endpoint uses Airtable's public API host")
	require.NotNil(t, client)
	require.Equal(t, "https://api.airtable.com", airtable.DefaultConfig().Endpoint)
}

func newTestConnection(t *testing.T) airtable.Connection {
	t.Helper()
	client := newTestClient(t, "http://127.0.0.1:1", airtable.Config{})
	connection, err := airtable.NewConnection(client, airtableConnection)
	require.NoError(t, err)
	return connection
}

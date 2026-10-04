// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryTarget struct {
	dex.StepDefaultsNoWaitFor[salesforce.QueryRecordsResult]
}

func (queryTarget) Execute(dex.Context, salesforce.QueryRecordsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type upsertTarget struct {
	dex.StepDefaultsNoWaitFor[salesforce.UpsertRecordByExternalIDResult]
}

func (upsertTarget) Execute(dex.Context, salesforce.UpsertRecordByExternalIDResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

var testAnnotations = sdkgo.StepAnnotations{GroupID: "salesforce", GroupLabel: "Salesforce", Explanation: "Use Salesforce."}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := salesforce.NewConnection(newSalesforceClient(t, "http://127.0.0.1:1"), salesforceConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		salesforce.NewQueryRecordsStep(salesforce.QueryRecordsStepConfig[string]{
			StepType: "Query", Annotations: testAnnotations, Connection: connection, ConnectionName: salesforceConnection.Name,
			MapToOperationInput: func(string) salesforce.QueryRecordsInput { return salesforce.QueryRecordsInput{} },
			Found:               sdkgo.GoTo(queryTarget{}),
		})
		salesforce.NewUpsertRecordByExternalIDStep(salesforce.UpsertRecordByExternalIDStepConfig[string]{
			StepType: "Upsert", Annotations: testAnnotations, Connection: connection, ConnectionName: salesforceConnection.Name,
			MapToOperationInput: func(string) salesforce.UpsertRecordByExternalIDInput {
				return salesforce.UpsertRecordByExternalIDInput{}
			},
			Upserted: sdkgo.GoTo(upsertTarget{}),
		})
	})
	require.Panics(t, func() {
		salesforce.NewUpsertRecordByExternalIDStep(salesforce.UpsertRecordByExternalIDStepConfig[string]{
			StepType: "Upsert", Annotations: testAnnotations, Connection: connection, ConnectionName: salesforceConnection.Name,
			MapToOperationInput: func(string) salesforce.UpsertRecordByExternalIDInput {
				return salesforce.UpsertRecordByExternalIDInput{}
			},
			RecordRejected: sdkgo.GoTo(upsertTarget{}),
		})
	})
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection, err := salesforce.NewConnection(newSalesforceClient(t, "http://127.0.0.1:1"), salesforceConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		salesforce.NewQueryRecordsStep(salesforce.QueryRecordsStepConfig[string]{
			StepType: "Query", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) salesforce.QueryRecordsInput { return salesforce.QueryRecordsInput{} },
			Found:               sdkgo.GoTo(queryTarget{}),
		})
	})
}

func TestSalesforceConnectionCannotBeSerialized(t *testing.T) {
	connection, err := salesforce.NewConnection(newSalesforceClient(t, "http://127.0.0.1:1"), salesforceConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), salesforceTestToken)
	require.Equal(t, "salesforce.Connection{[REDACTED]}", connection.String())
}

func TestNewRejectsUnsupportedConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[salesforce.Credentials]{}
	for name, config := range map[string]salesforce.Config{
		"old API version":     {APIVersion: "v45.0"},
		"malformed version":   {APIVersion: "62.0"},
		"batch below minimum": {QueryBatchSize: 100},
		"batch above maximum": {QueryBatchSize: 2001},
		"negative limit":      {MaxResponseBytes: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := salesforce.New(config, credentials)
			require.Error(t, err)
		})
	}
	_, err := salesforce.New(salesforce.Config{}, nil)
	require.Error(t, err)
	client, err := salesforce.New(salesforce.Config{APIVersion: "v64.0", QueryBatchSize: 2000}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

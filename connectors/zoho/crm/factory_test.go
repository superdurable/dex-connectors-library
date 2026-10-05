// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
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
	annotations := sdkgo.StepAnnotations{GroupID: "zoho-crm", GroupLabel: "Zoho CRM", Explanation: "Call Zoho CRM."}
	require.NotPanics(t, func() {
		crm.NewFindRecordsStep(crm.FindRecordsStepConfig[string]{
			StepType: "FindContact", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(string) crm.FindRecordsInput { return contactByEmailInput() },
			Found:               sdkgo.GoTo(completeTarget[crm.FindRecordsResult]{}),
		})
		crm.NewGetRecordStep(crm.GetRecordStepConfig[string]{
			StepType: "ReadDeal", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(id string) crm.GetRecordInput { return crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: id} },
			Found:               sdkgo.GoTo(completeTarget[crm.GetRecordResult]{}),
		})
		crm.NewListModifiedRecordsStep(crm.ListModifiedRecordsStepConfig[string]{
			StepType: "ListChangedDeals", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(cursor string) crm.ListModifiedRecordsInput {
				return crm.ListModifiedRecordsInput{Module: crm.ModuleDeals, Fields: []string{"Stage"}, Cursor: cursor}
			},
			Listed: sdkgo.GoTo(completeTarget[crm.ListModifiedRecordsResult]{}),
		})
		crm.NewUpsertRecordStep(crm.UpsertRecordStepConfig[string]{
			StepType: "UpsertContact", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(string) crm.UpsertRecordInput { return contactUpsertInput() },
			Upserted:            sdkgo.GoTo(completeTarget[crm.UpsertRecordResult]{}),
		})
		crm.NewUpdateRecordStep(crm.UpdateRecordStepConfig[string]{
			StepType: "AdvanceDeal", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(string) crm.UpdateRecordInput { return dealStageUpdateInput() },
			Updated:             sdkgo.GoTo(completeTarget[crm.UpdateRecordResult]{}),
		})
		crm.NewListModuleFieldsStep(crm.ListModuleFieldsStepConfig[string]{
			StepType: "ReadDealFields", Annotations: annotations, Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(module string) crm.ListModuleFieldsInput { return crm.ListModuleFieldsInput{Module: module} },
			Listed:              sdkgo.GoTo(completeTarget[crm.ListModuleFieldsResult]{}),
		})
	})
	require.Panics(t, func() {
		crm.NewUpsertRecordStep(crm.UpsertRecordStepConfig[string]{
			StepType: "UpsertContact", Connection: connection, ConnectionName: crmConnection.Name,
			MapToOperationInput: func(string) crm.UpsertRecordInput { return contactUpsertInput() },
			Conflict:            sdkgo.GoTo(completeTarget[crm.UpsertRecordResult]{}),
		})
	}, "upserted is the required branch")
	require.Panics(t, func() {
		crm.NewGetRecordStep(crm.GetRecordStepConfig[string]{
			StepType: "ReadDeal", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) crm.GetRecordInput { return crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: id} },
			Found:               sdkgo.GoTo(completeTarget[crm.GetRecordResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.FindRecordsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.GetRecordDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.ListModifiedRecordsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.ListModuleFieldsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"upserted": false, "conflict": true, "recordRejected": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.UpsertRecordDefinition.Branches), "a repeated upsert converges, so there is no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "conflict": true, "recordRejected": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(crm.UpdateRecordDefinition.Branches), "an absolute-value update is safe to repeat")
	for _, defaults := range []sdkgo.StepDefaults{
		crm.FindRecordsDefinition.StepDefaults, crm.GetRecordDefinition.StepDefaults, crm.ListModifiedRecordsDefinition.StepDefaults,
		crm.ListModuleFieldsDefinition.StepDefaults, crm.UpsertRecordDefinition.StepDefaults, crm.UpdateRecordDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "every operation is safe to dispatch twice")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "the 25-second operation deadline fits inside it")
		require.Equal(t, 5*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := testCredentials(crm.USDataCenterAuthMethodID)
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	require.NotContains(t, rendered, testRefreshToken)
	require.NotContains(t, rendered, "zoho-client-secret")
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestFieldValueHelpersEncodeZohoCRMValues(t *testing.T) {
	require.JSONEq(t, `"O'Brien \"Jr\""`, string(crm.TextFieldValue(`O'Brien "Jr"`)))
	require.JSONEq(t, `{"id":"`+testAccountID+`"}`, string(crm.LookupFieldValue(testAccountID)))
	record := crm.Record{Fields: map[string]json.RawMessage{"Stage": json.RawMessage(`null`), "Amount": json.RawMessage(`75000.5`)}}
	_, isFound := record.StringField("Stage")
	require.False(t, isFound, "null is absent")
	_, isFound = record.StringField("Amount")
	require.False(t, isFound, "a number is not text")
	var amount float64
	isFound, err := record.DecodeField("Amount", &amount)
	require.NoError(t, err)
	require.True(t, isFound)
	require.InDelta(t, 75000.5, amount, 0.001)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) crm.Connection {
	t.Helper()
	client, err := crm.New(crm.Config{}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := crm.NewConnection(client, crmConnection)
	require.NoError(t, err)
	return connection
}

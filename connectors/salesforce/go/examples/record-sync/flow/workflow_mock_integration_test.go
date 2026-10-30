//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordsync

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/salesforcemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRecordSyncUsesMockWithRealDex(t *testing.T) {
	mock := salesforcemock.New(t, ConnectionName)
	mock.QueryRecords().Respond(salesforcemock.QueryRecordsFound(salesforce.QueryRecordsOutput{
		Records: []salesforce.Record{{
			Type: "Contact", ID: "003RM000001AbcdYAC",
			Fields: map[string]json.RawMessage{
				"Id": json.RawMessage(`"003RM000001AbcdYAC"`), "ERP_Id__c": json.RawMessage(`null`),
			},
		}},
		TotalSize: 1, IsDone: true,
	}, nil))
	flow, harness := newSalesforceIntegrationHarnessForConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	input := Input{MatchValue: "priya@meridian.example.com", ExternalID: "ERP-88213", Fields: map[string]string{"Title": "CTO"}}
	run := runRecordSync(t, ctx, harness.client, flow, fmt.Sprintf("salesforce-mock-%d", time.Now().UnixNano()), input)
	require.Equal(t, dex.FlowCompleted, run.status, run.errorMessage)
	require.Equal(t, StatusLinked, run.outcome.Status)
	require.Equal(t, input.ExternalID, readBackText(t, run.outcome, externalIDField))
	require.Equal(t, input.Fields["Title"], readBackText(t, run.outcome, "Title"))

	queryCalls := mock.QueryRecords().Calls()
	require.Len(t, queryCalls, 1)
	require.Equal(t, salesforce.QueryRecordsInput{SOQL: matchQuery, Bindings: map[string]salesforce.SOQLValue{
		"externalIdField": salesforce.SOQLIdentifier("ERP_Id__c"),
		"sObjectType":     salesforce.SOQLIdentifier("Contact"),
		"matchField":      salesforce.SOQLIdentifier("Email"),
		"matchValue":      salesforce.SOQLString("priya@meridian.example.com"),
	}}, queryCalls[0].Input)
	require.Equal(t, salesforce.QueryRecordsBranchFound, queryCalls[0].Branch)
	require.Empty(t, mock.UpsertRecordByExternalID().Calls())
	updateCalls := mock.UpdateRecord().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, salesforce.UpdateRecordInput{
		SObjectType: "Contact", RecordID: "003RM000001AbcdYAC",
		Fields: map[string]json.RawMessage{"Title": json.RawMessage(`"CTO"`), "ERP_Id__c": json.RawMessage(`"ERP-88213"`)},
	}, updateCalls[0].Input)
	require.Equal(t, salesforce.UpdateRecordBranchUpdated, updateCalls[0].Branch)
	getCalls := mock.GetRecord().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, salesforce.GetRecordInput{
		SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"ERP_Id__c", "Title"},
	}, getCalls[0].Input)
	require.Equal(t, salesforce.GetRecordBranchFound, getCalls[0].Branch)
}

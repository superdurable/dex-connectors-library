// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToUpsertRowInputUsesEmailAsTheStableKey(t *testing.T) {
	input := Input{SpreadsheetID: "sheet-id", SheetName: "Contacts", Email: "person@example.com", Name: "Person", Status: "active"}
	require.Equal(t, spreadsheet.UpsertRowInput{
		SpreadsheetID: "sheet-id", SheetName: "Contacts", KeyColumn: "email", KeyValue: "person@example.com",
		Values: map[string]string{"email": "person@example.com", "name": "Person", "status": "active"},
	}, NewFlow(spreadsheet.Connection{}).MapToUpsertRowInput(input))
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(spreadsheet.Connection{})))
	require.Equal(t, recordContactStepType, dex.GetFinalStepType[Input](recordContact{}))
	require.Equal(t, completeContactStepType, dex.GetFinalStepType[spreadsheet.UpsertRowResult](completeContact{}))
	wait, err := recordContact{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := spreadsheet.New(spreadsheet.Config{}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{})
	require.NoError(t, err)
	connection, err := spreadsheet.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := spreadsheet.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}

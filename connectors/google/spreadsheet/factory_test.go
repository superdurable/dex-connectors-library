// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type sheetTarget struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.UpsertRowResult]
}

func (sheetTarget) Execute(dex.Context, spreadsheet.UpsertRowResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestUpsertFactoryRequiresHappyPathAndAllowsOptionalBranches(t *testing.T) {
	client := newSheetsClient(t, "http://127.0.0.1:1")
	connection, err := spreadsheet.NewConnection(client, sheetsConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		spreadsheet.NewUpsertRowStep(spreadsheet.UpsertRowStepConfig[string]{
			StepType: "Upsert", Annotations: sdkgo.StepAnnotations{GroupID: "google", GroupLabel: "Google", Explanation: "Upsert a row."},
			Connection: connection, MapToOperationInput: func(string) spreadsheet.UpsertRowInput { return spreadsheet.UpsertRowInput{} },
			Upserted: sdkgo.GoTo(sheetTarget{}),
		})
	})
	require.Panics(t, func() {
		spreadsheet.NewUpsertRowStep(spreadsheet.UpsertRowStepConfig[string]{
			StepType: "Upsert", Annotations: sdkgo.StepAnnotations{GroupID: "google", GroupLabel: "Google", Explanation: "Upsert a row."},
			Connection: connection, MapToOperationInput: func(string) spreadsheet.UpsertRowInput { return spreadsheet.UpsertRowInput{} },
			Conflict: sdkgo.GoTo(sheetTarget{}),
		})
	})
}

func TestSheetsConnectionCannotBeSerialized(t *testing.T) {
	client := newSheetsClient(t, "http://127.0.0.1:1")
	connection, err := spreadsheet.NewConnection(client, sheetsConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sheets-token")
}

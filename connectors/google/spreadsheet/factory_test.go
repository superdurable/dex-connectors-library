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
	dex.StepDefaultsNoWaitFor[spreadsheet.UpsertRowStepOutput[string]]
}

func (sheetTarget) Execute(dex.Context, spreadsheet.UpsertRowStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestUpsertFactoryRequiresEveryTypedBranch(t *testing.T) {
	client := newSheetsClient(t, "http://127.0.0.1:1")
	connection, err := spreadsheet.NewConnection(client, sheetsConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		spreadsheet.NewUpsertRowStep(spreadsheet.UpsertRowStepConfig[string]{
			StepType: "Upsert", Presentation: sdkgo.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Upsert a row."},
			Connection: connection, BuildInput: func(string) (spreadsheet.UpsertRowInput, error) { return spreadsheet.UpsertRowInput{}, nil },
			Upserted: sdkgo.GoTo(sheetTarget{}), Conflict: sdkgo.GoTo(sheetTarget{}), Rejected: sdkgo.GoTo(sheetTarget{}), Uncertain: sdkgo.GoTo(sheetTarget{}),
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

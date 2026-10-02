// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var testAnnotations = sdkgo.StepAnnotations{GroupID: "excel", GroupLabel: "Excel", Explanation: "Use Microsoft Excel."}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := excel.NewConnection(newExcelClient(t, "http://127.0.0.1:1"), excelConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		excel.NewAppendTableRowsStep(excel.AppendTableRowsStepConfig[string]{
			StepType: "Append", Annotations: testAnnotations, Connection: connection, ConnectionName: "approvals",
			MapToOperationInput: func(string) excel.AppendTableRowsInput { return excel.AppendTableRowsInput{} },
			Appended:            sdkgo.GoTo(sdkgo.StepRef[excel.AppendTableRowsResult]("Done")),
		})
		excel.NewGetTableRowsStep(excel.GetTableRowsStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) excel.GetTableRowsInput { return excel.GetTableRowsInput{} },
			Read:                sdkgo.GoTo(sdkgo.StepRef[excel.GetTableRowsResult]("Done")),
		})
	})
	require.Panics(t, func() {
		excel.NewAppendTableRowsStep(excel.AppendTableRowsStepConfig[string]{
			StepType: "Append", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) excel.AppendTableRowsInput { return excel.AppendTableRowsInput{} },
			Uncertain:           sdkgo.GoTo(sdkgo.StepRef[excel.AppendTableRowsResult]("Review")),
		})
	}, "the appended branch is required")
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection, err := excel.NewConnection(newExcelClient(t, "http://127.0.0.1:1"), excelConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		excel.NewGetValuesStep(excel.GetValuesStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) excel.GetValuesInput { return excel.GetValuesInput{} },
			Read:                sdkgo.GoTo(sdkgo.StepRef[excel.GetValuesResult]("Done")),
		})
	})
}

func TestExcelConnectionCannotBeSerialized(t *testing.T) {
	connection, err := excel.NewConnection(newExcelClient(t, "http://127.0.0.1:1"), excelConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), excelTestToken)
	require.Equal(t, "excel.Connection{[REDACTED]}", connection.String())
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[excel.Credentials]{}
	for name, scenario := range map[string]struct {
		config  excel.Config
		options []excel.Option
	}{
		"negative response limit": {config: excel.Config{MaxResponseBytes: -1}},
		"cell limit above 100000": {config: excel.Config{MaxCells: excel.MaximumCellsLimit + 1}},
		"remote local provider":   {options: []excel.Option{excel.WithLocalProviderURL("https://graph.example.com")}},
		"local provider path":     {options: []excel.Option{excel.WithLocalProviderURL("http://127.0.0.1:9/v1.0")}},
		"nil option":              {options: []excel.Option{nil}},
	} {
		_, err := excel.New(scenario.config, credentials, scenario.options...)
		require.Error(t, err, name)
	}
	_, err := excel.New(excel.Config{}, nil)
	require.Error(t, err)
	client, err := excel.New(excel.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestLocalProviderTransportRefusesHostsOtherThanMicrosoft(t *testing.T) {
	routed, err := excel.NewLocalProviderHTTPClientForTest("http://127.0.0.1:1")
	require.NoError(t, err)
	_, err = routed.Get("https://example.com/v1.0/me")
	require.Error(t, err)
	require.Contains(t, err.Error(), "refuses a host")
}

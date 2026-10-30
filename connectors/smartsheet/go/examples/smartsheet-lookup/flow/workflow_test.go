// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet"
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/smartsheetmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestMapToListSheetsInput(t *testing.T) {
	got := MapToListSheetsInput(Request{})
	require.Equal(t, smartsheet.ListSheetsInput{PageSize: 10}, got)
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := smartsheetmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}

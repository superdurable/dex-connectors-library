// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employeechangesweep

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordSweepWindowStepType, dex.GetFinalStepType[Input](recordSweepWindow{}))
	require.Equal(t, collectEmployeeChangesStepType, dex.GetFinalStepType[bamboohr.ListEmployeeChangesResult](collectEmployeeChanges{}))
	wait, err := recordSweepWindow{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildFirstSweepPageValidatesStartInput(t *testing.T) {
	sweep, page, err := BuildFirstSweepPage(Input{Since: " 2026-09-24T00:00:00-06:00 ", AfterEmployeeID: "41", ChangeType: bamboohr.EmployeeChangeTypeInserted})
	require.NoError(t, err)
	cursor := bamboohr.EmployeeChangeCursor{Since: time.Date(2026, 9, 24, 6, 0, 0, 0, time.UTC), AfterEmployeeID: "41"}
	require.Equal(t, EmployeeChangeSweep{
		ChangeType: bamboohr.EmployeeChangeTypeInserted, PageSize: DefaultPageSize, MaxPages: DefaultMaxPages,
		Changes: []bamboohr.EmployeeChange{}, NextCursor: cursor,
	}, sweep)
	require.Equal(t, SweepPage{Cursor: cursor, ChangeType: bamboohr.EmployeeChangeTypeInserted, PageSize: DefaultPageSize}, page)
	require.Equal(t, bamboohr.ListEmployeeChangesInput{
		Since: cursor.Since, AfterEmployeeID: "41", ChangeType: bamboohr.EmployeeChangeTypeInserted, Limit: DefaultPageSize,
	}, MapToListEmployeeChangesInput(page))
	for name, input := range map[string]Input{
		"no since":     {},
		"date only":    {Since: "2026-09-24"},
		"unknown type": {Since: "2026-09-24T00:00:00Z", ChangeType: "all"},
		"page 1001":    {Since: "2026-09-24T00:00:00Z", PageSize: 1001},
		"eleven pages": {Since: "2026-09-24T00:00:00Z", MaxPages: 11},
	} {
		_, _, err := BuildFirstSweepPage(input)
		require.Error(t, err, name)
	}
}

func newUnitTestConnection(t *testing.T, connectionName string) bamboohr.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "bamboohr", Name: connectionName}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme"},
		sdkgo.StaticCredentialProvider[bamboohr.Credentials]{reference: {APIKey: sdkgo.NewSecretString("0123456789abcdef" + "0123456789abcdef01234567")}})
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

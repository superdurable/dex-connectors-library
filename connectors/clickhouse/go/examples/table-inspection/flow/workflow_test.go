// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tableinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickhouse/go"
	"github.com/superdurable/dex-connectors-library/connectors/clickhouse/go/clickhousemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTableInspectionFlowIdentityAndMapping(t *testing.T) {
	input := clickhouse.ListTablesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(clickhouse.Connection{})))
	require.Equal(t, "StartListTables", dex.GetFinalStepType[clickhouse.ListTablesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := clickhousemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}

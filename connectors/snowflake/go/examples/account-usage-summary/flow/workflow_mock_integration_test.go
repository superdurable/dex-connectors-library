//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountusagesummary

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/snowflakemock"
)

func TestAccountUsageSummaryUsesMockWithRealDex(t *testing.T) {
	mock := snowflakemock.New(t, ConnectionName)
	harness := newUsageHarnessForConnection(t, mock.Connection(), 5)
	input := Input{AccountID: "acct_003", Since: "2026-01-01"}
	record := harness.runSummary(t, "mock-default", input)
	require.Equal(t, input, record.Request)
	require.Equal(t, PhaseCompleted, record.Phase)
	require.Equal(t, 0, record.StatusReads)
	require.NotNil(t, record.Usage)

	submitCalls := mock.SubmitStatement().Calls()
	require.Len(t, submitCalls, 1)
	require.Equal(t, snowflake.SubmitStatementInput{
		Statement: UsageSummaryStatement, Parameters: []any{"acct_003", "2026-01-01"},
	}, submitCalls[0].Input)
	require.Equal(t, snowflake.SubmitStatementBranchSubmitted, submitCalls[0].Branch)
	readCalls := mock.GetStatementResult().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, snowflake.GetStatementResultInput{
		StatementHandle: "00000000-0000-4000-8000-000000000003",
	}, readCalls[0].Input)
	require.Equal(t, snowflake.GetStatementResultBranchCompleted, readCalls[0].Branch)
	require.Empty(t, mock.CancelStatement().Calls())
}

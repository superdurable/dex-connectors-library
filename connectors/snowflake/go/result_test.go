// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/fakesnowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var text = fakesnowflake.Text

var usageColumns = []fakesnowflake.Column{
	{Name: "EVENT_COUNT", Type: "fixed", Precision: 18},
	{Name: "CREDITS_USED", Type: "fixed", Precision: 38, Scale: 4, Nullable: true},
	{Name: "LAST_EVENT_AT", Type: "timestamp_ntz", Scale: 9, Nullable: true},
}

func submitScripted(t *testing.T, client *snowflake.Client) string {
	t.Helper()
	submitted, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchSubmitted, submitted.Branch, submitted.Failure)
	return submitted.Value.StatementHandle
}

func TestGetStatementResultReportsRunningThenTheFirstPartition(t *testing.T) {
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{
		RunningReads: 2, Columns: usageColumns,
		Partitions: [][][]*string{
			{{text("9007199254740993"), text("1234.5000"), text("1767225600.123456789")}},
			{{text("7"), nil, nil}},
		},
	}))
	client := newTestClient(t, fake, tokenCredentials(), nil)
	handle := submitScripted(t, client)
	for range 2 {
		running, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: handle})
		require.NoError(t, err)
		require.Equal(t, snowflake.GetStatementResultBranchRunning, running.Branch, running.Failure)
		require.Equal(t, handle, running.Value.StatementHandle)
		require.Empty(t, running.Value.Rows)
	}
	completed, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: handle})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchCompleted, completed.Branch, completed.Failure)
	require.Equal(t, []snowflake.Column{
		{Name: "EVENT_COUNT", Type: "fixed", Precision: 18},
		{Name: "CREDITS_USED", Type: "fixed", Precision: 38, Scale: 4, Nullable: true},
		{Name: "LAST_EVENT_AT", Type: "timestamp_ntz", Scale: 9, Nullable: true},
	}, completed.Value.Columns)
	require.Equal(t, []map[string]any{{
		"EVENT_COUNT": "9007199254740993", "CREDITS_USED": "1234.5000", "LAST_EVENT_AT": "2026-01-01T00:00:00.123456789",
	}}, completed.Value.Rows)
	require.Equal(t, 2, completed.Value.PartitionCount)
	require.Equal(t, int64(2), *completed.Value.TotalRowCount)
	require.Equal(t, map[string]string{"code": "090001", "sqlState": "00000"}, completed.Receipt.Metadata)
	require.Equal(t, 3, fake.ReadCount(handle))
	requireProviderTextFree(t, completed)

	later, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: handle, Partition: 1, Columns: completed.Value.Columns})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchCompleted, later.Branch, later.Failure)
	require.Equal(t, []map[string]any{{"EVENT_COUNT": "7", "CREDITS_USED": nil, "LAST_EVENT_AT": nil}}, later.Value.Rows,
		"the gzip-compressed later partition is decoded with the partition-0 columns")
	require.Equal(t, 1, later.Value.Partition)
	require.Zero(t, later.Value.PartitionCount)

	missingColumns, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: handle, Partition: 1})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchDefect, missingColumns.Branch)
}

func TestGetStatementResultSelectsTruncatedAtTheRowAndByteBounds(t *testing.T) {
	rows := make([][]*string, 5)
	for index := range rows {
		rows[index] = []*string{text("1"), text("1.0000"), nil}
	}
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{Columns: usageColumns, Partitions: [][][]*string{rows}}))
	byRows := newTestClient(t, fake, tokenCredentials(), func(config *snowflake.Config) { config.MaxRows = 3 })
	result, err := readResult(byRows, snowflake.GetStatementResultInput{StatementHandle: submitScripted(t, byRows)})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchTruncated, result.Branch)
	require.Len(t, result.Value.Rows, 3)
	require.True(t, result.Value.Truncated)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	byBytes := newTestClient(t, fake, tokenCredentials(), func(config *snowflake.Config) { config.MaxResponseBytes = 140 })
	result, err = readResult(byBytes, snowflake.GetStatementResultInput{StatementHandle: submitScripted(t, byBytes)})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchTruncated, result.Branch)
	encoded, err := json.Marshal(result.Value.Rows)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 140)
	require.Len(t, result.Value.Rows, 2)
}

func TestGetStatementResultClassifiesFailuresWithoutProviderText(t *testing.T) {
	fake := startFake(t, func(submitted fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
		switch submitted.Statement {
		case "SELECT 1/0":
			return fakesnowflake.StatementScript{Failure: &fakesnowflake.StatementFailure{Code: "100051", SQLState: "22012"}}
		case "SELECT bad_value":
			return fakesnowflake.StatementScript{Columns: usageColumns[:1], Partitions: [][][]*string{{{text("12abc")}}}}
		default:
			return fakesnowflake.StatementScript{
				Columns: []fakesnowflake.Column{{Name: "ID", Type: "text"}, {Name: "ID", Type: "text"}}, Partitions: [][][]*string{{{text("a"), text("b")}}},
			}
		}
	})
	client := newTestClient(t, fake, tokenCredentials(), nil)
	read := func(statement string) snowflake.GetStatementResultResult {
		submitted, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: statement})
		require.NoError(t, err)
		result, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: submitted.Value.StatementHandle})
		require.NoError(t, err)
		requireProviderTextFree(t, result)
		return result
	}

	failed := read("SELECT 1/0")
	require.Equal(t, snowflake.GetStatementResultBranchProviderRejected, failed.Branch)
	require.Equal(t, sdkgo.FailureValidation, failed.Failure.Kind)
	require.Equal(t, map[string]string{"code": "100051", "sqlState": "22012"}, failed.Receipt.Metadata)

	undecodable := read("SELECT bad_value")
	require.Equal(t, snowflake.GetStatementResultBranchInvalidResponse, undecodable.Branch)
	require.Equal(t, `column "EVENT_COUNT" of type fixed returned text the connector cannot decode`, undecodable.Failure.Message)

	duplicate := read("SELECT a.id, b.id FROM a JOIN b")
	require.Equal(t, snowflake.GetStatementResultBranchDefect, duplicate.Branch)
	require.Contains(t, duplicate.Failure.Message, "unique alias")

	unknown, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: "01b99999-0000-4000-8000-000000099999"})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchProviderRejected, unknown.Branch)
	require.Equal(t, sdkgo.FailureNotFound, unknown.Failure.Kind)

	invalidHandle, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: "../statements"})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchDefect, invalidHandle.Branch)
}

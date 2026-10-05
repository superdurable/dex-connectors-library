// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountusagesummary

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMappersBindValuesAndNeverFormatThemIntoSQL(t *testing.T) {
	flow := NewFlow(snowflake.Connection{}, &StatusPolicy{CheckInterval: time.Second, MaximumRunningReads: 1})
	submit := flow.MapToSubmitUsageQueryInput(Input{AccountID: "acct_003'; DROP TABLE x; --", Since: "2026-01-01"})
	require.Equal(t, UsageSummaryStatement, submit.Statement)
	require.Equal(t, []any{"acct_003'; DROP TABLE x; --", "2026-01-01"}, submit.Parameters)
	require.Equal(t, 2, countPlaceholders(UsageSummaryStatement))

	check := StatementCheck{StatementHandle: "01b00001-0000-4000-8000-000000000001"}
	require.Equal(t, snowflake.GetStatementResultInput{StatementHandle: check.StatementHandle}, flow.MapToReadUsageResultInput(check))
	require.Equal(t, snowflake.CancelStatementInput{StatementHandle: check.StatementHandle}, flow.MapToCancelUsageQueryInput(check))
}

func TestRecordUsageRequestRejectsInvalidInputBeforeSnowflake(t *testing.T) {
	for name, testCase := range map[string]struct {
		input   Input
		message string
	}{
		"missing account": {Input{Since: "2026-01-01"}, "accountId is required and must be at most 100 bytes"},
		"not a date":      {Input{AccountID: "acct_003", Since: "01/01/2026"}, "since must be a date such as 2026-01-01"},
	} {
		t.Run(name, func(t *testing.T) {
			decision, err := recordUsageRequest{}.Execute(nil, testCase.input)
			require.NoError(t, err)
			require.Equal(t, dex.ForceFail(testCase.message), decision)
		})
	}
}

func TestDecodeAccountUsageAcceptsExactDecimalsAndSmallNumbers(t *testing.T) {
	usage, err := decodeAccountUsage(map[string]any{"EVENT_COUNT": "9007199254740993", "CREDITS_USED": "42.5000", "LAST_EVENT_AT": nil})
	require.NoError(t, err)
	require.Equal(t, AccountUsage{EventCount: "9007199254740993", CreditsUsed: "42.5000"}, usage)

	usage, err = decodeAccountUsage(map[string]any{"EVENT_COUNT": float64(12), "CREDITS_USED": json.Number("0"), "LAST_EVENT_AT": "2026-01-01T00:00:00"})
	require.NoError(t, err)
	require.Equal(t, "12", usage.EventCount)
	require.Equal(t, "2026-01-01T00:00:00", *usage.LastEventAt)

	_, err = decodeAccountUsage(map[string]any{"EVENT_COUNT": 1.5, "CREDITS_USED": "0"})
	require.Error(t, err)
}

func TestRecordFailureKeepsOnlyCodesAndTheFailureKind(t *testing.T) {
	record := UsageSummaryRecord{}
	recordFailure(&record, sdkgo.Receipt{Metadata: map[string]string{"code": "000904", "sqlState": "42000"}},
		&sdkgo.Failure{Kind: sdkgo.FailureValidation, Message: "Snowflake returned HTTP 422"})
	require.Equal(t, UsageSummaryRecord{SnowflakeCode: "000904", SQLState: "42000", FailureKind: sdkgo.FailureValidation}, record)
}

func TestFlowIdentitiesAndPolicyGuards(t *testing.T) {
	policy := DefaultStatusPolicy()
	require.Equal(t, 15*time.Second, policy.CheckInterval)
	require.Equal(t, 40, policy.MaximumRunningReads)
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(snowflake.Connection{}, &policy)))
	require.Equal(t, recordUsageRequestStepType, dex.GetFinalStepType[Input](recordUsageRequest{}))
	require.Equal(t, waitForUsageQueryStepType, dex.GetFinalStepType[StatementCheck](waitForUsageQuery{}))
	require.Panics(t, func() { NewFlow(snowflake.Connection{}, nil) })
	require.Panics(t, func() {
		NewFlow(snowflake.Connection{}, &StatusPolicy{CheckInterval: time.Millisecond, MaximumRunningReads: 1})
	})

	wait, err := waitForUsageQuery{checkInterval: 3 * time.Second}.WaitFor(nil, StatementCheck{})
	require.NoError(t, err)
	require.Equal(t, dex.Until(dex.Timer(3*time.Second)), wait, "the wait is a durable Timer, not a sleep")
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := snowflake.New(snowflake.Config{AccountIdentifier: "myorg-analytics"}, sdkgo.StaticCredentialProvider[snowflake.Credentials]{})
	require.NoError(t, err)
	policy := DefaultStatusPolicy()
	connection, err := snowflake.NewConnection(client, sdkgo.ConnectionRef{Provider: "snowflake", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, &policy)})
	require.NoError(t, err)

	other, err := snowflake.NewConnection(client, sdkgo.ConnectionRef{Provider: "snowflake", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(other, &policy)}) })
}

func countPlaceholders(statement string) int {
	count := 0
	for _, character := range statement {
		if character == '?' {
			count++
		}
	}
	return count
}

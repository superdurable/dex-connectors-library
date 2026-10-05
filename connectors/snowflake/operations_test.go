// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/fakesnowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const usageStatement = "SELECT COUNT(*) AS EVENT_COUNT FROM USAGE_EVENTS WHERE ACCOUNT_ID = ? AND EVENT_AT >= ? -- why '?' here"

func TestSubmitStatementSendsOneAsyncRequestWithTypedBindings(t *testing.T) {
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{RunningReads: 1}))
	client := newTestClient(t, fake, tokenCredentials(), func(config *snowflake.Config) { config.StatementTimeoutSeconds = 2700 })
	step := newStep()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", -8*3600))
	result, err := submit(step, client, snowflake.SubmitStatementInput{
		Statement: usageStatement, Parameters: []any{"acct_" + cardLikeText, since},
	})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchSubmitted, result.Branch, result.Failure)
	submissions := fake.Submissions()
	require.Len(t, submissions, 1)
	sent := submissions[0]
	require.Equal(t, string(result.Receipt.IdempotencyKey), sent.RequestID, "the Step's idempotency key is the requestId")
	require.True(t, sent.IsRetry, "every submission can be deduplicated")
	require.True(t, sent.IsAsync, "a warehouse statement never holds the Execute open")
	require.Equal(t, "PROGRAMMATIC_ACCESS_TOKEN", sent.TokenType)
	require.Equal(t, usageStatement, sent.Statement, "values are bound, never interpolated")
	require.Equal(t, int64(2700), sent.Timeout)
	require.Equal(t, []string{"REPORTING_WH", "DEX_REPORTING", "ANALYTICS", "BILLING"}, []string{sent.Warehouse, sent.Role, sent.Database, sent.Schema})
	require.Equal(t, map[string]string{"MULTI_STATEMENT_COUNT": "1"}, sent.Parameters)
	require.Equal(t, "TEXT", *sent.Bindings["1"]["type"])
	require.Equal(t, "acct_"+cardLikeText, *sent.Bindings["1"]["value"])
	require.Equal(t, "TIMESTAMP_TZ", *sent.Bindings["2"]["type"])
	require.Equal(t, "1767254400000000000 960", *sent.Bindings["2"]["value"], "epoch nanoseconds and the offset plus 1440 minutes")

	handle := fake.HandleForRequestID(sent.RequestID)
	require.Equal(t, handle, result.Value.StatementHandle)
	require.Equal(t, handle, result.Receipt.ProviderObjectID)
	require.NotNil(t, result.Value.SubmittedAt)
	require.Equal(t, fixedNow, result.Receipt.ObservedAt)
	requireProviderTextFree(t, result)
}

func TestSubmitStatementRepeatedByOneStepExecutionReturnsTheSameStatement(t *testing.T) {
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{RunningReads: 5}))
	client := newTestClient(t, fake, keyPairCredentials(t), nil)
	step := newStep()
	first, err := submit(step, client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchSubmitted, first.Branch, first.Failure)
	second, err := submit(step, client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, first.Value.StatementHandle, second.Value.StatementHandle)
	require.Equal(t, 1, fake.ExecutionCount(), "requestId with retry=true never runs the statement twice")
	submissions := fake.Submissions()
	require.Len(t, submissions, 2)
	require.Equal(t, submissions[0].RequestID, submissions[1].RequestID)
	require.Equal(t, "KEYPAIR_JWT", submissions[0].TokenType, "the fake verified the RS256 JWT, its iss fingerprint, and its sub")

	other, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.NotEqual(t, first.Value.StatementHandle, other.Value.StatementHandle, "another Step execution is another statement")
}

func TestSubmitStatementClassifiesSnowflakeResponses(t *testing.T) {
	compilationError := &fakesnowflake.StatementFailure{Code: "000904", SQLState: "42000"}
	fake := startFake(t, func(submitted fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
		switch submitted.Statement {
		case "SELECT missing_column FROM t":
			return fakesnowflake.StatementScript{RejectSubmission: compilationError}
		case "SELECT busy":
			return fakesnowflake.StatementScript{TransientSubmitStatuses: []int{http.StatusTooManyRequests}}
		default:
			return fakesnowflake.StatementScript{TransientSubmitStatuses: []int{http.StatusServiceUnavailable}}
		}
	})
	client := newTestClient(t, fake, tokenCredentials(), nil)

	rejected, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT missing_column FROM t"})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureValidation, rejected.Failure.Kind)
	require.Equal(t, "Snowflake returned HTTP 422 with code 000904 and SQLSTATE 42000", rejected.Failure.Message)
	require.Equal(t, map[string]string{"code": "000904", "sqlState": "42000"}, rejected.Receipt.Metadata)
	requireProviderTextFree(t, rejected)
	require.Zero(t, fake.ExecutionCount())

	step := newStep()
	_, err = submit(step, client, snowflake.SubmitStatementInput{Statement: "SELECT busy"})
	requireRetry(t, err, sdkgo.FailureRateLimit)
	retried, err := submit(step, client, snowflake.SubmitStatementInput{Statement: "SELECT busy"})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchSubmitted, retried.Branch)

	_, err = submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT unavailable"})
	requireRetry(t, err, sdkgo.FailureAvailability)

	unauthorized := newTestClient(t, fake, snowflake.Credentials{
		AuthMethodID: snowflake.ProgrammaticAccessTokenAuthMethodID, ProgrammaticAccessToken: sdkgo.NewSecretString("another-token"),
	}, nil)
	denied, err := submit(newStep(), unauthorized, snowflake.SubmitStatementInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, snowflake.SubmitStatementBranchProviderRejected, denied.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, denied.Failure.Kind)
	requireProviderTextFree(t, denied)
}

func TestSubmitStatementRejectsDefectsBeforeAnyRequest(t *testing.T) {
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{}))
	client := newTestClient(t, fake, tokenCredentials(), nil)
	for name, input := range map[string]snowflake.SubmitStatementInput{
		"blank statement":         {Statement: "  "},
		"too few parameters":      {Statement: "SELECT ? , ?", Parameters: []any{1}},
		"unsupported type":        {Statement: "SELECT ?", Parameters: []any{struct{}{}}},
		"non-finite float":        {Statement: "SELECT ?", Parameters: []any{math.Inf(1)}},
		"invalid json.Number":     {Statement: "SELECT ?", Parameters: []any{json.Number("NaN")}},
		"unterminated literal":    {Statement: "SELECT 'open"},
		"placeholder in comment":  {Statement: "SELECT 1 /* ? */", Parameters: []any{1}},
		"placeholder in a string": {Statement: "SELECT 'a?b', $$?$$, \"?\"", Parameters: []any{1}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := submit(newStep(), client, input)
			require.NoError(t, err)
			require.Equal(t, snowflake.SubmitStatementBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, fake.Submissions())
}

func TestCancelStatementCancelsARunningStatement(t *testing.T) {
	fake := startFake(t, scriptAlways(fakesnowflake.StatementScript{RunningReads: 100}))
	client := newTestClient(t, fake, tokenCredentials(), nil)
	submitted, err := submit(newStep(), client, snowflake.SubmitStatementInput{Statement: "SELECT SYSTEM$WAIT(600)"})
	require.NoError(t, err)
	handle := submitted.Value.StatementHandle

	canceled, err := cancel(client, handle)
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchCanceled, canceled.Branch, canceled.Failure)
	require.Equal(t, handle, canceled.Value.StatementHandle)
	require.Equal(t, map[string]string{"code": "000604", "sqlState": "57014"}, canceled.Receipt.Metadata)

	read, err := readResult(client, snowflake.GetStatementResultInput{StatementHandle: handle})
	require.NoError(t, err)
	require.Equal(t, snowflake.GetStatementResultBranchProviderRejected, read.Branch)
	require.Equal(t, sdkgo.FailureAvailability, read.Failure.Kind)
	require.Contains(t, read.Failure.Message, "exceeded statementTimeoutSeconds or was canceled")

	unknown, err := cancel(client, "01b99999-0000-4000-8000-000000099999")
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchProviderRejected, unknown.Branch)
	require.Equal(t, sdkgo.FailureNotFound, unknown.Failure.Kind)
	requireProviderTextFree(t, unknown)

	invalid, err := cancel(client, "not-a-handle")
	require.NoError(t, err)
	require.Equal(t, snowflake.CancelStatementBranchDefect, invalid.Branch)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/go/internal/scriptedtds"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

var addToBalanceInput = sqlserver.ExecuteStatementInput{
	Statement: "UPDATE dbo.balances SET amount = amount + @p1 WHERE account_id = @p2", Parameters: []any{"25.00", secretLookingText},
}

func newFencedStep() *singledispatchtest.Context {
	return singledispatchtest.NewContext("scripted-flow", fmt.Sprintf("fenced-step-%d", stepSequence.Add(1)))
}

func executeNonIdempotent(t *testing.T, client *sqlserver.Client, step *singledispatchtest.Context, input sqlserver.ExecuteStatementInput) (sqlserver.ExecuteStatementNonIdempotentResult, error) {
	t.Helper()
	return sdkgo.RunMutation(step, client.ExecuteStatementNonIdempotent(), scriptedConnection, input)
}

func lastMarker(t *testing.T, step *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := step.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func TestExecuteStatementNonIdempotentDeclaresTheUnsafeFencedTier(t *testing.T) {
	definition := sqlserver.ExecuteStatementNonIdempotentDefinition
	require.Equal(t, sdkgo.RepeatOnUnknownUnsafe, definition.RepeatOnUnknown)
	require.Equal(t, sdkgo.RepeatOnUnknownSafe, sqlserver.ExecuteStatementDefinition.RepeatOnUnknown)
	require.Equal(t, dex.StepDurabilitySync, definition.StepDefaults.ExecuteDurability)
	require.Equal(t, "executeNonIdempotent", definition.Operation.OperationID)
}

func TestExecuteStatementNonIdempotentClaimsBeforeCommitAndCommitsOnce(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, int64(1), result.Value.RowsAffected)
	require.True(t, database.hasCommitted.Load())
	require.Len(t, server.EventsOfKind(scriptedtds.EventCommit), 1)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventAttention), "ending the statement context sends no attention before COMMIT")
	require.Equal(t, singledispatch.StateClaimed, lastMarker(t, step).State, "COMMIT was claimed before it was sent")
	requireSecretFree(t, result)
}

func TestExecuteStatementNonIdempotentSelectsUncertainAndNeverRunsAgainAfterAnUnknownCommit(t *testing.T) {
	for name, commit := range map[string]scriptedtds.CommitAction{
		"a COMMIT whose reply was lost after it applied": scriptedtds.CommitThenDropConnection,
		"a connection lost after COMMIT was sent":        scriptedtds.DropConnectionWithoutCommitting,
		"a server error while committing":                scriptedtds.RejectCommitWithServerError,
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}, commit: commit}
			server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
			step := newFencedStep()
			result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err, "an unsafe write never retries an unknown COMMIT")
			require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchUncertain, result.Branch)
			require.NotEmpty(t, result.Receipt.IdempotencyKey)
			requireSecretFree(t, result)

			logins := len(server.EventsOfKind(scriptedtds.EventLogin))
			step.AdvanceAttempt()
			again, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err)
			require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchUncertain, again.Branch)
			require.Len(t, server.EventsOfKind(scriptedtds.EventLogin), logins, "a later attempt never connects")
		})
	}
}

func TestExecuteStatementNonIdempotentResendsAfterAWriteLostBeforeCommit(t *testing.T) {
	dropping := &scriptedDatabase{execution: scriptedtds.Execution{DropConnection: true}}
	server, client := startScriptedDatabase(t, dropping, scriptedtds.Options{}, nil)
	step := newFencedStep()
	_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	require.Equal(t, singledispatch.StateNotSent, lastMarker(t, step).State)

	// The checkpoint lives in the Step context, so the next attempt may reach another server.
	healthy := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	_, client = startScriptedDatabase(t, healthy, scriptedtds.Options{}, nil)
	step.AdvanceAttempt()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
	require.True(t, healthy.hasCommitted.Load())
}

func TestExecuteStatementNonIdempotentNeverRecordsNotSentAfterAPossibleEarlierCommit(t *testing.T) {
	dropping := &scriptedDatabase{execution: scriptedtds.Execution{DropConnection: true}}
	server, client := startScriptedDatabase(t, dropping, scriptedtds.Options{}, nil)
	step := newFencedStep()
	_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	marker := lastMarker(t, step)
	// A canResend marker that still counts one possibly effective send keeps SendAllowed.
	step.AdvanceAttempt()
	require.NoError(t, step.SetPersistedCheckpoint(singledispatch.Marker{CallID: marker.CallID, Attempt: 2, State: singledispatch.StateCanResend, SendCount: 1}))
	step.AdvanceAttempt()
	heartbeatCount := len(step.Heartbeats())

	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchUncertain, result.Branch, "the earlier attempt counts as a possible COMMIT")
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	require.Len(t, step.Heartbeats(), heartbeatCount, "notSent would hide the earlier possible COMMIT")
}

func TestExecuteStatementNonIdempotentSelectsUncertainAfterALostWorker(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	step := newFencedStep()
	step.AdvanceAttempt()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchUncertain, result.Branch, "attempt 2 without a checkpoint may follow a sent COMMIT")
	require.Empty(t, server.EventsOfKind(scriptedtds.EventLogin))
}

func TestExecuteStatementNonIdempotentRetriesAnUnreadableCheckpoint(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	step := newFencedStep()
	step.SetReadError(errors.New("heartbeat store unavailable"))
	_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "FailClosed never selects a terminal branch")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventLogin))
}

func TestExecuteStatementNonIdempotentRejectsADefiniteRefusalWithoutCommitting(t *testing.T) {
	violation := &scriptedDatabase{execution: scriptedtds.Execution{Error: &scriptedtds.ServerError{
		Number: 547, State: 0, Severity: 16, Message: "The UPDATE statement conflicted with the CHECK constraint \"ck_balances_amount\".",
	}}}
	server, client := startScriptedDatabase(t, violation, scriptedtds.Options{}, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchProviderRejected, result.Branch, result.Failure)
	require.Equal(t, "executeNonIdempotent", result.Failure.Operation)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	require.Equal(t, singledispatch.StateNotSent, lastMarker(t, step).State, "the refusal proves nothing was committed")
	requireSecretFree(t, result)

	noTransaction := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}, commit: scriptedtds.RejectCommitWithNoTransaction}
	_, client = startScriptedDatabase(t, noTransaction, scriptedtds.Options{}, nil)
	atCommit := newFencedStep()
	result, err = executeNonIdempotent(t, client, atCommit, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchProviderRejected, result.Branch, "error 3902 proves nothing committed")
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, atCommit).State)

	tooMany := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 3}}
	server, client = startScriptedDatabase(t, tooMany, scriptedtds.Options{}, nil)
	limited := addToBalanceInput
	maxRowsAffected := int64(1)
	limited.MaxRowsAffected = &maxRowsAffected
	result, err = executeNonIdempotent(t, client, newFencedStep(), limited)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchLimitExceeded, result.Branch)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))

	result, err = executeNonIdempotent(t, client, newFencedStep(), sqlserver.ExecuteStatementInput{Statement: "COMMIT"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementNonIdempotentBranchDefect, result.Branch)
}

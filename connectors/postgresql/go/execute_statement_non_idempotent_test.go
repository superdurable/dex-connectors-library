// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go/internal/scriptedpostgresql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const addToBalanceStatement = "UPDATE balances SET amount = amount + $1::numeric WHERE account_id = $2"

var addToBalanceInput = postgresql.ExecuteStatementInput{Statement: addToBalanceStatement, Parameters: []any{"25.00", secretLookingText}}

func newFencedStep() *singledispatchtest.Context {
	return singledispatchtest.NewContext("scripted-flow", fmt.Sprintf("fenced-step-%d", stepSequence.Add(1)))
}

func executeNonIdempotent(t *testing.T, client *postgresql.Client, step *singledispatchtest.Context, input postgresql.ExecuteStatementInput) (postgresql.ExecuteStatementNonIdempotentResult, error) {
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
	definition := postgresql.ExecuteStatementNonIdempotentDefinition
	require.Equal(t, sdkgo.RepeatOnUnknownUnsafe, definition.RepeatOnUnknown)
	require.Equal(t, sdkgo.RepeatOnUnknownSafe, postgresql.ExecuteStatementDefinition.RepeatOnUnknown)
	require.Equal(t, "executeNonIdempotent", definition.Operation.OperationID)
}

func TestExecuteStatementNonIdempotentClaimsBeforeCommitAndCommitsOnce(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}}
	server, client := startScriptedDatabase(t, database, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, postgresql.StatementExecution{Command: "UPDATE", RowsAffected: 1, Columns: []postgresql.Column{}, Rows: []map[string]any{}}, result.Value)
	require.True(t, database.hasCommitted.Load())
	require.Len(t, server.EventsOfKind(scriptedpostgresql.EventCommit), 1)
	require.Equal(t, singledispatch.StateClaimed, lastMarker(t, step).State, "COMMIT was claimed before it was sent")
	requireSecretFree(t, result)
}

func TestExecuteStatementNonIdempotentSelectsUncertainAndNeverRunsAgainAfterAnUnknownCommit(t *testing.T) {
	for name, testCase := range map[string]struct {
		commit        scriptedpostgresql.CommitAction
		wantCommitted bool
	}{
		"reply lost after the server committed": {scriptedpostgresql.CommitThenDropConnection, true},
		"connection lost before committing":     {scriptedpostgresql.DropConnectionWithoutCommitting, false},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}, commit: testCase.commit}
			server, client := startScriptedDatabase(t, database, nil)
			step := newFencedStep()
			result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err, "an unsafe write never retries an unknown COMMIT")
			require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
			require.NotEmpty(t, result.Receipt.IdempotencyKey)
			require.Equal(t, testCase.wantCommitted, database.hasCommitted.Load())
			requireSecretFree(t, result)

			startups := len(server.EventsOfKind(scriptedpostgresql.EventStartup))
			step.AdvanceAttempt()
			again, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err)
			require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchUncertain, again.Branch)
			require.Len(t, server.EventsOfKind(scriptedpostgresql.EventStartup), startups, "a later attempt never connects")
		})
	}
}

func TestExecuteStatementNonIdempotentResendsOnlyAfterAProvenRollback(t *testing.T) {
	t.Run("serialization failure at COMMIT", func(t *testing.T) {
		rejecting := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}, commit: scriptedpostgresql.RejectCommitWithSerializationFailure}
		_, client := startScriptedDatabase(t, rejecting, nil)
		step := newFencedStep()
		_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "SQLSTATE 40001 at COMMIT rolled back")
		require.Equal(t, singledispatch.StateCanResend, lastMarker(t, step).State)

		// The checkpoint lives in the Step context, so the next attempt may reach another server.
		healthy := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}}
		_, client = startScriptedDatabase(t, healthy, nil)
		step.AdvanceAttempt()
		result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
		require.NoError(t, err)
		require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
		require.True(t, healthy.hasCommitted.Load())
	})

	t.Run("connection lost before COMMIT", func(t *testing.T) {
		dropping := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{DropConnection: true}}
		server, client := startScriptedDatabase(t, dropping, nil)
		step := newFencedStep()
		_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit))
		require.Equal(t, singledispatch.StateNotSent, lastMarker(t, step).State)

		healthy := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}}
		_, client = startScriptedDatabase(t, healthy, nil)
		step.AdvanceAttempt()
		result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
		require.NoError(t, err)
		require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
	})
}

func TestExecuteStatementNonIdempotentSelectsUncertainAfterALostWorker(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}}
	server, client := startScriptedDatabase(t, database, nil)
	step := newFencedStep()
	step.AdvanceAttempt()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchUncertain, result.Branch, "attempt 2 without a checkpoint may follow a sent COMMIT")
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventStartup))
}

func TestExecuteStatementNonIdempotentRetriesAnUnreadableCheckpoint(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}}
	server, client := startScriptedDatabase(t, database, nil)
	step := newFencedStep()
	step.SetReadError(errors.New("heartbeat store unavailable"))
	_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "FailClosed never selects a terminal branch")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventStartup))
}

func TestExecuteStatementNonIdempotentRejectsADefiniteRefusalWithoutCommitting(t *testing.T) {
	violation := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{Error: &scriptedpostgresql.ServerError{
		Code: "23514", Message: "new row violates check constraint", ConstraintName: "balances_amount_check",
	}}}
	server, client := startScriptedDatabase(t, violation, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "executeNonIdempotent", result.Failure.Operation)
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit))
	require.Equal(t, singledispatch.StateNotSent, lastMarker(t, step).State, "the refusal proves nothing was committed")
	requireSecretFree(t, result)

	tooMany := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 2}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 3"}}
	server, client = startScriptedDatabase(t, tooMany, nil)
	limited := addToBalanceInput
	limited.MaxRowsAffected = int64Pointer(1)
	result, err = executeNonIdempotent(t, client, newFencedStep(), limited)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchLimitExceeded, result.Branch)
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit))

	result, err = executeNonIdempotent(t, client, newFencedStep(), postgresql.ExecuteStatementInput{Statement: "COMMIT"})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementNonIdempotentBranchDefect, result.Branch)
}

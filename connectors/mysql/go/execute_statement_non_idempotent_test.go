// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go/internal/scriptedmysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const addToBalanceStatement = "UPDATE balances SET amount = amount + ? WHERE account_id = ?"

var addToBalanceInput = mysql.ExecuteStatementInput{Statement: addToBalanceStatement, Parameters: []any{"25.00", secretLookingText}}

func newBalanceDatabase(execution scriptedmysql.Execution, commit scriptedmysql.CommitAction) *scriptedDatabase {
	return &scriptedDatabase{shape: scriptedmysql.Shape{ParameterCount: 2}, execution: execution, commit: commit}
}

func newFencedStep() *singledispatchtest.Context {
	return singledispatchtest.NewContext("scripted-flow", fmt.Sprintf("fenced-step-%d", stepSequence.Add(1)))
}

func executeNonIdempotent(t *testing.T, client *mysql.Client, step *singledispatchtest.Context, input mysql.ExecuteStatementInput) (mysql.ExecuteStatementNonIdempotentResult, error) {
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
	definition := mysql.ExecuteStatementNonIdempotentDefinition
	require.Equal(t, sdkgo.RepeatOnUnknownUnsafe, definition.RepeatOnUnknown)
	require.Equal(t, sdkgo.RepeatOnUnknownSafe, mysql.ExecuteStatementDefinition.RepeatOnUnknown)
	require.Equal(t, dex.StepDurabilitySync, definition.StepDefaults.ExecuteDurability)
	require.Equal(t, "executeNonIdempotent", definition.Operation.OperationID)
}

func TestExecuteStatementNonIdempotentClaimsBeforeCommitAndCommitsOnce(t *testing.T) {
	database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, int64(1), result.Value.RowsAffected)
	require.True(t, database.hasCommitted.Load())
	require.Len(t, server.EventsOfKind(scriptedmysql.EventCommit), 1)
	require.Equal(t, singledispatch.StateClaimed, lastMarker(t, step).State, "COMMIT was claimed before it was sent")
	requireSecretFree(t, result)
}

func TestExecuteStatementNonIdempotentSelectsUncertainAndNeverRunsAgainAfterAnUnknownCommit(t *testing.T) {
	for name, testCase := range map[string]struct {
		commit        scriptedmysql.CommitAction
		wantCommitted bool
	}{
		"reply lost after the server committed":         {scriptedmysql.CommitThenDropConnection, true},
		"connection lost before committing":             {scriptedmysql.DropConnectionWithoutCommitting, false},
		"server reported an error during COMMIT itself": {scriptedmysql.RejectCommitWithUnknownError, true},
	} {
		t.Run(name, func(t *testing.T) {
			database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, testCase.commit)
			server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
			step := newFencedStep()
			result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err, "an unsafe write never retries an unknown COMMIT")
			require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchUncertain, result.Branch)
			require.NotEmpty(t, result.Receipt.IdempotencyKey)
			require.Equal(t, testCase.wantCommitted, database.hasCommitted.Load())
			requireSecretFree(t, result)

			handshakes := len(server.EventsOfKind(scriptedmysql.EventHandshake))
			step.AdvanceAttempt()
			again, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err)
			require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchUncertain, again.Branch)
			require.Len(t, server.EventsOfKind(scriptedmysql.EventHandshake), handshakes, "a later attempt never connects")
		})
	}
}

func TestExecuteStatementNonIdempotentResendsOnlyAfterAProvenRollback(t *testing.T) {
	for name, first := range map[string]*scriptedDatabase{
		"deadlock at COMMIT":            newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.RejectCommitWithDeadlock),
		"connection lost before COMMIT": newBalanceDatabase(scriptedmysql.Execution{DropConnection: true}, scriptedmysql.CommitAndReply),
	} {
		t.Run(name, func(t *testing.T) {
			_, client := startScriptedDatabase(t, first, scriptedmysql.Options{}, nil)
			step := newFencedStep()
			_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the server rolled the transaction back")
			require.False(t, first.hasCommitted.Load())
			require.Contains(t, []singledispatch.State{singledispatch.StateCanResend, singledispatch.StateNotSent}, lastMarker(t, step).State)

			// The checkpoint lives in the Step context, so the next attempt may reach another server.
			healthy := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
			_, client = startScriptedDatabase(t, healthy, scriptedmysql.Options{}, nil)
			step.AdvanceAttempt()
			result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
			require.NoError(t, err)
			require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchCompleted, result.Branch, result.Failure)
			require.True(t, healthy.hasCommitted.Load())
		})
	}
}

func TestExecuteStatementNonIdempotentSelectsUncertainAfterALostWorker(t *testing.T) {
	database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	step := newFencedStep()
	step.AdvanceAttempt()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchUncertain, result.Branch, "attempt 2 without a checkpoint may follow a sent COMMIT")
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventHandshake))
}

func TestExecuteStatementNonIdempotentRetriesAnUnreadableCheckpoint(t *testing.T) {
	database := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 1}, scriptedmysql.CommitAndReply)
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	step := newFencedStep()
	step.SetReadError(errors.New("heartbeat store unavailable"))
	_, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "FailClosed never selects a terminal branch")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventHandshake))
}

func TestExecuteStatementNonIdempotentRejectsADefiniteRefusalWithoutCommitting(t *testing.T) {
	violation := newBalanceDatabase(scriptedmysql.Execution{Error: &scriptedmysql.ServerError{
		Number: 3819, SQLState: "HY000", Message: "Check constraint 'balances_amount_check' is violated.",
	}}, scriptedmysql.CommitAndReply)
	server, client := startScriptedDatabase(t, violation, scriptedmysql.Options{}, nil)
	step := newFencedStep()
	result, err := executeNonIdempotent(t, client, step, addToBalanceInput)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "executeNonIdempotent", result.Failure.Operation)
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit))
	require.Equal(t, singledispatch.StateNotSent, lastMarker(t, step).State, "the refusal proves nothing was committed")
	requireSecretFree(t, result)

	tooMany := newBalanceDatabase(scriptedmysql.Execution{AffectedRows: 3}, scriptedmysql.CommitAndReply)
	server, client = startScriptedDatabase(t, tooMany, scriptedmysql.Options{}, nil)
	limited := addToBalanceInput
	limited.MaxRowsAffected = int64Pointer(1)
	result, err = executeNonIdempotent(t, client, newFencedStep(), limited)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchLimitExceeded, result.Branch)
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit))

	result, err = executeNonIdempotent(t, client, newFencedStep(), mysql.ExecuteStatementInput{Statement: "COMMIT"})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementNonIdempotentBranchDefect, result.Branch)
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/scriptedpostgresql"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	scriptedPassword  = "scripted-password-value"
	secretLookingText = "customer-card-4242"
)

var scriptedConnection = sdkgo.ConnectionRef{Provider: "postgresql", Name: "scripted"}

// scriptedDatabase is one statement's scripted shape and result.
type scriptedDatabase struct {
	shape        scriptedpostgresql.Shape
	shapeError   *scriptedpostgresql.ServerError
	execution    scriptedpostgresql.Execution
	commit       scriptedpostgresql.CommitAction
	hasCommitted atomic.Bool
}

func (database *scriptedDatabase) script() scriptedpostgresql.Script {
	return scriptedpostgresql.Script{
		Describe: func(string) (scriptedpostgresql.Shape, *scriptedpostgresql.ServerError) {
			return database.shape, database.shapeError
		},
		Execute: func(scriptedpostgresql.Statement) scriptedpostgresql.Execution {
			execution := database.execution
			execution.OnCommit = func() { database.hasCommitted.Store(true) }
			return execution
		},
		Commit: func() scriptedpostgresql.CommitAction { return database.commit },
	}
}

func startScriptedDatabase(t *testing.T, database *scriptedDatabase, mutate func(*postgresql.Config)) (*scriptedpostgresql.Server, *postgresql.Client) {
	t.Helper()
	server, err := scriptedpostgresql.Start(scriptedPassword, database.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := postgresql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: postgresql.SSLModeDisable}
	if mutate != nil {
		mutate(&config)
	}
	client, err := postgresql.New(config, sdkgo.StaticCredentialProvider[postgresql.Credentials]{
		scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)},
	}, postgresql.WithClock(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	return server, client
}

var stepSequence atomic.Int64

func newStep() *testsupport.DexContext {
	return testsupport.NewDexContext("scripted-flow", fmt.Sprintf("step-%d", stepSequence.Add(1)))
}

func query(t *testing.T, client *postgresql.Client, input postgresql.QueryRowsInput) (postgresql.QueryRowsResult, error) {
	t.Helper()
	return sdkgo.RunQuery(newStep(), client.QueryRows(), scriptedConnection, input)
}

func execute(t *testing.T, client *postgresql.Client, step *testsupport.DexContext, input postgresql.ExecuteStatementInput) (postgresql.ExecuteStatementResult, error) {
	t.Helper()
	return sdkgo.RunMutation(step, client.ExecuteStatement(), scriptedConnection, input)
}

// requireSecretFree asserts a Result carries neither the password nor a parameter value.
func requireSecretFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), scriptedPassword)
	require.NotContains(t, string(encoded), secretLookingText)
}

var twoColumns = []scriptedpostgresql.Column{{Name: "id", TypeOID: 20}, {Name: "amount", TypeOID: 1700}}

func TestQueryRowsRunsOneReadOnlyTransactionAndDecodesRows(t *testing.T) {
	database := &scriptedDatabase{
		shape: scriptedpostgresql.Shape{ParameterCount: 2, Columns: twoColumns},
		execution: scriptedpostgresql.Execution{
			Rows: [][][]byte{{[]byte("9007199254740993"), []byte("1200.00")}, {[]byte("2"), nil}}, CommandTag: "SELECT 2",
		},
	}
	server, client := startScriptedDatabase(t, database, nil)
	result, err := query(t, client, postgresql.QueryRowsInput{
		Statement: "SELECT id, amount FROM subscriptions WHERE email = $1 AND seats > $2", Parameters: []any{"jane@example.com", 40},
	})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, []postgresql.Column{{Name: "id", TypeName: "int8", TypeOID: 20}, {Name: "amount", TypeName: "numeric", TypeOID: 1700}}, result.Value.Columns)
	require.Equal(t, []map[string]any{{"id": "9007199254740993", "amount": "1200.00"}, {"id": "2", "amount": nil}}, result.Value.Rows)
	require.False(t, result.Value.Truncated)
	require.Equal(t, "postgresql", result.Receipt.Provider)
	require.Equal(t, "1001", result.Receipt.Metadata["backendPid"])
	require.Equal(t, scriptedpostgresql.ServerVersion, result.Receipt.Metadata["serverVersion"])
	require.Equal(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), result.Receipt.ObservedAt)

	events := server.Events()
	require.Equal(t, scriptedpostgresql.EventStartup, events[0].Kind)
	require.Equal(t, "dex_app", events[0].Text)
	require.Equal(t, scriptedPassword, string(events[0].Parameters[0]), "the provider's password authenticated")
	require.Equal(t, scriptedpostgresql.EventQuery, events[1].Kind)
	require.True(t, strings.HasPrefix(events[1].Text, "begin transaction read only; set local statement_timeout = 5000;"), events[1].Text)
	for _, setting := range []string{"timezone = 'UTC'", "datestyle = 'ISO, MDY'", "intervalstyle = 'iso_8601'", "bytea_output = 'hex'"} {
		require.Contains(t, events[1].Text, setting)
	}
	require.Equal(t, scriptedpostgresql.EventParse, events[2].Kind)
	require.Equal(t, scriptedpostgresql.EventExecute, events[3].Kind)
	require.Equal(t, [][]byte{[]byte("jane@example.com"), []byte("40")}, events[3].Parameters, "values are bound, never interpolated")
	require.NotContains(t, events[2].Text, "jane@example.com")
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit), "a read-only query never commits")
	require.Eventually(t, func() bool { return len(server.EventsOfKind(scriptedpostgresql.EventTerminate)) == 1 }, 5*time.Second, 10*time.Millisecond)
}

func TestQueryRowsSelectsTruncatedAtTheRowAndByteBounds(t *testing.T) {
	rows := make([][][]byte, 5)
	for index := range rows {
		rows[index] = [][]byte{[]byte(fmt.Sprint(index + 1)), []byte("1.00")}
	}
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{Columns: twoColumns}, execution: scriptedpostgresql.Execution{Rows: rows, CommandTag: "SELECT 5"}}
	_, client := startScriptedDatabase(t, database, func(config *postgresql.Config) { config.MaxRows = 3 })
	result, err := query(t, client, postgresql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchTruncated, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Len(t, result.Value.Rows, 3)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	_, byteBounded := startScriptedDatabase(t, database, func(config *postgresql.Config) { config.MaxResponseBytes = 60 })
	result, err = query(t, byteBounded, postgresql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchTruncated, result.Branch)
	require.Len(t, result.Value.Rows, 2, "each row encodes to about 25 bytes")

	oversized := &scriptedDatabase{
		shape:     scriptedpostgresql.Shape{Columns: []scriptedpostgresql.Column{{Name: "document", TypeOID: 25}}},
		execution: scriptedpostgresql.Execution{Rows: [][][]byte{{make([]byte, 256<<10)}}, CommandTag: "SELECT 1"},
	}
	_, messageBounded := startScriptedDatabase(t, oversized, func(config *postgresql.Config) { config.MaxResponseBytes = 1024 })
	result, err = query(t, messageBounded, postgresql.QueryRowsInput{Statement: "SELECT document FROM t"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchTruncated, result.Branch, "a wire message beyond the bound is refused before it is buffered")
	require.Empty(t, result.Value.Rows)
}

func TestQueryRowsClassifiesServerAndTransportFailures(t *testing.T) {
	readOnly := &scriptedDatabase{
		shape:     scriptedpostgresql.Shape{ParameterCount: 1},
		execution: scriptedpostgresql.Execution{Error: &scriptedpostgresql.ServerError{Code: "25006", Message: "cannot execute INSERT in a read-only transaction " + secretLookingText}},
	}
	_, client := startScriptedDatabase(t, readOnly, nil)
	result, err := query(t, client, postgresql.QueryRowsInput{Statement: "INSERT INTO t VALUES ($1)", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "25006", result.Receipt.Metadata["sqlState"])
	require.Equal(t, []postgresql.Column{}, result.Value.Columns)
	requireSecretFree(t, result)

	syntax := &scriptedDatabase{shapeError: &scriptedpostgresql.ServerError{Code: "42601", Message: `syntax error at or near "SELEC"`}}
	_, client = startScriptedDatabase(t, syntax, nil)
	result, err = query(t, client, postgresql.QueryRowsInput{Statement: "SELEC 1"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "42601 (syntax_error)")

	dropped := &scriptedDatabase{execution: scriptedpostgresql.Execution{DropConnection: true}}
	_, client = startScriptedDatabase(t, dropped, nil)
	_, err = query(t, client, postgresql.QueryRowsInput{Statement: "SELECT 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a read lost mid-flight is retried")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	undecodable := &scriptedDatabase{
		shape:     scriptedpostgresql.Shape{Columns: []scriptedpostgresql.Column{{Name: "flag", TypeOID: 16}}},
		execution: scriptedpostgresql.Execution{Rows: [][][]byte{{[]byte(secretLookingText)}}, CommandTag: "SELECT 1"},
	}
	_, client = startScriptedDatabase(t, undecodable, nil)
	result, err = query(t, client, postgresql.QueryRowsInput{Statement: "SELECT flag FROM t"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	requireSecretFree(t, result)
}

func TestQueryRowsSelectsDefectBeforeContactingTheServer(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 1}}
	server, client := startScriptedDatabase(t, database, nil)
	for name, input := range map[string]postgresql.QueryRowsInput{
		"blank statement":     {Statement: "  "},
		"transaction control": {Statement: "COMMIT"},
		"unsupported value":   {Statement: "SELECT $1", Parameters: []any{map[string]string{"a": "b"}}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := query(t, client, input)
			require.NoError(t, err)
			require.Equal(t, postgresql.QueryRowsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, server.Events(), "invalid input never opens a connection")

	mismatch, err := query(t, client, postgresql.QueryRowsInput{Statement: "SELECT $1"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchDefect, mismatch.Branch)
	require.Contains(t, mismatch.Failure.Message, "1 placeholders but 0 parameters")
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventExecute), "a placeholder mismatch is caught at Prepare")

	withoutCredentials, err := postgresql.New(postgresql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: postgresql.SSLModeDisable},
		sdkgo.StaticCredentialProvider[postgresql.Credentials]{})
	require.NoError(t, err)
	missing, err := query(t, withoutCredentials, postgresql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchDefect, missing.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, missing.Failure.Kind)
}

func TestConnectFailuresAreClassifiedWithoutLeakingTheWrongPassword(t *testing.T) {
	database := &scriptedDatabase{}
	server, _ := startScriptedDatabase(t, database, nil)
	config := postgresql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: postgresql.SSLModeDisable}
	wrong, err := postgresql.New(config, sdkgo.StaticCredentialProvider[postgresql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(secretLookingText)}})
	require.NoError(t, err)
	result, err := query(t, wrong, postgresql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	requireSecretFree(t, result)

	config.Host, config.SSLMode = "localhost", postgresql.SSLModeRequire
	tlsRequired, err := postgresql.New(config, sdkgo.StaticCredentialProvider[postgresql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)}})
	require.NoError(t, err)
	result, err = query(t, tlsRequired, postgresql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch, "require never falls back to plaintext")
	require.Contains(t, result.Failure.Message, "does not accept TLS")
}

func TestExecuteStatementCommitsAndBindsTheIdempotencyKeyLast(t *testing.T) {
	database := &scriptedDatabase{
		shape:     scriptedpostgresql.Shape{ParameterCount: 3, Columns: twoColumns},
		execution: scriptedpostgresql.Execution{Rows: [][][]byte{{[]byte("7"), []byte("250.10")}}, CommandTag: "INSERT 0 1"},
	}
	server, client := startScriptedDatabase(t, database, nil)
	result, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{
		Statement:  "INSERT INTO refunds (order_id, amount, idempotency_key) VALUES ($1, $2, $3) ON CONFLICT (idempotency_key) DO NOTHING RETURNING id, amount",
		Parameters: []any{"88213", "250.10"}, IdempotencyKeyPlaceholder: 3, MaxRowsAffected: int64Pointer(1),
	})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, postgresql.StatementExecution{
		Command: "INSERT", RowsAffected: 1, Columns: []postgresql.Column{{Name: "id", TypeName: "int8", TypeOID: 20}, {Name: "amount", TypeName: "numeric", TypeOID: 1700}},
		Rows: []map[string]any{{"id": "7", "amount": "250.10"}},
	}, result.Value)
	require.True(t, database.hasCommitted.Load())
	executions := server.EventsOfKind(scriptedpostgresql.EventExecute)
	require.Len(t, executions, 1)
	require.Equal(t, [][]byte{[]byte("88213"), []byte("250.10"), []byte(result.Receipt.IdempotencyKey)}, executions[0].Parameters)
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey), "the key is the stable Call ID")
	queries := server.EventsOfKind(scriptedpostgresql.EventQuery)
	require.True(t, strings.HasPrefix(queries[0].Text, "begin; set local"), "writes inherit the role's default access mode")
	require.Equal(t, "commit", queries[1].Text)
}

func TestExecuteStatementReportsUncertainOnlyAfterCommitIsSent(t *testing.T) {
	for name, testCase := range map[string]struct {
		commit        scriptedpostgresql.CommitAction
		wantCommitted bool
	}{
		"reply lost after the server committed": {scriptedpostgresql.CommitThenDropConnection, true},
		"connection lost before committing":     {scriptedpostgresql.DropConnectionWithoutCommitting, false},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{
				shape: scriptedpostgresql.Shape{ParameterCount: 1}, execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}, commit: testCase.commit,
			}
			_, client := startScriptedDatabase(t, database, nil)
			result, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "UPDATE t SET note = $1", Parameters: []any{secretLookingText}})
			require.NoError(t, err, "uncertainty is a branch, never a retry")
			require.Equal(t, postgresql.ExecuteStatementBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
			require.NotEmpty(t, result.Receipt.IdempotencyKey, "the Receipt carries the key for reconciliation")
			require.Equal(t, testCase.wantCommitted, database.hasCommitted.Load())
			requireSecretFree(t, result)
		})
	}

	serialization := &scriptedDatabase{execution: scriptedpostgresql.Execution{CommandTag: "UPDATE 1"}, commit: scriptedpostgresql.RejectCommitWithSerializationFailure}
	_, client := startScriptedDatabase(t, serialization, nil)
	_, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "UPDATE t SET a = 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a COMMIT rejected with 40001 rolled back and is safe to retry")
	require.False(t, serialization.hasCommitted.Load())
}

func TestExecuteStatementRetriesAWriteLostBeforeCommit(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedpostgresql.Execution{DropConnection: true}}
	server, client := startScriptedDatabase(t, database, nil)
	_, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "UPDATE t SET a = 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Contains(t, retry.Failure.Message, "before anything was committed")
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit))
}

func TestExecuteStatementRollsBackWhenALimitIsExceeded(t *testing.T) {
	tooMany := &scriptedDatabase{execution: scriptedpostgresql.Execution{CommandTag: "DELETE 40"}}
	server, client := startScriptedDatabase(t, tooMany, nil)
	result, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "DELETE FROM t", MaxRowsAffected: int64Pointer(1)})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchLimitExceeded, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "affected 40 rows but maxRowsAffected is 1")
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit), "the connector never sent COMMIT")
	require.False(t, tooMany.hasCommitted.Load())

	returning := &scriptedDatabase{
		shape:     scriptedpostgresql.Shape{Columns: twoColumns},
		execution: scriptedpostgresql.Execution{Rows: [][][]byte{{[]byte("1"), []byte("1")}, {[]byte("2"), []byte("2")}}, CommandTag: "UPDATE 2"},
	}
	server, client = startScriptedDatabase(t, returning, func(config *postgresql.Config) { config.MaxRows = 1 })
	result, err = execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "UPDATE t SET a = 1 RETURNING id, amount"})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchLimitExceeded, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	require.Empty(t, server.EventsOfKind(scriptedpostgresql.EventCommit))
}

func TestExecuteStatementClassifiesConstraintViolationsAndInvalidInput(t *testing.T) {
	violation := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 1}, execution: scriptedpostgresql.Execution{Error: &scriptedpostgresql.ServerError{
		Code: "23505", Message: "duplicate key value violates unique constraint", ConstraintName: "refunds_order_id_key",
	}}}
	server, client := startScriptedDatabase(t, violation, nil)
	result, err := execute(t, client, newStep(), postgresql.ExecuteStatementInput{Statement: "INSERT INTO refunds (order_id) VALUES ($1)", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, `on constraint "refunds_order_id_key"`)
	requireSecretFree(t, result)
	require.Eventually(t, func() bool { return len(server.EventsOfKind(scriptedpostgresql.EventTerminate)) == 1 }, 5*time.Second, 10*time.Millisecond)
	before := len(server.Events())

	for name, input := range map[string]postgresql.ExecuteStatementInput{
		"key placeholder not last": {Statement: "INSERT INTO t VALUES ($1, $2)", Parameters: []any{"a"}, IdempotencyKeyPlaceholder: 1},
		"negative limit":           {Statement: "DELETE FROM t", MaxRowsAffected: int64Pointer(-1)},
		"two-phase commit":         {Statement: "PREPARE TRANSACTION 'dex'"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := execute(t, client, newStep(), input)
			require.NoError(t, err)
			require.Equal(t, postgresql.ExecuteStatementBranchDefect, result.Branch)
		})
	}
	require.Len(t, server.Events(), before, "invalid input never opens a connection")
}

func TestExecuteStatementKeepsOneKeyAcrossAttemptsOfOneStepExecution(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{ParameterCount: 1}, execution: scriptedpostgresql.Execution{CommandTag: "INSERT 0 1"}}
	server, client := startScriptedDatabase(t, database, nil)
	step := newStep()
	input := postgresql.ExecuteStatementInput{Statement: "INSERT INTO t (key) VALUES ($1) ON CONFLICT DO NOTHING", IdempotencyKeyPlaceholder: 1}
	first, err := execute(t, client, step, input)
	require.NoError(t, err)
	step.AttemptNumber = 2
	second, err := execute(t, client, step, input)
	require.NoError(t, err)
	third, err := execute(t, client, newStep(), input)
	require.NoError(t, err)
	require.Equal(t, first.Receipt.IdempotencyKey, second.Receipt.IdempotencyKey)
	require.NotEqual(t, first.Receipt.IdempotencyKey, third.Receipt.IdempotencyKey, "a new Step execution gets a new key")
	executions := server.EventsOfKind(scriptedpostgresql.EventExecute)
	require.Equal(t, executions[0].Parameters, executions[1].Parameters)
}

func TestConcurrentOperationsUseSeparateConnections(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedpostgresql.Shape{Columns: twoColumns}, execution: scriptedpostgresql.Execution{Rows: [][][]byte{{[]byte("1"), []byte("1")}}, CommandTag: "SELECT 1"}}
	server, client := startScriptedDatabase(t, database, nil)
	var group sync.WaitGroup
	results := make([]postgresql.QueryRowsResult, 8)
	errs := make([]error, len(results))
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = sdkgo.RunQuery(newStep(), client.QueryRows(), scriptedConnection, postgresql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
		}()
	}
	group.Wait()
	for index := range results {
		require.NoError(t, errs[index])
		require.Equal(t, postgresql.QueryRowsBranchCompleted, results[index].Branch)
	}
	require.Len(t, server.EventsOfKind(scriptedpostgresql.EventStartup), len(results))
}

func int64Pointer(value int64) *int64 { return &value }

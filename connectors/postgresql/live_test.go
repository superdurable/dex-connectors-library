//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/commitfaultproxy"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// liveDatabase is a real PostgreSQL role configured through POSTGRESQL_CONNECTOR_TEST_* variables.
type liveDatabase struct {
	host     string
	port     int64
	database string
	user     string
	password string
	sslMode  postgresql.SSLMode
}

func requireLiveDatabase(t *testing.T) liveDatabase {
	t.Helper()
	host := os.Getenv("POSTGRESQL_CONNECTOR_TEST_HOST")
	if host == "" {
		t.Skip("POSTGRESQL_CONNECTOR_TEST_HOST is not configured")
	}
	port, err := strconv.ParseInt(envOr("POSTGRESQL_CONNECTOR_TEST_PORT", "5432"), 10, 64)
	require.NoError(t, err)
	return liveDatabase{
		host: host, port: port, database: os.Getenv("POSTGRESQL_CONNECTOR_TEST_DATABASE"),
		user: os.Getenv("POSTGRESQL_CONNECTOR_TEST_USER"), password: os.Getenv("POSTGRESQL_CONNECTOR_TEST_PASSWORD"),
		sslMode: postgresql.SSLMode(envOr("POSTGRESQL_CONNECTOR_TEST_SSL_MODE", string(postgresql.SSLModeRequire))),
	}
}

func (database liveDatabase) config() postgresql.Config {
	return postgresql.Config{Host: database.host, Port: database.port, Database: database.database, User: database.user, SSLMode: database.sslMode}
}

func (database liveDatabase) newClient(t *testing.T, config postgresql.Config) *postgresql.Client {
	t.Helper()
	client, err := postgresql.New(config, sdkgo.StaticCredentialProvider[postgresql.Credentials]{
		liveConnection: {Password: sdkgo.NewSecretString(database.password)},
	})
	require.NoError(t, err)
	return client
}

var liveConnection = sdkgo.ConnectionRef{Provider: "postgresql", Name: "live"}

var liveStepSequence int

func newLiveStep(t *testing.T) *testsupport.DexContext {
	liveStepSequence++
	return testsupport.NewDexContext("live-"+t.Name(), fmt.Sprintf("step-%d-%d", time.Now().UnixNano(), liveStepSequence))
}

func runLiveQuery(t *testing.T, client *postgresql.Client, statement string, parameters ...any) postgresql.QueryRowsResult {
	t.Helper()
	result, err := sdkgo.RunQuery(newLiveStep(t), client.QueryRows(), liveConnection, postgresql.QueryRowsInput{Statement: statement, Parameters: parameters})
	require.NoError(t, err)
	return result
}

func runLiveExecute(t *testing.T, client *postgresql.Client, step *testsupport.DexContext, input postgresql.ExecuteStatementInput) (postgresql.ExecuteStatementResult, error) {
	t.Helper()
	return sdkgo.RunMutation(step, client.ExecuteStatement(), liveConnection, input)
}

func requireLiveExecute(t *testing.T, client *postgresql.Client, statement string, parameters ...any) postgresql.ExecuteStatementResult {
	t.Helper()
	result, err := runLiveExecute(t, client, newLiveStep(t), postgresql.ExecuteStatementInput{Statement: statement, Parameters: parameters})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	return result
}

// createLiveTable creates a uniquely named table and drops it when the test ends.
func createLiveTable(t *testing.T, client *postgresql.Client, columns string) string {
	t.Helper()
	table := fmt.Sprintf("connector_live_%d", time.Now().UnixNano())
	requireLiveExecute(t, client, "CREATE TABLE "+table+" ("+columns+")")
	t.Cleanup(func() { requireLiveExecute(t, client, "DROP TABLE IF EXISTS "+table) })
	return table
}

func countLiveRows(t *testing.T, client *postgresql.Client, table string) string {
	t.Helper()
	result := runLiveQuery(t, client, "SELECT count(*) AS row_count FROM "+table)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	return result.Value.Rows[0]["row_count"].(string)
}

func TestLiveQueryMapsPostgreSQLTypesToExactJSONValues(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	result := runLiveQuery(t, client, `SELECT
		true AS flag, $1::int2 AS small, $2::int4 AS regular, $3::int8 AS big,
		$4::float4 AS single, $5::float8 AS double, 'NaN'::float8 AS not_a_number,
		$6::numeric AS exact, 'NaN'::numeric AS numeric_nan, $7::text AS label, 'ab'::char(4) AS padded,
		$8::uuid AS identifier, $9::jsonb AS document, '{"b": 1, "a": [1, 2.50]}'::json AS raw_document,
		$10::bytea AS payload, $11::timestamptz AS moment, 'infinity'::timestamptz AS forever,
		'2026-01-01 12:34:56.5'::timestamp AS local_moment, '2026-02-03'::date AS day,
		'12:34:56'::time AS clock, '1 day 2 hours'::interval AS span, '{1,2,3}'::int4[] AS numbers,
		'192.168.0.1'::inet AS address, NULL::text AS missing, $12::text AS null_parameter`,
		"-7", int64(2147483647), "9007199254740993", float32(1.5), 0.1, json.Number("12345678901234567890.123456789"),
		"héllo 世界", "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d", json.RawMessage(`{"z": 10, "a": 1.10}`),
		[]byte{0, 1, 255}, time.Date(2026, 1, 1, 5, 30, 0, 123456000, time.FixedZone("IST", 5*3600+1800)), nil,
	)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Len(t, result.Value.Rows, 1)
	row := result.Value.Rows[0]
	require.Equal(t, true, row["flag"])
	require.Equal(t, int64(-7), row["small"])
	require.Equal(t, int64(2147483647), row["regular"])
	require.Equal(t, "9007199254740993", row["big"])
	require.Equal(t, 1.5, row["single"])
	require.Equal(t, 0.1, row["double"])
	require.Equal(t, "NaN", row["not_a_number"])
	require.Equal(t, "12345678901234567890.123456789", row["exact"])
	require.Equal(t, "NaN", row["numeric_nan"])
	require.Equal(t, "héllo 世界", row["label"])
	require.Equal(t, "ab  ", row["padded"])
	require.Equal(t, "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d", row["identifier"])
	require.JSONEq(t, `{"a": 1.10, "z": 10}`, string(row["document"].(json.RawMessage)))
	require.Equal(t, `{"b": 1, "a": [1, 2.50]}`, string(row["raw_document"].(json.RawMessage)))
	require.Equal(t, "AAH/", row["payload"])
	require.Equal(t, "2026-01-01T00:00:00.123456Z", row["moment"])
	require.Equal(t, "infinity", row["forever"])
	require.Equal(t, "2026-01-01T12:34:56.5", row["local_moment"])
	require.Equal(t, "2026-02-03", row["day"])
	require.Equal(t, "12:34:56", row["clock"])
	require.Equal(t, "P1DT2H", row["span"])
	require.Equal(t, "{1,2,3}", row["numbers"])
	require.Equal(t, "192.168.0.1", row["address"])
	require.Nil(t, row["missing"])
	require.Nil(t, row["null_parameter"])
	columnTypes := map[string]string{}
	for _, column := range result.Value.Columns {
		columnTypes[column.Name] = column.TypeName
	}
	require.Equal(t, "numeric", columnTypes["exact"])
	require.Equal(t, "timestamptz", columnTypes["moment"])
	require.Equal(t, "", columnTypes["numbers"], "arrays have no built-in TypeName")
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"exact":"12345678901234567890.123456789"`)
	require.Contains(t, string(encoded), `"big":"9007199254740993"`)
	require.NotContains(t, string(encoded), database.password)
}

func TestLiveQueryRunsInAReadOnlyTransaction(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, client, "id int PRIMARY KEY")

	result := runLiveQuery(t, client, "INSERT INTO "+table+" (id) VALUES ($1) RETURNING id", 1)
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "25006", result.Receipt.Metadata["sqlState"])
	require.Contains(t, result.Failure.Message, "read_only_sql_transaction")
	require.Equal(t, "0", countLiveRows(t, client, table))

	settings := runLiveQuery(t, client, `SELECT current_setting('transaction_read_only') AS read_only,
		current_setting('TimeZone') AS zone, current_setting('application_name') AS application,
		(SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()) AS encrypted`)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, settings.Branch, settings.Failure)
	require.Equal(t, "on", settings.Value.Rows[0]["read_only"])
	require.Equal(t, "UTC", settings.Value.Rows[0]["zone"])
	require.Equal(t, "dex-postgresql-connector", settings.Value.Rows[0]["application"])
	require.Equal(t, database.sslMode != postgresql.SSLModeDisable, settings.Value.Rows[0]["encrypted"])
}

func TestLiveQueryTruncatesAtMaxRowsAndMaxResponseBytes(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.MaxRows = 2
	client := database.newClient(t, config)
	result := runLiveQuery(t, client, "SELECT n FROM generate_series(1, $1::int) AS n ORDER BY n", 5)
	require.Equal(t, postgresql.QueryRowsBranchTruncated, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Equal(t, []map[string]any{{"n": int64(1)}, {"n": int64(2)}}, result.Value.Rows)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	exact := runLiveQuery(t, client, "SELECT n FROM generate_series(1, 2) AS n")
	require.Equal(t, postgresql.QueryRowsBranchCompleted, exact.Branch, "exactly maxRows rows is not truncated")

	config = database.config()
	config.MaxResponseBytes = 64
	byteBounded := database.newClient(t, config)
	bytes := runLiveQuery(t, byteBounded, "SELECT repeat('x', 20) AS text_value FROM generate_series(1, 10)")
	require.Equal(t, postgresql.QueryRowsBranchTruncated, bytes.Branch)
	require.Len(t, bytes.Value.Rows, 1)
}

func TestLiveQueryClassifiesRejectedStatementsAndDefects(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())

	syntax := runLiveQuery(t, client, "SELEC 1")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, syntax.Branch)
	require.Equal(t, "42601", syntax.Receipt.Metadata["sqlState"])
	require.Equal(t, sdkgo.FailureValidation, syntax.Failure.Kind)

	missing := runLiveQuery(t, client, "SELECT * FROM table_that_does_not_exist")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)

	placeholders := runLiveQuery(t, client, "SELECT $1::int AS a, $2::int AS b", 1)
	require.Equal(t, postgresql.QueryRowsBranchDefect, placeholders.Branch)
	require.Contains(t, placeholders.Failure.Message, "2 placeholders but 1 parameters")

	duplicate := runLiveQuery(t, client, "SELECT 1 AS a, 2 AS a")
	require.Equal(t, postgresql.QueryRowsBranchDefect, duplicate.Branch)

	multiple := runLiveQuery(t, client, "SELECT 1; SELECT 2")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, multiple.Branch, "the extended protocol accepts one statement")

	valueText := runLiveQuery(t, client, "SELECT $1::int AS n", "secret-looking-value")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, valueText.Branch)
	require.Equal(t, "22P02", valueText.Receipt.Metadata["sqlState"])
	require.NotContains(t, valueText.Failure.Message, "secret-looking-value", "Failures never repeat parameter values")
}

func TestLiveStatementTimeoutSelectsProviderRejected(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.StatementTimeout = 200 * time.Millisecond
	client := database.newClient(t, config)
	result := runLiveQuery(t, client, "SELECT pg_sleep(2)")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, "57014", result.Receipt.Metadata["sqlState"])
	require.Contains(t, result.Failure.Message, "statementTimeout")
}

func TestLiveConnectionFailuresAreClassified(t *testing.T) {
	database := requireLiveDatabase(t)
	wrongPassword, err := postgresql.New(database.config(), sdkgo.StaticCredentialProvider[postgresql.Credentials]{
		liveConnection: {Password: sdkgo.NewSecretString("not-the-password")},
	})
	require.NoError(t, err)
	result := runLiveQuery(t, wrongPassword, "SELECT 1")
	require.Equal(t, postgresql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "28P01", result.Receipt.Metadata["sqlState"])

	if database.sslMode != postgresql.SSLModeDisable {
		config := database.config()
		config.SSLMode = postgresql.SSLModeVerifyFull
		verified := database.newClient(t, config)
		unverified := runLiveQuery(t, verified, "SELECT 1")
		require.Equal(t, postgresql.QueryRowsBranchProviderRejected, unverified.Branch, "a self-signed test certificate fails verify-full")
		require.Equal(t, sdkgo.FailureAuthentication, unverified.Failure.Kind)
	}

	unused, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := int64(unused.Addr().(*net.TCPAddr).Port)
	require.NoError(t, unused.Close())
	config := database.config()
	config.Host, config.Port, config.SSLMode = "127.0.0.1", closedPort, postgresql.SSLModeDisable
	refused := database.newClient(t, config)
	_, err = sdkgo.RunQuery(newLiveStep(t), refused.QueryRows(), liveConnection, postgresql.QueryRowsInput{Statement: "SELECT 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refused connection is retried")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestLiveExecuteReplaysIdempotentInsertAsNoOp(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, client, "id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, idempotency_key uuid NOT NULL UNIQUE, amount numeric(12,2) NOT NULL")
	step := newLiveStep(t)
	input := postgresql.ExecuteStatementInput{
		Statement:  "INSERT INTO " + table + " (amount, idempotency_key) VALUES ($1::numeric, $2) ON CONFLICT (idempotency_key) DO NOTHING RETURNING id, amount, idempotency_key",
		Parameters: []any{"250.10"}, IdempotencyKeyPlaceholder: 2, MaxRowsAffected: int64Pointer(1),
	}
	first, err := runLiveExecute(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, first.Branch, first.Failure)
	require.Equal(t, "INSERT", first.Value.Command)
	require.Equal(t, int64(1), first.Value.RowsAffected)
	require.Len(t, first.Value.Rows, 1)
	require.Equal(t, "250.10", first.Value.Rows[0]["amount"])
	require.Equal(t, string(first.Receipt.IdempotencyKey), first.Value.Rows[0]["idempotency_key"])

	replay, err := runLiveExecute(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, replay.Branch, replay.Failure)
	require.Equal(t, first.Receipt.IdempotencyKey, replay.Receipt.IdempotencyKey, "one Step execution keeps its key")
	require.Equal(t, int64(0), replay.Value.RowsAffected)
	require.Empty(t, replay.Value.Rows)
	require.Equal(t, "1", countLiveRows(t, client, table))

	another, err := runLiveExecute(t, client, newLiveStep(t), input)
	require.NoError(t, err)
	require.Equal(t, int64(1), another.Value.RowsAffected, "a new Step execution has a new key")
	require.Equal(t, "2", countLiveRows(t, client, table))
}

// TestLiveConcurrentDispatchesOfOneStepExecutionWriteOnce models Dex dispatching a slow async
// Execute again: both dispatches share one key and both reach COMMIT.
func TestLiveConcurrentDispatchesOfOneStepExecutionWriteOnce(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, client, "idempotency_key uuid PRIMARY KEY, note text NOT NULL")
	lockConfig, err := pgconn.ParseConfig(fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=%s",
		database.host, database.port, database.database, database.user, database.sslMode))
	require.NoError(t, err)
	lockConfig.Password = database.password
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	lock, err := pgconn.ConnectConfig(ctx, lockConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close(ctx)) }()
	_, err = lock.Exec(ctx, "begin; lock table "+table+" in share row exclusive mode").ReadAll()
	require.NoError(t, err)

	step := newLiveStep(t)
	input := postgresql.ExecuteStatementInput{
		Statement:  "INSERT INTO " + table + " (note, idempotency_key) VALUES ($1, $2) ON CONFLICT (idempotency_key) DO NOTHING",
		Parameters: []any{"dispatched twice"}, IdempotencyKeyPlaceholder: 2,
	}
	results := make([]postgresql.ExecuteStatementResult, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = sdkgo.RunMutation(step, client.ExecuteStatement(), liveConnection, input)
		}()
	}
	// Elapsed time is the behavior under test: both dispatches must be blocked before the lock is released.
	time.Sleep(time.Second)
	_, err = lock.Exec(ctx, "rollback").ReadAll()
	require.NoError(t, err)
	group.Wait()
	affected := int64(0)
	for index := range results {
		require.NoError(t, errs[index])
		require.Equal(t, postgresql.ExecuteStatementBranchCompleted, results[index].Branch, results[index].Failure)
		affected += results[index].Value.RowsAffected
	}
	require.Equal(t, results[0].Receipt.IdempotencyKey, results[1].Receipt.IdempotencyKey)
	require.Equal(t, int64(1), affected, "one dispatch inserted and the other waited on the key, then did nothing")
	require.Equal(t, "1", countLiveRows(t, client, table))
}

func TestLiveExecuteRollsBackRejectedAndOverLimitStatements(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.MaxRows = 2
	client := database.newClient(t, config)
	table := createLiveTable(t, client, "id int PRIMARY KEY, status text NOT NULL")
	requireLiveExecute(t, client, "INSERT INTO "+table+" (id, status) SELECT n, 'open' FROM generate_series(1, 3) AS n")

	duplicate, err := runLiveExecute(t, client, newLiveStep(t), postgresql.ExecuteStatementInput{
		Statement: "INSERT INTO " + table + " (id, status) VALUES ($1, $2)", Parameters: []any{1, "open"},
	})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchProviderRejected, duplicate.Branch)
	require.Equal(t, sdkgo.FailureConflict, duplicate.Failure.Kind)
	require.Equal(t, "23505", duplicate.Receipt.Metadata["sqlState"])
	require.Contains(t, duplicate.Failure.Message, table+"_pkey")

	tooMany, err := runLiveExecute(t, client, newLiveStep(t), postgresql.ExecuteStatementInput{
		Statement: "UPDATE " + table + " SET status = $1", Parameters: []any{"closed"}, MaxRowsAffected: int64Pointer(1),
	})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchLimitExceeded, tooMany.Branch)
	require.Contains(t, tooMany.Failure.Message, "affected 3 rows but maxRowsAffected is 1")

	returning, err := runLiveExecute(t, client, newLiveStep(t), postgresql.ExecuteStatementInput{
		Statement: "UPDATE " + table + " SET status = $1 RETURNING id", Parameters: []any{"closed"},
	})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchLimitExceeded, returning.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, returning.Failure.Kind)

	unchanged := runLiveQuery(t, client, "SELECT count(*) AS open_count FROM "+table+" WHERE status = 'open'")
	require.Equal(t, "3", unchanged.Value.Rows[0]["open_count"], "every rejected or over-limit statement was rolled back")

	transactionControl, err := runLiveExecute(t, client, newLiveStep(t), postgresql.ExecuteStatementInput{Statement: "  /* comment */ COMMIT"})
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchDefect, transactionControl.Branch)
}

func TestLiveExecuteReportsUncertainOnlyWhenCommitMayHaveReachedTheServer(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, client, "idempotency_key uuid PRIMARY KEY, note text NOT NULL")
	faults := []commitfaultproxy.Fault{
		commitfaultproxy.DropAtStatementExecute,
		commitfaultproxy.DropBeforeCommitReachesServer,
		commitfaultproxy.DropAfterCommitReachesServer,
	}
	proxy, err := commitfaultproxy.Start(net.JoinHostPort(database.host, strconv.FormatInt(database.port, 10)), func(connectionNumber int) commitfaultproxy.Fault {
		if connectionNumber <= len(faults) {
			return faults[connectionNumber-1]
		}
		return commitfaultproxy.PassThrough
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, proxy.Close()) })
	config := database.config()
	config.Host, config.Port, config.SSLMode = "127.0.0.1", int64(proxy.Port()), postgresql.SSLModeDisable
	proxied := database.newClient(t, config)
	insert := func(step *testsupport.DexContext, note string) (postgresql.ExecuteStatementResult, error) {
		return runLiveExecute(t, proxied, step, postgresql.ExecuteStatementInput{
			Statement:  "INSERT INTO " + table + " (note, idempotency_key) VALUES ($1, $2) ON CONFLICT (idempotency_key) DO NOTHING",
			Parameters: []any{note}, IdempotencyKeyPlaceholder: 2,
		})
	}
	keyExists := func(key sdkgo.IdempotencyKey) bool {
		result := runLiveQuery(t, client, "SELECT count(*) AS matches FROM "+table+" WHERE idempotency_key = $1", string(key))
		return result.Value.Rows[0]["matches"] == "1"
	}

	atExecute := newLiveStep(t)
	_, err = insert(atExecute, "dropped at execute")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a connection lost before COMMIT committed nothing, so Dex may retry")
	require.Equal(t, "0", countLiveRows(t, client, table))

	beforeCommit, err := insert(newLiveStep(t), "dropped before commit")
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchUncertain, beforeCommit.Branch, "the client cannot tell whether COMMIT arrived")
	require.False(t, keyExists(beforeCommit.Receipt.IdempotencyKey), "this COMMIT never reached the server")

	afterCommitStep := newLiveStep(t)
	afterCommit, err := insert(afterCommitStep, "dropped after commit")
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchUncertain, afterCommit.Branch)
	require.Equal(t, sdkgo.FailureTransport, afterCommit.Failure.Kind)
	require.True(t, keyExists(afterCommit.Receipt.IdempotencyKey), "this COMMIT was applied although its reply was lost")

	replay, err := insert(afterCommitStep, "dropped after commit")
	require.NoError(t, err)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, replay.Branch, replay.Failure)
	require.Equal(t, int64(0), replay.Value.RowsAffected, "replaying the same key is a no-op")

	retried, err := insert(atExecute, "dropped at execute")
	require.NoError(t, err)
	require.Equal(t, int64(1), retried.Value.RowsAffected, "the retried Step execution writes once")
	require.Equal(t, "2", countLiveRows(t, client, table))
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

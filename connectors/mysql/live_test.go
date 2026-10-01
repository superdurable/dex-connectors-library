//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/commitfaultproxy"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// liveDatabase is a real MySQL or MariaDB account configured through MYSQL_CONNECTOR_TEST_* variables.
type liveDatabase struct {
	host     string
	port     int64
	database string
	user     string
	password string
	sslMode  mysql.SSLMode
	caFile   string
}

func requireLiveDatabase(t *testing.T) liveDatabase {
	t.Helper()
	host := os.Getenv("MYSQL_CONNECTOR_TEST_HOST")
	if host == "" {
		t.Skip("MYSQL_CONNECTOR_TEST_HOST is not configured")
	}
	port, err := strconv.ParseInt(envOr("MYSQL_CONNECTOR_TEST_PORT", "3306"), 10, 64)
	require.NoError(t, err)
	return liveDatabase{
		host: host, port: port, database: os.Getenv("MYSQL_CONNECTOR_TEST_DATABASE"),
		user: os.Getenv("MYSQL_CONNECTOR_TEST_USER"), password: os.Getenv("MYSQL_CONNECTOR_TEST_PASSWORD"),
		sslMode: mysql.SSLMode(envOr("MYSQL_CONNECTOR_TEST_SSL_MODE", string(mysql.SSLModeRequired))),
		caFile:  os.Getenv("MYSQL_CONNECTOR_TEST_CA_FILE"),
	}
}

func (database liveDatabase) config() mysql.Config {
	return mysql.Config{Host: database.host, Port: database.port, Database: database.database, User: database.user, SSLMode: database.sslMode}
}

func (database liveDatabase) newClient(t *testing.T, config mysql.Config) *mysql.Client {
	t.Helper()
	client, err := mysql.New(config, sdkgo.StaticCredentialProvider[mysql.Credentials]{
		liveConnection: {Password: sdkgo.NewSecretString(database.password)},
	})
	require.NoError(t, err)
	return client
}

// openAdministration opens a plain database/sql handle for DDL, locks, and inspection outside the connector.
func (database liveDatabase) openAdministration(t *testing.T) *sql.DB {
	t.Helper()
	config := mysqldriver.NewConfig()
	config.User, config.Passwd, config.DBName = database.user, database.password, database.database
	config.Net, config.Addr = "tcp", net.JoinHostPort(database.host, strconv.FormatInt(database.port, 10))
	if database.sslMode != mysql.SSLModeDisabled {
		config.TLSConfig = "skip-verify"
	}
	config.MultiStatements = true
	connector, err := mysqldriver.NewConnector(config)
	require.NoError(t, err)
	administration := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, administration.Close()) })
	return administration
}

func (database liveDatabase) isMariaDB(t *testing.T) bool {
	t.Helper()
	var version string
	require.NoError(t, database.openAdministration(t).QueryRow("SELECT VERSION()").Scan(&version))
	return strings.Contains(strings.ToLower(version), "mariadb")
}

// createLiveTable creates a uniquely named table and drops it when the test ends.
func createLiveTable(t *testing.T, database liveDatabase, columns string) string {
	t.Helper()
	table := fmt.Sprintf("connector_live_%d", time.Now().UnixNano())
	administration := database.openAdministration(t)
	_, err := administration.Exec("CREATE TABLE " + table + " (" + columns + ") ENGINE=InnoDB")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := administration.Exec("DROP TABLE IF EXISTS " + table)
		require.NoError(t, err)
	})
	return table
}

var liveConnection = sdkgo.ConnectionRef{Provider: "mysql", Name: "live"}

var liveStepSequence int

func newLiveStep(t *testing.T) *testsupport.DexContext {
	liveStepSequence++
	return testsupport.NewDexContext("live-"+t.Name(), fmt.Sprintf("step-%d-%d", time.Now().UnixNano(), liveStepSequence))
}

func runLiveQuery(t *testing.T, client *mysql.Client, statement string, parameters ...any) mysql.QueryRowsResult {
	t.Helper()
	result, err := sdkgo.RunQuery(newLiveStep(t), client.QueryRows(), liveConnection, mysql.QueryRowsInput{Statement: statement, Parameters: parameters})
	require.NoError(t, err)
	return result
}

func runLiveExecute(t *testing.T, client *mysql.Client, step *testsupport.DexContext, input mysql.ExecuteStatementInput) (mysql.ExecuteStatementResult, error) {
	t.Helper()
	return sdkgo.RunMutation(step, client.ExecuteStatement(), liveConnection, input)
}

func requireLiveExecute(t *testing.T, client *mysql.Client, statement string, parameters ...any) mysql.ExecuteStatementResult {
	t.Helper()
	result, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{Statement: statement, Parameters: parameters})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	return result
}

func countLiveRows(t *testing.T, client *mysql.Client, table string) string {
	t.Helper()
	result := runLiveQuery(t, client, "SELECT COUNT(*) AS row_count FROM "+table)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	return result.Value.Rows[0]["row_count"].(string)
}

func TestLiveQueryMapsMySQLTypesToExactJSONValues(t *testing.T) {
	database := requireLiveDatabase(t)
	isMariaDB := database.isMariaDB(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, database, `id INT PRIMARY KEY, flag TINYINT(1), small SMALLINT, medium MEDIUMINT UNSIGNED, regular INT,
		big BIGINT, unsigned_big BIGINT UNSIGNED, single FLOAT, doubled DOUBLE, exact DECIMAL(30,9), label VARCHAR(40), padded CHAR(4),
		body TEXT, choice ENUM('open','closed'), tags SET('a','b','c'), payload BLOB, fixed_binary BINARY(3), document JSON,
		moment TIMESTAMP(6) NULL, local_moment DATETIME(3), zero_moment DATETIME, day DATE, clock TIME(2), year_value YEAR,
		bits BIT(10), missing VARCHAR(10)`)
	inserted := requireLiveExecute(t, client, "INSERT INTO "+table+` VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, b'1000000001', ?)`,
		1, true, int16(-7), uint32(16777215), int32(2147483647), "9007199254740993", uint64(18446744073709551615), float32(1.5), 0.1,
		json.Number("12345678901234567890.123456789"), "héllo 世界", "ab", strings.Repeat("x", 300), "closed", "a,c",
		[]byte{0, 1, 255}, []byte{9, 8, 7}, json.RawMessage(`{"z": 10, "a": 1.10}`),
		time.Date(2026, 1, 1, 5, 30, 0, 123456000, time.FixedZone("IST", 5*3600+1800)), "2026-01-01 12:34:56.5", "2026-02-03",
		"-12:34:56.5", 2026, nil)
	require.Equal(t, int64(1), inserted.Value.RowsAffected)
	require.Equal(t, "0", inserted.Value.LastInsertID, "the table has no AUTO_INCREMENT column")
	// The connector's strict sql_mode rejects zero dates, so a legacy value is written outside it.
	_, err := database.openAdministration(t).Exec("SET SESSION sql_mode = ''; UPDATE " + table + " SET zero_moment = '0000-00-00 00:00:00'")
	require.NoError(t, err)

	result := runLiveQuery(t, client, "SELECT * FROM "+table+" WHERE id = ?", 1)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Len(t, result.Value.Rows, 1)
	row := result.Value.Rows[0]
	require.Equal(t, int64(1), row["flag"], "BOOLEAN is TINYINT(1)")
	require.Equal(t, int64(-7), row["small"])
	require.Equal(t, int64(16777215), row["medium"])
	require.Equal(t, int64(2147483647), row["regular"])
	require.Equal(t, "9007199254740993", row["big"])
	require.Equal(t, "18446744073709551615", row["unsigned_big"])
	require.Equal(t, 1.5, row["single"])
	require.Equal(t, 0.1, row["doubled"])
	require.Equal(t, "12345678901234567890.123456789", row["exact"])
	require.Equal(t, "héllo 世界", row["label"])
	require.Equal(t, "ab", row["padded"], "MySQL strips CHAR padding")
	require.Equal(t, strings.Repeat("x", 300), row["body"])
	require.Equal(t, "closed", row["choice"])
	require.Equal(t, "a,c", row["tags"])
	require.Equal(t, "AAH/", row["payload"])
	require.Equal(t, "CQgH", row["fixed_binary"])
	require.Equal(t, "2026-01-01T00:00:00.123456Z", row["moment"])
	require.Equal(t, "2026-01-01T12:34:56.5", row["local_moment"])
	require.Equal(t, "0000-00-00 00:00:00", row["zero_moment"], "a zero date keeps MySQL's text")
	require.Equal(t, "2026-02-03", row["day"])
	require.Equal(t, "-12:34:56.50", row["clock"], "TIME keeps the server's text")
	require.Equal(t, int64(2026), row["year_value"])
	require.Equal(t, "513", row["bits"])
	require.Nil(t, row["missing"])
	columnTypes := map[string]string{}
	for _, column := range result.Value.Columns {
		columnTypes[column.Name] = column.TypeName
	}
	require.Equal(t, "DECIMAL", columnTypes["exact"])
	require.Equal(t, "UNSIGNED BIGINT", columnTypes["unsigned_big"])
	require.Equal(t, "TIMESTAMP", columnTypes["moment"])
	if isMariaDB {
		require.Equal(t, `{"z": 10, "a": 1.10}`, row["document"], "MariaDB stores JSON as LONGTEXT and returns its text")
	} else {
		require.Equal(t, "JSON", columnTypes["document"])
		require.JSONEq(t, `{"a": 1.10, "z": 10}`, string(row["document"].(json.RawMessage)))
	}
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"exact":"12345678901234567890.123456789"`)
	require.Contains(t, string(encoded), `"big":"9007199254740993"`)
	require.NotContains(t, string(encoded), database.password)

	expressions := runLiveQuery(t, client, "SELECT ? AS null_parameter, CAST(? AS DECIMAL(5,2)) AS rounded, ? AS bound_time",
		nil, "1.005", time.Date(2026, 9, 30, 12, 0, 0, 999999999, time.UTC))
	require.Equal(t, mysql.QueryRowsBranchCompleted, expressions.Branch, expressions.Failure)
	require.Nil(t, expressions.Value.Rows[0]["null_parameter"])
	require.Equal(t, "1.01", expressions.Value.Rows[0]["rounded"])
	require.Equal(t, "2026-09-30 12:00:00.999999", expressions.Value.Rows[0]["bound_time"], "time.Time is sent in UTC, truncated to microseconds")
}

func TestLiveQueryRunsInAReadOnlyTransactionWithPinnedSessionSettings(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, database, "id INT PRIMARY KEY")
	function := table + "_writer"
	administration := database.openAdministration(t)
	_, err := administration.Exec("CREATE FUNCTION " + function + "() RETURNS INT MODIFIES SQL DATA NOT DETERMINISTIC BEGIN INSERT INTO " + table + " (id) VALUES (1); RETURN 1; END")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := administration.Exec("DROP FUNCTION IF EXISTS " + function)
		require.NoError(t, err)
	})

	written := runLiveQuery(t, client, "SELECT "+function+"() AS wrote")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, written.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, written.Failure.Kind)
	require.Equal(t, "1792", written.Receipt.Metadata["errorNumber"])
	require.Equal(t, "25006", written.Receipt.Metadata["sqlState"])
	require.Contains(t, written.Failure.Message, "ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION")
	require.Equal(t, "0", countLiveRows(t, client, table))

	for name, statement := range map[string]string{
		"insert":                    "INSERT INTO " + table + " (id) VALUES (2)",
		"implicitly committing DDL": "CREATE TABLE " + table + "_escape (id INT)",
		"executable comment":        "/*!50000 DROP TABLE " + table + " */",
	} {
		t.Run(name, func(t *testing.T) {
			rejected := runLiveQuery(t, client, statement)
			require.Equal(t, mysql.QueryRowsBranchDefect, rejected.Branch, "only read statements reach the server")
		})
	}
	var escaped int
	require.NoError(t, administration.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table+"_escape").Scan(&escaped))
	require.Zero(t, escaped)

	settings := runLiveQuery(t, client, "SELECT @@session.time_zone AS zone, @@session.sql_mode AS mode, CONNECTION_ID() AS connection_id")
	require.Equal(t, mysql.QueryRowsBranchCompleted, settings.Branch, settings.Failure)
	require.Equal(t, "+00:00", settings.Value.Rows[0]["zone"])
	require.Contains(t, settings.Value.Rows[0]["mode"], "STRICT_TRANS_TABLES")
	require.Equal(t, fmt.Sprint(settings.Value.Rows[0]["connection_id"]), settings.Receipt.Metadata["connectionId"],
		"MySQL reports CONNECTION_ID() as BIGINT and MariaDB as INT")
	require.NotEmpty(t, settings.Receipt.Metadata["serverVersion"])

	cipher := runLiveQuery(t, client, "SHOW SESSION STATUS LIKE 'Ssl_cipher'")
	require.Equal(t, mysql.QueryRowsBranchCompleted, cipher.Branch, cipher.Failure)
	require.Equal(t, database.sslMode != mysql.SSLModeDisabled, cipher.Value.Rows[0]["Value"] != "", "TLS is in use unless disabled")
}

func TestLiveQueryTruncatesAtMaxRowsAndMaxResponseBytes(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.MaxRows = 2
	client := database.newClient(t, config)
	sequence := "SELECT CAST(n AS SIGNED) AS n FROM (SELECT 1 AS n UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5) AS numbers"
	result := runLiveQuery(t, client, sequence+" WHERE n <= ? ORDER BY n", 5)
	require.Equal(t, mysql.QueryRowsBranchTruncated, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Equal(t, []map[string]any{{"n": "1"}, {"n": "2"}}, result.Value.Rows, "CAST(... AS SIGNED) is BIGINT, an exact string")
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	exact := runLiveQuery(t, client, sequence+" WHERE n <= 2")
	require.Equal(t, mysql.QueryRowsBranchCompleted, exact.Branch, "exactly maxRows rows is not truncated")

	config = database.config()
	config.MaxResponseBytes = 64
	byteBounded := database.newClient(t, config)
	bytes := runLiveQuery(t, byteBounded, "SELECT REPEAT('x', 20) AS text_value FROM ("+sequence+") AS rows_to_repeat")
	require.Equal(t, mysql.QueryRowsBranchTruncated, bytes.Branch)
	require.Len(t, bytes.Value.Rows, 1)

	config = database.config()
	config.MaxResponseBytes = 1024
	wireBounded := database.newClient(t, config)
	huge := runLiveQuery(t, wireBounded, "SELECT REPEAT('y', 4000000) AS huge_value")
	require.Equal(t, mysql.QueryRowsBranchTruncated, huge.Branch, "a row far beyond the bound is refused while it streams in")
	require.Empty(t, huge.Value.Rows)
}

func TestLiveQueryClassifiesRejectedStatementsAndDefects(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, database, "id INT PRIMARY KEY, amount INT NOT NULL")

	syntax := runLiveQuery(t, client, "SELECT FROM")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, syntax.Branch)
	require.Equal(t, "1064", syntax.Receipt.Metadata["errorNumber"])
	require.Equal(t, sdkgo.FailureValidation, syntax.Failure.Kind)

	missing := runLiveQuery(t, client, "SELECT * FROM table_that_does_not_exist")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)

	placeholders := runLiveQuery(t, client, "SELECT ? AS a, ? AS b", 1)
	require.Equal(t, mysql.QueryRowsBranchDefect, placeholders.Branch)
	require.Contains(t, placeholders.Failure.Message, "2 placeholders but 1 parameters")

	duplicate := runLiveQuery(t, client, "SELECT 1 AS a, 2 AS a")
	require.Equal(t, mysql.QueryRowsBranchDefect, duplicate.Branch)

	multiple := runLiveQuery(t, client, "SELECT 1; SELECT 2")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, multiple.Branch, "a prepared statement holds one statement")

	valueText, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{
		Statement: "INSERT INTO " + table + " (id, amount) VALUES (?, ?)", Parameters: []any{1, "secret-looking-value"},
	})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchProviderRejected, valueText.Branch)
	require.Equal(t, "1366", valueText.Receipt.Metadata["errorNumber"])
	require.Equal(t, sdkgo.FailureValidation, valueText.Failure.Kind)
	require.NotContains(t, valueText.Failure.Message, "secret-looking-value", "Failures never repeat parameter values")
}

func TestLiveStatementTimeoutSelectsProviderRejected(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.StatementTimeout = 200 * time.Millisecond
	client := database.newClient(t, config)
	result := runLiveQuery(t, client, "SELECT SLEEP(2) AS slept FROM (SELECT 1 AS n UNION ALL SELECT 2) AS two_rows")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch, result.Failure)
	require.Contains(t, []string{"3024", "1969"}, result.Receipt.Metadata["errorNumber"], "MySQL max_execution_time or MariaDB max_statement_time")
	require.Contains(t, result.Failure.Message, "statementTimeout")
}

func TestLiveConnectionFailuresAreClassified(t *testing.T) {
	database := requireLiveDatabase(t)
	wrongPassword, err := mysql.New(database.config(), sdkgo.StaticCredentialProvider[mysql.Credentials]{
		liveConnection: {Password: sdkgo.NewSecretString("not-the-password")},
	})
	require.NoError(t, err)
	result := runLiveQuery(t, wrongPassword, "SELECT 1")
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "1045", result.Receipt.Metadata["errorNumber"])
	require.NotContains(t, result.Failure.Message, database.user, "the server's message, which names the account, is not copied")

	if database.sslMode != mysql.SSLModeDisabled {
		config := database.config()
		config.SSLMode = mysql.SSLModeVerifyIdentity
		identity := database.newClient(t, config)
		unverified := runLiveQuery(t, identity, "SELECT 1")
		require.Equal(t, mysql.QueryRowsBranchProviderRejected, unverified.Branch, "a self-signed test certificate fails verify-identity")
		require.Equal(t, sdkgo.FailureAuthentication, unverified.Failure.Kind)
		if database.caFile != "" {
			t.Setenv("MYSQL_SSL_CA", database.caFile)
			config.SSLMode = mysql.SSLModeVerifyCa
			chained := runLiveQuery(t, database.newClient(t, config), "SELECT 1 AS verified")
			require.Equal(t, mysql.QueryRowsBranchCompleted, chained.Branch, "verify-ca trusts the server's CA from MYSQL_SSL_CA")
			config.SSLMode = mysql.SSLModeVerifyIdentity
			stillUnverified := runLiveQuery(t, database.newClient(t, config), "SELECT 1")
			require.Equal(t, mysql.QueryRowsBranchProviderRejected, stillUnverified.Branch, "the auto-generated certificate does not name the host")
		}
	}

	unused, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := int64(unused.Addr().(*net.TCPAddr).Port)
	require.NoError(t, unused.Close())
	config := database.config()
	config.Host, config.Port, config.SSLMode = "127.0.0.1", closedPort, mysql.SSLModeDisabled
	refused := database.newClient(t, config)
	_, err = sdkgo.RunQuery(newLiveStep(t), refused.QueryRows(), liveConnection, mysql.QueryRowsInput{Statement: "SELECT 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refused connection is retried")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestLiveExecuteReplaysIdempotentUpsertAsNoOp(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, database, "id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, idempotency_key CHAR(36) NOT NULL UNIQUE, amount DECIMAL(12,2) NOT NULL")
	step := newLiveStep(t)
	input := mysql.ExecuteStatementInput{
		Statement:  "INSERT INTO " + table + " (amount, idempotency_key) VALUES (?, ?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)",
		Parameters: []any{"250.10"}, IdempotencyKeyPlaceholder: 2, MaxRowsAffected: int64Pointer(1),
	}
	first, err := runLiveExecute(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, first.Branch, first.Failure)
	require.Equal(t, int64(1), first.Value.RowsAffected)
	require.NotEqual(t, "0", first.Value.LastInsertID)

	replay, err := runLiveExecute(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, replay.Branch, replay.Failure)
	require.Equal(t, first.Receipt.IdempotencyKey, replay.Receipt.IdempotencyKey, "one Step execution keeps its key")
	require.Equal(t, int64(0), replay.Value.RowsAffected, "the duplicate key turned the INSERT into a no-op update")
	require.Equal(t, first.Value.LastInsertID, replay.Value.LastInsertID, "LAST_INSERT_ID(id) reports the existing row")
	require.Equal(t, "1", countLiveRows(t, client, table))

	stored := runLiveQuery(t, client, "SELECT id, idempotency_key, amount FROM "+table+" WHERE id = ?", first.Value.LastInsertID)
	require.Equal(t, string(first.Receipt.IdempotencyKey), stored.Value.Rows[0]["idempotency_key"])
	require.Equal(t, "250.10", stored.Value.Rows[0]["amount"])

	another, err := runLiveExecute(t, client, newLiveStep(t), input)
	require.NoError(t, err)
	require.Equal(t, int64(1), another.Value.RowsAffected, "a new Step execution has a new key")
	require.Equal(t, "2", countLiveRows(t, client, table))
}

// TestLiveConcurrentDispatchesOfOneStepExecutionWriteOnce models Dex dispatching a slow async
// Execute again: both dispatches share one key and both wait on a table lock before inserting.
func TestLiveConcurrentDispatchesOfOneStepExecutionWriteOnce(t *testing.T) {
	database := requireLiveDatabase(t)
	config := database.config()
	config.StatementTimeout = 15 * time.Second
	client := database.newClient(t, config)
	table := createLiveTable(t, database, "idempotency_key CHAR(36) PRIMARY KEY, note VARCHAR(40) NOT NULL")
	lock := holdTableReadLock(t, database, table)

	step := newLiveStep(t)
	input := mysql.ExecuteStatementInput{
		Statement:  "INSERT INTO " + table + " (note, idempotency_key) VALUES (?, ?) ON DUPLICATE KEY UPDATE idempotency_key = idempotency_key",
		Parameters: []any{"dispatched twice"}, IdempotencyKeyPlaceholder: 2,
	}
	results := make([]mysql.ExecuteStatementResult, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = sdkgo.RunMutation(step, client.ExecuteStatement(), liveConnection, input)
		}()
	}
	lock.waitForBlockedStatements(t, "INSERT INTO "+table, 2)
	lock.release(t)
	group.Wait()
	affected := int64(0)
	for index := range results {
		require.NoError(t, errs[index])
		require.Equal(t, mysql.ExecuteStatementBranchCompleted, results[index].Branch, results[index].Failure)
		affected += results[index].Value.RowsAffected
	}
	require.Equal(t, results[0].Receipt.IdempotencyKey, results[1].Receipt.IdempotencyKey)
	require.Equal(t, int64(1), affected, "one dispatch inserted and the other waited on the key, then changed nothing")
	require.Equal(t, "1", countLiveRows(t, client, table))
}

func TestLiveExecuteRollsBackRejectedAndOverLimitStatements(t *testing.T) {
	database := requireLiveDatabase(t)
	isMariaDB := database.isMariaDB(t)
	config := database.config()
	config.MaxRows = 2
	client := database.newClient(t, config)
	table := createLiveTable(t, database, "id INT PRIMARY KEY, status VARCHAR(10) NOT NULL, amount DECIMAL(8,2) NOT NULL DEFAULT 1, CONSTRAINT amount_positive CHECK (amount > 0)")
	requireLiveExecute(t, client, "INSERT INTO "+table+" (id, status) VALUES (1, 'open'), (2, 'open'), (3, 'open')")

	duplicate, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{
		Statement: "INSERT INTO " + table + " (id, status) VALUES (?, ?)", Parameters: []any{1, "open"},
	})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchProviderRejected, duplicate.Branch)
	require.Equal(t, sdkgo.FailureConflict, duplicate.Failure.Kind)
	require.Equal(t, "1062", duplicate.Receipt.Metadata["errorNumber"])
	require.Contains(t, duplicate.Failure.Message, "PRIMARY", "a duplicate key names its key, never the value")

	check, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{
		Statement: "UPDATE " + table + " SET amount = ? WHERE id = 1", Parameters: []any{"-5"},
	})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchProviderRejected, check.Branch)
	require.Equal(t, sdkgo.FailureConflict, check.Failure.Kind)
	require.Contains(t, check.Failure.Message, "amount_positive")

	tooMany, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{
		Statement: "UPDATE " + table + " SET status = ?", Parameters: []any{"closed"}, MaxRowsAffected: int64Pointer(1),
	})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchLimitExceeded, tooMany.Branch)
	require.Contains(t, tooMany.Failure.Message, "affected 3 rows but maxRowsAffected is 1")

	if isMariaDB {
		returning, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{
			Statement: "DELETE FROM " + table + " WHERE status = ? RETURNING id", Parameters: []any{"open"},
		})
		require.NoError(t, err)
		require.Equal(t, mysql.ExecuteStatementBranchLimitExceeded, returning.Branch)
		require.Equal(t, sdkgo.FailureResponseTooLarge, returning.Failure.Kind)

		returned := requireLiveExecute(t, client, "INSERT INTO "+table+" (id, status) VALUES (?, ?) RETURNING id, status", 4, "new")
		require.Equal(t, int64(1), returned.Value.RowsAffected)
		require.Equal(t, []map[string]any{{"id": int64(4), "status": "new"}}, returned.Value.Rows)
		requireLiveExecute(t, client, "DELETE FROM "+table+" WHERE id = ?", 4)
	}

	unchanged := runLiveQuery(t, client, "SELECT COUNT(*) AS open_count FROM "+table+" WHERE status = 'open'")
	require.Equal(t, "3", unchanged.Value.Rows[0]["open_count"], "every rejected or over-limit statement was rolled back")

	transactionControl, err := runLiveExecute(t, client, newLiveStep(t), mysql.ExecuteStatementInput{Statement: "  /* comment */ COMMIT"})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchDefect, transactionControl.Branch)
}

func TestLiveExecuteReportsUncertainOnlyWhenCommitMayHaveReachedTheServer(t *testing.T) {
	database := requireLiveDatabase(t)
	client := database.newClient(t, database.config())
	table := createLiveTable(t, database, "idempotency_key CHAR(36) PRIMARY KEY, note VARCHAR(40) NOT NULL")
	faults := []commitfaultproxy.Fault{
		commitfaultproxy.DropAtStatementExecute,
		commitfaultproxy.DropBeforeCommitReachesServer,
		commitfaultproxy.DropAfterCommitReachesServer,
	}
	proxy, err := commitfaultproxy.Start(net.JoinHostPort(database.host, strconv.FormatInt(database.port, 10)), func(sessionNumber int) commitfaultproxy.Fault {
		if sessionNumber <= len(faults) {
			return faults[sessionNumber-1]
		}
		return commitfaultproxy.PassThrough
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, proxy.Close()) })
	config := database.config()
	config.Host, config.Port, config.SSLMode = "127.0.0.1", int64(proxy.Port()), mysql.SSLModeDisabled
	proxied := database.newClient(t, config)
	insert := func(step *testsupport.DexContext, note string) (mysql.ExecuteStatementResult, error) {
		return runLiveExecute(t, proxied, step, mysql.ExecuteStatementInput{
			Statement:  "INSERT INTO " + table + " (note, idempotency_key) VALUES (?, ?) ON DUPLICATE KEY UPDATE idempotency_key = idempotency_key",
			Parameters: []any{note}, IdempotencyKeyPlaceholder: 2,
		})
	}
	keyExists := func(key sdkgo.IdempotencyKey) bool {
		result := runLiveQuery(t, client, "SELECT COUNT(*) AS matches FROM "+table+" WHERE idempotency_key = ?", string(key))
		return result.Value.Rows[0]["matches"] == "1"
	}

	atExecute := newLiveStep(t)
	_, err = insert(atExecute, "dropped at execute")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a connection lost before COMMIT committed nothing, so Dex may retry")
	require.Equal(t, "0", countLiveRows(t, client, table))

	beforeCommit, err := insert(newLiveStep(t), "dropped before commit")
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchUncertain, beforeCommit.Branch, "the client cannot tell whether COMMIT arrived")
	require.False(t, keyExists(beforeCommit.Receipt.IdempotencyKey), "this COMMIT never reached the server")

	afterCommitStep := newLiveStep(t)
	afterCommit, err := insert(afterCommitStep, "dropped after commit")
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchUncertain, afterCommit.Branch)
	require.Equal(t, sdkgo.FailureTransport, afterCommit.Failure.Kind)
	require.True(t, keyExists(afterCommit.Receipt.IdempotencyKey), "this COMMIT was applied although its reply was lost")

	replay, err := insert(afterCommitStep, "dropped after commit")
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, replay.Branch, replay.Failure)
	require.Equal(t, int64(0), replay.Value.RowsAffected, "replaying the same key is a no-op")

	retried, err := insert(atExecute, "dropped at execute")
	require.NoError(t, err)
	require.Equal(t, int64(1), retried.Value.RowsAffected, "the retried Step execution writes once")
	require.Equal(t, "2", countLiveRows(t, client, table))
	require.Equal(t, 5, proxy.Sessions())
}

// tableReadLock holds LOCK TABLES ... READ, which blocks writes but not reads from other sessions.
type tableReadLock struct {
	administration *sql.DB
	connection     *sql.Conn
}

func holdTableReadLock(t *testing.T, database liveDatabase, table string) *tableReadLock {
	t.Helper()
	administration := database.openAdministration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := administration.Conn(ctx)
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, "LOCK TABLES "+table+" READ")
	require.NoError(t, err)
	lock := &tableReadLock{administration: administration, connection: connection}
	t.Cleanup(func() { lock.release(t) })
	return lock
}

// waitForBlockedStatements polls the process list until count sessions wait on the lock with the statement prefix.
func (lock *tableReadLock) waitForBlockedStatements(t *testing.T, statementPrefix string, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked int
		err := lock.administration.QueryRow(
			"SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE INFO LIKE CONCAT(?, '%') AND STATE LIKE 'Waiting for table%'", statementPrefix,
		).Scan(&blocked)
		return err == nil && blocked >= count
	}, 30*time.Second, 100*time.Millisecond, "%d statements starting %q never blocked on the table lock", count, statementPrefix)
}

func (lock *tableReadLock) release(t *testing.T) {
	t.Helper()
	if lock.connection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := lock.connection.ExecContext(ctx, "UNLOCK TABLES")
	require.NoError(t, err)
	require.NoError(t, lock.connection.Close())
	lock.connection = nil
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

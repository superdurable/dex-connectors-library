// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/scriptedtds"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/testcertificates"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	scriptedPassword  = "scripted-password-value"
	secretLookingText = "customer-card-4242"
)

var scriptedConnection = sdkgo.ConnectionRef{Provider: "microsoft-sql-server", Name: "scripted"}

// scriptedDatabase is one statement's scripted result and commit behavior.
type scriptedDatabase struct {
	execution     scriptedtds.Execution
	commit        scriptedtds.CommitAction
	hasCommitted  atomic.Bool
	hasRolledBack atomic.Bool
}

func (database *scriptedDatabase) script() scriptedtds.Script {
	return scriptedtds.Script{
		Execute: func(scriptedtds.Statement) scriptedtds.Execution {
			execution := database.execution
			execution.OnCommit = func() { database.hasCommitted.Store(true) }
			execution.OnRollback = func() { database.hasRolledBack.Store(true) }
			return execution
		},
		Commit: func() scriptedtds.CommitAction { return database.commit },
	}
}

func startScriptedDatabase(t *testing.T, database *scriptedDatabase, options scriptedtds.Options, mutate func(*sqlserver.Config)) (*scriptedtds.Server, *sqlserver.Client) {
	t.Helper()
	if options.Password == "" {
		options.Password = scriptedPassword
	}
	server, err := scriptedtds.Start(options, database.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := sqlserver.Config{
		Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", Encrypt: sqlserver.EncryptDisable,
	}
	if mutate != nil {
		mutate(&config)
	}
	return server, newScriptedClient(t, config)
}

func newScriptedClient(t *testing.T, config sqlserver.Config) *sqlserver.Client {
	t.Helper()
	client, err := sqlserver.New(config, sdkgo.StaticCredentialProvider[sqlserver.Credentials]{
		scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)},
	}, sqlserver.WithClock(func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	return client
}

var stepSequence atomic.Int64

func newStep() *testsupport.DexContext {
	return testsupport.NewDexContext("scripted-flow", fmt.Sprintf("step-%d", stepSequence.Add(1)))
}

func query(t *testing.T, client *sqlserver.Client, input sqlserver.QueryRowsInput) (sqlserver.QueryRowsResult, error) {
	t.Helper()
	return sdkgo.RunQuery(newStep(), client.QueryRows(), scriptedConnection, input)
}

func execute(t *testing.T, client *sqlserver.Client, step *testsupport.DexContext, input sqlserver.ExecuteStatementInput) (sqlserver.ExecuteStatementResult, error) {
	t.Helper()
	return sdkgo.RunMutation(step, client.ExecuteStatement(), scriptedConnection, input)
}

// requireSecretFree asserts a Result carries neither the password nor a parameter or server value.
func requireSecretFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), scriptedPassword)
	require.NotContains(t, string(encoded), secretLookingText)
}

var twoColumns = []scriptedtds.Column{
	{Name: "id", Type: scriptedtds.TypeBigInt}, {Name: "amount", Type: scriptedtds.TypeDecimal, Precision: 12, Scale: 2},
}

func TestQueryRowsRunsOneRolledBackTransactionAndDecodesRows(t *testing.T) {
	database := &scriptedDatabase{
		execution: scriptedtds.Execution{Columns: twoColumns, Rows: [][]any{{int64(9007199254740993), "1200.00"}, {int64(2), nil}}},
	}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	result, err := query(t, client, sqlserver.QueryRowsInput{
		Statement: "SELECT id, amount FROM dbo.subscriptions WHERE email = @p1 AND seats > @p2", Parameters: []any{"jane@example.com", 40},
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, []sqlserver.Column{{Name: "id", TypeName: "BIGINT"}, {Name: "amount", TypeName: "DECIMAL"}}, result.Value.Columns)
	require.Equal(t, []map[string]any{{"id": "9007199254740993", "amount": "1200.00"}, {"id": "2", "amount": nil}}, result.Value.Rows)
	require.False(t, result.Value.Truncated)
	require.Equal(t, "microsoft-sql-server", result.Receipt.Provider)
	require.Equal(t, "51", result.Receipt.Metadata["processId"])
	require.Equal(t, scriptedtds.DefaultProductVersion, result.Receipt.Metadata["serverVersion"])
	require.Equal(t, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), result.Receipt.ObservedAt)

	logins := server.EventsOfKind(scriptedtds.EventLogin)
	require.Len(t, logins, 1, "the provider's password logged in on the first try")
	require.Equal(t, "dex_app", logins[0].Text)
	require.Equal(t, "app", logins[0].Database)
	require.Equal(t, "dex-sql-server-connector", logins[0].ApplicationName)
	settings := server.EventsOfKind(scriptedtds.EventSessionSettings)
	require.Len(t, settings, 1)
	require.Equal(t, "SET LANGUAGE us_english; SET DATEFORMAT ymd; SET XACT_ABORT ON; SET NOCOUNT OFF; SET IMPLICIT_TRANSACTIONS OFF; "+
		"SET ANSI_NULLS ON; SET ANSI_PADDING ON; SET ANSI_WARNINGS ON; SET ARITHABORT ON; SET CONCAT_NULL_YIELDS_NULL ON; "+
		"SET QUOTED_IDENTIFIER ON; SET NUMERIC_ROUNDABORT OFF; SET TEXTSIZE 2147483647; SET TRANSACTION ISOLATION LEVEL READ COMMITTED; "+
		"SET LOCK_TIMEOUT 5000; SELECT @@SPID, CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))", settings[0].Text)
	require.Len(t, server.EventsOfKind(scriptedtds.EventBeginTransaction), 1)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit), "a query never commits")
	statements := server.EventsOfKind(scriptedtds.EventRPC)
	require.Len(t, statements, 1)
	require.Equal(t, "SELECT id, amount FROM dbo.subscriptions WHERE email = @p1 AND seats > @p2", statements[0].Statement.SQL)
	require.Equal(t, "@p1 nvarchar(16),@p2 bigint", statements[0].Statement.Declarations, "values are typed sp_executesql parameters")
	require.Equal(t, "jane@example.com", statements[0].Statement.Parameters[0].Text)
	require.Equal(t, "40", statements[0].Statement.Parameters[1].Text)
	require.NotZero(t, statements[0].Statement.TransactionDescriptor, "the statement ran inside the connector's transaction")
	require.Eventually(t, database.hasRolledBack.Load, 5*time.Second, 10*time.Millisecond, "closing the connection rolled the transaction back")
	require.False(t, database.hasCommitted.Load())
	requireSecretFree(t, result)
}

func TestQueryRowsDecodesTheDocumentedTypeMapping(t *testing.T) {
	columns := []scriptedtds.Column{
		{Name: "tiny", Type: scriptedtds.TypeTinyInt}, {Name: "small", Type: scriptedtds.TypeSmallInt},
		{Name: "regular", Type: scriptedtds.TypeInt}, {Name: "big", Type: scriptedtds.TypeBigInt},
		{Name: "flag", Type: scriptedtds.TypeBit}, {Name: "single", Type: scriptedtds.TypeReal}, {Name: "doubled", Type: scriptedtds.TypeFloat},
		{Name: "exact", Type: scriptedtds.TypeDecimal, Precision: 38, Scale: 4}, {Name: "price", Type: scriptedtds.TypeMoney},
		{Name: "small_price", Type: scriptedtds.TypeSmallMoney}, {Name: "label", Type: scriptedtds.TypeNVarChar},
		{Name: "document", Type: scriptedtds.TypeNVarCharMax}, {Name: "code", Type: scriptedtds.TypeVarChar},
		{Name: "payload", Type: scriptedtds.TypeVarBinary}, {Name: "large_payload", Type: scriptedtds.TypeVarBinaryMax},
		{Name: "row_version", Type: scriptedtds.TypeBinary, Length: 8}, {Name: "identifier", Type: scriptedtds.TypeUniqueIdentifier},
		{Name: "day", Type: scriptedtds.TypeDate}, {Name: "clock", Type: scriptedtds.TypeTime, Scale: 3},
		{Name: "local_moment", Type: scriptedtds.TypeDateTime2}, {Name: "moment", Type: scriptedtds.TypeDateTimeOffset},
		{Name: "legacy_moment", Type: scriptedtds.TypeDateTime}, {Name: "minute_moment", Type: scriptedtds.TypeSmallDateTime},
		{Name: "markup", Type: scriptedtds.TypeXML}, {Name: "place", Type: scriptedtds.TypeGeography},
	}
	database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: columns, Rows: [][]any{
		{
			int64(255), int64(-32768), int64(-2147483648), int64(-9007199254740993), true, float32(0.1), 0.1,
			"12345678901234567890123456789012.3456", "922337203685477.5807", "-214748.3648", "héllo ☃", strings.Repeat("x", 9000),
			"ASCII", []byte{0, 1, 255}, []byte{0xde, 0xad}, []byte{0, 0, 0, 0, 0, 0, 7, 0xd1},
			"6f9619ff-8b86-d011-b42d-00c04fc964ff", "2026-02-03", "12:34:56.789", "2026-01-01T12:34:56.5",
			"2026-01-01T09:00:00.1234567+02:00", "2026-01-01T12:34:56.123", "2026-01-01T12:34:00", "<a>1</a>", []byte{0xe6, 0x10},
		},
		make([]any, len(columns)),
	}}}
	_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT * FROM dbo.every_type"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, map[string]any{
		"tiny": int64(255), "small": int64(-32768), "regular": int64(-2147483648), "big": "-9007199254740993", "flag": true,
		"single": 0.1, "doubled": 0.1, "exact": "12345678901234567890123456789012.3456", "price": "922337203685477.5807",
		"small_price": "-214748.3648", "label": "héllo ☃", "document": strings.Repeat("x", 9000), "code": "ASCII",
		"payload": "AAH/", "large_payload": "3q0=", "row_version": "AAAAAAAAB9E=", "identifier": "6f9619ff-8b86-d011-b42d-00c04fc964ff",
		"day": "2026-02-03", "clock": "12:34:56.789", "local_moment": "2026-01-01T12:34:56.5", "moment": "2026-01-01T09:00:00.1234567+02:00",
		"legacy_moment": "2026-01-01T12:34:56.123", "minute_moment": "2026-01-01T12:34:00", "markup": "<a>1</a>", "place": "5hA=",
	}, result.Value.Rows[0])
	for name, value := range result.Value.Rows[1] {
		require.Nil(t, value, "SQL NULL in %s maps to null", name)
	}
	typeNames := map[string]string{}
	for _, column := range result.Value.Columns {
		typeNames[column.Name] = column.TypeName
	}
	require.Equal(t, "UNIQUEIDENTIFIER", typeNames["identifier"])
	require.Equal(t, "DATETIMEOFFSET", typeNames["moment"])
	require.Equal(t, "GEOGRAPHY", typeNames["place"])
	require.Equal(t, "MONEY", typeNames["price"])
}

func TestQueryRowsSelectsTruncatedAtTheRowAndByteBounds(t *testing.T) {
	rows := make([][]any, 5)
	for index := range rows {
		rows[index] = []any{int64(index + 1), "1.00"}
	}
	database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns, Rows: rows}}
	_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, func(config *sqlserver.Config) { config.MaxRows = 3 })
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchTruncated, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Len(t, result.Value.Rows, 3)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	_, byteBounded := startScriptedDatabase(t, database, scriptedtds.Options{}, func(config *sqlserver.Config) { config.MaxResponseBytes = 60 })
	result, err = query(t, byteBounded, sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchTruncated, result.Branch)
	require.Len(t, result.Value.Rows, 2, "each row encodes to about 25 bytes")

	oversized := &scriptedDatabase{execution: scriptedtds.Execution{
		Columns: []scriptedtds.Column{{Name: "document", Type: scriptedtds.TypeNVarCharMax}}, Rows: [][]any{{strings.Repeat("x", 256<<10)}},
	}}
	_, messageBounded := startScriptedDatabase(t, oversized, scriptedtds.Options{}, func(config *sqlserver.Config) { config.MaxResponseBytes = 1024 })
	result, err = query(t, messageBounded, sqlserver.QueryRowsInput{Statement: "SELECT document FROM dbo.t"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchTruncated, result.Branch, "a row beyond the bound is refused while it streams in")
	require.Empty(t, result.Value.Rows)
}

func TestQueryRowsClassifiesServerAndTransportFailures(t *testing.T) {
	denied := &scriptedDatabase{execution: scriptedtds.Execution{Error: &scriptedtds.ServerError{
		Number: 229, State: 5, Severity: 14, Message: "The SELECT permission was denied on the object '" + secretLookingText + "'",
	}}}
	_, client := startScriptedDatabase(t, denied, scriptedtds.Options{}, nil)
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT secret FROM dbo.cards WHERE owner = @p1", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "the server returned error 229 (permission denied on an object), severity 14, state 5 during the statement", result.Failure.Message)
	require.Equal(t, map[string]string{"processId": "51", "serverVersion": scriptedtds.DefaultProductVersion, "errorNumber": "229", "errorState": "5", "errorSeverity": "14"},
		result.Receipt.Metadata)
	require.Equal(t, []sqlserver.Column{}, result.Value.Columns)
	requireSecretFree(t, result)

	for name, testCase := range map[string]struct {
		number int32
		kind   sdkgo.FailureKind
	}{
		"syntax error":   {102, sdkgo.FailureValidation},
		"missing table":  {208, sdkgo.FailureNotFound},
		"divide by zero": {8134, sdkgo.FailureValidation},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: scriptedtds.Execution{Error: &scriptedtds.ServerError{Number: testCase.number, State: 1, Severity: 16, Message: secretLookingText}}}
			_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
			result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one FROM dbo.t"})
			require.NoError(t, err)
			require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			requireSecretFree(t, result)
		})
	}

	for name, testCase := range map[string]struct {
		number   int32
		severity byte
		kind     sdkgo.FailureKind
	}{
		"deadlock victim":          {1205, 13, sdkgo.FailureConflict},
		"lock timeout":             {1222, 16, sdkgo.FailureConflict},
		"Azure SQL unavailable":    {40613, 17, sdkgo.FailureAvailability},
		"unknown fatal error":      {9999, 20, sdkgo.FailureAvailability},
		"Azure SQL resource limit": {10928, 16, sdkgo.FailureAvailability},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: scriptedtds.Execution{Error: &scriptedtds.ServerError{Number: testCase.number, State: 1, Severity: testCase.severity}}}
			_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
			_, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one FROM dbo.t WITH (UPDLOCK)"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, testCase.kind, retry.Failure.Kind)
		})
	}

	dropped := &scriptedDatabase{execution: scriptedtds.Execution{DropConnection: true}}
	_, client = startScriptedDatabase(t, dropped, scriptedtds.Options{}, nil)
	_, err = query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a read lost mid-flight is retried")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	undecodable := &scriptedDatabase{execution: scriptedtds.Execution{
		Columns: []scriptedtds.Column{{Name: "value", Type: scriptedtds.TypeSQLVariant}}, Rows: [][]any{{int64(7)}},
	}}
	_, client = startScriptedDatabase(t, undecodable, scriptedtds.Options{}, nil)
	result, err = query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT value FROM dbo.settings"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, `column "value" of type SQL_VARIANT`)
}

func TestQueryRowsSelectsDefectForResultsARowMapCannotHold(t *testing.T) {
	for name, testCase := range map[string]struct {
		execution scriptedtds.Execution
		message   string
	}{
		"duplicate column names": {
			execution: scriptedtds.Execution{Columns: []scriptedtds.Column{{Name: "id", Type: scriptedtds.TypeInt}, {Name: "id", Type: scriptedtds.TypeInt}}},
			message:   "unique alias",
		},
		"unnamed column": {
			execution: scriptedtds.Execution{Columns: []scriptedtds.Column{{Name: "", Type: scriptedtds.TypeInt}}},
			message:   "alias with AS",
		},
		"no result set": {
			execution: scriptedtds.Execution{RowsAffected: 3},
			message:   "no result set",
		},
		"a second result set": {
			execution: scriptedtds.Execution{Columns: []scriptedtds.Column{{Name: "id", Type: scriptedtds.TypeInt}}, Rows: [][]any{{int64(1)}}, ExtraResultSet: true},
			message:   "more than one result set",
		},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: testCase.execution}
			_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
			result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "WITH ids AS (SELECT 1 AS id) SELECT id FROM ids"})
			require.NoError(t, err)
			require.Equal(t, sqlserver.QueryRowsBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, testCase.message)
			require.Eventually(t, database.hasRolledBack.Load, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestQueryRowsSelectsDefectBeforeContactingTheServer(t *testing.T) {
	database := &scriptedDatabase{}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	for name, input := range map[string]sqlserver.QueryRowsInput{
		"blank statement":             {Statement: "  "},
		"write statement":             {Statement: "INSERT INTO dbo.t VALUES (1)"},
		"second statement":            {Statement: "SELECT 1 AS one; DELETE FROM dbo.t"},
		"transaction control":         {Statement: "SELECT 1 AS one\nCOMMIT"},
		"dynamic SQL":                 {Statement: "SELECT 1 AS one EXEC sp_executesql N'DELETE FROM dbo.t'"},
		"missing placeholder":         {Statement: "SELECT @p1 AS value", Parameters: []any{1, 2}},
		"placeholder without value":   {Statement: "SELECT @p2 AS value", Parameters: []any{1}},
		"undeclared variable":         {Statement: "SELECT @total AS value"},
		"upper-case placeholder":      {Statement: "SELECT @P1 AS value", Parameters: []any{1}},
		"unsupported value":           {Statement: "SELECT @p1 AS value", Parameters: []any{map[string]string{"a": "b"}}},
		"not a number":                {Statement: "SELECT @p1 AS value", Parameters: []any{json.Number("1e")}},
		"unsigned beyond bigint":      {Statement: "SELECT @p1 AS value", Parameters: []any{uint64(1 << 63)}},
		"time outside the date range": {Statement: "SELECT @p1 AS value", Parameters: []any{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
		"unterminated string":         {Statement: "SELECT 'open AS value"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := query(t, client, input)
			require.NoError(t, err)
			require.Equal(t, sqlserver.QueryRowsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, server.Events(), "invalid input never opens a connection")
}

func TestQueryRowsCancelsAStatementThatExceedsStatementTimeout(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns, Delay: 10 * time.Second}}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, func(config *sqlserver.Config) { config.StatementTimeout = 300 * time.Millisecond })
	started := time.Now()
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t WITH (HOLDLOCK)"})
	require.NoError(t, err)
	require.Less(t, time.Since(started), 5*time.Second, "the attention ended the statement")
	require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch, result.Failure)
	require.Equal(t, sdkgo.FailureAvailability, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "statementTimeout")
	require.Len(t, server.EventsOfKind(scriptedtds.EventAttention), 1, "the driver canceled the statement on the server")
	require.Contains(t, server.EventsOfKind(scriptedtds.EventSessionSettings)[0].Text, "SET LOCK_TIMEOUT 300;")
}

func TestLoginFailuresAreConclusiveAndUnreachableServersAreRetried(t *testing.T) {
	database := &scriptedDatabase{}
	_, client := startScriptedDatabase(t, database, scriptedtds.Options{Password: "a-different-password"}, nil)
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "18456", result.Receipt.Metadata["errorNumber"])
	require.NotContains(t, result.Failure.Message, "dex_app", "server message text is never copied")

	firewall := &scriptedDatabase{}
	_, client = startScriptedDatabase(t, firewall, scriptedtds.Options{LoginError: &scriptedtds.ServerError{Number: 40615, State: 1, Severity: 14, Message: "Client with IP address '203.0.113.9' is not allowed"}}, nil)
	result, err = query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "203.0.113.9")

	busy := &scriptedDatabase{}
	_, client = startScriptedDatabase(t, busy, scriptedtds.Options{LoginError: &scriptedtds.ServerError{Number: 40501, State: 1, Severity: 20}}, nil)
	_, err = query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an Azure SQL busy login is retried")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	unreachable := newScriptedClient(t, sqlserver.Config{Host: "127.0.0.1", Port: int64(closedPort), Database: "app", User: "dex_app", Encrypt: sqlserver.EncryptDisable})
	_, err = query(t, unreachable, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestEncryptModesNegotiateAndVerifyTLS(t *testing.T) {
	authority, err := testcertificates.Issue([]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	trustedRoots := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(trustedRoots, authority.CertificatePEM, 0o600))
	selectOne := sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t"}

	t.Run("mandatory verifies the chain and the host inside TDS 7.4", func(t *testing.T) {
		t.Setenv("SQLSERVER_SSL_CA", trustedRoots)
		database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
		server, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: scriptedtds.EncryptionOn, TLS: authority.ServerTLS},
			func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptMandatory })
		result, err := query(t, client, selectOne)
		require.NoError(t, err)
		require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
		require.Equal(t, "on", server.EventsOfKind(scriptedtds.EventPrelogin)[0].Text)
		require.False(t, server.EventsOfKind(scriptedtds.EventPrelogin)[0].IsEncrypted, "TDS 7.4 negotiates in plaintext")
		require.True(t, server.EventsOfKind(scriptedtds.EventLogin)[0].IsEncrypted, "the password travels inside TLS")
		require.True(t, server.EventsOfKind(scriptedtds.EventBatch)[0].IsEncrypted, "a statement without parameters is a SQL batch")
	})

	t.Run("strict starts TLS before any TDS byte", func(t *testing.T) {
		t.Setenv("SQLSERVER_SSL_CA", trustedRoots)
		database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
		server, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: scriptedtds.EncryptionStrict, TLS: authority.ServerTLS},
			func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptStrict })
		result, err := query(t, client, selectOne)
		require.NoError(t, err)
		require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
		prelogin := server.EventsOfKind(scriptedtds.EventPrelogin)[0]
		require.Equal(t, "strict", prelogin.Text)
		require.True(t, prelogin.IsEncrypted, "TDS 8.0 encrypts PRELOGIN too")
	})

	for name, mutate := range map[string]func(*sqlserver.Config){
		"mandatory rejects an untrusted chain": func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptMandatory },
		"strict rejects an untrusted chain":    func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptStrict },
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SQLSERVER_SSL_CA", "")
			encryption := scriptedtds.EncryptionOn
			if strings.HasPrefix(name, "strict") {
				encryption = scriptedtds.EncryptionStrict
			}
			database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
			server, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: encryption, TLS: authority.ServerTLS}, mutate)
			result, err := query(t, client, selectOne)
			require.NoError(t, err)
			require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "certificate failed encrypt verification")
			require.Empty(t, server.EventsOfKind(scriptedtds.EventLogin), "no password is sent to an unverified server")
		})
	}

	t.Run("mandatory rejects a certificate for another host", func(t *testing.T) {
		otherHost, err := testcertificates.Issue([]string{"db.example.com"}, nil)
		require.NoError(t, err)
		otherRoots := filepath.Join(t.TempDir(), "other-ca.pem")
		require.NoError(t, os.WriteFile(otherRoots, otherHost.CertificatePEM, 0o600))
		t.Setenv("SQLSERVER_SSL_CA", otherRoots)
		database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
		_, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: scriptedtds.EncryptionOn, TLS: otherHost.ServerTLS},
			func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptMandatory })
		result, err := query(t, client, selectOne)
		require.NoError(t, err)
		require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	})

	t.Run("trust-server-certificate encrypts without verifying", func(t *testing.T) {
		t.Setenv("SQLSERVER_SSL_CA", "")
		database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
		server, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: scriptedtds.EncryptionOn, TLS: authority.ServerTLS},
			func(config *sqlserver.Config) { config.Encrypt = sqlserver.EncryptTrustServerCertificate })
		result, err := query(t, client, selectOne)
		require.NoError(t, err)
		require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
		require.True(t, server.EventsOfKind(scriptedtds.EventLogin)[0].IsEncrypted)
	})

	for name, encryption := range map[string]scriptedtds.Encryption{
		"a server without TLS":         scriptedtds.EncryptionNotSupported,
		"a server offering login-only": scriptedtds.EncryptionLoginOnly,
	} {
		t.Run("mandatory refuses "+name, func(t *testing.T) {
			database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns}}
			server, client := startScriptedDatabase(t, database, scriptedtds.Options{Encryption: encryption},
				func(config *sqlserver.Config) { config.Encrypt = sqlserver.EncryptTrustServerCertificate })
			result, err := query(t, client, selectOne)
			require.NoError(t, err)
			require.Equal(t, sqlserver.QueryRowsBranchProviderRejected, result.Branch)
			require.Contains(t, result.Failure.Message, "does not accept TLS")
			require.Empty(t, server.EventsOfKind(scriptedtds.EventLogin), "the login is never sent in plaintext")
		})
	}

	t.Run("an unreadable SQLSERVER_SSL_CA is a defect", func(t *testing.T) {
		t.Setenv("SQLSERVER_SSL_CA", filepath.Join(t.TempDir(), "missing.pem"))
		database := &scriptedDatabase{}
		server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, func(config *sqlserver.Config) { config.Encrypt = sqlserver.EncryptMandatory })
		result, err := query(t, client, selectOne)
		require.NoError(t, err)
		require.Equal(t, sqlserver.QueryRowsBranchDefect, result.Branch)
		require.Contains(t, result.Failure.Message, "SQLSERVER_SSL_CA")
		require.Empty(t, server.Events())
	})
}

func TestAzureSQLRedirectReconnectsAndVerifiesTheRoutedServer(t *testing.T) {
	authority, err := testcertificates.Issue([]string{"localhost"}, nil)
	require.NoError(t, err)
	trustedRoots := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(trustedRoots, authority.CertificatePEM, 0o600))
	t.Setenv("SQLSERVER_SSL_CA", trustedRoots)
	worker := &scriptedDatabase{execution: scriptedtds.Execution{Columns: twoColumns, Rows: [][]any{{int64(1), "2.00"}}}}
	workerServer, err := scriptedtds.Start(scriptedtds.Options{Password: scriptedPassword, Encryption: scriptedtds.EncryptionOn, TLS: authority.ServerTLS}, worker.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workerServer.Close()) })
	gateway := &scriptedDatabase{}
	gatewayServer, client := startScriptedDatabase(t, gateway, scriptedtds.Options{
		Encryption: scriptedtds.EncryptionOn, TLS: authority.ServerTLS, RouteTo: &scriptedtds.Route{Host: "localhost", Port: uint16(workerServer.Port())},
	}, func(config *sqlserver.Config) { config.Host, config.Encrypt = "localhost", sqlserver.EncryptMandatory })
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Len(t, result.Value.Rows, 1)
	require.Len(t, gatewayServer.EventsOfKind(scriptedtds.EventLogin), 1)
	require.Empty(t, gatewayServer.EventsOfKind(scriptedtds.EventSessionSettings), "the gateway only redirects")
	require.Empty(t, gatewayServer.EventsOfKind(scriptedtds.EventBatch))
	require.Len(t, workerServer.EventsOfKind(scriptedtds.EventLogin), 1, "the driver logged in again at the routed server")
	require.Len(t, workerServer.EventsOfKind(scriptedtds.EventBatch), 1)
}

func TestAMalformedServerReplySelectsInvalidResponseInsteadOfPanicking(t *testing.T) {
	database := &scriptedDatabase{}
	_, client := startScriptedDatabase(t, database, scriptedtds.Options{SendsMalformedPrelogin: true}, nil)
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT 1 AS one"})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func TestExecuteStatementCommitsAndBindsTheIdempotencyKeyLast(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	maxRowsAffected := int64(1)
	step := newStep()
	result, err := execute(t, client, step, sqlserver.ExecuteStatementInput{
		Statement: "INSERT INTO dbo.refunds (order_id, amount, idempotency_key) " +
			"SELECT @p1, CAST(@p2 AS decimal(12, 2)), @p3 WHERE NOT EXISTS (SELECT 1 FROM dbo.refunds WITH (UPDLOCK, HOLDLOCK) WHERE idempotency_key = @p3)",
		Parameters: []any{"88213", "250.00"}, IdempotencyKeyPlaceholder: 3, MaxRowsAffected: &maxRowsAffected,
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, sqlserver.StatementExecution{RowsAffected: 1, Columns: []sqlserver.Column{}, Rows: []map[string]any{}}, result.Value)
	key := string(result.Receipt.IdempotencyKey)
	require.Len(t, key, 36)
	statements := server.EventsOfKind(scriptedtds.EventRPC)
	require.Len(t, statements, 1)
	require.Equal(t, "@p1 nvarchar(5),@p2 nvarchar(6),@p3 nvarchar(36)", statements[0].Statement.Declarations)
	require.Equal(t, key, statements[0].Statement.Parameters[2].Text, "the key is the last placeholder")
	commits := server.EventsOfKind(scriptedtds.EventCommit)
	require.Len(t, commits, 1)
	require.True(t, database.hasCommitted.Load())

	replay, err := execute(t, client, step, sqlserver.ExecuteStatementInput{
		Statement: statements[0].Statement.SQL, Parameters: []any{"88213", "250.00"}, IdempotencyKeyPlaceholder: 3,
	})
	require.NoError(t, err)
	require.Equal(t, key, string(replay.Receipt.IdempotencyKey), "a retry of the same Step execution keeps its key")
}

func TestExecuteStatementReturnsOutputRows(t *testing.T) {
	outputColumns := []scriptedtds.Column{{Name: "refund_id", Type: scriptedtds.TypeBigInt}, {Name: "recorded_at", Type: scriptedtds.TypeDateTime2, Scale: 6}}
	database := &scriptedDatabase{execution: scriptedtds.Execution{Columns: outputColumns, Rows: [][]any{{int64(17), "2026-10-01T12:00:00.25"}}}}
	_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
	result, err := execute(t, client, newStep(), sqlserver.ExecuteStatementInput{
		Statement:         "INSERT INTO dbo.refunds (order_id) OUTPUT INSERTED.refund_id, INSERTED.recorded_at VALUES (@p1)",
		Parameters:        []any{"88213"},
		ReturnsOutputRows: true,
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, int64(1), result.Value.RowsAffected, "each OUTPUT row is one affected row")
	require.Equal(t, []map[string]any{{"refund_id": "17", "recorded_at": "2026-10-01T12:00:00.25"}}, result.Value.Rows)
	require.True(t, database.hasCommitted.Load())

	missing := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}}
	server, client := startScriptedDatabase(t, missing, scriptedtds.Options{}, nil)
	result, err = execute(t, client, newStep(), sqlserver.ExecuteStatementInput{
		Statement: "UPDATE dbo.refunds SET note = @p1", Parameters: []any{"x"}, ReturnsOutputRows: true,
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "no OUTPUT rows result set")
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	require.Eventually(t, missing.hasRolledBack.Load, 5*time.Second, 10*time.Millisecond)
}

func TestExecuteStatementRollsBackWhenALimitIsExceeded(t *testing.T) {
	maxRowsAffected := int64(1)
	for name, testCase := range map[string]struct {
		execution         scriptedtds.Execution
		returnsOutputRows bool
		mutate            func(*sqlserver.Config)
	}{
		"rows affected":              {execution: scriptedtds.Execution{RowsAffected: 40}},
		"rows an AFTER trigger adds": {execution: scriptedtds.Execution{RowsAffected: 1, TriggerRowsAffected: 1}},
		"OUTPUT rows beyond maxRows": {
			execution:         scriptedtds.Execution{Columns: []scriptedtds.Column{{Name: "id", Type: scriptedtds.TypeInt}}, Rows: [][]any{{int64(1)}, {int64(2)}, {int64(3)}}},
			returnsOutputRows: true, mutate: func(config *sqlserver.Config) { config.MaxRows = 2 },
		},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: testCase.execution}
			server, client := startScriptedDatabase(t, database, scriptedtds.Options{}, testCase.mutate)
			input := sqlserver.ExecuteStatementInput{Statement: "DELETE FROM dbo.sessions WHERE expires_at < @p1", Parameters: []any{"2026-01-01"}, ReturnsOutputRows: testCase.returnsOutputRows}
			if !testCase.returnsOutputRows {
				input.MaxRowsAffected = &maxRowsAffected
			} else {
				input.Statement = "DELETE FROM dbo.sessions OUTPUT DELETED.id WHERE expires_at < @p1"
			}
			result, err := execute(t, client, newStep(), input)
			require.NoError(t, err)
			require.Equal(t, sqlserver.ExecuteStatementBranchLimitExceeded, result.Branch, result.Failure)
			require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit), "nothing was committed")
			require.Eventually(t, database.hasRolledBack.Load, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestExecuteStatementClassifiesEveryCommitFault(t *testing.T) {
	for name, testCase := range map[string]struct {
		commit scriptedtds.CommitAction
		branch sdkgo.BranchID
	}{
		"a COMMIT whose reply was lost after it applied": {scriptedtds.CommitThenDropConnection, sqlserver.ExecuteStatementBranchUncertain},
		"a connection lost after COMMIT was sent":        {scriptedtds.DropConnectionWithoutCommitting, sqlserver.ExecuteStatementBranchUncertain},
		"a server error while committing":                {scriptedtds.RejectCommitWithServerError, sqlserver.ExecuteStatementBranchUncertain},
		"a COMMIT the server reports it never started":   {scriptedtds.RejectCommitWithNoTransaction, sqlserver.ExecuteStatementBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1}, commit: testCase.commit}
			_, client := startScriptedDatabase(t, database, scriptedtds.Options{}, nil)
			result, err := execute(t, client, newStep(), sqlserver.ExecuteStatementInput{
				Statement: "UPDATE dbo.accounts SET plan = @p1 WHERE id = @p2", Parameters: []any{"team", 7},
			})
			require.NoError(t, err, "an ambiguous COMMIT is never retried")
			require.Equal(t, testCase.branch, result.Branch, result.Failure)
			require.NotEmpty(t, result.Receipt.IdempotencyKey, "the uncertain Result names the write to look for")
		})
	}
}

func TestExecuteStatementClassifiesStatementFailures(t *testing.T) {
	duplicate := &scriptedDatabase{execution: scriptedtds.Execution{Error: &scriptedtds.ServerError{
		Number: 2627, State: 1, Severity: 14,
		Message: "Violation of UNIQUE KEY constraint 'uq_refunds_order_id'. Cannot insert duplicate key in object 'dbo.refunds'. The duplicate key value is (" + secretLookingText + ").",
	}}}
	server, client := startScriptedDatabase(t, duplicate, scriptedtds.Options{}, nil)
	result, err := execute(t, client, newStep(), sqlserver.ExecuteStatementInput{Statement: "INSERT INTO dbo.refunds (order_id) VALUES (@p1)", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, `the server returned error 2627 (unique or primary key constraint violation), severity 14, state 1 during the statement on "uq_refunds_order_id"`, result.Failure.Message)
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	requireSecretFree(t, result)

	dropped := &scriptedDatabase{execution: scriptedtds.Execution{DropConnection: true}}
	_, client = startScriptedDatabase(t, dropped, scriptedtds.Options{}, nil)
	_, err = execute(t, client, newStep(), sqlserver.ExecuteStatementInput{Statement: "DELETE FROM dbo.t WHERE id = @p1", Parameters: []any{1}})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a write lost before COMMIT committed nothing and is retried")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	slow := &scriptedDatabase{execution: scriptedtds.Execution{RowsAffected: 1, Delay: 10 * time.Second}}
	server, client = startScriptedDatabase(t, slow, scriptedtds.Options{}, func(config *sqlserver.Config) { config.StatementTimeout = 200 * time.Millisecond })
	result, err = execute(t, client, newStep(), sqlserver.ExecuteStatementInput{Statement: "DELETE FROM dbo.t WHERE id = @p1", Parameters: []any{1}})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchProviderRejected, result.Branch, result.Failure)
	require.Contains(t, result.Failure.Message, "statementTimeout")
	require.Empty(t, server.EventsOfKind(scriptedtds.EventCommit))
	require.True(t, slow.hasRolledBack.Load(), "the canceled statement wrote nothing")

	invalid := &scriptedDatabase{}
	server, client = startScriptedDatabase(t, invalid, scriptedtds.Options{}, nil)
	negativeLimit := int64(-1)
	for name, input := range map[string]sqlserver.ExecuteStatementInput{
		"read statement":                 {Statement: "SELECT 1 AS one"},
		"schema change":                  {Statement: "UPDATE dbo.t SET a = 1 DROP TABLE dbo.t"},
		"key placeholder not last":       {Statement: "INSERT INTO dbo.t VALUES (@p1, @p2)", Parameters: []any{1, 2}, IdempotencyKeyPlaceholder: 1},
		"key placeholder not referenced": {Statement: "INSERT INTO dbo.t VALUES (@p1)", Parameters: []any{1}, IdempotencyKeyPlaceholder: 2},
		"negative limit":                 {Statement: "DELETE FROM dbo.t", MaxRowsAffected: &negativeLimit},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := execute(t, client, newStep(), input)
			require.NoError(t, err)
			require.Equal(t, sqlserver.ExecuteStatementBranchDefect, result.Branch)
		})
	}
	require.Empty(t, server.Events())
}

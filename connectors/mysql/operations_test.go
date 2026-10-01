// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/scriptedmysql"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/testcertificates"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	scriptedPassword  = "scripted-password-value"
	secretLookingText = "customer-card-4242"
)

var scriptedConnection = sdkgo.ConnectionRef{Provider: "mysql", Name: "scripted"}

// scriptedDatabase is one statement's scripted shape and result.
type scriptedDatabase struct {
	shape        scriptedmysql.Shape
	prepareError *scriptedmysql.ServerError
	execution    scriptedmysql.Execution
	commit       scriptedmysql.CommitAction
	hasCommitted atomic.Bool
}

func (database *scriptedDatabase) script() scriptedmysql.Script {
	return scriptedmysql.Script{
		Prepare: func(string) (scriptedmysql.Shape, *scriptedmysql.ServerError) {
			return database.shape, database.prepareError
		},
		Execute: func(scriptedmysql.Statement) scriptedmysql.Execution {
			execution := database.execution
			execution.OnCommit = func() { database.hasCommitted.Store(true) }
			return execution
		},
		Commit: func() scriptedmysql.CommitAction { return database.commit },
	}
}

func startScriptedDatabase(t *testing.T, database *scriptedDatabase, options scriptedmysql.Options, mutate func(*mysql.Config)) (*scriptedmysql.Server, *mysql.Client) {
	t.Helper()
	if options.Password == "" {
		options.Password = scriptedPassword
	}
	server, err := scriptedmysql.Start(options, database.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := mysql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: mysql.SSLModeDisabled}
	if mutate != nil {
		mutate(&config)
	}
	client, err := mysql.New(config, sdkgo.StaticCredentialProvider[mysql.Credentials]{
		scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)},
	}, mysql.WithClock(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	return server, client
}

var stepSequence atomic.Int64

func newStep() *testsupport.DexContext {
	return testsupport.NewDexContext("scripted-flow", fmt.Sprintf("step-%d", stepSequence.Add(1)))
}

func query(t *testing.T, client *mysql.Client, input mysql.QueryRowsInput) (mysql.QueryRowsResult, error) {
	t.Helper()
	return sdkgo.RunQuery(newStep(), client.QueryRows(), scriptedConnection, input)
}

func execute(t *testing.T, client *mysql.Client, step *testsupport.DexContext, input mysql.ExecuteStatementInput) (mysql.ExecuteStatementResult, error) {
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

var twoColumns = []scriptedmysql.Column{{Name: "id", Type: scriptedmysql.TypeLongLong}, {Name: "amount", Type: scriptedmysql.TypeNewDecimal}}

func queryTexts(server *scriptedmysql.Server) []string {
	var texts []string
	for _, event := range server.EventsOfKind(scriptedmysql.EventQuery) {
		texts = append(texts, event.Text)
	}
	return texts
}

func TestQueryRowsRunsOneReadOnlyTransactionAndDecodesRows(t *testing.T) {
	database := &scriptedDatabase{
		shape:     scriptedmysql.Shape{ParameterCount: 2},
		execution: scriptedmysql.Execution{Columns: twoColumns, Rows: [][]any{{int64(9007199254740993), "1200.00"}, {int64(2), nil}}},
	}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	result, err := query(t, client, mysql.QueryRowsInput{
		Statement: "SELECT id, amount FROM subscriptions WHERE email = ? AND seats > ?", Parameters: []any{"jane@example.com", 40},
	})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, []mysql.Column{{Name: "id", TypeName: "BIGINT"}, {Name: "amount", TypeName: "DECIMAL"}}, result.Value.Columns)
	require.Equal(t, []map[string]any{{"id": "9007199254740993", "amount": "1200.00"}, {"id": "2", "amount": nil}}, result.Value.Rows)
	require.False(t, result.Value.Truncated)
	require.Equal(t, "mysql", result.Receipt.Provider)
	require.Equal(t, "1001", result.Receipt.Metadata["connectionId"])
	require.Equal(t, scriptedmysql.DefaultServerVersion, result.Receipt.Metadata["serverVersion"])
	require.Equal(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), result.Receipt.ObservedAt)

	handshakes := server.EventsOfKind(scriptedmysql.EventHandshake)
	require.Len(t, handshakes, 1, "the provider's password authenticated on the first try")
	require.Equal(t, "dex_app", handshakes[0].Text)
	require.Equal(t, "app", handshakes[0].Database)
	require.Equal(t, []string{
		"SELECT CONNECTION_ID(), VERSION()",
		"SET SESSION time_zone = '+00:00', sql_mode = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION', " +
			"wait_timeout = 30, innodb_lock_wait_timeout = 5, lock_wait_timeout = 5, max_execution_time = 5000",
		"START TRANSACTION READ ONLY",
	}, queryTexts(server))
	prepares := server.EventsOfKind(scriptedmysql.EventPrepare)
	require.Len(t, prepares, 1)
	require.NotContains(t, prepares[0].Text, "jane@example.com", "values are bound, never interpolated")
	executions := server.EventsOfKind(scriptedmysql.EventExecute)
	require.Len(t, executions, 1)
	require.Equal(t, []scriptedmysql.Parameter{
		{Type: scriptedmysql.TypeString, Text: "jane@example.com"}, {Type: scriptedmysql.TypeLongLong, Text: "40"},
	}, executions[0].Parameters)
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit), "a read-only query never commits")
	require.Eventually(t, func() bool { return len(server.EventsOfKind(scriptedmysql.EventQuit)) == 1 }, 5*time.Second, 10*time.Millisecond)
}

func TestSessionSettingsUseMariaDBStatementTimeout(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns}}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{ServerVersion: "11.4.2-MariaDB-log"}, func(config *mysql.Config) {
		config.StatementTimeout = 1500 * time.Millisecond
	})
	result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, "11.4.2-MariaDB-log", result.Receipt.Metadata["serverVersion"])
	settings := queryTexts(server)[1]
	require.True(t, strings.HasSuffix(settings, "innodb_lock_wait_timeout = 2, lock_wait_timeout = 2, max_statement_time = 1.500000"), settings)
	require.NotContains(t, settings, "max_execution_time")
}

func TestQueryRowsDecodesTheDocumentedTypeMapping(t *testing.T) {
	columns := []scriptedmysql.Column{
		{Name: "flag", Type: scriptedmysql.TypeTiny}, {Name: "small", Type: scriptedmysql.TypeShort, Flags: scriptedmysql.FlagUnsigned},
		{Name: "regular", Type: scriptedmysql.TypeLong}, {Name: "unsigned_big", Type: scriptedmysql.TypeLongLong, Flags: scriptedmysql.FlagUnsigned},
		{Name: "single", Type: scriptedmysql.TypeFloat}, {Name: "doubled", Type: scriptedmysql.TypeDouble},
		{Name: "label", Type: scriptedmysql.TypeVarString}, {Name: "payload", Type: scriptedmysql.TypeBlob, Charset: scriptedmysql.CharsetBinary},
		{Name: "document", Type: scriptedmysql.TypeJSON}, {Name: "moment", Type: scriptedmysql.TypeTimestamp, Decimals: 6},
		{Name: "local_moment", Type: scriptedmysql.TypeDateTime, Decimals: 3}, {Name: "zero_moment", Type: scriptedmysql.TypeDateTime},
		{Name: "day", Type: scriptedmysql.TypeDate}, {Name: "clock", Type: scriptedmysql.TypeTime, Decimals: 2},
		{Name: "year_value", Type: scriptedmysql.TypeYear}, {Name: "bits", Type: scriptedmysql.TypeBit, Charset: scriptedmysql.CharsetBinary},
	}
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: columns, Rows: [][]any{{
		int64(-1), uint64(65535), int64(-2147483648), uint64(18446744073709551615), float32(0.1), 0.1,
		"héllo", []byte{0, 1, 255}, `{"a": [1, 2.5]}`, "2026-01-01 00:00:00.123456", "2026-01-01 12:34:56.5",
		"0000-00-00 00:00:00", "2026-02-03", "-12:34:56.5", int64(2026), []byte{0x02, 0x01},
	}}}}
	_, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT * FROM every_type"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, map[string]any{
		"flag": int64(-1), "small": int64(65535), "regular": int64(-2147483648), "unsigned_big": "18446744073709551615",
		"single": 0.1, "doubled": 0.1, "label": "héllo", "payload": "AAH/", "document": json.RawMessage(`{"a": [1, 2.5]}`),
		"moment": "2026-01-01T00:00:00.123456Z", "local_moment": "2026-01-01T12:34:56.5", "zero_moment": "0000-00-00 00:00:00",
		"day": "2026-02-03", "clock": "-12:34:56.50", "year_value": int64(2026), "bits": "513",
	}, result.Value.Rows[0])
	typeNames := map[string]string{}
	for _, column := range result.Value.Columns {
		typeNames[column.Name] = column.TypeName
	}
	require.Equal(t, "UNSIGNED BIGINT", typeNames["unsigned_big"])
	require.Equal(t, "BLOB", typeNames["payload"])
	require.Equal(t, "VARCHAR", typeNames["label"])
}

func TestQueryRowsSelectsTruncatedAtTheRowAndByteBounds(t *testing.T) {
	rows := make([][]any, 5)
	for index := range rows {
		rows[index] = []any{int64(index + 1), "1.00"}
	}
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns, Rows: rows}}
	_, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, func(config *mysql.Config) { config.MaxRows = 3 })
	result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchTruncated, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Len(t, result.Value.Rows, 3)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	_, byteBounded := startScriptedDatabase(t, database, scriptedmysql.Options{}, func(config *mysql.Config) { config.MaxResponseBytes = 60 })
	result, err = query(t, byteBounded, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchTruncated, result.Branch)
	require.Len(t, result.Value.Rows, 2, "each row encodes to about 25 bytes")

	oversized := &scriptedDatabase{execution: scriptedmysql.Execution{
		Columns: []scriptedmysql.Column{{Name: "document", Type: scriptedmysql.TypeBlob}}, Rows: [][]any{{strings.Repeat("x", 256<<10)}},
	}}
	_, messageBounded := startScriptedDatabase(t, oversized, scriptedmysql.Options{}, func(config *mysql.Config) { config.MaxResponseBytes = 1024 })
	result, err = query(t, messageBounded, mysql.QueryRowsInput{Statement: "SELECT document FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchTruncated, result.Branch, "a row beyond the bound is refused while it streams in")
	require.Empty(t, result.Value.Rows)
}

func TestQueryRowsClassifiesServerAndTransportFailures(t *testing.T) {
	readOnly := &scriptedDatabase{
		shape: scriptedmysql.Shape{ParameterCount: 1},
		execution: scriptedmysql.Execution{Error: &scriptedmysql.ServerError{
			Number: 1792, SQLState: "25006", Message: "Cannot execute statement in a READ ONLY transaction " + secretLookingText,
		}},
	}
	_, client := startScriptedDatabase(t, readOnly, scriptedmysql.Options{}, nil)
	result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT write_audit(?)", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "1792", result.Receipt.Metadata["errorNumber"])
	require.Equal(t, "25006", result.Receipt.Metadata["sqlState"])
	require.Equal(t, []mysql.Column{}, result.Value.Columns)
	requireSecretFree(t, result)

	syntax := &scriptedDatabase{prepareError: &scriptedmysql.ServerError{Number: 1064, SQLState: "42000", Message: "You have an error in your SQL syntax near 'SELEC'"}}
	_, client = startScriptedDatabase(t, syntax, scriptedmysql.Options{}, nil)
	result, err = query(t, client, mysql.QueryRowsInput{Statement: "SELECT FROM"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "error 1064 (ER_PARSE_ERROR, SQLSTATE 42000)")
	require.NotContains(t, result.Failure.Message, "SELEC")

	dropped := &scriptedDatabase{execution: scriptedmysql.Execution{DropConnection: true}}
	_, client = startScriptedDatabase(t, dropped, scriptedmysql.Options{}, nil)
	_, err = query(t, client, mysql.QueryRowsInput{Statement: "SELECT 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a read lost mid-flight is retried")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	deadlock := &scriptedDatabase{execution: scriptedmysql.Execution{Error: &scriptedmysql.ServerError{Number: 1213, SQLState: "40001"}}}
	_, client = startScriptedDatabase(t, deadlock, scriptedmysql.Options{}, nil)
	_, err = query(t, client, mysql.QueryRowsInput{Statement: "SELECT 1 FOR UPDATE"})
	require.ErrorAs(t, err, &retry, "a deadlock is retried")
	require.Equal(t, sdkgo.FailureConflict, retry.Failure.Kind)

	undecodable := &scriptedDatabase{execution: scriptedmysql.Execution{
		Columns: []scriptedmysql.Column{{Name: "amount", Type: scriptedmysql.TypeNewDecimal}}, Rows: [][]any{{secretLookingText}},
	}}
	_, client = startScriptedDatabase(t, undecodable, scriptedmysql.Options{}, nil)
	result, err = query(t, client, mysql.QueryRowsInput{Statement: "SELECT amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	requireSecretFree(t, result)

	duplicateNames := &scriptedDatabase{execution: scriptedmysql.Execution{
		Columns: []scriptedmysql.Column{{Name: "id", Type: scriptedmysql.TypeLong}, {Name: "id", Type: scriptedmysql.TypeLong}},
	}}
	_, client = startScriptedDatabase(t, duplicateNames, scriptedmysql.Options{}, nil)
	result, err = query(t, client, mysql.QueryRowsInput{Statement: "SELECT a.id, b.id FROM a JOIN b"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "unique alias")
}

func TestOversizedOrEmptyConnectorRepliesSelectInvalidResponse(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns}}
	_, oversized := startScriptedDatabase(t, database, scriptedmysql.Options{ServerVersion: strings.Repeat("8", 5<<20)}, nil)
	result, err := query(t, oversized, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchInvalidResponse, result.Branch, "a handshake beyond the control read bound is refused")
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	_, emptyIdentity := startScriptedDatabase(t, database, scriptedmysql.Options{ReturnsEmptyIdentityResult: true}, nil)
	result, err = query(t, emptyIdentity, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "CONNECTION_ID()")
}

func TestQueryRowsSelectsDefectBeforeContactingTheServer(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedmysql.Shape{ParameterCount: 1}}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	for name, input := range map[string]mysql.QueryRowsInput{
		"blank statement":             {Statement: "  "},
		"write statement":             {Statement: "INSERT INTO t VALUES (1)"},
		"implicitly committing DDL":   {Statement: "CREATE TABLE t (id int)"},
		"executable comment":          {Statement: "/*!50000 DROP TABLE t */ SELECT 1"},
		"transaction control":         {Statement: "-- hidden\nCOMMIT"},
		"unsupported value":           {Statement: "SELECT ?", Parameters: []any{map[string]string{"a": "b"}}},
		"not a number":                {Statement: "SELECT ?", Parameters: []any{json.Number("1e")}},
		"time outside DATETIME range": {Statement: "SELECT ?", Parameters: []any{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := query(t, client, input)
			require.NoError(t, err)
			require.Equal(t, mysql.QueryRowsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, server.Events(), "invalid input never opens a connection")

	mismatch, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT ?"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchDefect, mismatch.Branch)
	require.Contains(t, mismatch.Failure.Message, "1 placeholders but 0 parameters")
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventExecute), "a placeholder mismatch is caught after COM_STMT_PREPARE")

	withoutCredentials, err := mysql.New(mysql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: mysql.SSLModeDisabled},
		sdkgo.StaticCredentialProvider[mysql.Credentials]{})
	require.NoError(t, err)
	missing, err := query(t, withoutCredentials, mysql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchDefect, missing.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, missing.Failure.Kind)
}

func TestConnectFailuresAreClassifiedWithoutLeakingTheWrongPassword(t *testing.T) {
	database := &scriptedDatabase{}
	server, _ := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	config := mysql.Config{Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: mysql.SSLModeDisabled}
	wrong, err := mysql.New(config, sdkgo.StaticCredentialProvider[mysql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(secretLookingText)}})
	require.NoError(t, err)
	result, err := query(t, wrong, mysql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "1045", result.Receipt.Metadata["errorNumber"])
	require.NotContains(t, result.Failure.Message, "dex_app", "the server message is not copied")
	requireSecretFree(t, result)

	config.Host, config.SSLMode = "localhost", mysql.SSLModeRequired
	tlsRequired, err := mysql.New(config, sdkgo.StaticCredentialProvider[mysql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)}})
	require.NoError(t, err)
	result, err = query(t, tlsRequired, mysql.QueryRowsInput{Statement: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, result.Branch, "required never falls back to plaintext")
	require.Contains(t, result.Failure.Message, "does not accept TLS")
}

func TestTLSModesVerifyTheServerCertificate(t *testing.T) {
	authority, err := testcertificates.Issue([]string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	authorityFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(authorityFile, authority.CertificatePEM, 0o600))
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns}}
	server, err := scriptedmysql.Start(scriptedmysql.Options{Password: scriptedPassword, TLS: authority.ServerTLS}, database.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	run := func(host string, mode mysql.SSLMode) mysql.QueryRowsResult {
		client, err := mysql.New(mysql.Config{Host: host, Port: int64(server.Port()), Database: "app", User: "dex_app", SSLMode: mode},
			sdkgo.StaticCredentialProvider[mysql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)}})
		require.NoError(t, err)
		result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
		require.NoError(t, err)
		return result
	}

	required := run("127.0.0.1", mysql.SSLModeRequired)
	require.Equal(t, mysql.QueryRowsBranchCompleted, required.Branch, required.Failure)
	for _, event := range server.Events() {
		require.True(t, event.IsTLS, "every command after the handshake travelled over TLS: %s", event.Kind)
	}
	for _, mode := range []mysql.SSLMode{mysql.SSLModeVerifyCa, mysql.SSLModeVerifyIdentity} {
		unknown := run("localhost", mode)
		require.Equal(t, mysql.QueryRowsBranchProviderRejected, unknown.Branch, "the system store does not trust the test CA: %s", mode)
		require.Equal(t, sdkgo.FailureAuthentication, unknown.Failure.Kind)
	}

	t.Setenv("MYSQL_SSL_CA", authorityFile)
	for _, host := range []string{"localhost", "127.0.0.1"} {
		for _, mode := range []mysql.SSLMode{mysql.SSLModeVerifyCa, mysql.SSLModeVerifyIdentity} {
			trusted := run(host, mode)
			require.Equal(t, mysql.QueryRowsBranchCompleted, trusted.Branch, "%s %s: %v", host, mode, trusted.Failure)
		}
	}

	otherHost, err := testcertificates.Issue([]string{"db.example.com"}, nil)
	require.NoError(t, err)
	otherAuthorityFile := filepath.Join(t.TempDir(), "other-ca.pem")
	require.NoError(t, os.WriteFile(otherAuthorityFile, otherHost.CertificatePEM, 0o600))
	mismatched, err := scriptedmysql.Start(scriptedmysql.Options{Password: scriptedPassword, TLS: otherHost.ServerTLS}, database.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mismatched.Close()) })
	t.Setenv("MYSQL_SSL_CA", otherAuthorityFile)
	runMismatched := func(mode mysql.SSLMode) mysql.QueryRowsResult {
		client, err := mysql.New(mysql.Config{Host: "127.0.0.1", Port: int64(mismatched.Port()), Database: "app", User: "dex_app", SSLMode: mode},
			sdkgo.StaticCredentialProvider[mysql.Credentials]{scriptedConnection: {Password: sdkgo.NewSecretString(scriptedPassword)}})
		require.NoError(t, err)
		result, err := query(t, client, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
		require.NoError(t, err)
		return result
	}
	require.Equal(t, mysql.QueryRowsBranchCompleted, runMismatched(mysql.SSLModeVerifyCa).Branch, "verify-ca checks only the chain")
	identity := runMismatched(mysql.SSLModeVerifyIdentity)
	require.Equal(t, mysql.QueryRowsBranchProviderRejected, identity.Branch, "verify-identity also checks the host")
	require.Contains(t, identity.Failure.Message, "sslMode verification")

	notPEM := filepath.Join(t.TempDir(), "not-a-certificate.pem")
	require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0o600))
	t.Setenv("MYSQL_SSL_CA", notPEM)
	unreadable := runMismatched(mysql.SSLModeVerifyCa)
	require.Equal(t, mysql.QueryRowsBranchDefect, unreadable.Branch)
	require.Contains(t, unreadable.Failure.Message, "MYSQL_SSL_CA")
}

func TestExecuteStatementCommitsAndBindsTheIdempotencyKeyLast(t *testing.T) {
	database := &scriptedDatabase{
		shape:     scriptedmysql.Shape{ParameterCount: 3},
		execution: scriptedmysql.Execution{AffectedRows: 1, LastInsertID: 7, Warnings: 1},
	}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	result, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{
		Statement:  "INSERT INTO refunds (order_id, amount, idempotency_key) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)",
		Parameters: []any{"88213", "250.10"}, IdempotencyKeyPlaceholder: 3, MaxRowsAffected: int64Pointer(1),
	})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, mysql.StatementExecution{RowsAffected: 1, LastInsertID: "7", WarningCount: 1, Columns: []mysql.Column{}, Rows: []map[string]any{}}, result.Value)
	require.True(t, database.hasCommitted.Load())
	executions := server.EventsOfKind(scriptedmysql.EventExecute)
	require.Len(t, executions, 1)
	require.Equal(t, []scriptedmysql.Parameter{
		{Type: scriptedmysql.TypeString, Text: "88213"}, {Type: scriptedmysql.TypeString, Text: "250.10"},
		{Type: scriptedmysql.TypeString, Text: string(result.Receipt.IdempotencyKey)},
	}, executions[0].Parameters)
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey), "the key is the stable Call ID")
	queries := queryTexts(server)
	require.Equal(t, []string{"START TRANSACTION", "SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count", "COMMIT"}, queries[2:])
}

func TestExecuteStatementReturnsMariaDBReturningRows(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns, Rows: [][]any{{int64(7), "250.10"}}, LastInsertID: 7}}
	_, client := startScriptedDatabase(t, database, scriptedmysql.Options{ServerVersion: "11.4.2-MariaDB"}, nil)
	result, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "INSERT INTO refunds (amount) VALUES (250.10) RETURNING id, amount"})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, int64(1), result.Value.RowsAffected, "ROW_COUNT() is -1 after a result set, so the returned rows count")
	require.Equal(t, []map[string]any{{"id": "7", "amount": "250.10"}}, result.Value.Rows)
	require.Equal(t, "7", result.Value.LastInsertID)
}

func TestExecuteStatementReportsUncertainOnlyAfterCommitIsSent(t *testing.T) {
	for name, testCase := range map[string]struct {
		commit        scriptedmysql.CommitAction
		wantCommitted bool
	}{
		"reply lost after the server committed":         {scriptedmysql.CommitThenDropConnection, true},
		"connection lost before committing":             {scriptedmysql.DropConnectionWithoutCommitting, false},
		"server reported an error during COMMIT itself": {scriptedmysql.RejectCommitWithUnknownError, true},
	} {
		t.Run(name, func(t *testing.T) {
			database := &scriptedDatabase{shape: scriptedmysql.Shape{ParameterCount: 1}, execution: scriptedmysql.Execution{AffectedRows: 1}, commit: testCase.commit}
			_, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
			result, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "UPDATE t SET note = ?", Parameters: []any{secretLookingText}})
			require.NoError(t, err, "uncertainty is a branch, never a retry")
			require.Equal(t, mysql.ExecuteStatementBranchUncertain, result.Branch)
			require.NotEmpty(t, result.Receipt.IdempotencyKey, "the Receipt carries the key for reconciliation")
			require.Equal(t, testCase.wantCommitted, database.hasCommitted.Load())
			requireSecretFree(t, result)
		})
	}

	deadlock := &scriptedDatabase{execution: scriptedmysql.Execution{AffectedRows: 1}, commit: scriptedmysql.RejectCommitWithDeadlock}
	_, client := startScriptedDatabase(t, deadlock, scriptedmysql.Options{}, nil)
	_, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "UPDATE t SET a = 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a COMMIT rejected with a deadlock rolled back and is safe to retry")
	require.False(t, deadlock.hasCommitted.Load())
}

func TestExecuteStatementRetriesAWriteLostBeforeCommit(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedmysql.Execution{DropConnection: true}}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	_, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "UPDATE t SET a = 1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Contains(t, retry.Failure.Message, "before anything was committed")
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit))
}

func TestExecuteStatementRollsBackWhenALimitIsExceeded(t *testing.T) {
	tooMany := &scriptedDatabase{execution: scriptedmysql.Execution{AffectedRows: 40}}
	server, client := startScriptedDatabase(t, tooMany, scriptedmysql.Options{}, nil)
	result, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "DELETE FROM t", MaxRowsAffected: int64Pointer(1)})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchLimitExceeded, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "affected 40 rows but maxRowsAffected is 1")
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit), "the connector never sent COMMIT")
	require.False(t, tooMany.hasCommitted.Load())

	returning := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns, Rows: [][]any{{int64(1), "1"}, {int64(2), "2"}}}}
	server, client = startScriptedDatabase(t, returning, scriptedmysql.Options{}, func(config *mysql.Config) { config.MaxRows = 1 })
	result, err = execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "DELETE FROM t RETURNING id, amount"})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchLimitExceeded, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	require.Empty(t, server.EventsOfKind(scriptedmysql.EventCommit))
}

func TestExecuteStatementClassifiesConstraintViolationsAndInvalidInput(t *testing.T) {
	violation := &scriptedDatabase{shape: scriptedmysql.Shape{ParameterCount: 1}, execution: scriptedmysql.Execution{Error: &scriptedmysql.ServerError{
		Number: 1062, SQLState: "23000", Message: "Duplicate entry '" + secretLookingText + "' for key 'refunds.order_id'",
	}}}
	server, client := startScriptedDatabase(t, violation, scriptedmysql.Options{}, nil)
	result, err := execute(t, client, newStep(), mysql.ExecuteStatementInput{Statement: "INSERT INTO refunds (order_id) VALUES (?)", Parameters: []any{secretLookingText}})
	require.NoError(t, err)
	require.Equal(t, mysql.ExecuteStatementBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, `on "refunds.order_id"`)
	requireSecretFree(t, result)
	require.Eventually(t, func() bool { return len(server.EventsOfKind(scriptedmysql.EventQuit)) == 1 }, 5*time.Second, 10*time.Millisecond)
	before := len(server.Events())

	for name, input := range map[string]mysql.ExecuteStatementInput{
		"key placeholder not last": {Statement: "INSERT INTO t VALUES (?, ?)", Parameters: []any{"a"}, IdempotencyKeyPlaceholder: 1},
		"negative limit":           {Statement: "DELETE FROM t", MaxRowsAffected: int64Pointer(-1)},
		"implicit commit":          {Statement: "ALTER TABLE t ADD COLUMN note TEXT"},
		"table lock":               {Statement: "LOCK TABLES t WRITE"},
		"stored procedure":         {Statement: "CALL refund_order(?)", Parameters: []any{"1"}},
		"read statement":           {Statement: "SELECT 1"},
		"NaN":                      {Statement: "UPDATE t SET ratio = ?", Parameters: []any{float32(math.NaN())}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := execute(t, client, newStep(), input)
			require.NoError(t, err)
			require.Equal(t, mysql.ExecuteStatementBranchDefect, result.Branch)
		})
	}
	require.Len(t, server.Events(), before, "invalid input never opens a connection")
}

func TestExecuteStatementKeepsOneKeyAcrossAttemptsOfOneStepExecution(t *testing.T) {
	database := &scriptedDatabase{shape: scriptedmysql.Shape{ParameterCount: 1}, execution: scriptedmysql.Execution{AffectedRows: 1}}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	step := newStep()
	input := mysql.ExecuteStatementInput{Statement: "INSERT INTO t (idempotency_key) VALUES (?) ON DUPLICATE KEY UPDATE idempotency_key = idempotency_key", IdempotencyKeyPlaceholder: 1}
	first, err := execute(t, client, step, input)
	require.NoError(t, err)
	step.AttemptNumber = 2
	second, err := execute(t, client, step, input)
	require.NoError(t, err)
	third, err := execute(t, client, newStep(), input)
	require.NoError(t, err)
	require.Equal(t, first.Receipt.IdempotencyKey, second.Receipt.IdempotencyKey)
	require.NotEqual(t, first.Receipt.IdempotencyKey, third.Receipt.IdempotencyKey, "a new Step execution gets a new key")
	executions := server.EventsOfKind(scriptedmysql.EventExecute)
	require.Equal(t, executions[0].Parameters, executions[1].Parameters)
}

func TestConcurrentOperationsUseSeparateConnections(t *testing.T) {
	database := &scriptedDatabase{execution: scriptedmysql.Execution{Columns: twoColumns, Rows: [][]any{{int64(1), "1"}}}}
	server, client := startScriptedDatabase(t, database, scriptedmysql.Options{}, nil)
	var group sync.WaitGroup
	results := make([]mysql.QueryRowsResult, 8)
	errs := make([]error, len(results))
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = sdkgo.RunQuery(newStep(), client.QueryRows(), scriptedConnection, mysql.QueryRowsInput{Statement: "SELECT id, amount FROM t"})
		}()
	}
	group.Wait()
	for index := range results {
		require.NoError(t, errs[index])
		require.Equal(t, mysql.QueryRowsBranchCompleted, results[index].Branch)
	}
	require.Len(t, server.EventsOfKind(scriptedmysql.EventHandshake), len(results))
}

func int64Pointer(value int64) *int64 { return &value }

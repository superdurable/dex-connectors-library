//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// liveConfig reads SQLSERVER_CONNECTOR_TEST_* for a disposable database whose user may create tables.
func liveConfig(t *testing.T) (sqlserver.Config, string) {
	t.Helper()
	port, err := strconv.ParseInt(os.Getenv("SQLSERVER_CONNECTOR_TEST_PORT"), 10, 64)
	require.NoError(t, err, "set SQLSERVER_CONNECTOR_TEST_PORT")
	config := sqlserver.Config{
		Host: os.Getenv("SQLSERVER_CONNECTOR_TEST_HOST"), Port: port, Database: os.Getenv("SQLSERVER_CONNECTOR_TEST_DATABASE"),
		User: os.Getenv("SQLSERVER_CONNECTOR_TEST_USER"), Encrypt: sqlserver.Encrypt(os.Getenv("SQLSERVER_CONNECTOR_TEST_ENCRYPT")),
	}
	return config, os.Getenv("SQLSERVER_CONNECTOR_TEST_PASSWORD")
}

func newLiveClient(t *testing.T, config sqlserver.Config, password string) *sqlserver.Client {
	t.Helper()
	client, err := sqlserver.New(config, sdkgo.StaticCredentialProvider[sqlserver.Credentials]{
		scriptedConnection: {Password: sdkgo.NewSecretString(password)},
	})
	require.NoError(t, err)
	return client
}

// openLiveAdministration opens a database/sql handle for table setup, which the connector deliberately cannot run.
func openLiveAdministration(t *testing.T, config sqlserver.Config, password string) *sql.DB {
	t.Helper()
	driverConfig := msdsn.Config{
		Host: config.Host, Port: uint64(config.Port), Database: config.Database, User: config.User, Password: password,
		Encryption: msdsn.EncryptionRequired, TLSConfig: nil, Protocols: []string{"tcp"}, Parameters: map[string]string{},
		DisableRetry: true, Encoding: msdsn.EncodeParameters{Timezone: time.UTC},
	}
	if config.Encrypt == sqlserver.EncryptDisable {
		driverConfig.Encryption = msdsn.EncryptionDisabled
	}
	database := sql.OpenDB(mssql.NewConnectorConfig(driverConfig))
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}

func TestLiveQueryRowsMapsServerTypes(t *testing.T) {
	config, password := liveConfig(t)
	client := newLiveClient(t, config, password)
	result, err := query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT CAST(@p1 AS bigint) AS big, CAST(@p2 AS decimal(12, 2)) AS exact, " +
		"CAST(0.1 AS real) AS single, CAST('6F9619FF-8B86-D011-B42D-00C04FC964FF' AS uniqueidentifier) AS identifier, " +
		"CAST('2026-01-01T09:00:00.5+02:00' AS datetimeoffset(7)) AS moment, CAST(N'héllo' AS nvarchar(10)) AS label",
		Parameters: []any{"9007199254740993", "1200.5"}})
	require.NoError(t, err)
	require.Equal(t, sqlserver.QueryRowsBranchCompleted, result.Branch, result.Failure)
	require.Equal(t, map[string]any{
		"big": "9007199254740993", "exact": "1200.50", "single": 0.1, "identifier": "6f9619ff-8b86-d011-b42d-00c04fc964ff",
		"moment": "2026-01-01T09:00:00.5+02:00", "label": "héllo",
	}, result.Value.Rows[0])
	require.NotEmpty(t, result.Receipt.Metadata["serverVersion"])
}

func TestLiveExecuteStatementIsIdempotentAndRollsBackConflicts(t *testing.T) {
	config, password := liveConfig(t)
	administration := openLiveAdministration(t, config, password)
	table := fmt.Sprintf("dex_connector_live_%d", time.Now().UnixNano())
	_, err := administration.ExecContext(context.Background(), "CREATE TABLE dbo."+table+
		" (id int IDENTITY PRIMARY KEY, idempotency_key uniqueidentifier NOT NULL CONSTRAINT uq_"+table+" UNIQUE, note nvarchar(20) NOT NULL)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := administration.ExecContext(context.Background(), "DROP TABLE dbo."+table)
		require.NoError(t, err)
	})
	client := newLiveClient(t, config, password)
	insert := sqlserver.ExecuteStatementInput{
		Statement: "INSERT INTO dbo." + table + " (idempotency_key, note) OUTPUT INSERTED.id SELECT CAST(@p2 AS uniqueidentifier), @p1 " +
			"WHERE NOT EXISTS (SELECT 1 FROM dbo." + table + " WITH (UPDLOCK, HOLDLOCK) WHERE idempotency_key = CAST(@p2 AS uniqueidentifier))",
		Parameters: []any{"first"}, IdempotencyKeyPlaceholder: 2, ReturnsOutputRows: true,
	}
	step := newStep()
	first, err := execute(t, client, step, insert)
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchCompleted, first.Branch, first.Failure)
	require.Equal(t, int64(1), first.Value.RowsAffected)
	replay, err := execute(t, client, step, insert)
	require.NoError(t, err)
	require.Equal(t, int64(0), replay.Value.RowsAffected, "the replayed Step execution inserted nothing")

	duplicate, err := execute(t, client, newStep(), sqlserver.ExecuteStatementInput{
		Statement:  "INSERT INTO dbo." + table + " (idempotency_key, note) VALUES (CAST(@p1 AS uniqueidentifier), @p2)",
		Parameters: []any{string(first.Receipt.IdempotencyKey), "duplicate"},
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchProviderRejected, duplicate.Branch)
	require.Contains(t, duplicate.Failure.Message, `"uq_`+table+`"`)

	limit := int64(0)
	limited, err := execute(t, client, newStep(), sqlserver.ExecuteStatementInput{
		Statement: "UPDATE dbo." + table + " SET note = @p1", Parameters: []any{"changed"}, MaxRowsAffected: &limit,
	})
	require.NoError(t, err)
	require.Equal(t, sqlserver.ExecuteStatementBranchLimitExceeded, limited.Branch)
	var note string
	require.NoError(t, administration.QueryRowContext(context.Background(), "SELECT note FROM dbo."+table).Scan(&note))
	require.Equal(t, "first", note, "the limited UPDATE was rolled back")
}

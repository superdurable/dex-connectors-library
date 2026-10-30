// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/scriptedtds"
)

// TestSessionsLeaveNoDriverGoroutinesBehind covers results the connector abandons mid-stream.
func TestSessionsLeaveNoDriverGoroutinesBehind(t *testing.T) {
	rows := make([][]any, 50)
	for index := range rows {
		rows[index] = []any{int64(index), "1.00"}
	}
	executions := []scriptedtds.Execution{
		{Columns: twoColumns, Rows: rows},
		{Error: &scriptedtds.ServerError{Number: 208, State: 1, Severity: 16}},
		{Columns: twoColumns, Rows: rows, Error: &scriptedtds.ServerError{Number: 8134, State: 1, Severity: 16}},
		{Columns: twoColumns, Delay: 5 * time.Second},
	}
	baseline := runtime.NumGoroutine()
	databases := make([]*scriptedDatabase, len(executions))
	servers := make([]*scriptedtds.Server, len(executions))
	clients := make([]*sqlserver.Client, len(executions))
	for index, execution := range executions {
		databases[index] = &scriptedDatabase{execution: execution}
		server, err := scriptedtds.Start(scriptedtds.Options{Password: scriptedPassword}, databases[index].script())
		require.NoError(t, err)
		servers[index] = server
		clients[index] = newScriptedClient(t, sqlserver.Config{
			Host: "127.0.0.1", Port: int64(server.Port()), Database: "app", User: "dex_app", Encrypt: sqlserver.EncryptDisable,
			MaxRows: 2, StatementTimeout: 100 * time.Millisecond,
		})
	}
	for round := 0; round < 3; round++ {
		for _, client := range clients {
			// Every outcome is acceptable here; only the goroutines left behind matter.
			_, _ = query(t, client, sqlserver.QueryRowsInput{Statement: "SELECT id, amount FROM dbo.t"})
		}
	}
	for _, server := range servers {
		require.NoError(t, server.Close())
	}
	// Poll in this goroutine: require.Eventually runs its condition in goroutines that the count would include.
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	require.LessOrEqual(t, runtime.NumGoroutine(), baseline, "a truncated, failed, or canceled result leaves no reader goroutine")
}

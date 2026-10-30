//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRecordRefundExampleAgainstALiveServer creates dbo.refund_ledger from schema.sql in the SQLSERVER_CONNECTOR_TEST_* database.
func TestRecordRefundExampleAgainstALiveServer(t *testing.T) {
	port, err := strconv.ParseInt(os.Getenv("SQLSERVER_CONNECTOR_TEST_PORT"), 10, 64)
	require.NoError(t, err, "set SQLSERVER_CONNECTOR_TEST_PORT")
	config := sqlserver.Config{
		Host: os.Getenv("SQLSERVER_CONNECTOR_TEST_HOST"), Port: port, Database: os.Getenv("SQLSERVER_CONNECTOR_TEST_DATABASE"),
		User: os.Getenv("SQLSERVER_CONNECTOR_TEST_USER"), Encrypt: sqlserver.Encrypt(os.Getenv("SQLSERVER_CONNECTOR_TEST_ENCRYPT")),
	}
	password := os.Getenv("SQLSERVER_CONNECTOR_TEST_PASSWORD")
	administration := sql.OpenDB(mssql.NewConnectorConfig(msdsn.Config{
		Host: config.Host, Port: uint64(port), Database: config.Database, User: config.User, Password: password,
		Encryption: msdsn.EncryptionRequired, Protocols: []string{"tcp"}, Parameters: map[string]string{}, DisableRetry: true,
		Encoding: msdsn.EncodeParameters{Timezone: time.UTC},
	}))
	t.Cleanup(func() { require.NoError(t, administration.Close()) })
	schema, err := os.ReadFile("../schema.sql")
	require.NoError(t, err)
	_, err = administration.ExecContext(context.Background(), "IF OBJECT_ID(N'dbo.refund_ledger') IS NOT NULL DROP TABLE dbo.refund_ledger")
	require.NoError(t, err)
	_, err = administration.ExecContext(context.Background(), string(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := administration.ExecContext(context.Background(), "DROP TABLE dbo.refund_ledger")
		require.NoError(t, err)
	})
	harness := startTestWorker(t, config, password)
	orderID := fmt.Sprintf("live-%d", time.Now().UnixNano())

	flowID, result := harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "250.00", ExternalReference: "tkt_5488"})
	outcome := harness.requireCompletedOutcome(t, flowID, result)
	require.Equal(t, StatusRecorded, outcome.Status)
	require.Equal(t, "250.00", outcome.Refund.AmountUSD)
	require.Len(t, outcome.Refund.IdempotencyKey, 36)

	flowID, result = harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "250.00"})
	require.Equal(t, StatusAlreadyRecorded, harness.requireCompletedOutcome(t, flowID, result).Status)

	concurrentOrder := orderID + "-concurrent"
	var waitGroup sync.WaitGroup
	results := make([]dex.FlowResult, 2)
	for index := range results {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, results[index] = harness.runRefundFlow(t, Input{OrderID: concurrentOrder, AmountUSD: "5.00"})
		}()
	}
	waitGroup.Wait()
	for _, concurrent := range results {
		require.Equal(t, dex.FlowCompleted, concurrent.Status, concurrent.ErrorMessage)
	}
	var rowCount int
	require.NoError(t, administration.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM dbo.refund_ledger WHERE order_id = @p1", concurrentOrder).Scan(&rowCount))
	require.Equal(t, 1, rowCount, "two concurrent Flows for one order wrote one row")
}

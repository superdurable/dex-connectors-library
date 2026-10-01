//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/commitfaultproxy"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// liveLedger reads the real refund_ledger through the connector and administers it through database/sql.
type liveLedger struct {
	client         *mysql.Client
	config         mysql.Config
	password       string
	administration *sql.DB
}

func requireLiveLedger(t *testing.T) liveLedger {
	t.Helper()
	host := os.Getenv("MYSQL_CONNECTOR_TEST_HOST")
	if host == "" {
		t.Skip("MYSQL_CONNECTOR_TEST_HOST is not configured")
	}
	port, err := strconv.ParseInt(os.Getenv("MYSQL_CONNECTOR_TEST_PORT"), 10, 64)
	require.NoError(t, err)
	config := mysql.Config{
		Host: host, Port: port, Database: os.Getenv("MYSQL_CONNECTOR_TEST_DATABASE"),
		User: os.Getenv("MYSQL_CONNECTOR_TEST_USER"), SSLMode: mysql.SSLMode(os.Getenv("MYSQL_CONNECTOR_TEST_SSL_MODE")),
	}
	password := os.Getenv("MYSQL_CONNECTOR_TEST_PASSWORD")
	client, err := mysql.New(config, sdkgo.StaticCredentialProvider[mysql.Credentials]{
		{Provider: "mysql", Name: "live-ledger"}: {Password: sdkgo.NewSecretString(password)},
	})
	require.NoError(t, err)
	administrationConfig := mysqldriver.NewConfig()
	administrationConfig.User, administrationConfig.Passwd, administrationConfig.DBName = config.User, password, config.Database
	administrationConfig.Net, administrationConfig.Addr = "tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10))
	if config.SSLMode != mysql.SSLModeDisabled {
		administrationConfig.TLSConfig = "skip-verify"
	}
	administrationConfig.MultiStatements = true
	connector, err := mysqldriver.NewConnector(administrationConfig)
	require.NoError(t, err)
	administration := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, administration.Close()) })
	schema, err := os.ReadFile("../schema.sql")
	require.NoError(t, err)
	_, err = administration.Exec(string(schema))
	require.NoError(t, err, "the checked-in schema creates the ledger")
	return liveLedger{client: client, config: config, password: password, administration: administration}
}

func (ledger liveLedger) refundsForOrder(t *testing.T, orderID string) []map[string]any {
	t.Helper()
	step := testsupport.NewDexContext("live-ledger", fmt.Sprintf("read-%d", time.Now().UnixNano()))
	result, err := sdkgo.RunQuery(step, ledger.client.QueryRows(), sdkgo.ConnectionRef{Provider: "mysql", Name: "live-ledger"},
		mysql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{orderID}})
	require.NoError(t, err)
	require.Equal(t, mysql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	return result.Value.Rows
}

// ledgerReadLock holds LOCK TABLES refund_ledger READ, which blocks INSERT but not SELECT from other sessions.
type ledgerReadLock struct {
	connection *sql.Conn
}

func (ledger liveLedger) holdReadLock(t *testing.T) *ledgerReadLock {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := ledger.administration.Conn(ctx)
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, "LOCK TABLES refund_ledger READ")
	require.NoError(t, err)
	lock := &ledgerReadLock{connection: connection}
	t.Cleanup(func() { lock.release(t) })
	return lock
}

func (lock *ledgerReadLock) release(t *testing.T) {
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

// recordBlockedInsertSessions adds the connection IDs whose InsertRefund currently waits on the table lock.
func (ledger liveLedger) recordBlockedInsertSessions(sessions map[int64]bool) error {
	rows, err := ledger.administration.Query(
		"SELECT ID FROM information_schema.PROCESSLIST WHERE INFO LIKE 'INSERT INTO refund_ledger%' AND STATE LIKE 'Waiting for table%'",
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID int64
		if err := rows.Scan(&sessionID); err != nil {
			return err
		}
		sessions[sessionID] = true
	}
	return rows.Err()
}

func TestRecordRefundExampleWithRealDexAndMySQL(t *testing.T) {
	ledger := requireLiveLedger(t)
	harness := startTestWorker(t, ledger.config, ledger.password)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("records a refund once and then reports it as already recorded", func(t *testing.T) {
		orderID := "live-" + runID + "-a"
		flowID, result := harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "12345678.90", ExternalReference: "tkt_live"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.False(t, outcome.WasReconciled)
		require.Equal(t, "12345678.90", outcome.Refund.AmountUSD, "DECIMAL survives exactly")
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1)
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
		recordedAt, err := time.Parse(time.RFC3339Nano, outcome.Refund.RecordedAt)
		require.NoError(t, err, "TIMESTAMP is RFC 3339 UTC")
		require.WithinDuration(t, time.Now(), recordedAt, time.Minute, "the session time zone is UTC")

		flowID, result = harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "12345678.90"})
		repeated := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusAlreadyRecorded, repeated.Status)
		require.Equal(t, outcome.Refund, repeated.Refund)
		require.Len(t, ledger.refundsForOrder(t, orderID), 1)
	})

	t.Run("concurrent Flows for one order write one row and both complete", func(t *testing.T) {
		orderID := "live-" + runID + "-b"
		flowIDs := []string{
			harness.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "5.00"}),
			harness.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "5.00"}),
		}
		statuses := map[string]int{}
		for _, flowID := range flowIDs {
			outcome := harness.requireCompletedOutcome(t, flowID, harness.waitForRefundFlow(t, flowID))
			statuses[outcome.Status]++
		}
		require.Equal(t, map[string]int{StatusRecorded: 1, StatusAlreadyRecorded: 1}, statuses)
		require.Len(t, ledger.refundsForOrder(t, orderID), 1, "the order_id unique key admits one refund")
	})

	t.Run("an INSERT that outlasts the async local phase writes one row when Dex dispatches it again", func(t *testing.T) {
		config := ledger.config
		config.StatementTimeout = 15 * time.Second
		slow := startTestWorker(t, config, ledger.password)
		lock := ledger.holdReadLock(t)
		orderID := "live-" + runID + "-d"
		started := time.Now()
		flowID := slow.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "7.77"})
		// MySQL keeps a canceled dispatch waiting until the lock is released; MariaDB aborts it when its client disconnects.
		blockedSessions := map[int64]bool{}
		require.Eventually(t, func() bool {
			return ledger.recordBlockedInsertSessions(blockedSessions) == nil && len(blockedSessions) >= 2
		}, 20*time.Second, 100*time.Millisecond, "Dex dispatched InsertRefund again on a new connection while the lock was held")
		t.Logf("second InsertRefund dispatch reached the server after %s", time.Since(started).Round(100*time.Millisecond))
		// Elapsed time is the behavior under test: the database answers about nine seconds after the Flow starts.
		time.Sleep(time.Until(started.Add(9 * time.Second)))
		lock.release(t)
		outcome := slow.requireCompletedOutcome(t, flowID, slow.waitForRefundFlow(t, flowID))
		require.Equal(t, StatusRecorded, outcome.Status)
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1, "every dispatch of one Step execution carried the same idempotency key")
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
	})

	t.Run("reconciles a COMMIT whose reply was lost after MySQL applied it", func(t *testing.T) {
		proxy, err := commitfaultproxy.Start(net.JoinHostPort(ledger.config.Host, strconv.FormatInt(ledger.config.Port, 10)),
			func(int) commitfaultproxy.Fault { return commitfaultproxy.DropAfterCommitReachesServer })
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, proxy.Close()) })
		config := ledger.config
		config.Host, config.Port, config.SSLMode = "127.0.0.1", int64(proxy.Port()), mysql.SSLModeDisabled
		faulty := startTestWorker(t, config, ledger.password)
		orderID := "live-" + runID + "-c"
		flowID, result := faulty.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "99.99"})
		outcome := faulty.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "InsertRefund selected uncertain and the read-back found the row")
		require.Equal(t, 1, proxy.CommitMessages(), "the uncertain write was never retried")
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1)
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
	})

	t.Run("fails invalid input without connecting", func(t *testing.T) {
		_, result := harness.runRefundFlow(t, Input{OrderID: "live-" + runID + "-e", AmountUSD: "0"})
		require.Equal(t, dex.FlowFailed, result.Status)
	})
}

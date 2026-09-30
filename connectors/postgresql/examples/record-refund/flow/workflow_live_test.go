//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/commitfaultproxy"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// liveLedger reads the real refund_ledger directly through the connector, outside any Flow.
type liveLedger struct {
	client   *postgresql.Client
	config   postgresql.Config
	password string
}

func requireLiveLedger(t *testing.T) liveLedger {
	t.Helper()
	host := os.Getenv("POSTGRESQL_CONNECTOR_TEST_HOST")
	if host == "" {
		t.Skip("POSTGRESQL_CONNECTOR_TEST_HOST is not configured")
	}
	port, err := strconv.ParseInt(os.Getenv("POSTGRESQL_CONNECTOR_TEST_PORT"), 10, 64)
	require.NoError(t, err)
	sslMode := postgresql.SSLMode(os.Getenv("POSTGRESQL_CONNECTOR_TEST_SSL_MODE"))
	config := postgresql.Config{
		Host: host, Port: port, Database: os.Getenv("POSTGRESQL_CONNECTOR_TEST_DATABASE"),
		User: os.Getenv("POSTGRESQL_CONNECTOR_TEST_USER"), SSLMode: sslMode,
	}
	password := os.Getenv("POSTGRESQL_CONNECTOR_TEST_PASSWORD")
	client, err := postgresql.New(config, sdkgo.StaticCredentialProvider[postgresql.Credentials]{
		{Provider: "postgresql", Name: "live-ledger"}: {Password: sdkgo.NewSecretString(password)},
	})
	require.NoError(t, err)
	ledger := liveLedger{client: client, config: config, password: password}
	schema, err := os.ReadFile("../schema.sql")
	require.NoError(t, err)
	created := ledger.execute(t, string(schema))
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, created.Branch, created.Failure)
	return ledger
}

func (ledger liveLedger) execute(t *testing.T, statement string, parameters ...any) postgresql.ExecuteStatementResult {
	t.Helper()
	result, err := sdkgo.RunMutation(ledger.step(), ledger.client.ExecuteStatement(), sdkgo.ConnectionRef{Provider: "postgresql", Name: "live-ledger"},
		postgresql.ExecuteStatementInput{Statement: statement, Parameters: parameters})
	require.NoError(t, err)
	return result
}

func (ledger liveLedger) refundsForOrder(t *testing.T, orderID string) []map[string]any {
	t.Helper()
	result, err := sdkgo.RunQuery(ledger.step(), ledger.client.QueryRows(), sdkgo.ConnectionRef{Provider: "postgresql", Name: "live-ledger"},
		postgresql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{orderID}})
	require.NoError(t, err)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, result.Branch, result.Failure)
	return result.Value.Rows
}

// tableLock holds a lock that blocks INSERT but not SELECT on refund_ledger.
type tableLock struct {
	connection *pgconn.PgConn
}

func (ledger liveLedger) holdTableLock(t *testing.T) tableLock {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgconn.ParseConfig(fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=%s",
		ledger.config.Host, ledger.config.Port, ledger.config.Database, ledger.config.User, ledger.config.SSLMode))
	require.NoError(t, err)
	config.Password = ledger.password
	connection, err := pgconn.ConnectConfig(ctx, config)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, "begin; lock table refund_ledger in share row exclusive mode").ReadAll()
	require.NoError(t, err)
	return tableLock{connection: connection}
}

func (lock tableLock) release(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := lock.connection.Exec(ctx, "rollback").ReadAll()
	require.NoError(t, err)
	require.NoError(t, lock.connection.Close(ctx))
}

func (liveLedger) step() *testsupport.DexContext {
	return testsupport.NewDexContext("live-ledger", fmt.Sprintf("read-%d", time.Now().UnixNano()))
}

func TestRecordRefundExampleWithRealDexAndPostgreSQL(t *testing.T) {
	ledger := requireLiveLedger(t)
	harness := startTestWorker(t, ledger.config, ledger.password)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("records a refund once and then reports it as already recorded", func(t *testing.T) {
		orderID := "live-" + runID + "-a"
		flowID, result := harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "12345678.90", ExternalReference: "tkt_live"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.Equal(t, "12345678.90", outcome.Refund.AmountUSD, "numeric survives exactly")
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1)
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
		_, err := time.Parse(time.RFC3339Nano, outcome.Refund.RecordedAt)
		require.NoError(t, err, "timestamptz is RFC 3339")

		flowID, result = harness.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "12345678.90"})
		repeated := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusAlreadyRecorded, repeated.Status)
		require.Equal(t, outcome.Refund, repeated.Refund)
		require.Len(t, ledger.refundsForOrder(t, orderID), 1)
	})

	t.Run("concurrent Flows for one order write one row", func(t *testing.T) {
		orderID := "live-" + runID + "-b"
		flowIDs := []string{
			harness.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "5.00"}),
			harness.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "5.00"}),
		}
		results := make([]dex.FlowResult, len(flowIDs))
		for index, flowID := range flowIDs {
			results[index] = harness.waitForRefundFlow(t, flowID)
		}
		require.Len(t, ledger.refundsForOrder(t, orderID), 1, "the order_id unique constraint admits one refund")
		completed := 0
		for _, result := range results {
			if result.Status == dex.FlowCompleted {
				completed++
				continue
			}
			require.Contains(t, result.ErrorMessage, "providerRejected", "the losing INSERT hit SQLSTATE 23505")
		}
		require.GreaterOrEqual(t, completed, 1)
	})

	t.Run("an INSERT that outlasts the async local phase writes one row when Dex dispatches it again", func(t *testing.T) {
		proxy, err := commitfaultproxy.Start(net.JoinHostPort(ledger.config.Host, strconv.FormatInt(ledger.config.Port, 10)),
			func(int) commitfaultproxy.Fault { return commitfaultproxy.PassThrough })
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, proxy.Close()) })
		config := ledger.config
		config.Host, config.Port, config.SSLMode = "127.0.0.1", int64(proxy.Port()), postgresql.SSLModeDisable
		config.StatementTimeout = 15 * time.Second
		slow := startTestWorker(t, config, ledger.password)

		lock := ledger.holdTableLock(t)
		orderID := "live-" + runID + "-d"
		flowID := slow.startRefundFlow(t, Input{OrderID: orderID, AmountUSD: "7.77"})
		// Elapsed time is the behavior under test: the INSERT must wait past Dex's seven-second local phase.
		time.Sleep(9 * time.Second)
		lock.release(t)
		outcome := slow.requireRecordedOutcome(t, flowID, slow.waitForRefundFlow(t, flowID))
		require.Equal(t, StatusRecorded, outcome.Status)
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1, "every dispatch of one Step execution carried the same idempotency key")
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
		t.Logf("sessions: %d; INSERT dispatches that reached COMMIT: %d; outcome reconciled: %t", proxy.Sessions(), proxy.CommitMessages(), outcome.WasReconciled)
		require.GreaterOrEqual(t, proxy.Sessions(), 3, "FindRecordedRefund plus at least two InsertRefund dispatches")
		require.GreaterOrEqual(t, proxy.CommitMessages(), 1)
	})

	t.Run("reconciles a COMMIT whose reply was lost after PostgreSQL applied it", func(t *testing.T) {
		proxy, err := commitfaultproxy.Start(net.JoinHostPort(ledger.config.Host, strconv.FormatInt(ledger.config.Port, 10)),
			func(int) commitfaultproxy.Fault { return commitfaultproxy.DropAfterCommitReachesServer })
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, proxy.Close()) })
		config := ledger.config
		config.Host, config.Port, config.SSLMode = "127.0.0.1", int64(proxy.Port()), postgresql.SSLModeDisable
		faulty := startTestWorker(t, config, ledger.password)
		orderID := "live-" + runID + "-c"
		flowID, result := faulty.runRefundFlow(t, Input{OrderID: orderID, AmountUSD: "99.99"})
		outcome := faulty.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "InsertRefund selected uncertain and the read-back found the row")
		rows := ledger.refundsForOrder(t, orderID)
		require.Len(t, rows, 1)
		require.Equal(t, outcome.Refund.IdempotencyKey, rows[0]["idempotency_key"])
	})
}

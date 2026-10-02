//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/scriptedtds"
)

var refundLedgerColumns = []scriptedtds.Column{
	{Name: "refund_id", Type: scriptedtds.TypeBigInt}, {Name: "order_id", Type: scriptedtds.TypeNVarChar, Length: 100},
	{Name: "amount_usd", Type: scriptedtds.TypeDecimal, Precision: 12, Scale: 2},
	{Name: "external_reference", Type: scriptedtds.TypeNVarChar, Length: 200},
	{Name: "idempotency_key", Type: scriptedtds.TypeUniqueIdentifier}, {Name: "recorded_at", Type: scriptedtds.TypeDateTime2, Scale: 6},
}

// ledgerRow is one committed scripted refund_ledger row.
type ledgerRow struct {
	refundID          int64
	orderID           string
	amountUSD         string
	externalReference any
	idempotencyKey    string
}

func (row ledgerRow) values() []any {
	return []any{row.refundID, row.orderID, row.amountUSD, row.externalReference, row.idempotencyKey, "2026-10-01T12:00:00.25"}
}

// scriptedRefundLedger holds committed rows; like UPDLOCK, HOLDLOCK, an INSERT waits on a key or order another transaction holds.
type scriptedRefundLedger struct {
	mu                      sync.Mutex
	keyResolved             *sync.Cond
	pendingKeys             map[string]bool
	pendingOrders           map[string]bool
	rows                    []ledgerRow
	nextRefundID            int64
	commitActions           []scriptedtds.CommitAction
	insertFaults            []bool
	slowInsertDelay         time.Duration
	racingOrderBeforeInsert string
}

func newScriptedRefundLedger() *scriptedRefundLedger {
	ledger := &scriptedRefundLedger{pendingKeys: map[string]bool{}, pendingOrders: map[string]bool{}, nextRefundID: 1}
	ledger.keyResolved = sync.NewCond(&ledger.mu)
	return ledger
}

func (ledger *scriptedRefundLedger) script() scriptedtds.Script {
	return scriptedtds.Script{Execute: ledger.execute, Commit: ledger.commit}
}

func (ledger *scriptedRefundLedger) execute(statement scriptedtds.Statement) scriptedtds.Execution {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	switch statement.SQL {
	case FindRecordedRefundStatement:
		return ledger.selectRows(func(row ledgerRow) bool { return row.orderID == statement.Parameters[0].Text })
	case FindInsertedRefundStatement:
		return ledger.selectRows(func(row ledgerRow) bool {
			return row.idempotencyKey == statement.Parameters[0].Text || row.orderID == statement.Parameters[1].Text
		})
	case InsertRefundStatement:
		return ledger.insert(statement)
	}
	return scriptedtds.Execution{Error: &scriptedtds.ServerError{Number: 102, State: 1, Severity: 15, Message: "unexpected statement"}}
}

// insert mirrors INSERT ... OUTPUT ... SELECT ... WHERE NOT EXISTS under UPDLOCK, HOLDLOCK.
func (ledger *scriptedRefundLedger) insert(statement scriptedtds.Statement) scriptedtds.Execution {
	if len(ledger.insertFaults) > 0 {
		isDropped := ledger.insertFaults[0]
		ledger.insertFaults = ledger.insertFaults[1:]
		if isDropped {
			return scriptedtds.Execution{DropConnection: true}
		}
	}
	orderID, key := statement.Parameters[0].Text, statement.Parameters[3].Text
	if ledger.racingOrderBeforeInsert == orderID {
		ledger.racingOrderBeforeInsert = ""
		ledger.appendRow(ledgerRow{orderID: orderID, amountUSD: "1.00", idempotencyKey: racingFlowKey})
	}
	for ledger.pendingKeys[key] || ledger.pendingOrders[orderID] {
		ledger.keyResolved.Wait()
	}
	for _, row := range ledger.rows {
		if row.idempotencyKey == key || row.orderID == orderID {
			return scriptedtds.Execution{Columns: refundLedgerColumns, Rows: [][]any{}}
		}
	}
	var externalReference any
	if !statement.Parameters[2].IsNull {
		externalReference = statement.Parameters[2].Text
	}
	row := ledgerRow{refundID: ledger.nextRefundID, orderID: orderID, amountUSD: statement.Parameters[1].Text, externalReference: externalReference, idempotencyKey: key}
	ledger.nextRefundID++
	ledger.pendingKeys[key], ledger.pendingOrders[orderID] = true, true
	delay := ledger.slowInsertDelay
	ledger.slowInsertDelay = 0
	return scriptedtds.Execution{
		Columns: refundLedgerColumns, Rows: [][]any{row.values()}, Delay: delay,
		OnCommit:   func() { ledger.resolveInsert(row, true) },
		OnRollback: func() { ledger.resolveInsert(row, false) },
	}
}

func (ledger *scriptedRefundLedger) selectRows(matches func(ledgerRow) bool) scriptedtds.Execution {
	execution := scriptedtds.Execution{Columns: refundLedgerColumns, Rows: [][]any{}}
	for _, row := range ledger.rows {
		if matches(row) {
			execution.Rows = append(execution.Rows, row.values())
		}
	}
	return execution
}

func (ledger *scriptedRefundLedger) resolveInsert(row ledgerRow, isCommitted bool) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if isCommitted {
		ledger.rows = append(ledger.rows, row)
	}
	delete(ledger.pendingKeys, row.idempotencyKey)
	delete(ledger.pendingOrders, row.orderID)
	ledger.keyResolved.Broadcast()
}

func (ledger *scriptedRefundLedger) appendRow(row ledgerRow) {
	row.refundID = ledger.nextRefundID
	ledger.nextRefundID++
	ledger.rows = append(ledger.rows, row)
}

func (ledger *scriptedRefundLedger) commit() scriptedtds.CommitAction {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.commitActions) == 0 {
		return scriptedtds.CommitAndReply
	}
	action := ledger.commitActions[0]
	ledger.commitActions = ledger.commitActions[1:]
	return action
}

func (ledger *scriptedRefundLedger) committedRowCount() int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.rows)
}

// racingFlowKey is the key of a refund another Flow commits between this Flow's check and its INSERT.
const racingFlowKey = "7d3c6a52-90f1-4b8e-a2d4-6c1f0e9b5a33"

func insertExecutions(server *scriptedtds.Server) []scriptedtds.Statement {
	var inserts []scriptedtds.Statement
	for _, event := range server.EventsOfKind(scriptedtds.EventRPC) {
		if event.Statement.SQL == InsertRefundStatement {
			inserts = append(inserts, event.Statement)
		}
	}
	return inserts
}

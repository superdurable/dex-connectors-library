//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoicechanged

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
)

// TestInvoiceChangesStartOneFlowEachAcrossARestartWithRealDex: existing invoices are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestInvoiceChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newInvoiceChangedEnvironment(t, xero.InvoiceChangedTriggerConfiguration{})
	ctx := integrationContext(t, 2*time.Minute)
	existing, draft := newInvoiceID(t), newInvoiceID(t)
	environment.provider.Upsert(existing, "INV-0001", "ACCREC", "PAID", "49.95")
	environment.provider.Upsert(draft, "INV-0002", "ACCREC", "DRAFT", "20.00")
	environment.provider.AdvanceClock(time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	bill := newInvoiceID(t)
	environment.provider.Upsert(bill, "BILL-77", "ACCPAY", "AUTHORISED", "120.00")
	environment.provider.SetStatus(draft, "AUTHORISED")
	first.runner.WakePoller(InvoiceChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, xero.InvoiceTypeAccountsPayable, outcomes[bill].Current.Type)
	require.Equal(t, xero.InvoiceStatusAuthorised, outcomes[draft].DeliveredStatus)
	require.Equal(t, xero.InvoiceStatusAuthorised, outcomes[draft].Current.Status, "the Flow reads the invoice back from Xero")
	first.stop(t)

	offline := newInvoiceID(t)
	environment.provider.Upsert(offline, "INV-0003", "ACCREC", "DRAFT", "5.00")
	environment.provider.SetStatus(draft, "PAID")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(InvoiceChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "INV-0003", outcomes[offline].Current.InvoiceNumber)
	require.Equal(t, xero.InvoiceStatusPaid, outcomes[draft].Current.Status)
	require.NotContains(t, outcomes, existing, "the baseline is never delivered")
	environment.waitForCommittedSince(t, environment.lastDeliveredUpdate())
	require.Equal(t, 4, environment.deliveryCount(), "no change is delivered twice")
}

// TestChangedFilterRecordsANewBaselineWithRealDex: a saved filter change reseeds without delivering the gap.
func TestChangedFilterRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newInvoiceChangedEnvironment(t, xero.InvoiceChangedTriggerConfiguration{})
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)
	first.stop(t)

	gap := newInvoiceID(t)
	environment.provider.Upsert(gap, "INV-GAP", "ACCREC", "PAID", "9.00")
	environment.provider.AdvanceClock(time.Minute)
	environment.configuration = xero.InvoiceChangedTriggerConfiguration{Statuses: []xero.InvoiceStatus{xero.InvoiceStatusPaid}}
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	paid := newInvoiceID(t)
	environment.provider.Upsert(paid, "INV-PAID", "ACCREC", "PAID", "12.00")
	environment.provider.Upsert(newInvoiceID(t), "INV-OPEN", "ACCREC", "AUTHORISED", "3.00")
	second.runner.WakePoller(InvoiceChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, xero.InvoiceStatusPaid, outcomes[paid].Current.Status)
	require.NotContains(t, outcomes, gap, "changes between the old cursor and the new baseline are not delivered")
}

// TestMinuteRateLimitPausesThenDeliversWithRealDex: a 429 with Retry-After pauses the poll, then the change
// arrives with one Flow.
func TestMinuteRateLimitPausesThenDeliversWithRealDex(t *testing.T) {
	environment := newInvoiceChangedEnvironment(t, xero.InvoiceChangedTriggerConfiguration{})
	ctx := integrationContext(t, 2*time.Minute)
	process := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)
	invoiceID := newInvoiceID(t)
	environment.provider.Upsert(invoiceID, "INV-0429", "ACCREC", "AUTHORISED", "7.00")
	environment.provider.RateLimitNext(1, "minute", "1")
	process.runner.WakePoller(InvoiceChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "INV-0429", outcomes[invoiceID].Current.InvoiceNumber)
	throttled := 0
	for _, request := range environment.provider.Requests() {
		if request.Status == 429 {
			throttled++
		}
	}
	require.Equal(t, 1, throttled)
}

// newInvoiceID returns a random UUID, so Flow IDs stay unique across runs against one Dex server.
func newInvoiceID(t *testing.T) string {
	t.Helper()
	var random [16]byte
	_, err := rand.Read(random[:])
	require.NoError(t, err)
	random[6], random[8] = random[6]&0x0f|0x40, random[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", random[0:4], random[4:6], random[6:8], random[8:10], random[10:16])
}

func (environment *invoiceChangedEnvironment) waitForCommittedCursor(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
}

// waitForCommittedSince waits until the committed cursor's high-water mark reaches updatedAt.
func (environment *invoiceChangedEnvironment) waitForCommittedSince(t *testing.T, updatedAt time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		if state == nil {
			return false
		}
		var position struct {
			Since int64 `json:"since"`
		}
		require.NoError(t, json.Unmarshal([]byte(state.Token), &position))
		return position.Since == updatedAt.UnixMilli()
	}, 30*time.Second, 50*time.Millisecond)
}

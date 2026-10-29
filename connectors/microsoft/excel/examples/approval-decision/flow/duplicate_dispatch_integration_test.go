//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvaldecision

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex/sdk-go/dex"
)

// A healthy queued append completes once while its provider answer is delayed.
func TestApprovalDecisionSlowQueuedAppendIsSentOnceWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{DelayBeforeApplying: slowProviderDelay})
	harness := newApprovalHarness(t, provider)

	startedAt := time.Now()
	decision := harness.runDecision(t, "slow-queued-append", travelRequest("REQ-2001", 50))
	require.GreaterOrEqual(t, time.Since(startedAt), slowProviderDelay)
	require.True(t, decision.IsLogRowAppended)
	provider.WaitForDelayedRequests(t, 30*time.Second)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived), "no second dispatch while the first was in flight")
	require.Len(t, decisionRows(provider), 1)
}

func TestApprovalDecisionSlowAppendResponseIsSentOnceWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{DelayAfterApplying: slowProviderDelay})
	harness := newApprovalHarness(t, provider)

	decision := harness.runDecision(t, "slow-append-response", travelRequest("REQ-2002", 50))
	require.True(t, decision.IsLogRowAppended)
	provider.WaitForDelayedRequests(t, 30*time.Second)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.Len(t, decisionRows(provider), 1)
}

// Microsoft says to repeat an append after a 504; the connector reads the key column instead and finds its row.
func TestApprovalDecisionAppendAppliedBeforeAGatewayTimeoutIsFoundNotResentWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusGatewayTimeout})
	harness := newApprovalHarness(t, provider)

	decision := harness.runDecision(t, "gateway-timeout", travelRequest("REQ-2003", 50))
	require.Equal(t, PhaseCompleted, decision.Phase)
	require.False(t, decision.IsLogRowAppended)
	require.True(t, decision.WasLogRowAlreadyPresent, "the retry found the row the timed-out attempt appended")
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.Len(t, decisionRows(provider), 1)
}

func TestApprovalDecisionUnconfirmedAppendFailsWithoutResendingWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true})
	harness := newApprovalHarness(t, provider)

	result := harness.waitForFlow(t, harness.startDecision(t, "unconfirmed", travelRequest("REQ-2004", 50)))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "uncertain")
	require.Contains(t, result.ErrorMessage, "no target")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.Zero(t, provider.Count(fakeexcel.CountRangeUpdatesReceived), "the summary is not written for an uncertain log row")
}

func TestApprovalDecisionLostWorkerDuringTheAppendIsNeverResentWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	hold := make(chan struct{})
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{Hold: hold, IsHiddenFromKeyReads: true})
	harness := newApprovalHarness(t, provider)

	flowID := harness.startDecision(t, "lost-worker", travelRequest("REQ-2005", 50))
	require.Eventually(t, func() bool { return provider.Count(fakeexcel.CountAppendsReceived) == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Excel")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(hold)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "uncertain")
	require.Contains(t, result.ErrorMessage, "no target")
	provider.WaitForDelayedRequests(t, 30*time.Second)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived), "the attempt on the new Worker did not resend the append")
}

func TestApprovalDecisionThrottledAppendIsRetriedOnceRefusedWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusWithoutApplying: http.StatusTooManyRequests, RetryAfterSeconds: 1})
	harness := newApprovalHarness(t, provider)

	decision := harness.runDecision(t, "throttled-append", travelRequest("REQ-2006", 50))
	require.True(t, decision.IsLogRowAppended)
	require.Equal(t, 2, provider.Count(fakeexcel.CountAppendsReceived))
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsApplied))
	require.Len(t, decisionRows(provider), 1)
}

// updateValues survives a lost Worker after its first write is applied.
func TestApprovalDecisionSlowSummaryWriteIsHarmlessWhenRepeatedWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	hold := make(chan struct{})
	provider.QueueRangeUpdateHolds(hold)
	harness := newApprovalHarness(t, provider)
	flowID := harness.startDecision(t, "lost-worker-summary", travelRequest("REQ-2007", 50))
	require.Eventually(t, func() bool { return provider.Count(fakeexcel.CountRangeUpdatesApplied) == 1 }, 30*time.Second, 50*time.Millisecond)
	harness.replaceWorker(t)
	require.Eventually(t, func() bool { return provider.Count(fakeexcel.CountRangeUpdatesReceived) == 2 }, time.Minute, 50*time.Millisecond)
	close(hold)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var decision ApprovalDecision
	require.NoError(t, result.DecodeSingleOutput(&decision))
	require.True(t, decision.IsSummaryConfirmed)
	provider.WaitForDelayedRequests(t, 30*time.Second)
	require.Equal(t, 2, provider.Count(fakeexcel.CountRangeUpdatesReceived))
	require.Equal(t, "REQ-2007", provider.CellValue(integrationDriveID, summaryWorkbookID, "Summary", "A2"))
	require.Len(t, decisionRows(provider), 1)
}

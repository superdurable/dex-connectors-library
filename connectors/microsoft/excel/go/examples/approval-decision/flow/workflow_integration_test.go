//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvaldecision

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex/sdk-go/dex"
)

func travelRequest(requestID string, amount float64) Input {
	return Input{RequestID: requestID, Requester: "dana@contoso.com", Category: "Travel", AmountUSD: amount}
}

func TestApprovalDecisionLogsAndSummarizesTheDecisionWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	harness := newApprovalHarness(t, provider)

	decision := harness.runDecision(t, "approved", travelRequest("REQ-1001", 420))
	require.Equal(t, PhaseCompleted, decision.Phase)
	require.Equal(t, DecisionApproved, decision.Decision)
	require.Equal(t, 500.0, decision.MaxAutoApproveUSD)
	require.True(t, decision.IsLogRowAppended)
	require.False(t, decision.WasLogRowAlreadyPresent)
	require.True(t, decision.IsSummaryConfirmed)
	require.Equal(t, "Summary!"+SummaryAddress, decision.SummaryAddress)
	require.Equal(t, [][]any{{"REQ-1001", "dana@contoso.com", "Travel", 420.0, "approved", "", decision.DecidedAt}}, decisionRows(provider))
	require.Equal(t, "REQ-1001", provider.CellValue(integrationDriveID, summaryWorkbookID, "Summary", "A2"))
	require.Equal(t, 420.0, provider.CellValue(integrationDriveID, summaryWorkbookID, "Summary", "C2"))
	require.Equal(t, decision.DecidedAt, provider.CellValue(integrationDriveID, summaryWorkbookID, "Summary", "E2"),
		"the RFC 3339 time stays text instead of becoming an Excel date")
	require.Zero(t, provider.Count(fakeexcel.CountFormulasWritten))
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
}

func TestApprovalDecisionForTheSameRequestAgainLogsNothingWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	harness := newApprovalHarness(t, provider)

	first := harness.runDecision(t, "first", travelRequest("REQ-1002", 900))
	require.Equal(t, DecisionEscalated, first.Decision)
	require.Equal(t, "lead@contoso.com", first.Approver)
	second := harness.runDecision(t, "second", travelRequest("REQ-1002", 900))
	require.False(t, second.IsLogRowAppended)
	require.True(t, second.WasLogRowAlreadyPresent, "another Flow already logged the request ID")
	require.Len(t, decisionRows(provider), 1)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
}

func TestApprovalDecisionKeepsFormulaLikeTextLiteralWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	harness := newApprovalHarness(t, provider)

	input := Input{RequestID: "00123", Requester: `=HYPERLINK("https://example.com")`, Category: "catering", AmountUSD: 10}
	decision := harness.runDecision(t, "literal", input)
	require.Equal(t, DecisionNeedsReview, decision.Decision)
	require.NotEmpty(t, decision.Reason)
	require.Equal(t, [][]any{{"00123", `=HYPERLINK("https://example.com")`, "catering", 10.0, "needsReview", "", decision.DecidedAt}}, decisionRows(provider))
	require.Zero(t, provider.Count(fakeexcel.CountFormulasWritten), "no text became a formula")
	require.True(t, decision.IsSummaryConfirmed, "00123 reads back as text, not the number 123")
}

func TestApprovalDecisionWaitsForRetryAfterWhenTheReadIsThrottledWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	provider.QueueReadFailures(fakeexcel.ReadFailure{Status: http.StatusTooManyRequests, RetryAfterSeconds: 2, SecondLevelCode: "tooManyRequestsUncategorized"})
	harness := newApprovalHarness(t, provider)

	decision := harness.runDecision(t, "throttled", travelRequest("REQ-1003", 100))
	require.Equal(t, PhaseCompleted, decision.Phase)
	require.Equal(t, 2, provider.Count(fakeexcel.CountColumnReads), "the throttled policy read was retried once")
}

func TestApprovalDecisionInvalidRequestFailsBeforeCallingExcelWithRealDex(t *testing.T) {
	provider := newApprovalProvider(t)
	harness := newApprovalHarness(t, provider)

	result := harness.waitForFlow(t, harness.startDecision(t, "invalid", Input{RequestID: " ", Requester: "a", Category: "travel", AmountUSD: 1}))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.Count(fakeexcel.CountColumnReads))
}

func TestApprovalDecisionMissingTableFailsTheFlowThroughAnUnwiredBranchWithRealDex(t *testing.T) {
	provider := fakeexcel.New(t, integrationAccessToken, "integration-refresh")
	harness := newApprovalHarness(t, provider)

	result := harness.waitForFlow(t, harness.startDecision(t, "missing-table", travelRequest("REQ-1004", 10)))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.Equal(t, 1, provider.Count(fakeexcel.CountColumnReads), "notFound is a branch, not a retry")
}

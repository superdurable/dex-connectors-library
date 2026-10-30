//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountusagesummary

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/fakesnowflake"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	usageColumns = []fakesnowflake.Column{
		{Name: "EVENT_COUNT", Type: "fixed", Precision: 18},
		{Name: "CREDITS_USED", Type: "fixed", Precision: 38, Scale: 4},
		{Name: "LAST_EVENT_AT", Type: "timestamp_ntz", Scale: 9, Nullable: true},
	}
	usageRow = []*string{fakesnowflake.Text("128"), fakesnowflake.Text("42.5000"), fakesnowflake.Text("1767225600.500000000")}
	request  = Input{AccountID: "acct_003", Since: "2026-01-01"}
)

func completedScript(runningReads int) fakesnowflake.StatementScript {
	return fakesnowflake.StatementScript{RunningReads: runningReads, Columns: usageColumns, Partitions: [][][]*string{{usageRow}}}
}

func requireUsage(t *testing.T, record UsageSummaryRecord) {
	t.Helper()
	require.Equal(t, PhaseCompleted, record.Phase)
	require.NotNil(t, record.Usage)
	require.Equal(t, "128", record.Usage.EventCount)
	require.Equal(t, "42.5000", record.Usage.CreditsUsed)
	require.Equal(t, "2026-01-01T00:00:00.5", *record.Usage.LastEventAt)
}

func TestAccountUsageSummaryWaitsDurablyUntilTheStatementCompletesWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return completedScript(2) })
	harness := newUsageHarness(t, provider, 5)

	record := harness.runSummary(t, "completed", request)
	requireUsage(t, record)
	require.Equal(t, 2, record.StatusReads, "two reads found the statement running, the third read its rows")
	require.Equal(t, 1, provider.ExecutionCount())
	require.Equal(t, 3, provider.ReadCount(record.StatementHandle))
	submissions := provider.Submissions()
	require.Len(t, submissions, 1)
	require.Equal(t, UsageSummaryStatement, submissions[0].Statement)
	require.Equal(t, "acct_003", *submissions[0].Bindings["1"]["value"], "the account is bound, never interpolated")
	require.Equal(t, "2026-01-01", *submissions[0].Bindings["2"]["value"])
	require.True(t, submissions[0].IsAsync && submissions[0].IsRetry)
}

func TestAccountUsageSummaryFailsOnFailedStatementWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
		return fakesnowflake.StatementScript{RunningReads: 1, Failure: &fakesnowflake.StatementFailure{Code: "100038", SQLState: "22018"}}
	})
	harness := newUsageHarness(t, provider, 5)

	result := harness.waitForFlow(t, harness.startSummary(t, "failed", request))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "providerRejected")
	require.Contains(t, result.ErrorMessage, "no target")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	require.Equal(t, 1, provider.ExecutionCount())
}

func TestAccountUsageSummaryFailsOnRejectedSubmissionWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
		return fakesnowflake.StatementScript{RejectSubmission: &fakesnowflake.StatementFailure{Code: "002003", SQLState: "02000"}}
	})
	harness := newUsageHarness(t, provider, 5)

	result := harness.waitForFlow(t, harness.startSummary(t, "rejected", request))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "providerRejected")
	require.Contains(t, result.ErrorMessage, "no target")
	require.Zero(t, provider.ExecutionCount())
}

func TestAccountUsageSummaryCancelsAfterTheWaitBudgetWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return completedScript(100) })
	harness := newUsageHarness(t, provider, 3)

	record := harness.runSummary(t, "wait-budget", request)
	require.Equal(t, PhaseWaitBudgetExhausted, record.Phase)
	require.True(t, record.IsCancellationAccepted)
	require.Equal(t, 3, record.StatusReads)
	require.Equal(t, 3, provider.ReadCount(record.StatementHandle))
	require.Equal(t, 1, provider.CancelCount(record.StatementHandle))
}

func TestAccountUsageSummaryCompletesWhenCancellationIsRefusedAfterTheStatementFinishesWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return completedScript(3) })
	harness := newUsageHarness(t, provider, 3)

	record := harness.runSummary(t, "cancel-finished", request)
	require.Equal(t, PhaseWaitBudgetExhausted, record.Phase)
	require.False(t, record.IsCancellationAccepted)
	require.Equal(t, 3, record.StatusReads)
	require.Equal(t, 1, provider.CancelCount(record.StatementHandle))
}

// The fake starts the statement, then loses its reply when the Worker stops.
func TestAccountUsageSummarySlowSubmitDispatchedTwiceRunsOneStatementWithRealDex(t *testing.T) {
	firstSubmitHold := make(chan struct{})
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript {
		script := completedScript(0)
		script.FirstSubmitHold = firstSubmitHold
		return script
	})
	harness := newUsageHarness(t, provider, 5)

	flowID := harness.startSummary(t, "lost-submit", request)
	require.Eventually(t, func() bool { return len(provider.Submissions()) == 1 && provider.ExecutionCount() == 1 },
		30*time.Second, 10*time.Millisecond)
	harness.replaceWorker(t)
	record := harness.requireCompletedRecord(t, flowID)
	requireUsage(t, record)
	submissions := provider.Submissions()
	require.Len(t, submissions, 2, "the replacement Worker resubmits once")
	for _, submission := range submissions {
		require.Equal(t, submissions[0].RequestID, submission.RequestID, "every dispatch of one Step execution reuses its requestId")
		require.True(t, submission.IsRetry)
	}
	require.Equal(t, 1, provider.ExecutionCount(), "retry=true with the same requestId never runs the statement twice")
	require.Equal(t, provider.HandleForRequestID(submissions[0].RequestID), record.StatementHandle)
}

func TestAccountUsageSummaryResumesAfterTheWorkerIsLostDuringTheWaitWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return completedScript(3) })
	harness := newUsageHarness(t, provider, 10)

	flowID := harness.startSummary(t, "worker-restart", request)
	require.Eventually(t, func() bool {
		return provider.ExecutionCount() == 1 && provider.ReadCount(provider.HandleForRequestID(provider.Submissions()[0].RequestID)) >= 1
	},
		30*time.Second, 50*time.Millisecond, "the Flow submitted the statement and read it running at least once")
	harness.replaceWorker(t)

	record := harness.requireCompletedRecord(t, flowID)
	requireUsage(t, record)
	require.Equal(t, 1, provider.ExecutionCount(), "the replacement Worker resumed the durable wait instead of resubmitting")
	require.Equal(t, 3, record.StatusReads)
}

func TestAccountUsageSummaryRejectsInvalidInputBeforeSnowflakeWithRealDex(t *testing.T) {
	provider := newUsageProvider(t, func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript { return completedScript(0) })
	harness := newUsageHarness(t, provider, 5)

	result := harness.waitForFlow(t, harness.startSummary(t, "invalid", Input{AccountID: "acct_003", Since: "yesterday"}))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "since must be a date")
	require.Empty(t, provider.Submissions())
}

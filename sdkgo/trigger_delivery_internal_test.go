// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
)

func TestClassifyDexTriggerError(t *testing.T) {
	serviceError := func(code codes.Code, detail string) *dex.ServiceError {
		return &dex.ServiceError{Op: "InvokeRPC", FlowID: "flow-1", Code: code, Detail: detail}
	}
	// Dex reports every Worker failure with an outer FailedPrecondition code.
	workerError := func(code codes.Code, detail string) *dex.WorkerInvocationError {
		return &dex.WorkerInvocationError{
			ServiceError: serviceError(codes.FailedPrecondition, detail),
			Worker:       &dex.WorkerError{Code: code, Type: "*errors.joinError", Detail: detail},
		}
	}
	undeliverable := []struct {
		name string
		err  error
	}{
		{"completed Flow", &dex.FlowNotActiveError{ServiceError: serviceError(codes.NotFound, "workflow execution already completed")}},
		{"never started Flow", &dex.FlowNotActiveError{ServiceError: serviceError(codes.NotFound, "workflow not found for ID: x")}},
		{"missing Flow", &dex.FlowNotFoundError{ServiceError: serviceError(codes.NotFound, "workflow not found")}},
		{"handler marker", workerError(codes.FailedPrecondition, "Trigger event is undeliverable: approver is not allowed")},
		{"handler marker without reason", workerError(codes.FailedPrecondition, "Trigger event is undeliverable")},
		{"unencodable input", &dex.ValueMappingError{Operation: "encode", Err: errors.New("json: unsupported type")}},
	}
	for _, testCase := range undeliverable {
		t.Run("undeliverable "+testCase.name, func(t *testing.T) {
			classified := classifyDexTriggerError(testCase.err)
			require.True(t, IsTriggerUndeliverable(classified))
			require.ErrorIs(t, classified, testCase.err)
			require.Contains(t, classified.Error(), "Trigger event is undeliverable: ")
		})
	}

	retryable := []struct {
		name string
		err  error
	}{
		{"handler error", workerError(codes.Unknown, "downstream is temporarily unavailable")},
		{"handler panic", workerError(codes.Internal, "panic: boom")},
		{"handler FailedPrecondition without the marker", workerError(codes.FailedPrecondition, "approver is not allowed")},
		{"handler returned another Flow's Dex error", workerError(codes.FailedPrecondition,
			`dex: InvokeRPC flow "downstream": FailedPrecondition: downstream is temporarily unavailable`)},
		{"handler wrapped the marker", workerError(codes.FailedPrecondition, "approve: Trigger event is undeliverable: approver is not allowed")},
		{"Worker unavailable", workerError(codes.Unavailable, "connection refused")},
		{"Worker lacks RPC", workerError(codes.NotFound, `dex: flow "approval" is not registered`)},
		{"Worker cannot decode input", workerError(codes.InvalidArgument, "dex: decode value")},
		{"Worker error without details", &dex.WorkerInvocationError{ServiceError: serviceError(codes.FailedPrecondition, "worker failed")}},
		{"lock conflict", &dex.RPCLockConflictError{ServiceError: serviceError(codes.Aborted, "lock conflict")}},
		{"long poll timeout", &dex.LongPollTimeoutError{ServiceError: serviceError(codes.DeadlineExceeded, "long poll timed out")}},
		{"wait handler timeout", &dex.WaitHandlerTimeoutError{ServiceError: serviceError(codes.DeadlineExceeded, "wait timed out")}},
		{"Channel message consumed concurrently", &dex.ChannelMessageNotFoundError{ServiceError: serviceError(codes.NotFound, "message not found")}},
		{"undecodable output", &dex.ValueMappingError{Operation: "decode", Err: errors.New("json: cannot unmarshal")}},
		{"unregistered Flow", &dex.FlowDefinitionError{FlowType: "approval", Err: errors.New("not registered")}},
		{"server rejects a Step option", serviceError(codes.InvalidArgument, "heartbeat timeout must be zero or at least 10s")},
		{"server rejects an Attribute Store", serviceError(codes.InvalidArgument, `unknown Attribute Store "archive"`)},
		{"invalid request", serviceError(codes.InvalidArgument, "flow ID is too long")},
		{"Dex unavailable", serviceError(codes.Unavailable, "connection refused")},
		{"deadline exceeded", serviceError(codes.DeadlineExceeded, "deadline exceeded")},
		{"call canceled", serviceError(codes.Canceled, "canceled")},
		{"internal", serviceError(codes.Internal, "internal")},
		{"unknown", serviceError(codes.Unknown, "unknown")},
		{"resource exhausted", serviceError(codes.ResourceExhausted, "busy")},
		{"terminal cleanup in progress", serviceError(codes.FailedPrecondition, "terminal cleanup in progress")},
		{"unauthenticated", serviceError(codes.Unauthenticated, "unauthenticated")},
		{"permission denied", serviceError(codes.PermissionDenied, "permission denied")},
		{"unimplemented", serviceError(codes.Unimplemented, "unimplemented")},
		{"context canceled", context.Canceled},
		{"closed client", errors.New("dex: Client is closed")},
		{"client validation", fmt.Errorf("dex: RPC input %T is not assignable to %s", 0, "string")},
	}
	for _, testCase := range retryable {
		t.Run("retryable "+testCase.name, func(t *testing.T) {
			classified := classifyDexTriggerError(testCase.err)
			require.False(t, IsTriggerUndeliverable(classified))
			require.Same(t, testCase.err, classified)
		})
	}

	require.NoError(t, classifyDexTriggerError(nil))
	marked := MarkTriggerUndeliverable(errors.New("already marked"))
	require.Same(t, marked, classifyDexTriggerError(marked))
}

// TestDeliverTriggerWaitsTheRetrySchedule records the waits DeliverTrigger requests between attempts, and
// the backoff record it logs before each wait.
func TestDeliverTriggerWaitsTheRetrySchedule(t *testing.T) {
	previousWait := waitForTriggerRetry
	t.Cleanup(func() { waitForTriggerRetry = previousWait })
	var waits []time.Duration
	waitForTriggerRetry = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	logs := testsupport.NewLogRecorder()
	attempts := 0
	err := DeliverTrigger(context.Background(), TriggerTargetFunc[string](func(context.Context, TriggerEvent[string]) error {
		attempts++
		if attempts <= 9 {
			return errors.New("Dex is unavailable")
		}
		return nil
	}), TriggerEvent[string]{ID: "Ev1", Payload: "SENTINEL-MESSAGE-TEXT"}, WithTriggerLogger(logs.Logger().With("binding", "approval-reply")))
	require.NoError(t, err)
	require.Equal(t, 10, attempts)
	schedule := []time.Duration{
		250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	require.Equal(t, schedule, waits)

	retries := logs.Find("trigger delivery failed; retrying")
	require.Len(t, retries, len(schedule))
	for index, record := range retries {
		require.Equal(t, slog.LevelWarn, record.Level)
		require.Equal(t, map[string]string{
			"binding": "approval-reply", "event_id": "Ev1", "attempt": strconv.Itoa(index + 1),
			"delay": schedule[index].String(), "error": "Dex is unavailable",
		}, record.Attrs)
	}
	recovered := logs.Find("trigger delivered after retry")
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, map[string]string{"binding": "approval-reply", "event_id": "Ev1", "attempts": "10"}, recovered[0].Attrs)
	require.Len(t, logs.Records(), len(schedule)+1)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// TestDeliverTriggerStopsWaitingWhenContextEnds cancels during a long backoff, not during an attempt.
func TestDeliverTriggerStopsWaitingWhenContextEnds(t *testing.T) {
	previousDelay := triggerRetryInitialDelay
	triggerRetryInitialDelay = time.Hour
	t.Cleanup(func() { triggerRetryInitialDelay = previousDelay })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- DeliverTrigger(ctx, TriggerTargetFunc[string](func(context.Context, TriggerEvent[string]) error {
			close(attempted)
			return errors.New("Dex is unavailable")
		}), TriggerEvent[string]{ID: "Ev1"})
	}()
	<-attempted
	// Give DeliverTrigger time to start waiting before cancelling.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("DeliverTrigger kept waiting for its backoff after cancellation")
	}
}

func TestTriggerRetryDelayIsCappedExponential(t *testing.T) {
	expected := []time.Duration{
		250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	for index, delay := range expected {
		require.Equal(t, delay, triggerRetryDelay(index+1), "attempt %d", index+1)
	}
	require.Equal(t, 30*time.Second, triggerRetryDelay(1000))
}

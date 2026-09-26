// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMarkTriggerUndeliverablePreservesCauseAndGRPCStatus(t *testing.T) {
	require.NoError(t, sdkgo.MarkTriggerUndeliverable(nil))
	require.False(t, sdkgo.IsTriggerUndeliverable(nil))

	cause := &dex.FlowNotActiveError{ServiceError: &dex.ServiceError{
		Op: "InvokeRPC", FlowID: "flow-1", Code: codes.NotFound, Detail: "workflow execution already completed",
	}}
	marked := sdkgo.MarkTriggerUndeliverable(cause)
	require.True(t, sdkgo.IsTriggerUndeliverable(marked))
	require.ErrorIs(t, marked, cause)
	var notActive *dex.FlowNotActiveError
	require.ErrorAs(t, marked, &notActive)
	require.Same(t, cause, notActive)
	var undeliverable *sdkgo.UndeliverableTriggerError
	require.ErrorAs(t, marked, &undeliverable)
	require.Same(t, cause, undeliverable.Err)
	require.Equal(t, "Trigger event is undeliverable: "+cause.Error(), marked.Error())
	require.Same(t, marked, sdkgo.MarkTriggerUndeliverable(marked))

	wrapped := fmt.Errorf("deliver approval: %w", marked)
	require.True(t, sdkgo.IsTriggerUndeliverable(wrapped))
	rpcStatus, ok := status.FromError(marked)
	require.True(t, ok)
	require.Equal(t, codes.FailedPrecondition, rpcStatus.Code())
	require.Equal(t, marked.Error(), rpcStatus.Message())
	require.Equal(t, codes.FailedPrecondition, status.Code(wrapped))

	// Marking an error that only wraps a marker adds an outer marker, so a Dex Worker reports it first.
	remarked := sdkgo.MarkTriggerUndeliverable(wrapped)
	require.ErrorIs(t, remarked, wrapped)
	require.Equal(t, "Trigger event is undeliverable: "+wrapped.Error(), remarked.Error())

	require.Equal(t, "Trigger event is undeliverable", (&sdkgo.UndeliverableTriggerError{}).Error())
	var nilError *sdkgo.UndeliverableTriggerError
	require.Equal(t, "<nil>", nilError.Error())
	require.NoError(t, nilError.Unwrap())
}

func TestDeliverTriggerRetriesUntilConsumedAndHonorsCancellation(t *testing.T) {
	event := sdkgo.TriggerEvent[string]{ID: "Ev1", Payload: "SENTINEL-MESSAGE-TEXT approve"}
	unavailable := &dex.ServiceError{Op: "InvokeRPC", Code: codes.Unavailable, Detail: "connection refused"}

	t.Run("retryable errors are retried with backoff", func(t *testing.T) {
		logs := testsupport.NewLogRecorder()
		var attempts atomic.Int32
		started := time.Now()
		err := sdkgo.DeliverTrigger(context.Background(), sdkgo.TriggerTargetFunc[string](func(_ context.Context, received sdkgo.TriggerEvent[string]) error {
			require.Equal(t, event, received)
			if attempts.Add(1) <= 2 {
				return unavailable
			}
			return nil
		}), event, sdkgo.WithTriggerLogger(logs.Logger()))
		require.NoError(t, err)
		require.Equal(t, int32(3), attempts.Load())
		require.GreaterOrEqual(t, time.Since(started), 700*time.Millisecond)
		retries := logs.Find("trigger delivery failed; retrying")
		require.Len(t, retries, 2)
		for index, delay := range []string{"250ms", "500ms"} {
			require.Equal(t, slog.LevelWarn, retries[index].Level)
			require.Equal(t, map[string]string{
				"event_id": "Ev1", "attempt": fmt.Sprint(index + 1), "delay": delay, "error": unavailable.Error(),
			}, retries[index].Attrs)
		}
		recovered := logs.Find("trigger delivered after retry")
		require.Len(t, recovered, 1)
		require.Equal(t, slog.LevelInfo, recovered[0].Level)
		require.Equal(t, map[string]string{"event_id": "Ev1", "attempts": "3"}, recovered[0].Attrs)
		require.Len(t, logs.Records(), 3)
		require.NotContains(t, logs.Text(), "SENTINEL")
	})

	t.Run("a first-attempt delivery logs nothing", func(t *testing.T) {
		logs := testsupport.NewLogRecorder()
		require.NoError(t, sdkgo.DeliverTrigger(context.Background(), sdkgo.TriggerTargetFunc[string](
			func(context.Context, sdkgo.TriggerEvent[string]) error { return nil },
		), event, sdkgo.WithTriggerLogger(logs.Logger())))
		require.Empty(t, logs.Records())
	})

	t.Run("undeliverable events are consumed", func(t *testing.T) {
		logs := testsupport.NewLogRecorder()
		var attempts atomic.Int32
		undeliverable := sdkgo.MarkTriggerUndeliverable(errors.New("thread closed"))
		err := sdkgo.DeliverTrigger(context.Background(), sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
			attempts.Add(1)
			return undeliverable
		}), event, sdkgo.WithTriggerLogger(logs.Logger().With("binding", "approval-reply")))
		require.NoError(t, err)
		require.Equal(t, int32(1), attempts.Load())
		records := logs.Records()
		require.Len(t, records, 1)
		require.Equal(t, slog.LevelWarn, records[0].Level)
		require.Equal(t, "trigger event skipped: undeliverable", records[0].Message)
		require.Equal(t, map[string]string{
			"binding": "approval-reply", "event_id": "Ev1", "error": "Trigger event is undeliverable: thread closed",
		}, records[0].Attrs)
		require.NotContains(t, logs.Text(), "SENTINEL")
	})

	t.Run("cancellation stops retries", func(t *testing.T) {
		logs := testsupport.NewLogRecorder()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var attempts atomic.Int32
		started := time.Now()
		err := sdkgo.DeliverTrigger(ctx, sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
			attempts.Add(1)
			cancel()
			return unavailable
		}), event, sdkgo.WithTriggerLogger(logs.Logger()))
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, int32(1), attempts.Load())
		require.Less(t, time.Since(started), time.Second)
		require.Empty(t, logs.Records(), "an attempt that failed because the context ended is not a retry")
	})
}

type scanningTriggerSource struct {
	events []sdkgo.TriggerEvent[string]
}

// Run mimics a polling source that stops the current scan at the first delivery error.
func (source scanningTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[string]) error {
	for _, event := range source.events {
		if err := sdkgo.PrepareTriggerDelivery(ctx, target, event); err != nil {
			return err
		}
		if err := target.HandleTrigger(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type preparingTriggerTarget struct {
	prepared []string
	handled  []string
}

func (target *preparingTriggerTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[string]) error {
	target.prepared = append(target.prepared, event.ID)
	return nil
}

func (target *preparingTriggerTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[string]) error {
	target.handled = append(target.handled, event.ID)
	if event.ID == "poison" {
		return sdkgo.MarkTriggerUndeliverable(errors.New("reply belongs to a closed thread"))
	}
	return nil
}

func TestTriggerRunnerConsumesUndeliverableEventsBeforeSource(t *testing.T) {
	definition := sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "gmail", TriggerName: "replyReceived"},
		Description: "Receive a matching reply.",
	}
	target := &preparingTriggerTarget{}
	logs := testsupport.NewLogRecorder()
	runner, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[string]{
		Definition: definition,
		Binding: sdkgo.TriggerBindingRef{
			Connection: sdkgo.ConnectionRef{Provider: "google", Name: "inbox"}, Trigger: definition.Trigger, Name: "approval-reply",
		},
		Source: scanningTriggerSource{events: []sdkgo.TriggerEvent[string]{
			{ID: "poison", Payload: "SENTINEL-MESSAGE-TEXT"}, {ID: "good", Payload: "SENTINEL-MESSAGE-TEXT"},
		}},
		Target: target,
		Logger: logs.Logger(),
	})
	require.NoError(t, err)
	require.NoError(t, runner.Run(context.Background()))
	require.Equal(t, []string{"poison", "good"}, target.prepared)
	require.Equal(t, []string{"poison", "good"}, target.handled)
	records := logs.Records()
	require.Len(t, records, 1)
	require.Equal(t, slog.LevelWarn, records[0].Level)
	require.Equal(t, "trigger event skipped: undeliverable", records[0].Message)
	require.Equal(t, map[string]string{
		"connector": "gmail", "connection": "inbox", "trigger": "replyReceived", "binding": "approval-reply",
		"event_id": "poison", "error": "Trigger event is undeliverable: reply belongs to a closed thread",
	}, records[0].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// retryingTriggerSource delivers each event with DeliverTrigger, as the Slack source does.
type retryingTriggerSource struct {
	events []sdkgo.TriggerEvent[string]
	logger *slog.Logger
}

func (source retryingTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[string]) error {
	for _, event := range source.events {
		if err := sdkgo.DeliverTrigger(ctx, target, event, sdkgo.WithTriggerLogger(source.logger)); err != nil {
			return err
		}
	}
	return nil
}

// TestTriggerRunnerLogsSkipAfterRetryOnce covers an event that fails once and is then undeliverable: the
// source logs the retry, the runner logs the skip, and nothing reports the event as delivered.
func TestTriggerRunnerLogsSkipAfterRetryOnce(t *testing.T) {
	definition := sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "slack", TriggerName: "threadReplyCreated"},
		Description: "Receive a matching reply.",
	}
	logs := testsupport.NewLogRecorder()
	var attempts atomic.Int32
	runner, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[string]{
		Definition: definition,
		Binding: sdkgo.TriggerBindingRef{
			Connection: sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}, Trigger: definition.Trigger, Name: "approval-reply",
		},
		Source: retryingTriggerSource{events: []sdkgo.TriggerEvent[string]{{ID: "EvLate"}}, logger: logs.Logger()},
		Target: sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
			if attempts.Add(1) == 1 {
				return errors.New("Dex is unavailable")
			}
			return sdkgo.MarkTriggerUndeliverable(errors.New("workflow execution already completed"))
		}),
		Logger: logs.Logger(),
	})
	require.NoError(t, err)
	require.NoError(t, runner.Run(context.Background()))
	require.Equal(t, int32(2), attempts.Load())
	messages := []string{}
	for _, record := range logs.Records() {
		messages = append(messages, record.Message)
	}
	require.Equal(t, []string{"trigger delivery failed; retrying", "trigger event skipped: undeliverable"}, messages)
	require.Equal(t, "approval-reply", logs.Find("trigger event skipped: undeliverable")[0].Attrs["binding"])
}

func TestDexTriggerTargetsRejectEmptyFlowIDAsUndeliverable(t *testing.T) {
	flow := &filterTestFlow{}
	accept := func(sdkgo.TriggerEvent[string]) bool { return true }
	emptyFlowID := func(sdkgo.TriggerEvent[string]) string { return "" }
	event := sdkgo.TriggerEvent[string]{ID: "Ev1", Payload: "approve"}

	flowTarget := sdkgo.NewDexFlowTriggerTarget(&dex.Client{}, flow, accept, emptyFlowID,
		func(sdkgo.TriggerEvent[string]) string { return "input" })
	err := flowTarget.HandleTrigger(context.Background(), event)
	require.ErrorContains(t, err, "requires Flow ID and event ID")
	require.True(t, sdkgo.IsTriggerUndeliverable(err))

	rpcTarget := sdkgo.NewDexRPCTriggerTarget(&dex.Client{}, flow.ReceiveEvent, accept, emptyFlowID,
		func(sdkgo.TriggerEvent[string]) int { return 42 })
	err = rpcTarget.HandleTrigger(context.Background(), event)
	require.ErrorContains(t, err, "requires Flow ID and event ID")
	require.True(t, sdkgo.IsTriggerUndeliverable(err))
}

type unencodableStartInput struct {
	Callback func() `json:"callback"`
}

type unencodableStartStep struct {
	dex.StepDefaultsNoWaitFor[unencodableStartInput]
}

func (unencodableStartStep) Execute(dex.Context, unencodableStartInput) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

type unencodableStartFlow struct {
	dex.FlowDefaults
}

func (*unencodableStartFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(unencodableStartStep{})}
}

func (*unencodableStartFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// newUnreachableDexClient returns a client for a Unix socket that nothing listens on, so no call reaches Dex.
func newUnreachableDexClient(t *testing.T, flows ...dex.Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 1 << 20})
	require.NoError(t, err)
	missingSocket := filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: "unix://" + missingSocket})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}

func TestDexFlowTriggerTargetMarksUnencodableInputUndeliverable(t *testing.T) {
	flow := &unencodableStartFlow{}
	target := sdkgo.NewDexFlowTriggerTarget(newUnreachableDexClient(t, flow), flow,
		func(sdkgo.TriggerEvent[string]) bool { return true },
		func(sdkgo.TriggerEvent[string]) string { return "unencodable-flow" },
		func(sdkgo.TriggerEvent[string]) unencodableStartInput {
			return unencodableStartInput{Callback: func() {}}
		})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := target.HandleTrigger(ctx, sdkgo.TriggerEvent[string]{ID: "Ev1"})
	require.True(t, sdkgo.IsTriggerUndeliverable(err), "Flow target error: %v", err)
	var mappingFailure *dex.ValueMappingError
	require.ErrorAs(t, err, &mappingFailure)
	require.Equal(t, "encode", mappingFailure.Operation)
}

type deliveryTestFlow struct {
	dex.FlowDefaults
}

func (*deliveryTestFlow) GetSteps() []dex.StepDef { return nil }

func (flow *deliveryTestFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.ReceiveApproval, nil)}
}

func (*deliveryTestFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (*deliveryTestFlow) ReceiveApproval(_ dex.Context, input string) (*dex.RPCResult[string], error) {
	return &dex.RPCResult[string]{Output: input}, nil
}

func TestDexRPCTriggerTargetKeepsDexOutageRetryable(t *testing.T) {
	flow := &deliveryTestFlow{}
	target := sdkgo.NewDexRPCTriggerTarget(newUnreachableDexClient(t, flow), flow.ReceiveApproval,
		func(sdkgo.TriggerEvent[string]) bool { return true },
		func(sdkgo.TriggerEvent[string]) string { return "approval-flow" },
		func(event sdkgo.TriggerEvent[string]) string { return event.Payload },
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := target.HandleTrigger(ctx, sdkgo.TriggerEvent[string]{ID: "Ev1", Payload: "approve"})
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "Dex outage must fail the attempt before the test deadline")
	require.False(t, sdkgo.IsTriggerUndeliverable(err))
	var serviceError *dex.ServiceError
	require.ErrorAs(t, err, &serviceError)
	require.Equal(t, codes.Unavailable, serviceError.Code)
}

// pollingTriggerSource retries on its own schedule, as the Gmail poller does: each call to Run makes one
// attempt per event and reports recovery only when the attempt did not skip the event.
type pollingTriggerSource struct {
	event    sdkgo.TriggerEvent[string]
	attempts int
	logger   *slog.Logger
}

func (source *pollingTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[string]) error {
	for ; source.attempts < 3; source.attempts++ {
		attemptCtx, skipped := sdkgo.TriggerAttempt(ctx)
		if err := target.HandleTrigger(attemptCtx, source.event); err != nil {
			continue
		}
		if source.attempts > 0 && !skipped() {
			source.logger.InfoContext(ctx, "trigger delivered after retry")
		}
		return nil
	}
	return errors.New("attempts exhausted")
}

// TestTriggerAttemptReportsRunnerSkip covers a NewTrigger runner around a poller: the runner logs the
// undeliverable event it consumes, and TriggerAttempt keeps the poller from reporting it as delivered.
func TestTriggerAttemptReportsRunnerSkip(t *testing.T) {
	definition := sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "gmail", TriggerName: "replyReceived"},
		Description: "Receive a matching reply.",
	}
	binding := sdkgo.TriggerBindingRef{
		Connection: sdkgo.ConnectionRef{Provider: "google", Name: "inbox"}, Trigger: definition.Trigger, Name: "approval-reply",
	}
	for _, testCase := range []struct {
		name      string
		second    error
		messages  []string
		skipAttrs map[string]string
	}{
		{
			name:     "skip after a failed attempt",
			second:   sdkgo.MarkTriggerUndeliverable(errors.New("workflow execution already completed")),
			messages: []string{"trigger event skipped: undeliverable"},
			skipAttrs: map[string]string{
				"connector": "gmail", "connection": "inbox", "trigger": "replyReceived", "binding": "approval-reply",
				"event_id": "EvLate", "error": "Trigger event is undeliverable: workflow execution already completed",
			},
		},
		{name: "delivery after a failed attempt", messages: []string{"trigger delivered after retry"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			logs := testsupport.NewLogRecorder()
			var attempts atomic.Int32
			runner, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[string]{
				Definition: definition, Binding: binding,
				Source: &pollingTriggerSource{event: sdkgo.TriggerEvent[string]{ID: "EvLate"}, logger: logs.Logger()},
				Target: sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
					if attempts.Add(1) == 1 {
						return errors.New("Dex is unavailable")
					}
					return testCase.second
				}),
				Logger: logs.Logger(),
			})
			require.NoError(t, err)
			require.NoError(t, runner.Run(context.Background()))
			messages := []string{}
			for _, record := range logs.Records() {
				messages = append(messages, record.Message)
			}
			require.Equal(t, testCase.messages, messages)
			if testCase.skipAttrs != nil {
				require.Equal(t, testCase.skipAttrs, logs.Records()[0].Attrs)
			}
		})
	}
}

// TestDeliverTriggerRecordsCarryContextAttrsAndDropURLQueries covers event-scoped context attributes, a
// record's own attribute winning over a context attribute with the same key, and a target error that
// embeds a keyed webhook URL.
func TestDeliverTriggerRecordsCarryContextAttrsAndDropURLQueries(t *testing.T) {
	logs := testsupport.NewLogRecorder()
	ctx := sdkgo.ContextWithTriggerLogAttrs(context.Background(), slog.String("channel", "C1"))
	ctx = sdkgo.ContextWithTriggerLogAttrs(ctx, slog.String("thread_ts", "1.0"), slog.String("event_id", "context-value"))
	var attempts atomic.Int32
	webhookFailure := fmt.Errorf("notify approver: %w", &url.Error{
		Op: "Post", URL: "https://hooks.example.test/notify?key=SENTINEL-WEBHOOK-KEY", Err: errors.New("connection refused"),
	})
	err := sdkgo.DeliverTrigger(ctx, sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
		if attempts.Add(1) == 1 {
			return webhookFailure
		}
		return nil
	}), sdkgo.TriggerEvent[string]{ID: "Ev1", Payload: "SENTINEL-MESSAGE-TEXT"}, sdkgo.WithTriggerLogger(logs.Logger()))
	require.NoError(t, err)
	retries := logs.Find("trigger delivery failed; retrying")
	require.Len(t, retries, 1)
	require.Equal(t, map[string]string{
		"channel": "C1", "thread_ts": "1.0", "event_id": "Ev1", "attempt": "1", "delay": "250ms",
		"error": `notify approver: Post "https://hooks.example.test/notify": connection refused`,
	}, retries[0].Attrs)
	require.Equal(t, map[string]string{"channel": "C1", "thread_ts": "1.0", "event_id": "Ev1", "attempts": "2"},
		logs.Find("trigger delivered after retry")[0].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// TestDeliverTriggerDropsURLQueriesAcrossJoinedErrors covers a keyed URL inside errors.Join and inside a
// multi-%w error, which a single errors.Unwrap chain never reaches.
func TestDeliverTriggerDropsURLQueriesAcrossJoinedErrors(t *testing.T) {
	keyed := func() error {
		return &url.Error{Op: "Get", URL: "https://api.example.test/list?q=from:boss&key=SENTINEL-API-KEY", Err: errors.New("connection refused")}
	}
	for name, failure := range map[string]error{
		"errors.Join":  errors.Join(errors.New("first"), keyed()),
		"multiple %w":  fmt.Errorf("a: %w; b: %w", errors.New("first"), keyed()),
		"nested joins": fmt.Errorf("outer: %w", errors.Join(errors.New("first"), fmt.Errorf("inner: %w", keyed()))),
	} {
		t.Run(name, func(t *testing.T) {
			logs := testsupport.NewLogRecorder()
			var attempts atomic.Int32
			err := sdkgo.DeliverTrigger(context.Background(), sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error {
				if attempts.Add(1) == 1 {
					return failure
				}
				return nil
			}), sdkgo.TriggerEvent[string]{ID: "Ev1"}, sdkgo.WithTriggerLogger(logs.Logger()))
			require.NoError(t, err)
			retries := logs.Find("trigger delivery failed; retrying")
			require.Len(t, retries, 1)
			require.Contains(t, retries[0].Attrs["error"], `Get "https://api.example.test/list": connection refused`)
			require.NotContains(t, logs.Text(), "SENTINEL")
		})
	}
}

// TestDeliverTriggerRecordsReportTheirCallerAsSource checks that a handler with AddSource sees the SDK
// function that logged a record, not the shared logging helper.
func TestDeliverTriggerRecordsReportTheirCallerAsSource(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{AddSource: true}))
	require.NoError(t, sdkgo.DeliverTrigger(context.Background(), sdkgo.TriggerTargetFunc[string](
		func(context.Context, sdkgo.TriggerEvent[string]) error {
			return sdkgo.MarkTriggerUndeliverable(errors.New("thread closed"))
		},
	), sdkgo.TriggerEvent[string]{ID: "Ev1"}, sdkgo.WithTriggerLogger(logger)))
	require.Contains(t, output.String(), "sdkgo/trigger_delivery.go:")
	require.NotContains(t, output.String(), "triggerlog")
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
)

type rpcTargetTestFlow struct {
	dex.FlowDefaults
}

func (*rpcTargetTestFlow) GetSteps() []dex.StepDef { return nil }

func (*rpcTargetTestFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (*rpcTargetTestFlow) ReceiveReply(_ dex.Context, input string) (*dex.RPCResult[string], error) {
	return &dex.RPCResult[string]{Output: input}, nil
}

// recordedRPCInvocation is one InvokeRPCWithOptions call a recordingRPCInvoker received.
type recordedRPCInvocation struct {
	flowID    string
	input     any
	requestID *string
}

// recordingRPCInvoker records every call and answers each with the next scripted error, then nil.
type recordingRPCInvoker struct {
	invocations []recordedRPCInvocation
	errs        []error
}

func (invoker *recordingRPCInvoker) InvokeRPCWithOptions(_ context.Context, flowID string, _ any, input any, _ any, options dex.RPCInvokeOptions) error {
	invoker.invocations = append(invoker.invocations, recordedRPCInvocation{flowID: flowID, input: input, requestID: options.RequestID})
	if len(invoker.errs) == 0 {
		return nil
	}
	err := invoker.errs[0]
	invoker.errs = invoker.errs[1:]
	return err
}

func (invoker *recordingRPCInvoker) requestIDs() []string {
	requestIDs := make([]string, 0, len(invoker.invocations))
	for _, invocation := range invoker.invocations {
		if invocation.requestID == nil {
			requestIDs = append(requestIDs, "<none>")
			continue
		}
		requestIDs = append(requestIDs, *invocation.requestID)
	}
	return requestIDs
}

func newRecordedRPCTarget(invoker *recordingRPCInvoker, rpc dex.RPC[string, string], logs *testsupport.LogRecorder, options ...TriggerOption) TriggerTarget[string] {
	return newDexRPCTriggerTarget[string](invoker, rpc,
		func(TriggerEvent[string]) bool { return true },
		func(TriggerEvent[string]) string { return "review-flow" },
		func(event TriggerEvent[string]) string { return event.Payload },
		append([]TriggerOption{WithTriggerLogger(logs.Logger())}, options...)...)
}

func mustTriggerRPCRequestID(t *testing.T, rpcName string, eventID string) string {
	t.Helper()
	requestID, err := TriggerRPCRequestID(rpcName, eventID)
	require.NoError(t, err)
	return requestID
}

func TestDexRPCTriggerTargetRepeatsOneRequestIDForEveryDeliveryOfAnEvent(t *testing.T) {
	flow := &rpcTargetTestFlow{}
	invoker := &recordingRPCInvoker{errs: []error{&dex.ServiceError{Op: "InvokeRPC", FlowID: "review-flow", Code: codes.Unavailable, Detail: "connection reset"}}}
	logs := testsupport.NewLogRecorder()
	target := newRecordedRPCTarget(invoker, flow.ReceiveReply, logs)
	event := TriggerEvent[string]{ID: "Ev1", Payload: "approve"}

	err := target.HandleTrigger(context.Background(), event)
	require.Error(t, err)
	require.False(t, IsTriggerUndeliverable(err))
	require.NoError(t, target.HandleTrigger(context.Background(), event))
	require.NoError(t, target.HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev2", Payload: "approve"}))

	first := mustTriggerRPCRequestID(t, "ReceiveReply", "Ev1")
	require.Equal(t, []string{first, first, mustTriggerRPCRequestID(t, "ReceiveReply", "Ev2")}, invoker.requestIDs())
	require.Equal(t, "review-flow", invoker.invocations[0].flowID)
	require.Equal(t, "approve", invoker.invocations[0].input)
	delivered := logs.Find("trigger event delivered")
	require.Len(t, delivered, 2)
	require.Equal(t, slog.LevelDebug, delivered[0].Level)
	require.Equal(t, map[string]string{"target": "rpc", "event_id": "Ev1", "flow_id": "review-flow", "request_id": first}, delivered[0].Attrs)
}

func TestDexRPCTriggerTargetDerivesRequestIDFromConfiguredRPCName(t *testing.T) {
	flow := &rpcTargetTestFlow{}
	invoker := &recordingRPCInvoker{}
	target := newRecordedRPCTarget(invoker, flow.ReceiveReply, testsupport.NewLogRecorder(), WithTriggerRPCName("send_user_message"))

	require.NoError(t, target.HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev1", Payload: "approve"}))
	require.Equal(t, []string{"trigger-rpc-nvt7sgyIgs8au6Su_i3G_A"}, invoker.requestIDs())
}

func TestDexRPCTriggerTargetSendsNoRequestIDWithoutAnRPCName(t *testing.T) {
	invoker := &recordingRPCInvoker{}
	logs := testsupport.NewLogRecorder()
	unboundRPC := func(_ dex.Context, input string) (*dex.RPCResult[string], error) {
		return &dex.RPCResult[string]{Output: input}, nil
	}
	target := newRecordedRPCTarget(invoker, unboundRPC, logs)

	require.NoError(t, target.HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev1", Payload: "approve"}))
	require.Equal(t, []string{"<none>"}, invoker.requestIDs())
	require.Equal(t, map[string]string{"target": "rpc", "event_id": "Ev1", "flow_id": "review-flow"}, logs.Find("trigger event delivered")[0].Attrs)
}

func TestDexRPCTriggerTargetDropsARequestIDConflictAsUndeliverable(t *testing.T) {
	flow := &rpcTargetTestFlow{}
	requestID := mustTriggerRPCRequestID(t, "ReceiveReply", "Ev1")
	conflict := &dex.ServiceError{
		Op: "InvokeRPC", FlowID: "review-flow", Code: codes.InvalidArgument,
		Detail: "request ID " + requestID + " was already used for a different request",
	}
	invoker := &recordingRPCInvoker{errs: []error{conflict}}
	logs := testsupport.NewLogRecorder()
	target := newRecordedRPCTarget(invoker, flow.ReceiveReply, logs)

	err := target.HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev1", Payload: "SENTINEL-MESSAGE-TEXT"})
	require.True(t, IsTriggerUndeliverable(err))
	var conflictError *TriggerRPCRequestConflictError
	require.ErrorAs(t, err, &conflictError)
	require.Equal(t, &TriggerRPCRequestConflictError{
		EventID: "Ev1", FlowID: "review-flow", RPCName: "ReceiveReply", RequestID: requestID, Err: conflict,
	}, conflictError)
	require.Same(t, conflict, errors.Unwrap(conflictError))
	require.Equal(t, "Trigger event is undeliverable: RPC trigger event Ev1 for RPC ReceiveReply on Flow review-flow mapped to a different RPC "+
		"request than an earlier attempt with request ID "+requestID+" (non-deterministic or changed input mapper, another binding, "+
		"changed RPC options, or Dex payload encryption on a Dex Server before 0.1.37); the event was dropped", err.Error())
	records := logs.Records()
	require.Len(t, records, 1)
	require.Equal(t, slog.LevelError, records[0].Level)
	require.Equal(t, "trigger rpc request conflict; event dropped", records[0].Message)
	require.Equal(t, map[string]string{
		"target": "rpc", "event_id": "Ev1", "request_id": requestID, "rpc_name": "ReceiveReply", "flow_id": "review-flow",
	}, records[0].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

func TestDexRPCTriggerTargetKeepsOtherInvalidArgumentRetryable(t *testing.T) {
	flow := &rpcTargetTestFlow{}
	otherRequestID := mustTriggerRPCRequestID(t, "ReceiveReply", "Ev2")
	testCases := []struct {
		name string
		err  error
	}{
		{"another request ID", &dex.ServiceError{Code: codes.InvalidArgument, Detail: "request ID " + otherRequestID + " was already used for a different request"}},
		{"another invalid argument", &dex.ServiceError{Code: codes.InvalidArgument, Detail: "heartbeat timeout is below the server minimum"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			invoker := &recordingRPCInvoker{errs: []error{testCase.err}}
			logs := testsupport.NewLogRecorder()
			err := newRecordedRPCTarget(invoker, flow.ReceiveReply, logs).HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev1", Payload: "approve"})
			require.Same(t, testCase.err, err)
			require.False(t, IsTriggerUndeliverable(err))
			require.Empty(t, logs.Records())
		})
	}
}

func TestDexRPCTriggerTargetConsumesUndecodableResponseWithRequestID(t *testing.T) {
	flow := &rpcTargetTestFlow{}
	invoker := &recordingRPCInvoker{errs: []error{&dex.ValueMappingError{Operation: "decode", Err: errors.New("bad output")}}}
	logs := testsupport.NewLogRecorder()

	require.NoError(t, newRecordedRPCTarget(invoker, flow.ReceiveReply, logs).HandleTrigger(context.Background(), TriggerEvent[string]{ID: "Ev1", Payload: "approve"}))
	warnings := logs.Find("trigger event delivered; rpc response undecodable")
	require.Len(t, warnings, 1)
	require.Equal(t, mustTriggerRPCRequestID(t, "ReceiveReply", "Ev1"), warnings[0].Attrs["request_id"])
}

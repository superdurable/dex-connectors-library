//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	durabilityProbeFlowType      = "OutlookMailSendDurabilityProbe"
	durabilityRetryProbeFlowType = "OutlookMailSendRetryProbe"
)

// TestSyncSendMessageSendsOnceWithRealDex checks the default durability's send count.
func TestSyncSendMessageSendsOnceWithRealDex(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	connection, err := outlookmail.NewConnection(client, outlookConnection)
	require.NoError(t, err)
	result := runDurabilityProbe(t, &durabilityProbeFlow{connection: connection})
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var branch sdkgo.BranchID
	require.NoError(t, result.DecodeSingleOutput(&branch))
	require.Equal(t, outlookmail.SendMessageBranchSent, branch)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft))
	require.Len(t, fake.Deliveries(), 1)
}

// TestAsyncRetryAfterAcceptedSendDoesNotResendWithRealDex forces a second Execute after Graph accepts the send.
func TestAsyncRetryAfterAcceptedSendDoesNotResendWithRealDex(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	connection, err := outlookmail.NewConnection(client, outlookConnection)
	require.NoError(t, err)
	flow := &durabilityProbeFlow{connection: connection, retryClient: client}
	result := runDurabilityProbe(t, flow)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome sendRetryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, outlookmail.SendMessageBranchSent, outcome.Branch)
	require.True(t, outcome.WasAlreadySent)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft))
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Len(t, fake.Deliveries(), 1)
}

// durabilityProbeFlow sends one message with optional Step options.
type durabilityProbeFlow struct {
	dex.FlowDefaults
	connection  outlookmail.Connection
	retryClient *outlookmail.Client
}

func (flow *durabilityProbeFlow) GetFlowType() string {
	if flow.retryClient != nil {
		return durabilityRetryProbeFlowType
	}
	return durabilityProbeFlowType
}

func (flow *durabilityProbeFlow) GetSteps() []dex.StepDef {
	if flow.retryClient != nil {
		return []dex.StepDef{dex.DefineStartStep(sendThenFailFirstAttempt{client: flow.retryClient})}
	}
	return []dex.StepDef{
		dex.DefineStartStep(startDurabilityProbe{}),
		dex.DefineStep(outlookmail.NewSendMessageStep(outlookmail.SendMessageStepConfig[outlookmail.SendMessageInput]{
			StepType: "SendProbeMessage", Connection: flow.connection, ConnectionName: outlookConnection.Name,
			Annotations:         sdkgo.StepAnnotations{GroupID: "outlook-mail", GroupLabel: "Outlook Mail", Explanation: "Send one probe message."},
			MapToOperationInput: func(input outlookmail.SendMessageInput) outlookmail.SendMessageInput { return input },
			Sent:                sdkgo.GoTo(completeDurabilityProbe{}),
			Uncertain:           sdkgo.GoTo(completeDurabilityProbe{}),
		})),
		dex.DefineStep(completeDurabilityProbe{}),
	}
}

type sendRetryOutcome struct {
	Branch         sdkgo.BranchID `json:"branch"`
	WasAlreadySent bool           `json:"wasAlreadySent"`
}

// sendThenFailFirstAttempt retries after an accepted send with the same Dex Call ID.
type sendThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[outlookmail.SendMessageInput]
	client *outlookmail.Client
}

func (sendThenFailFirstAttempt) GetStepType() string { return "SendThenFailFirstAttempt" }

func (sendThenFailFirstAttempt) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync, ExecuteRetry: &dex.RetryPolicy{
		InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second, MaximumAttempts: 3, TotalDuration: time.Minute,
	}}
}

func (step sendThenFailFirstAttempt) Execute(ctx dex.Context, input outlookmail.SendMessageInput) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.SendMessage(), outlookConnection, input)
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("the probe fails after Graph accepts the first send")
	}
	return dex.GracefulComplete(sendRetryOutcome{Branch: result.Branch, WasAlreadySent: result.Value.WasAlreadySent}), nil
}

func (*durabilityProbeFlow) GetRPCs() []dex.RPCDef { return nil }

func (*durabilityProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type startDurabilityProbe struct {
	dex.StepDefaults
}

func (startDurabilityProbe) GetStepType() string { return "StartDurabilityProbe" }

func (startDurabilityProbe) WaitFor(dex.Context, outlookmail.SendMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startDurabilityProbe) Execute(_ dex.Context, input outlookmail.SendMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[outlookmail.SendMessageInput]("SendProbeMessage"), input), nil
}

type completeDurabilityProbe struct {
	dex.StepDefaultsNoWaitFor[outlookmail.SendMessageResult]
}

func (completeDurabilityProbe) GetStepType() string { return "CompleteDurabilityProbe" }

func (completeDurabilityProbe) Execute(_ dex.Context, result outlookmail.SendMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Branch), nil
}

// runDurabilityProbe runs flow on a real Worker against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func runDurabilityProbe(t *testing.T, flow *durabilityProbeFlow) dex.FlowResult {
	t.Helper()
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	flowID := "outlook-mail-durability-probe-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, validSendMessageInput(), dex.StartFlowOptions{})
	require.NoError(t, err)
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}

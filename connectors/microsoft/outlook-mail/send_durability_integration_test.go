//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	durabilityProbeFlowType = "OutlookMailSendDurabilityProbe"
	// probeSendDelay outlasts Dex's roughly seven-second async local phase.
	probeSendDelay = 9 * time.Second
)

// TestSyncSendMessageSendsOnceToASlowGraphWithRealDex holds Graph's send action for nine seconds; the sync
// default keeps Dex from dispatching a second attempt, so the draft is sent once.
func TestSyncSendMessageSendsOnceToASlowGraphWithRealDex(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Delay: probeSendDelay})
	client, _ := delegatedClient(t, fake)
	connection, err := outlookmail.NewConnection(client, outlookConnection)
	require.NoError(t, err)
	result := runDurabilityProbe(t, &durabilityProbeFlow{connection: connection})
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var branch sdkgo.BranchID
	require.NoError(t, result.DecodeSingleOutput(&branch))
	require.Equal(t, outlookmail.SendMessageBranchSent, branch)
	fake.WaitForHeldRequests()
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft))
	require.Len(t, fake.Deliveries(), 1)
}

// TestAsyncOverrideOfSendMessageSendsTwiceToASlowGraphWithRealDex is the contrast for the sync default:
// overriding sendMessage to async durability lets Dex dispatch a second attempt while Graph still holds the
// first send. The local phase's dispatch checkpoint is invisible to that attempt, which finds the marked
// draft and sends it again, so the fake, like any server without a send idempotency key, delivers twice.
func TestAsyncOverrideOfSendMessageSendsTwiceToASlowGraphWithRealDex(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Delay: probeSendDelay})
	client, _ := delegatedClient(t, fake)
	connection, err := outlookmail.NewConnection(client, outlookConnection)
	require.NoError(t, err)
	flow := &durabilityProbeFlow{connection: connection, stepOptions: &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync}}
	result := runDurabilityProbe(t, flow)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	fake.WaitForHeldRequests()
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft), "the marker kept the second attempt on the first attempt's draft")
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointSendDraft), "async durability dispatched the send again past the local phase")
	require.Len(t, fake.Deliveries(), 2)
}

// durabilityProbeFlow sends one message with optional Step options.
type durabilityProbeFlow struct {
	dex.FlowDefaults
	connection  outlookmail.Connection
	stepOptions *dex.StepOptions
}

func (*durabilityProbeFlow) GetFlowType() string { return durabilityProbeFlowType }

func (flow *durabilityProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDurabilityProbe{}),
		dex.DefineStep(outlookmail.NewSendMessageStep(outlookmail.SendMessageStepConfig[outlookmail.SendMessageInput]{
			StepType: "SendProbeMessage", Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "outlook-mail", GroupLabel: "Outlook Mail", Explanation: "Send one probe message."},
			MapToOperationInput: func(input outlookmail.SendMessageInput) outlookmail.SendMessageInput { return input },
			Sent:                sdkgo.GoTo(completeDurabilityProbe{}),
			Uncertain:           sdkgo.GoTo(completeDurabilityProbe{}),
			StepOptionsOverride: flow.stepOptions,
		})),
		dex.DefineStep(completeDurabilityProbe{}),
	}
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress}})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
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

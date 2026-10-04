//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

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
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	durabilityProbeFlowType = "EmailSubmissionDurabilityProbe"
	// probeEndOfDataDelay outlasts Dex's roughly seven-second async local phase.
	probeEndOfDataDelay = 9 * time.Second
)

// TestAsyncOverrideOfSendMessageSubmitsTwiceToASlowServerWithRealDex is the contrast for the sync default:
// overriding sendMessage to async durability lets Dex dispatch a second attempt while a slow server still
// holds the first, and the local phase's heartbeat checkpoint is invisible to that attempt, so the server
// receives the message twice. The example's slow-server tests prove the sync default submits once.
func TestAsyncOverrideOfSendMessageSubmitsTwiceToASlowServerWithRealDex(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.HoldNextEndOfDataReply(probeEndOfDataDelay)
	connection, err := email.NewConnection(fixture.client(t), emailConnection)
	require.NoError(t, err)
	flow := &durabilityProbeFlow{connection: connection, stepOptions: &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync}}
	result := runDurabilityProbe(t, flow)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	fixture.smtp.WaitForHeldReplies()
	require.Equal(t, 2, fixture.smtp.DataCount(), "async durability dispatched the submission again past the local phase")
	require.Len(t, fixture.smtp.Submissions(), 2)
}

// durabilityProbeFlow sends one message with optional Step options.
type durabilityProbeFlow struct {
	dex.FlowDefaults
	connection  email.Connection
	stepOptions *dex.StepOptions
}

func (*durabilityProbeFlow) GetFlowType() string { return durabilityProbeFlowType }

func (flow *durabilityProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDurabilityProbe{}),
		dex.DefineStep(email.NewSendMessageStep(email.SendMessageStepConfig[email.SendMessageInput]{
			StepType: "SendProbeMessage", Connection: flow.connection, ConnectionName: emailConnection.Name,
			Annotations:         sdkgo.StepAnnotations{GroupID: "email", GroupLabel: "Email", Explanation: "Send one probe message."},
			MapToOperationInput: func(input email.SendMessageInput) email.SendMessageInput { return input },
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

func (startDurabilityProbe) WaitFor(dex.Context, email.SendMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startDurabilityProbe) Execute(_ dex.Context, input email.SendMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[email.SendMessageInput]("SendProbeMessage"), input), nil
}

type completeDurabilityProbe struct {
	dex.StepDefaultsNoWaitFor[email.SendMessageResult]
}

func (completeDurabilityProbe) GetStepType() string { return "CompleteDurabilityProbe" }

func (completeDurabilityProbe) Execute(_ dex.Context, result email.SendMessageResult) (*dex.StepDecision, error) {
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
	flowID := "email-durability-probe-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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

//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"context"
	"errors"
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
	durabilityProbeFlowType      = "EmailSubmissionDurabilityProbe"
	durabilityRetryProbeFlowType = "EmailSubmissionRetryProbe"
)

// TestAsyncRetryAfterAcceptedSubmissionDoesNotResubmitWithRealDex forces a second Execute after SMTP accepts the message.
func TestAsyncRetryAfterAcceptedSubmissionDoesNotResubmitWithRealDex(t *testing.T) {
	fixture := newMailFixture(t)
	client := fixture.client(t)
	connection, err := email.NewConnection(client, emailConnection)
	require.NoError(t, err)
	flow := &durabilityProbeFlow{connection: connection, retryClient: client}
	result := runDurabilityProbe(t, flow)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome submissionRetryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, email.SendMessageBranchUncertain, outcome.Branch)
	require.Equal(t, 1, fixture.smtp.DataCount())
	submissions := fixture.smtp.Submissions()
	require.Len(t, submissions, 1)
	header, _ := readSubmittedMessage(t, submissions[0].Data)
	messageID, err := header.MessageID()
	require.NoError(t, err)
	require.Equal(t, outcome.MessageID, messageID)
}

// durabilityProbeFlow sends one message with optional Step options.
type durabilityProbeFlow struct {
	dex.FlowDefaults
	connection  email.Connection
	retryClient *email.Client
}

func (flow *durabilityProbeFlow) GetFlowType() string {
	if flow.retryClient != nil {
		return durabilityRetryProbeFlowType
	}
	return durabilityProbeFlowType
}

func (flow *durabilityProbeFlow) GetSteps() []dex.StepDef {
	if flow.retryClient != nil {
		return []dex.StepDef{dex.DefineStartStep(submitThenFailFirstAttempt{client: flow.retryClient})}
	}
	return []dex.StepDef{
		dex.DefineStartStep(startDurabilityProbe{}),
		dex.DefineStep(email.NewSendMessageStep(email.SendMessageStepConfig[email.SendMessageInput]{
			StepType: "SendProbeMessage", Connection: flow.connection, ConnectionName: emailConnection.Name,
			Annotations:         sdkgo.StepAnnotations{GroupID: "email", GroupLabel: "Email", Explanation: "Send one probe message."},
			MapToOperationInput: func(input email.SendMessageInput) email.SendMessageInput { return input },
			Sent:                sdkgo.GoTo(completeDurabilityProbe{}),
			Uncertain:           sdkgo.GoTo(completeDurabilityProbe{}),
		})),
		dex.DefineStep(completeDurabilityProbe{}),
	}
}

type submissionRetryOutcome struct {
	Branch    sdkgo.BranchID `json:"branch"`
	MessageID string         `json:"messageId"`
}

// submitThenFailFirstAttempt retries after an accepted message with the same Dex Call ID.
type submitThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[email.SendMessageInput]
	client *email.Client
}

func (submitThenFailFirstAttempt) GetStepType() string { return "SubmitThenFailFirstAttempt" }

func (submitThenFailFirstAttempt) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync, ExecuteRetry: &dex.RetryPolicy{
		InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second, MaximumAttempts: 3, TotalDuration: time.Minute,
	}}
}

func (step submitThenFailFirstAttempt) Execute(ctx dex.Context, input email.SendMessageInput) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.SendMessage(), emailConnection, input)
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("the probe fails after SMTP accepts the first message")
	}
	return dex.GracefulComplete(submissionRetryOutcome{Branch: result.Branch, MessageID: result.Value.MessageID}), nil
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

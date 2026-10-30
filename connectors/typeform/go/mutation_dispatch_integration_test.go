//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const upsertProbeFlowType = "TypeformUpsertWebhookProbe"

// upsertOutcome is what the probe Flow completes with.
type upsertOutcome struct {
	Branch      sdkgo.BranchID    `json:"branch"`
	WebhookID   string            `json:"webhookId,omitempty"`
	IsEnabled   bool              `json:"isEnabled"`
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// TestUpsertWebhookLeavesOneWebhookUnderDuplicateDispatchWithRealDex retries after the first accepted PUT.
func TestUpsertWebhookLeavesOneWebhookUnderDuplicateDispatchWithRealDex(t *testing.T) {
	store := newFakeWebhooks()
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT " + testWebhookPath: store.upsertRoute(testFormID, testWebhookTag)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
	flow := &probeFlow{steps: []dex.StepDef{
		dex.DefineStartStep(upsertThenFailFirstAttempt{client: client}),
	}}
	dexClient := startProbeWorker(t, flow)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := fmt.Sprintf("typeform-probe-upsert-%d", time.Now().UnixNano())
	_, err := dexClient.StartFlow(ctx, flow, flowID, "probe", dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	result, err := dexClient.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome upsertOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))

	require.Equal(t, typeform.UpsertWebhookBranchUpserted, outcome.Branch)
	require.True(t, outcome.IsEnabled)
	puts := fake.requestsTo(http.MethodPut, testWebhookPath)
	require.Len(t, puts, 2, "the failed first Execute is retried once")
	for _, put := range puts {
		require.Equal(t, puts[0].body, put.body, "every dispatch sends the same idempotent body")
	}
	require.Equal(t, 1, store.count(), "the same tag leaves one webhook")
}

// probeFlow runs one connector Mutation as its start Step and completes with its outcome.
type probeFlow struct {
	dex.FlowDefaults
	steps []dex.StepDef
}

func (*probeFlow) GetFlowType() string          { return upsertProbeFlowType }
func (flow *probeFlow) GetSteps() []dex.StepDef { return flow.steps }

// GetPersistenceSchema registers nothing: the probe returns its outcome as the Flow output.
func (*probeFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetRPCs registers no RPCs.
func (*probeFlow) GetRPCs() []dex.RPCDef { return nil }

type upsertThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[string]
	client *typeform.Client
}

func (upsertThenFailFirstAttempt) GetStepType() string { return "UpsertThenFailFirstAttempt" }

func (upsertThenFailFirstAttempt) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{
		InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second,
		MaximumAttempts: 3, TotalDuration: time.Minute,
	}}
}

func (step upsertThenFailFirstAttempt) Execute(ctx dex.Context, _ string) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.UpsertWebhook(), testConnection,
		typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL})
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("the probe fails after the first accepted webhook PUT")
	}
	outcome := upsertOutcome{Branch: result.Branch, WebhookID: result.Value.ID, IsEnabled: result.Value.IsEnabled}
	if result.Failure != nil {
		outcome.FailureKind = result.Failure.Kind
	}
	return dex.GracefulComplete(outcome), nil
}

// startProbeWorker runs one Worker for flow until the test ends and returns its Client.
func startProbeWorker(t *testing.T, flow dex.Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: dexServiceAddress()})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexServiceAddress()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), client.Close(), cache.Close()))
		<-workerResult
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	return client
}

func dexServiceAddress() string {
	if address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS"); address != "" {
		return address
	}
	return "127.0.0.1:8801"
}

//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowProviderDelay outlasts Dex's seven-second local async phase, so an async Step is re-dispatched.
const slowProviderDelay = 9 * time.Second

const upsertProbeFlowType = "TypeformUpsertWebhookProbe"

// upsertOutcome is what the probe Flow completes with.
type upsertOutcome struct {
	Branch      sdkgo.BranchID    `json:"branch"`
	WebhookID   string            `json:"webhookId,omitempty"`
	IsEnabled   bool              `json:"isEnabled"`
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// TestUpsertWebhookLeavesOneWebhookUnderDuplicateDispatchWithRealDex lets Dex re-dispatch the async Step
// while Typeform is slow: both attempts PUT the same body to the same tag, and one webhook remains.
func TestUpsertWebhookLeavesOneWebhookUnderDuplicateDispatchWithRealDex(t *testing.T) {
	store := newFakeWebhooks()
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT " + testWebhookPath: storeThenDelay(store.upsertRoute(testFormID, testWebhookTag))})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
	connection, err := typeform.NewConnection(client, testConnection)
	require.NoError(t, err)
	flow := &probeFlow{steps: []dex.StepDef{
		dex.DefineStartStep(typeform.NewUpsertWebhookStep(typeform.UpsertWebhookStepConfig[string]{
			StepType: "RegisterWebhook", Connection: connection, ConnectionName: testConnection.Name,
			Annotations: sdkgo.StepAnnotations{GroupID: "probe", GroupLabel: "Probe", Explanation: "Upsert one webhook against the slow fake Typeform."},
			MapToOperationInput: func(string) typeform.UpsertWebhookInput {
				return typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL}
			},
			Upserted: sdkgo.GoTo(recordUpsertOutcome{}), NotFound: sdkgo.GoTo(recordUpsertOutcome{}),
			ProviderRejected: sdkgo.GoTo(recordUpsertOutcome{}), InvalidResponse: sdkgo.GoTo(recordUpsertOutcome{}),
			Defect: sdkgo.GoTo(recordUpsertOutcome{}),
		})),
		dex.DefineStep(recordUpsertOutcome{}),
	}}
	dexClient := startProbeWorker(t, flow)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := fmt.Sprintf("typeform-probe-upsert-%d", time.Now().UnixNano())
	_, err = dexClient.StartFlow(ctx, flow, flowID, "probe", dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	result, err := dexClient.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome upsertOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))

	require.Equal(t, typeform.UpsertWebhookBranchUpserted, outcome.Branch)
	require.True(t, outcome.IsEnabled)
	puts := fake.requestsTo(http.MethodPut, testWebhookPath)
	t.Logf("upsertWebhook sent %d PUTs to a %s provider", len(puts), slowProviderDelay)
	require.GreaterOrEqual(t, len(puts), 2, "the slow async Step was dispatched again")
	for _, put := range puts {
		require.Equal(t, puts[0].body, put.body, "every dispatch sends the same idempotent body")
	}
	require.Equal(t, 1, store.count(), "the same tag leaves one webhook")
}

// storeThenDelay applies the PUT at once and answers late, so Typeform holds it before the answer.
func storeThenDelay(handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		recorder := &bufferedResponse{header: http.Header{}}
		handler(recorder, request)
		time.Sleep(slowProviderDelay)
		for name, values := range recorder.header {
			response.Header()[name] = values
		}
		response.WriteHeader(recorder.status)
		_, _ = response.Write(recorder.body) // A failed write leaves the Step to retry.
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   []byte
}

func (response *bufferedResponse) Header() http.Header { return response.header }
func (response *bufferedResponse) WriteHeader(status int) {
	response.status = status
}
func (response *bufferedResponse) Write(contents []byte) (int, error) {
	response.body = append(response.body, contents...)
	return len(contents), nil
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

type recordUpsertOutcome struct {
	dex.StepDefaultsNoWaitFor[typeform.UpsertWebhookResult]
}

func (recordUpsertOutcome) Execute(_ dex.Context, result typeform.UpsertWebhookResult) (*dex.StepDecision, error) {
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
	workerAddress := "127.0.0.1:" + unusedTestPort(t)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: dexServiceAddress(), WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexServiceAddress(), WorkerTarget: worker.WorkerTarget()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), client.Close(), cache.Close()))
		<-workerResult
	})
	return client
}

func dexServiceAddress() string {
	if address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS"); address != "" {
		return address
	}
	return "127.0.0.1:8801"
}

func unusedTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

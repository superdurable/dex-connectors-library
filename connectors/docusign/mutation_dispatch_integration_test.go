//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowProviderDelay outlasts Dex's seven-second local async phase, so an async Step is re-dispatched.
const slowProviderDelay = 9 * time.Second

// mutationOutcome is what each probe Flow completes with.
type mutationOutcome struct {
	Branch           sdkgo.BranchID    `json:"branch"`
	EnvelopeID       string            `json:"envelopeId,omitempty"`
	WasRecovered     bool              `json:"wasRecovered,omitempty"`
	WasAlreadyVoided bool              `json:"wasAlreadyVoided,omitempty"`
	FailureKind      sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// TestCreateEnvelopePostsOnceToASlowProviderWithRealDex proves a sync Step posts once to a nine-second provider.
func TestCreateEnvelopePostsOnceToASlowProviderWithRealDex(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			store.create(request)
			time.Sleep(slowProviderDelay)
			writeJSON(response, http.StatusCreated, createdEnvelopeJSON("sent"))
		},
		"GET " + envelopesPath: store.list,
	})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, createProbeFlowType, "create-slow")
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, outcome.Branch)
	require.Equal(t, testEnvelopeID, outcome.EnvelopeID)
	require.False(t, outcome.WasRecovered)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1, "one Step execution created one envelope")
	require.Equal(t, 1, store.count())
}

// TestCreateEnvelopeFindsItsEnvelopeAfterAServerErrorWithRealDex reads back an envelope created before a 500.
func TestCreateEnvelopeFindsItsEnvelopeAfterAServerErrorWithRealDex(t *testing.T) {
	store := &fakeEnvelopeStore{}
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST " + envelopesPath: func(response http.ResponseWriter, request *http.Request) {
			store.create(request)
			writeJSON(response, http.StatusInternalServerError, docusignError("UNSPECIFIED_ERROR"))
		},
		"GET " + envelopesPath: store.list,
	})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, createProbeFlowType, "create-recovered")
	require.Equal(t, docusign.CreateEnvelopeFromTemplateBranchCreated, outcome.Branch)
	require.True(t, outcome.WasRecovered)
	require.Len(t, fake.requestsTo(http.MethodPost, envelopesPath), 1)
	require.Equal(t, 1, store.count())
}

// TestVoidEnvelopeIsSafeUnderDuplicateDispatchWithRealDex ends voided when Dex re-dispatches a slow void.
func TestVoidEnvelopeIsSafeUnderDuplicateDispatchWithRealDex(t *testing.T) {
	var mu sync.Mutex
	isVoided, puts := false, 0
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"PUT " + envelopePath: func(response http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			wasVoided := isVoided
			isVoided = true
			puts++
			mu.Unlock()
			if wasVoided {
				writeJSON(response, http.StatusBadRequest, docusignError("ENVELOPE_CANNOT_VOID_INVALID_STATE"))
				return
			}
			time.Sleep(slowProviderDelay)
			writeJSON(response, http.StatusOK, `{"envelopeId":"`+testEnvelopeID+`"}`)
		},
		"GET " + envelopePath: func(response http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			status := map[bool]string{true: "voided", false: "sent"}[isVoided]
			mu.Unlock()
			writeJSON(response, http.StatusOK, envelopeJSON(testEnvelopeID, status, "marker"))
		},
	})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, voidProbeFlowType, "void")
	require.Equal(t, docusign.VoidEnvelopeBranchVoided, outcome.Branch)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("voidEnvelope sent %d PUTs to a %s provider; wasAlreadyVoided=%t", puts, slowProviderDelay, outcome.WasAlreadyVoided)
	require.GreaterOrEqual(t, puts, 2, "the slow async Step was dispatched again")
}

const (
	createProbeFlowType = "DocuSignCreateEnvelopeProbe"
	voidProbeFlowType   = "DocuSignVoidEnvelopeProbe"
)

// probeFlow runs one connector Mutation as its start Step and completes with its outcome.
type probeFlow struct {
	dex.FlowDefaults
	flowType string
	steps    []dex.StepDef
}

func (flow *probeFlow) GetFlowType() string                    { return flow.flowType }
func (flow *probeFlow) GetSteps() []dex.StepDef                { return flow.steps }
func (*probeFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (*probeFlow) GetRPCs() []dex.RPCDef                       { return nil }

var probeAnnotations = sdkgo.StepAnnotations{GroupID: "probe", GroupLabel: "Probe", Explanation: "Run one DocuSign Mutation against the fake provider."}

func newProbeFlows(connection docusign.Connection) []dex.Flow {
	return []dex.Flow{
		&probeFlow{flowType: createProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(docusign.NewCreateEnvelopeFromTemplateStep(docusign.CreateEnvelopeFromTemplateStepConfig[string]{
				StepType: "CreateEnvelope", Connection: connection, ConnectionName: testConnection.Name, Annotations: probeAnnotations,
				MapToOperationInput: func(string) docusign.CreateEnvelopeFromTemplateInput { return validCreateInput() },
				Created:             sdkgo.GoTo(recordCreateOutcome{}), ProviderRejected: sdkgo.GoTo(recordCreateOutcome{}),
				Uncertain: sdkgo.GoTo(recordCreateOutcome{}), Defect: sdkgo.GoTo(recordCreateOutcome{}),
			})),
			dex.DefineStep(recordCreateOutcome{}),
		}},
		&probeFlow{flowType: voidProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(docusign.NewVoidEnvelopeStep(docusign.VoidEnvelopeStepConfig[string]{
				StepType: "VoidEnvelope", Connection: connection, ConnectionName: testConnection.Name, Annotations: probeAnnotations,
				MapToOperationInput: func(string) docusign.VoidEnvelopeInput {
					return docusign.VoidEnvelopeInput{EnvelopeID: testEnvelopeID, VoidedReason: "Deal lost"}
				},
				Voided: sdkgo.GoTo(recordVoidOutcome{}), NotVoidable: sdkgo.GoTo(recordVoidOutcome{}), NotFound: sdkgo.GoTo(recordVoidOutcome{}),
				ProviderRejected: sdkgo.GoTo(recordVoidOutcome{}), InvalidResponse: sdkgo.GoTo(recordVoidOutcome{}), Defect: sdkgo.GoTo(recordVoidOutcome{}),
			})),
			dex.DefineStep(recordVoidOutcome{}),
		}},
	}
}

type recordCreateOutcome struct {
	dex.StepDefaultsNoWaitFor[docusign.CreateEnvelopeFromTemplateResult]
}

func (recordCreateOutcome) Execute(_ dex.Context, result docusign.CreateEnvelopeFromTemplateResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{
		Branch: result.Branch, EnvelopeID: result.Value.EnvelopeID, WasRecovered: result.Value.WasRecovered, FailureKind: failureKindOf(result.Failure),
	}), nil
}

type recordVoidOutcome struct {
	dex.StepDefaultsNoWaitFor[docusign.VoidEnvelopeResult]
}

func (recordVoidOutcome) Execute(_ dex.Context, result docusign.VoidEnvelopeResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{
		Branch: result.Branch, EnvelopeID: result.Value.EnvelopeID, WasAlreadyVoided: result.Value.WasAlreadyVoided, FailureKind: failureKindOf(result.Failure),
	}), nil
}

func failureKindOf(failure *sdkgo.Failure) sdkgo.FailureKind {
	if failure == nil {
		return ""
	}
	return failure.Kind
}

// probeHarness is one Worker and Client registered with every probe Flow against the fake provider.
type probeHarness struct {
	client *dex.Client
	flows  map[string]dex.Flow
}

func startProbeHarness(t *testing.T, fake *fakeDocuSign) *probeHarness {
	t.Helper()
	// The real clock: the probe's read-back window starts from the dispatch time.
	client, err := docusign.New(docusign.Config{}, staticCredentials(productionCredentials()), docusign.WithHTTPClient(fake.redirectingClient()))
	require.NoError(t, err)
	connection, err := docusign.NewConnection(client, testConnection)
	require.NoError(t, err)
	flows := newProbeFlows(connection)
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	flowsByType := make(map[string]dex.Flow, len(flows))
	for _, flow := range flows {
		flowsByType[flow.GetFlowType()] = flow
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := "127.0.0.1:" + unusedTestPort(t)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: dexServiceAddress(), WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	dexClient, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexServiceAddress(), WorkerTarget: worker.WorkerTarget()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), dexClient.Close(), cache.Close()))
		<-workerResult
	})
	return &probeHarness{client: dexClient, flows: flowsByType}
}

// runProbe starts flowType once and waits for its completed outcome.
func (harness *probeHarness) runProbe(t *testing.T, flowType string, name string) mutationOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flow, isFound := harness.flows[flowType]
	require.True(t, isFound, flowType)
	flowID := fmt.Sprintf("docusign-probe-%s-%d", name, time.Now().UnixNano())
	_, err := harness.client.StartFlow(ctx, flow, flowID, "probe", dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome mutationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
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

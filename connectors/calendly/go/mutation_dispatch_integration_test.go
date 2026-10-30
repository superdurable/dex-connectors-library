//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowProviderDelay exercises the synchronous scheduling-link path.
const slowProviderDelay = 9 * time.Second

// mutationOutcome is what each probe Flow completes with.
type mutationOutcome struct {
	Branch          sdkgo.BranchID    `json:"branch"`
	AlreadyCanceled bool              `json:"alreadyCanceled,omitempty"`
	WasCreated      bool              `json:"wasCreated,omitempty"`
	BookingURL      string            `json:"bookingUrl,omitempty"`
	SubscriptionURI string            `json:"subscriptionUri,omitempty"`
	FailureKind     sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// TestCreateSchedulingLinkPostsOnceToASlowProviderWithRealDex proves the sync choice: Calendly has no
// idempotency key, and a sync Step is not re-dispatched while its one POST waits nine seconds.
func TestCreateSchedulingLinkPostsOnceToASlowProviderWithRealDex(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": delayed(respondJSON(http.StatusCreated, createdLinkBody))})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, linkProbeFlowType, "sync")
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, outcome.Branch)
	require.Equal(t, "https://calendly.com/d/abcd-1234/30-minute-meeting", outcome.BookingURL)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1, "one Step execution created one link")
}

// TestCreateSchedulingLinkRetries503WithRealDex proves the checkpoint survives a regular sync retry.
func TestCreateSchedulingLinkRetries503WithRealDex(t *testing.T) {
	var requests atomic.Int32
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(response http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			writeJSON(response, http.StatusServiceUnavailable, providerError("Unavailable", "later"))
			return
		}
		respondJSON(http.StatusCreated, createdLinkBody)(response, request)
	}})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, linkProbeFlowType, "retry-503")
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, outcome.Branch)
	require.Equal(t, int32(2), requests.Load())
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 2)
}

// TestCancelScheduledEventIsSafeUnderDuplicateDispatchWithRealDex retries after the first accepted cancellation.
func TestCancelScheduledEventIsSafeUnderDuplicateDispatchWithRealDex(t *testing.T) {
	var mu sync.Mutex
	cancellations := 0
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /scheduled_events/EVENT0001/cancellation": func(response http.ResponseWriter, request *http.Request) {
			mu.Lock()
			isAlreadyCanceled := cancellations > 0
			cancellations++
			mu.Unlock()
			if isAlreadyCanceled {
				writeJSON(response, http.StatusForbidden, `{"title":"Permission Denied","message":"Event is already canceled"}`)
				return
			}
			writeJSON(response, http.StatusCreated, `{"resource":{"canceled_by":"Hana Host","reason":null,"canceler_type":"host","created_at":"2026-09-30T12:00:00.000000Z"}}`)
		},
		"GET /scheduled_events/EVENT0001": func(response http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			status := map[bool]string{true: "canceled", false: "active"}[cancellations > 0]
			mu.Unlock()
			writeJSON(response, http.StatusOK, `{"resource":`+scheduledEventJSON("EVENT0001", status, fixedNow.Add(24*time.Hour))+`}`)
		},
	})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, cancelProbeFlowType, "cancel")
	require.Equal(t, calendly.CancelScheduledEventBranchCanceled, outcome.Branch)
	posts := fake.requestsTo(http.MethodPost, "/scheduled_events/EVENT0001/cancellation")
	require.Len(t, posts, 2, "the second Execute verifies the accepted cancellation")
	require.Equal(t, 2, cancellations)
}

// TestCreateWebhookSubscriptionCreatesOneSubscriptionUnderDuplicateDispatchWithRealDex retries after creation.
func TestCreateWebhookSubscriptionCreatesOneSubscriptionUnderDuplicateDispatchWithRealDex(t *testing.T) {
	store := &fakeSubscriptions{}
	fake := newFakeCalendly(t, store.routes())
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, subscriptionProbeFlowType, "subscribe")
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, outcome.Branch)
	store.mu.Lock()
	subscriptionCount := len(store.subscriptions)
	store.mu.Unlock()
	require.Equal(t, 1, subscriptionCount)
	lists := fake.requestsTo(http.MethodGet, "/webhook_subscriptions")
	require.Len(t, lists, 2, "the second Execute lists the existing subscription")
	require.Len(t, fake.requestsTo(http.MethodPost, "/webhook_subscriptions"), 1)
}

// delayed answers after slowProviderDelay, as a slow Calendly would.
func delayed(handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(slowProviderDelay)
		handler(response, request)
	}
}

const (
	linkProbeFlowType         = "CalendlyCreateSchedulingLinkProbe"
	cancelProbeFlowType       = "CalendlyCancelScheduledEventProbe"
	subscriptionProbeFlowType = "CalendlyCreateWebhookSubscriptionProbe"
)

// probeFlow runs one connector Mutation as its start Step and completes with its outcome.
type probeFlow struct {
	dex.FlowDefaults
	flowType string
	steps    []dex.StepDef
}

func (flow *probeFlow) GetFlowType() string     { return flow.flowType }
func (flow *probeFlow) GetSteps() []dex.StepDef { return flow.steps }

// GetPersistenceSchema registers nothing: each probe returns its outcome as the Flow output.
func (*probeFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetRPCs registers no RPCs.
func (*probeFlow) GetRPCs() []dex.RPCDef { return nil }

var probeAnnotations = sdkgo.StepAnnotations{GroupID: "probe", GroupLabel: "Probe", Explanation: "Run one Calendly Mutation against the slow fake provider."}

func newProbeFlows(connection calendly.Connection, client *calendly.Client) []dex.Flow {
	linkStep := func(stepType string) dex.StepDef {
		return dex.DefineStartStep(calendly.NewCreateSchedulingLinkStep(calendly.CreateSchedulingLinkStepConfig[string]{
			StepType: stepType, Connection: connection, ConnectionName: testConnection.Name, Annotations: probeAnnotations,
			MapToOperationInput: func(string) calendly.CreateSchedulingLinkInput {
				return calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
			},
			Created: sdkgo.GoTo(recordLinkOutcome{}), ProviderRejected: sdkgo.GoTo(recordLinkOutcome{}),
			Uncertain: sdkgo.GoTo(recordLinkOutcome{}), Defect: sdkgo.GoTo(recordLinkOutcome{}),
		}))
	}
	return []dex.Flow{
		&probeFlow{flowType: linkProbeFlowType, steps: []dex.StepDef{linkStep("CreateLinkSync"), dex.DefineStep(recordLinkOutcome{})}},
		&probeFlow{flowType: cancelProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(cancelThenFailFirstAttempt{client: client}),
		}},
		&probeFlow{flowType: subscriptionProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(subscribeThenFailFirstAttempt{client: client}),
		}},
	}
}

type cancelThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[string]
	client *calendly.Client
}

func (cancelThenFailFirstAttempt) GetStepType() string              { return "CancelEvent" }
func (cancelThenFailFirstAttempt) GetStepOptions() *dex.StepOptions { return probeRetryOptions() }
func (step cancelThenFailFirstAttempt) Execute(ctx dex.Context, _ string) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.CancelScheduledEvent(), testConnection,
		calendly.CancelScheduledEventInput{ScheduledEventURI: testEventURI})
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("probe fails after cancellation was accepted")
	}
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, AlreadyCanceled: result.Value.AlreadyCanceled, FailureKind: failureKindOf(result.Failure)}), nil
}

type subscribeThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[string]
	client *calendly.Client
}

func (subscribeThenFailFirstAttempt) GetStepType() string              { return "SubscribeCallback" }
func (subscribeThenFailFirstAttempt) GetStepOptions() *dex.StepOptions { return probeRetryOptions() }
func (step subscribeThenFailFirstAttempt) Execute(ctx dex.Context, _ string) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.CreateWebhookSubscription(), testConnection,
		calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("probe fails after subscription was created")
	}
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, WasCreated: result.Value.WasCreated, SubscriptionURI: result.Value.URI, FailureKind: failureKindOf(result.Failure)}), nil
}

func probeRetryOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second, MaximumAttempts: 3, TotalDuration: time.Minute}}
}

type recordLinkOutcome struct {
	dex.StepDefaultsNoWaitFor[calendly.CreateSchedulingLinkResult]
}

func (recordLinkOutcome) Execute(_ dex.Context, result calendly.CreateSchedulingLinkResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, BookingURL: result.Value.BookingURL, FailureKind: failureKindOf(result.Failure)}), nil
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

func startProbeHarness(t *testing.T, fake *fakeCalendly) *probeHarness {
	t.Helper()
	calendlyClient := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	connection, err := calendly.NewConnection(calendlyClient, testConnection)
	require.NoError(t, err)
	flows := newProbeFlows(connection, calendlyClient)
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	flowsByType := make(map[string]dex.Flow, len(flows))
	for _, flow := range flows {
		flowsByType[flow.GetFlowType()] = flow
	}
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
	return &probeHarness{client: client, flows: flowsByType}
}

// runProbe starts flowType once and waits for its completed outcome.
func (harness *probeHarness) runProbe(t *testing.T, flowType string, name string) mutationOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flow, isFound := harness.flows[flowType]
	require.True(t, isFound, flowType)
	flowID := fmt.Sprintf("calendly-probe-%s-%d", name, time.Now().UnixNano())
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

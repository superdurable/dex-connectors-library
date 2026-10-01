//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

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
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowProviderDelay outlasts Dex's seven-second local async phase, so an async Step is re-dispatched.
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

// TestCreateSchedulingLinkPostsOnceUnderAnAsyncOverrideWithRealDex overrides the Step to async durability. The
// short local attempt has too little time before its deadline to send, so the one POST runs in the regular
// attempt and no duplicate dispatch can repeat it.
func TestCreateSchedulingLinkPostsOnceUnderAnAsyncOverrideWithRealDex(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": delayed(respondJSON(http.StatusCreated, createdLinkBody))})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, asyncLinkProbeFlowType, "async")
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, outcome.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1, "the deadline guard keeps an async override single")
}

// TestCancelScheduledEventIsSafeUnderDuplicateDispatchWithRealDex lets Dex re-dispatch the async Step while
// Calendly is slow: the repeated POST is refused because the event is canceled, and the read-back makes it
// the same canceled outcome.
func TestCancelScheduledEventIsSafeUnderDuplicateDispatchWithRealDex(t *testing.T) {
	var mu sync.Mutex
	cancellations := 0
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /scheduled_events/EVENT0001/cancellation": func(response http.ResponseWriter, request *http.Request) {
			mu.Lock()
			isAlreadyCanceled := cancellations > 0
			cancellations++
			mu.Unlock()
			time.Sleep(slowProviderDelay)
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
	t.Logf("cancelScheduledEvent sent %d POSTs to a %s provider; alreadyCanceled=%t", len(posts), slowProviderDelay, outcome.AlreadyCanceled)
	require.GreaterOrEqual(t, len(posts), 2, "the slow async Step was dispatched again")
}

// TestCreateWebhookSubscriptionCreatesOneSubscriptionUnderDuplicateDispatchWithRealDex lets the duplicate
// attempt list while the first create is still in flight: it finds the subscription and creates none.
func TestCreateWebhookSubscriptionCreatesOneSubscriptionUnderDuplicateDispatchWithRealDex(t *testing.T) {
	store := &fakeSubscriptions{}
	routes := store.routes()
	routes["POST /webhook_subscriptions"] = storeThenDelay(routes["POST /webhook_subscriptions"])
	fake := newFakeCalendly(t, routes)
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, subscriptionProbeFlowType, "subscribe")
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, outcome.Branch)
	store.mu.Lock()
	subscriptionCount := len(store.subscriptions)
	store.mu.Unlock()
	require.Equal(t, 1, subscriptionCount, "a duplicate dispatch reused the subscription")
	lists := fake.requestsTo(http.MethodGet, "/webhook_subscriptions")
	t.Logf("createWebhookSubscription sent %d lists and %d creates; wasCreated=%t", len(lists),
		len(fake.requestsTo(http.MethodPost, "/webhook_subscriptions")), outcome.WasCreated)
	require.GreaterOrEqual(t, len(lists), 2, "the slow async Step was dispatched again and listed first")
}

// delayed answers after slowProviderDelay, as a slow Calendly would.
func delayed(handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(slowProviderDelay)
		handler(response, request)
	}
}

// storeThenDelay applies the create at once and answers late, so Calendly holds it before the answer.
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

const (
	linkProbeFlowType         = "CalendlyCreateSchedulingLinkProbe"
	asyncLinkProbeFlowType    = "CalendlyCreateSchedulingLinkAsyncProbe"
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

func newProbeFlows(connection calendly.Connection) []dex.Flow {
	asyncOverride := &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync}
	linkStep := func(stepType string, override *dex.StepOptions) dex.StepDef {
		return dex.DefineStartStep(calendly.NewCreateSchedulingLinkStep(calendly.CreateSchedulingLinkStepConfig[string]{
			StepType: stepType, Connection: connection, StepOptionsOverride: override, Annotations: probeAnnotations,
			MapToOperationInput: func(string) calendly.CreateSchedulingLinkInput {
				return calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
			},
			Created: sdkgo.GoTo(recordLinkOutcome{}), ProviderRejected: sdkgo.GoTo(recordLinkOutcome{}),
			Uncertain: sdkgo.GoTo(recordLinkOutcome{}), Defect: sdkgo.GoTo(recordLinkOutcome{}),
		}))
	}
	return []dex.Flow{
		&probeFlow{flowType: linkProbeFlowType, steps: []dex.StepDef{linkStep("CreateLinkSync", nil), dex.DefineStep(recordLinkOutcome{})}},
		&probeFlow{flowType: asyncLinkProbeFlowType, steps: []dex.StepDef{linkStep("CreateLinkAsync", asyncOverride), dex.DefineStep(recordLinkOutcome{})}},
		&probeFlow{flowType: cancelProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(calendly.NewCancelScheduledEventStep(calendly.CancelScheduledEventStepConfig[string]{
				StepType: "CancelEvent", Connection: connection, Annotations: probeAnnotations,
				MapToOperationInput: func(string) calendly.CancelScheduledEventInput {
					return calendly.CancelScheduledEventInput{ScheduledEventURI: testEventURI}
				},
				Canceled: sdkgo.GoTo(recordCancelOutcome{}), NotFound: sdkgo.GoTo(recordCancelOutcome{}),
				ProviderRejected: sdkgo.GoTo(recordCancelOutcome{}), InvalidResponse: sdkgo.GoTo(recordCancelOutcome{}), Defect: sdkgo.GoTo(recordCancelOutcome{}),
			})),
			dex.DefineStep(recordCancelOutcome{}),
		}},
		&probeFlow{flowType: subscriptionProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(calendly.NewCreateWebhookSubscriptionStep(calendly.CreateWebhookSubscriptionStepConfig[string]{
				StepType: "SubscribeCallback", Connection: connection, Annotations: probeAnnotations,
				MapToOperationInput: func(string) calendly.CreateWebhookSubscriptionInput {
					return calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL}
				},
				Subscribed: sdkgo.GoTo(recordSubscriptionOutcome{}), ConflictingSubscription: sdkgo.GoTo(recordSubscriptionOutcome{}),
				ProviderRejected: sdkgo.GoTo(recordSubscriptionOutcome{}), InvalidResponse: sdkgo.GoTo(recordSubscriptionOutcome{}),
				Defect: sdkgo.GoTo(recordSubscriptionOutcome{}),
			})),
			dex.DefineStep(recordSubscriptionOutcome{}),
		}},
	}
}

type recordLinkOutcome struct {
	dex.StepDefaultsNoWaitFor[calendly.CreateSchedulingLinkResult]
}

func (recordLinkOutcome) Execute(_ dex.Context, result calendly.CreateSchedulingLinkResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, BookingURL: result.Value.BookingURL, FailureKind: failureKindOf(result.Failure)}), nil
}

type recordCancelOutcome struct {
	dex.StepDefaultsNoWaitFor[calendly.CancelScheduledEventResult]
}

func (recordCancelOutcome) Execute(_ dex.Context, result calendly.CancelScheduledEventResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, AlreadyCanceled: result.Value.AlreadyCanceled, FailureKind: failureKindOf(result.Failure)}), nil
}

type recordSubscriptionOutcome struct {
	dex.StepDefaultsNoWaitFor[calendly.CreateWebhookSubscriptionResult]
}

func (recordSubscriptionOutcome) Execute(_ dex.Context, result calendly.CreateWebhookSubscriptionResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{
		Branch: result.Branch, WasCreated: result.Value.WasCreated, SubscriptionURI: result.Value.URI, FailureKind: failureKindOf(result.Failure),
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

func startProbeHarness(t *testing.T, fake *fakeCalendly) *probeHarness {
	t.Helper()
	calendlyClient := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	connection, err := calendly.NewConnection(calendlyClient, testConnection)
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

func unusedTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

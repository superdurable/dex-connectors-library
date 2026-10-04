//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

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
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowProviderDelay outlasts Dex's seven-second local async phase, so an async Step is dispatched again.
const slowProviderDelay = 9 * time.Second

const (
	replyProbeFlowType            = "HelpScoutReplyProbe"
	asyncReplyProbeFlowType       = "HelpScoutReplyAsyncProbe"
	replyRetryProbeFlowType       = "HelpScoutReplyRetryProbe"
	updateProbeFlowType           = "HelpScoutUpdateProbe"
	probeConversationID     int64 = 501
)

// mutationOutcome is what each probe Flow completes with.
type mutationOutcome struct {
	Branch            sdkgo.BranchID    `json:"branch"`
	ThreadID          int64             `json:"threadId,omitempty"`
	WasAlreadyApplied bool              `json:"wasAlreadyApplied,omitempty"`
	Status            string            `json:"status,omitempty"`
	FailureKind       sdkgo.FailureKind `json:"failureKind,omitempty"`
}

// TestReplyToConversationPostsOnceToASlowProviderWithRealDex proves the sync choice: Help Scout has no
// idempotency key, and a sync Step is not dispatched again while its one POST waits nine seconds.
func TestReplyToConversationPostsOnceToASlowProviderWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t, slowReplyRoutes())
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, replyProbeFlowType, "sync")
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, outcome.Branch)
	require.EqualValues(t, 567, outcome.ThreadID)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1, "one Step execution sent one reply")
}

// TestReplyToConversationPostsOnceUnderAnAsyncOverrideWithRealDex overrides the Step to async durability. The
// short local attempt has too little time before its deadline to send, so the one POST runs in the regular
// attempt and no duplicate dispatch can repeat it.
func TestReplyToConversationPostsOnceUnderAnAsyncOverrideWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t, slowReplyRoutes())
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, asyncReplyProbeFlowType, "async")
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, outcome.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1, "the deadline guard keeps an async override single")
}

// TestReplyCheckpointSurvivesADexRetryWithRealDex fails the Step's first attempt after the reply was sent;
// Dex's retry of the same Step execution finds the heartbeat checkpoint and reports uncertain unsent.
func TestReplyCheckpointSurvivesADexRetryWithRealDex(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501":        respondJSON(http.StatusOK, conversationJSON(501, "active", 123)),
		"POST /v2/conversations/501/reply": createdThread("567"),
	})
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, replyRetryProbeFlowType, "retry")
	require.Equal(t, sdkgo.UncertainBranchID, outcome.Branch)
	require.Equal(t, sdkgo.FailureTransport, outcome.FailureKind)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1, "the retry did not send a second reply")
}

// TestUpdateConversationConvergesUnderDuplicateDispatchWithRealDex lets Dex dispatch the async Step again
// while the first status write is still answering; both attempts set the same final values.
func TestUpdateConversationConvergesUnderDuplicateDispatchWithRealDex(t *testing.T) {
	stored := &storedConversation{id: probeConversationID, status: "active", assigneeID: 99, tags: []string{"billing"}}
	routes := stored.routes()
	patch := routes["PATCH /v2/conversations/501"]
	routes["PATCH /v2/conversations/501"] = func(response http.ResponseWriter, request *http.Request) {
		recorder := &bufferedResponse{header: http.Header{}}
		patch(recorder, request)
		time.Sleep(slowProviderDelay)
		response.WriteHeader(recorder.status)
	}
	fake := newFakeHelpScout(t, routes)
	harness := startProbeHarness(t, fake)
	outcome := harness.runProbe(t, updateProbeFlowType, "update")
	require.Equal(t, helpscout.UpdateConversationBranchUpdated, outcome.Branch)
	require.Equal(t, "closed", outcome.Status)
	status, assigneeID, tags := stored.snapshot()
	require.Equal(t, "closed", status)
	require.EqualValues(t, 99, assigneeID)
	require.Equal(t, []string{"billing", "dex-triaged"}, tags, "the tag is added once")
	reads := fake.requestsTo(http.MethodGet, "/v2/conversations/501")
	t.Logf("updateConversation sent %d reads, %d PATCHes, and %d PUTs to a %s provider; wasAlreadyApplied=%t", len(reads),
		len(fake.requestsTo(http.MethodPatch, "/v2/conversations/501")), len(fake.requestsTo(http.MethodPut, "/v2/conversations/501/tags")),
		slowProviderDelay, outcome.WasAlreadyApplied)
	require.GreaterOrEqual(t, len(reads), 3, "the slow async Step was dispatched again and read first")
}

func slowReplyRoutes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /v2/conversations/501": respondJSON(http.StatusOK, conversationJSON(501, "active", 123)),
		"POST /v2/conversations/501/reply": func(response http.ResponseWriter, request *http.Request) {
			time.Sleep(slowProviderDelay)
			createdThread("567")(response, request)
		},
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
	flowType string
	steps    []dex.StepDef
}

func (flow *probeFlow) GetFlowType() string     { return flow.flowType }
func (flow *probeFlow) GetSteps() []dex.StepDef { return flow.steps }

// GetPersistenceSchema registers nothing: each probe returns its outcome as the Flow output.
func (*probeFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetRPCs registers no RPCs.
func (*probeFlow) GetRPCs() []dex.RPCDef { return nil }

var probeAnnotations = sdkgo.StepAnnotations{GroupID: "probe", GroupLabel: "Probe", Explanation: "Run one Help Scout Mutation against the fake provider."}

func newProbeFlows(connection helpscout.Connection, client *helpscout.Client) []dex.Flow {
	replyStep := func(stepType string, override *dex.StepOptions) dex.StepDef {
		return dex.DefineStartStep(helpscout.NewReplyToConversationStep(helpscout.ReplyToConversationStepConfig[string]{
			StepType: stepType, Connection: connection, ConnectionName: testConnection.Name, StepOptionsOverride: override, Annotations: probeAnnotations,
			MapToOperationInput: func(string) helpscout.ReplyToConversationInput {
				return helpscout.ReplyToConversationInput{ConversationID: probeConversationID, Text: "We refunded the duplicate charge."}
			},
			Replied: sdkgo.GoTo(recordReplyOutcome{}), NotFound: sdkgo.GoTo(recordReplyOutcome{}), ProviderRejected: sdkgo.GoTo(recordReplyOutcome{}),
			Uncertain: sdkgo.GoTo(recordReplyOutcome{}), Defect: sdkgo.GoTo(recordReplyOutcome{}),
		}))
	}
	return []dex.Flow{
		&probeFlow{flowType: replyProbeFlowType, steps: []dex.StepDef{replyStep("ReplySync", nil), dex.DefineStep(recordReplyOutcome{})}},
		&probeFlow{flowType: asyncReplyProbeFlowType, steps: []dex.StepDef{
			replyStep("ReplyAsync", &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync}), dex.DefineStep(recordReplyOutcome{}),
		}},
		&probeFlow{flowType: replyRetryProbeFlowType, steps: []dex.StepDef{dex.DefineStartStep(replyThenFailFirstAttempt{client: client})}},
		&probeFlow{flowType: updateProbeFlowType, steps: []dex.StepDef{
			dex.DefineStartStep(helpscout.NewUpdateConversationStep(helpscout.UpdateConversationStepConfig[string]{
				StepType: "CloseAndTag", Connection: connection, ConnectionName: testConnection.Name, Annotations: probeAnnotations,
				MapToOperationInput: func(string) helpscout.UpdateConversationInput {
					return helpscout.UpdateConversationInput{ConversationID: probeConversationID, Status: helpscout.ConversationStatusClosed, AddTags: []string{"dex-triaged"}}
				},
				Updated: sdkgo.GoTo(recordUpdateOutcome{}), NotFound: sdkgo.GoTo(recordUpdateOutcome{}), ProviderRejected: sdkgo.GoTo(recordUpdateOutcome{}),
				InvalidResponse: sdkgo.GoTo(recordUpdateOutcome{}), Defect: sdkgo.GoTo(recordUpdateOutcome{}),
			})),
			dex.DefineStep(recordUpdateOutcome{}),
		}},
	}
}

type recordReplyOutcome struct {
	dex.StepDefaultsNoWaitFor[helpscout.ReplyToConversationResult]
}

func (recordReplyOutcome) Execute(_ dex.Context, result helpscout.ReplyToConversationResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, ThreadID: result.Value.ThreadID, FailureKind: failureKindOf(result.Failure)}), nil
}

type recordUpdateOutcome struct {
	dex.StepDefaultsNoWaitFor[helpscout.UpdateConversationResult]
}

func (recordUpdateOutcome) Execute(_ dex.Context, result helpscout.UpdateConversationResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(mutationOutcome{
		Branch: result.Branch, WasAlreadyApplied: result.Value.WasAlreadyApplied, Status: string(result.Value.Conversation.Status),
		FailureKind: failureKindOf(result.Failure),
	}), nil
}

// replyThenFailFirstAttempt fails its first attempt after the reply was sent, like a Worker lost after dispatch.
type replyThenFailFirstAttempt struct {
	dex.StepDefaultsNoWaitFor[string]
	client *helpscout.Client
}

func (replyThenFailFirstAttempt) GetStepType() string { return "ReplyThenFailFirstAttempt" }

func (replyThenFailFirstAttempt) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: &dex.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second, MaximumAttempts: 3, TotalDuration: time.Minute},
	}
}

func (step replyThenFailFirstAttempt) Execute(ctx dex.Context, _ string) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.client.ReplyToConversation(), testConnection,
		helpscout.ReplyToConversationInput{ConversationID: probeConversationID, Text: "We refunded the duplicate charge."})
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("the probe fails its first attempt after the reply was sent")
	}
	return dex.GracefulComplete(mutationOutcome{Branch: result.Branch, ThreadID: result.Value.ThreadID, FailureKind: failureKindOf(result.Failure)}), nil
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

func startProbeHarness(t *testing.T, fake *fakeHelpScout) *probeHarness {
	t.Helper()
	helpScoutClient := newTestClient(t, fake.redirectingClient(), staticCredentials(sentinelWebhookSecret), helpscout.Config{})
	connection, err := helpscout.NewConnection(helpScoutClient, testConnection)
	require.NoError(t, err)
	flows := newProbeFlows(connection, helpScoutClient)
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
	var stopOnce sync.Once
	t.Cleanup(func() {
		stopOnce.Do(func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, errors.Join(worker.Stop(stopCtx), client.Close(), cache.Close()))
			<-workerResult
		})
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
	flowID := fmt.Sprintf("helpscout-probe-%s-%d", name, time.Now().UnixNano())
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

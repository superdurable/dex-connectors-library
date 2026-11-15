//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package memberchangeintake

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	mailchimp "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go/internal/mailchimpaudiencefake"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "mailchimp-member-change-intake", Kind: "live"}

// TestMemberChangesStartOneFlowEachAcrossARestartWithRealDex: existing contacts are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestMemberChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newMemberChangeEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Upsert("ben@example.com", "subscribed", "Ben")
	environment.provider.Upsert("cara@example.com", "subscribed", "Cara")
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t)

	environment.provider.Upsert("dana@example.com", "pending", "Dana")
	environment.provider.Upsert("ben@example.com", "unsubscribed", "Ben")
	first.runner.WakePoller(MemberChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, mailchimp.MemberStatusPending, outcomes["dana@example.com"].Current.Status)
	require.Equal(t, mailchimp.MemberStatusUnsubscribed, outcomes["ben@example.com"].DeliveredStatus)
	require.Equal(t, "Ben", outcomes["ben@example.com"].Current.MergeFields["FNAME"], "the Flow reads the contact back from Mailchimp")
	first.stop(t)

	environment.provider.Upsert("eli@example.com", "subscribed", "Eli")
	environment.provider.Upsert("dana@example.com", "subscribed", "Dana")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(MemberChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, mailchimp.MemberStatusSubscribed, outcomes["eli@example.com"].Current.Status)
	require.Equal(t, mailchimp.MemberStatusSubscribed, outcomes["dana@example.com"].DeliveredStatus)
	environment.waitForWatermark(t)
	environment.pollAgain(t, second)
	require.Equal(t, 4, environment.deliveryCount(), "no version is delivered twice and the baseline is never delivered")
}

// TestRejectedCursorQueryRecordsANewBaselineWithRealDex: a 400 reseeds without delivering the gap.
func TestRejectedCursorQueryRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newMemberChangeEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Upsert("ben@example.com", "subscribed", "Ben")
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t)
	first.stop(t)

	environment.provider.Upsert("gap@example.com", "subscribed", "Gap")
	environment.provider.FailNextList(mailchimpaudiencefake.Failure{Status: http.StatusBadRequest})
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.provider.Upsert("after@example.com", "subscribed", "After")
	second.runner.WakePoller(MemberChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Contains(t, outcomes, "after@example.com")
	require.NotContains(t, outcomes, "gap@example.com", "changes between the rejected cursor and the new baseline are not delivered")
}

// TestRateLimitedPollPausesThenDeliversWithRealDex: a 429 with Retry-After pauses polling, then the change
// arrives exactly once.
func TestRateLimitedPollPausesThenDeliversWithRealDex(t *testing.T) {
	environment := newMemberChangeEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	process := environment.startTriggerProcess(t)
	environment.waitForRequests(t, 1)
	environment.provider.FailNextList(mailchimpaudiencefake.Failure{Status: http.StatusTooManyRequests, RetryAfter: "1"})
	environment.provider.Upsert("ben@example.com", "subscribed", "Ben")
	process.runner.WakePoller(MemberChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, mailchimp.MemberStatusSubscribed, outcomes["ben@example.com"].Current.Status)
	require.Equal(t, 1, environment.deliveryCount())
}

type memberChangeEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *mailchimpaudiencefake.Audience
	connection mailchimp.Connection
	objects    *testsupport.ObjectStore
	// configuration is the saved unit value; a unique audience keeps Flow IDs unique per run.
	configuration mailchimp.MemberChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]
	order         []string
}

func newMemberChangeEnvironment(t *testing.T) *memberChangeEnvironment {
	t.Helper()
	audienceID := "it" + strconv.FormatInt(time.Now().UnixNano(), 10)
	fake := mailchimpaudiencefake.New(t, audienceID)
	reference := sdkgo.ConnectionRef{Provider: "mailchimp", Name: ConnectionName}
	client, err := mailchimp.New(mailchimp.Config{PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[mailchimp.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(mailchimpaudiencefake.APIKey)},
	}, mailchimp.WithAPIBaseURL(fake.URL))
	require.NoError(t, err)
	connection, err := mailchimp.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &memberChangeEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]{},
		configuration: mailchimp.MemberChangedTriggerConfiguration{AudienceID: audienceID},
	}
}

func (environment *memberChangeEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: mailchimp.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: mailchimp.MemberChangedTriggerDefinition.Trigger.TriggerName, BindingName: MemberChangedTriggerBinding,
	}
}

// triggerProcess is one run of the application's Trigger side; stopping it models a process exit.
type triggerProcess struct {
	runner   *pollingtrigger.Runner
	cancel   context.CancelFunc
	result   chan error
	stopOnce sync.Once
	stopErr  error
}

func (process *triggerProcess) stop(t *testing.T) {
	t.Helper()
	require.ErrorIs(t, process.stopAndWait(), context.Canceled)
}

func (process *triggerProcess) stopAndWait() error {
	process.stopOnce.Do(func() {
		process.cancel()
		process.stopErr = <-process.result
	})
	return process.stopErr
}

// startTriggerProcess wires the example's target behind the durable inbox and project cursor, as
// mailchimp.NewProjectMemberTriggerRunner does, over storage that survives the process.
func (environment *memberChangeEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryMemberChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[mailchimp.MemberChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]) error {
		if err := start.HandleTrigger(ctx, event); err != nil {
			return err
		}
		environment.mutex.Lock()
		defer environment.mutex.Unlock()
		if _, ok := environment.delivered[event.ID]; !ok {
			environment.order = append(environment.order, event.ID)
		}
		environment.delivered[event.ID] = event
		return nil
	})
	key := environment.inboxKey()
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, integrationScope, key)
	require.NoError(t, err)
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[mailchimp.MemberChangedEvent](counting))
	require.NoError(t, err)
	runner, err := mailchimp.NewMemberTriggerRunner(mailchimp.MemberTriggerRunnerConfig{
		Connection: environment.connection,
		MemberChangedRoutes: []mailchimp.MemberChangedTriggerRoute{{
			BindingName: MemberChangedTriggerBinding, Configuration: environment.configuration,
			Target: target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *memberChangeEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *memberChangeEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForWatermark waits until the committed cursor has read the fake's latest change and ended its pass.
func (environment *memberChangeEnvironment) waitForWatermark(t *testing.T) {
	t.Helper()
	expected := `"watermark":"` + environment.provider.Clock().Format(time.RFC3339) + `"`
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && strings.Contains(state.Token, expected) && !strings.Contains(state.Token, "passSince")
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *memberChangeEnvironment) waitForRequests(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(environment.provider.Requests()) >= count }, 30*time.Second, 20*time.Millisecond)
}

// pollAgain runs two more polls; polls are sequential, so the first one's deliveries finish before the second.
func (environment *memberChangeEnvironment) pollAgain(t *testing.T, process *triggerProcess) {
	t.Helper()
	for range 2 {
		count := len(environment.provider.Requests())
		process.runner.WakePoller(MemberChangedTriggerBinding)
		environment.waitForRequests(t, count+1)
	}
}

func (environment *memberChangeEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by email address;
// a later change to the same contact replaces the earlier outcome.
func (environment *memberChangeEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[mailchimp.MemberChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string]ChangeOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Member.EmailAddress, outcome.EmailAddress)
		outcomes[outcome.EmailAddress] = outcome
	}
	return outcomes
}

type dexHarness struct {
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
	cache        *blobcache.Cache
}

func newDexHarness(t *testing.T, flow *Flow) *dexHarness {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8801"
	}
	harness := &dexHarness{cache: cache, workerResult: make(chan error, 1)}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, harness.worker.WaitPullRegistered(registrationContext))
	return harness
}

func integrationContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

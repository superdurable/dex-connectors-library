//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package profilechanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/linkedinprofilefake"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "linkedin-profile-changed", Kind: "live"}

// TestProfileChangesStartOneFlowEachAcrossARestartWithRealDex: the current profile is a baseline, each later
// change starts exactly one Flow that reads the verified profile back, and a change made while the process
// is stopped arrives after a restart.
func TestProfileChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newProfileChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForCommits(t, 1)
	require.Zero(t, environment.deliveryCount(), "the baseline starts no Flow")

	environment.provider.Update(func(profile *linkedinprofilefake.Profile) { profile.Name, profile.FamilyName = "Ada King", "King" })
	first.runner.WakePoller(ProfileChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, []linkedin.ProfileField{linkedin.ProfileFieldName, linkedin.ProfileFieldFamilyName}, outcomes[0].ChangedFields)
	require.NotNil(t, outcomes[0].VerifiedProfile, "the Flow reads the verified profile back from LinkedIn")
	require.Equal(t, "Ada King", outcomes[0].VerifiedProfile.Name)
	require.Equal(t, environment.subject, outcomes[0].VerifiedProfile.Subject)
	environment.waitForCommits(t, 2)
	first.stop(t)

	environment.provider.Update(func(profile *linkedinprofilefake.Profile) { profile.EmailVerified = false })
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(ProfileChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, []linkedin.ProfileField{linkedin.ProfileFieldEmailVerified}, outcomes[1].ChangedFields)
	require.Nil(t, outcomes[1].VerifiedProfile, "verifiedEmailRequired records the change without a verified profile")
	environment.waitForCommits(t, 3)
	require.Equal(t, 2, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
}

// TestAnotherMembersProfileRecordsANewBaselineWithRealDex: a connection re-authorized as another member
// reseeds instead of reporting the other member's profile as a change of the first.
func TestAnotherMembersProfileRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newProfileChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForCommits(t, 1)
	first.stop(t)

	otherSubject := environment.subject + "-other"
	environment.provider.Update(func(profile *linkedinprofilefake.Profile) {
		profile.Subject, profile.Name, profile.Email = otherSubject, "Grace Hopper", "grace@example.com"
	})
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	require.Zero(t, environment.deliveryCount(), "the other member's profile is the new baseline")

	environment.provider.Update(func(profile *linkedinprofilefake.Profile) { profile.Locale = "en-GB" })
	second.runner.WakePoller(ProfileChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, otherSubject, outcomes[0].Subject)
	require.Equal(t, []linkedin.ProfileField{linkedin.ProfileFieldLocale}, outcomes[0].ChangedFields)
	require.Equal(t, "Grace Hopper", outcomes[0].VerifiedProfile.Name)
}

type profileChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *linkedinprofilefake.UserInfo
	connection linkedin.Connection
	objects    *testsupport.ObjectStore
	subject    string
	mutex      sync.Mutex
	delivered  map[string]sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]
	order      []string
}

func newProfileChangedEnvironment(t *testing.T) *profileChangedEnvironment {
	t.Helper()
	profile := linkedinprofilefake.AdaProfile()
	// A unique subject keeps Flow IDs unique per run against a long-lived Dex server.
	profile.Subject = "ada-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	fake := linkedinprofilefake.New(t, profile)
	reference := sdkgo.ConnectionRef{Provider: "linkedin", Name: ConnectionName}
	client, err := linkedin.New(linkedin.Config{UserInfoURL: fake.UserInfoURL(), PollInterval: time.Minute}, sdkgo.StaticCredentialProvider[linkedin.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(linkedinprofilefake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := linkedin.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &profileChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, subject: profile.Subject,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]{},
	}
}

func (environment *profileChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: linkedin.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: linkedin.ProfileChangedTriggerDefinition.Trigger.TriggerName, BindingName: ProfileChangedTriggerBinding,
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
// linkedin.NewProjectProfileTriggerRunner does, over storage that survives the process.
func (environment *profileChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryProfileChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[linkedin.ProfileChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[linkedin.ProfileChangedEvent](counting))
	require.NoError(t, err)
	runner, err := linkedin.NewProfileTriggerRunner(linkedin.ProfileTriggerRunnerConfig{
		Connection: environment.connection,
		ProfileChangedRoutes: []linkedin.ProfileChangedTriggerRoute{{
			BindingName: ProfileChangedTriggerBinding,
			Target:      target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *profileChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *profileChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCommits waits until the committed cursor has at least count commits: the seed, then each change.
func (environment *profileChangedEnvironment) waitForCommits(t *testing.T, count int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.CommitCount >= count
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *profileChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then returns each change's Flow outcome in delivery order.
func (environment *profileChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) []ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[linkedin.ProfileChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := make([]ChangeOutcome, 0, len(events))
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Subject, outcome.Subject)
		outcomes = append(outcomes, outcome)
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

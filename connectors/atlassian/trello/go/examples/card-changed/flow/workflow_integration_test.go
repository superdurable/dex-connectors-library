//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cardchanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trello "github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go/internal/trelloboardfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "trello-card-changed", Kind: "live"}

// TestCardActionsStartOneFlowEachAcrossARestartWithRealDex: earlier actions are a baseline, each later
// card action starts exactly one Flow, and actions made while the process is stopped arrive after a
// restart whose first poll is rate limited.
func TestCardActionsStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newCardChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	existing := environment.board.CreateCard("Existing card")
	first := environment.startTriggerProcess(t)
	environment.waitForCursorIncluding(t, environment.board.LastActionID())

	created := environment.board.CreateCard("Replace badge reader")
	environment.board.MoveCard(created)
	environment.board.CommentOnCard(created, "Ordered the part.")
	environment.board.DeleteCard(existing)
	first.runner.WakePoller(CardChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 4)
	require.Len(t, outcomes[created], 3)
	for _, outcome := range outcomes[created] {
		require.Equal(t, trelloboardfake.DoneListID, outcome.Current.ListID, "the Flow reads the card back from Trello")
	}
	require.Equal(t, []trello.CardActionType{trello.CardActionCreated, trello.CardActionUpdated, trello.CardActionCommented},
		[]trello.CardActionType{outcomes[created][0].ActionType, outcomes[created][1].ActionType, outcomes[created][2].ActionType})
	require.Equal(t, trello.CardActionDeleted, outcomes[existing][0].ActionType)
	require.Nil(t, outcomes[existing][0].Current, "a deleted card is not read back")
	first.stop(t)

	offline := environment.board.CreateCard("Planned while stopped")
	shortLived := environment.board.CreateCard("Short-lived")
	environment.board.DeleteCard(shortLived)
	environment.board.RateLimitNextActionRequests(1)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(CardChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 7)
	require.Equal(t, "Planned while stopped", outcomes[offline][0].Current.Name)
	require.Len(t, outcomes[shortLived], 2)
	require.Equal(t, trello.CardActionCreated, outcomes[shortLived][0].ActionType)
	require.Nil(t, outcomes[shortLived][0].Current, "a card deleted after its action completes without a current state")
	require.Equal(t, 1, environment.board.RateLimitedRequests())
	environment.waitForCursorIncluding(t, environment.board.LastActionID())
	require.Equal(t, 7, environment.deliveryCount(), "no action is delivered twice and the baseline is never delivered")
}

// TestRejectedCursorRecordsANewBaselineWithRealDex: a 400 for since reseeds without delivering the gap.
func TestRejectedCursorRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newCardChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.board.CreateCard("Existing card")
	first := environment.startTriggerProcess(t)
	environment.waitForCursorIncluding(t, environment.board.LastActionID())
	first.stop(t)

	gap := environment.board.CreateCard("Lost in the gap")
	environment.board.RejectNextCursor()
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForCursorIncluding(t, environment.board.LastActionID())
	afterReset := environment.board.CreateCard("After the reset")
	second.runner.WakePoller(CardChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes[afterReset][0].Current.Name)
	require.NotContains(t, outcomes, gap, "actions between the rejected cursor and the new baseline are not delivered")
}

type cardChangedEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	board         *trelloboardfake.Board
	connection    trello.Connection
	objects       *testsupport.ObjectStore
	configuration trello.CardChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[trello.CardChangedEvent]
	order         []string
}

func newCardChangedEnvironment(t *testing.T) *cardChangedEnvironment {
	t.Helper()
	board := trelloboardfake.New(t)
	reference := sdkgo.ConnectionRef{Provider: "trello", Name: ConnectionName}
	client, err := trello.New(trello.Config{Endpoint: board.URL, PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[trello.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(trelloboardfake.APIKey), Token: sdkgo.NewSecretString(trelloboardfake.Token)},
	})
	require.NoError(t, err)
	connection, err := trello.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &cardChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, board: board, connection: connection,
		objects: testsupport.NewObjectStore(), configuration: trello.CardChangedTriggerConfiguration{BoardID: board.BoardID},
		delivered: map[string]sdkgo.TriggerEvent[trello.CardChangedEvent]{},
	}
}

func (environment *cardChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: trello.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: trello.CardChangedTriggerDefinition.Trigger.TriggerName, BindingName: CardChangedTriggerBinding,
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
// trello.NewProjectCardTriggerRunner does, over storage that survives the process.
func (environment *cardChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryCardChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[trello.CardChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[trello.CardChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[trello.CardChangedEvent](counting))
	require.NoError(t, err)
	runner, err := trello.NewCardTriggerRunner(trello.CardTriggerRunnerConfig{
		Connection: environment.connection,
		CardChangedRoutes: []trello.CardChangedTriggerRoute{{
			BindingName: CardChangedTriggerBinding, Configuration: environment.configuration,
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

func (environment *cardChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *cardChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCursorIncluding waits until the committed cursor lists actionID as delivered or baselined.
func (environment *cardChangedEnvironment) waitForCursorIncluding(t *testing.T, actionID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && strings.Contains(state.Token, `"`+actionID+`"`)
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *cardChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered actions, then for each action's Flow; outcomes are keyed by
// card ID in delivery order.
func (environment *cardChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string][]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 60*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[trello.CardChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string][]ChangeOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Card.ID, outcome.CardID)
		outcomes[outcome.CardID] = append(outcomes[outcome.CardID], outcome)
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

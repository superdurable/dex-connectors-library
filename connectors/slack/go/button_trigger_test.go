// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/internal/testlog"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// Sentinels stand in for the button value and the interaction's capabilities. No log record may contain them.
const (
	sentinelButtonValue = "SENTINEL-BUTTON-VALUE"
	sentinelResponseURL = "https://hooks.slack.com/actions/SENTINEL-RESPONSE-URL"
	sentinelTriggerID   = "SENTINEL-TRIGGER-ID"
	sentinelVerifyToken = "SENTINEL-VERIFICATION-TOKEN"
)

func TestButtonClickedTriggerDecodesBlockActionsPayload(t *testing.T) {
	source := (&Client{}).buttonClickedTriggerSource(sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"},
		ButtonClickedTriggerConfiguration{ChannelID: "C1", ActionIDs: []string{"approve", "reject"}, ClickerUserIDs: []string{"U2"}})

	threaded := decodeBlockActions(t, buttonClickPayload("C1", "1710000012.000700", "1710000000.000100", "U2", "approve", sentinelButtonValue, "1710000020.123456"))
	events, ignored := source.matchPayload(threaded)
	require.Empty(t, ignored)
	require.Equal(t, []sdkgo.TriggerEvent[ButtonClickEvent]{{
		ID:         "T1:C1:1710000012.000700:approve:1710000020.123456",
		OccurredAt: time.Unix(1710000020, 123456000).UTC(),
		Payload: ButtonClickEvent{
			TeamID: "T1", ChannelID: "C1", MessageTimestamp: "1710000012.000700", ThreadTimestamp: "1710000000.000100",
			UserID: "U2", ActionID: "approve", Value: sentinelButtonValue, ActionTimestamp: "1710000020.123456",
		},
	}}, events)

	// A top-level message reports itself as the thread root, as MessageEvent does.
	topLevel := buttonClickPayload("C1", "1710000012.000700", "", "U2", "reject", "", "1710000021.000001")
	delete(topLevel["container"].(map[string]any), "channel_id")
	events, ignored = source.matchPayload(decodeBlockActions(t, topLevel))
	require.Empty(t, ignored)
	require.Len(t, events, 1)
	require.Equal(t, "C1", events[0].Payload.ChannelID, "the channel falls back to the payload's channel object")
	require.Equal(t, "1710000012.000700", events[0].Payload.ThreadTimestamp)
	require.Empty(t, events[0].Payload.Value)

	encoded, err := json.Marshal(events[0])
	require.NoError(t, err)
	for _, secret := range []string{"SENTINEL", "hooks.slack.com", "trigger_id", "token"} {
		require.NotContains(t, string(encoded), secret, "the event carries no interaction capability")
	}
}

func TestButtonClickedTriggerIgnoresUnmatchedClicksAndLogsReasonsAtDebug(t *testing.T) {
	logs := testlog.NewLogRecorder()
	client := &Client{logger: logs.Logger()}
	source := client.buttonClickedTriggerSource(sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"},
		ButtonClickedTriggerConfiguration{ChannelID: "C1", ActionIDs: []string{"approve"}, ClickerUserIDs: []string{"U2"}})
	source.bindingName = "approval-decision"
	routes := []buttonClickedTriggerRoute{{source: source, target: sdkgo.TriggerTargetFunc[ButtonClickEvent](
		func(context.Context, sdkgo.TriggerEvent[ButtonClickEvent]) error { return nil },
	)}}
	click := func(change func(map[string]any)) map[string]any {
		payload := buttonClickPayload("C1", "5.0", "", "U2", "approve", sentinelButtonValue, "6.0")
		change(payload)
		return payload
	}
	cases := []struct {
		reason  string
		payload map[string]any
	}{
		{"not_block_actions", click(func(payload map[string]any) { payload["type"] = "view_submission" })},
		{"not_a_message", click(func(payload map[string]any) { payload["container"] = map[string]any{"type": "view", "view_id": "V1"} })},
		{"channel_mismatch", click(func(payload map[string]any) { payload["container"].(map[string]any)["channel_id"] = "C9" })},
		{"missing_user", click(func(payload map[string]any) { delete(payload, "user") })},
		{"missing_message_identity", click(func(payload map[string]any) { delete(payload, "team") })},
		{"clicker_mismatch", click(func(payload map[string]any) { payload["user"] = map[string]any{"id": "U3"} })},
		{"not_a_button", click(func(payload map[string]any) { firstAction(payload)["type"] = "static_select" })},
		{"missing_action_identity", click(func(payload map[string]any) { delete(firstAction(payload), "action_ts") })},
		{"action_mismatch", click(func(payload map[string]any) { firstAction(payload)["action_id"] = "reject" })},
		{"no_actions", click(func(payload map[string]any) { payload["actions"] = []any{} })},
	}
	for index, testCase := range cases {
		deliveries, err := decodeButtonClickedDeliveries(context.Background(), routes, interactiveEnvelope(t, "env-"+testCase.reason, testCase.payload))
		require.NoError(t, err)
		require.Empty(t, deliveries, testCase.reason)
		records := logs.Records()
		require.Len(t, records, index+1, testCase.reason)
		require.Equal(t, slog.LevelDebug, records[index].Level)
		require.Equal(t, "trigger event ignored", records[index].Message)
		require.Equal(t, testCase.reason, records[index].Attrs["reason"])
		require.Equal(t, "approval-decision", records[index].Attrs["binding"])
		require.Equal(t, "buttonClicked", records[index].Attrs["trigger"])
		require.Equal(t, "env-"+testCase.reason, records[index].Attrs["envelope_id"])
	}
	deliveries, err := decodeButtonClickedDeliveries(context.Background(), routes, interactiveEnvelope(t, "env-match", click(func(map[string]any) {})))
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Len(t, logs.Records(), len(cases), "a matched click is not logged as ignored")
	_, err = decodeButtonClickedDeliveries(context.Background(), routes, socketEnvelope{EnvelopeID: "env-bad", Type: "interactive", Payload: json.RawMessage(`[]`)})
	require.Error(t, err)
	requireNoSentinel(t, logs)
}

func TestButtonClickedTriggerConfigurationRequiresChannelAndUniqueFilters(t *testing.T) {
	require.NoError(t, ButtonClickedTriggerConfiguration{ChannelID: "C1"}.Validate())
	require.EqualError(t, ButtonClickedTriggerConfiguration{}.Validate(), "Slack channel ID is required")
	require.EqualError(t, ButtonClickedTriggerConfiguration{ChannelID: "C1", ActionIDs: []string{"approve", "approve"}}.Validate(),
		"Slack button action IDs must be non-empty and unique")
	require.EqualError(t, ButtonClickedTriggerConfiguration{ChannelID: "C1", ClickerUserIDs: []string{" "}}.Validate(),
		"Slack clicker user IDs must be non-empty and unique")
}

// TestButtonClickedTriggerFactoryRequiresDurableInbox covers the generated per-Trigger factory, whose runner
// hides any durable inbox from the source, so it refuses to acknowledge clicks it could lose.
func TestButtonClickedTriggerFactoryRequiresDurableInbox(t *testing.T) {
	var dials atomic.Int32
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) {
		dials.Add(1)
		return nil, errors.New("unused")
	})
	objects := testsupport.NewObjectStore()
	for name, target := range map[string]sdkgo.TriggerTarget[ButtonClickEvent]{
		"plain":   sdkgo.TriggerTargetFunc[ButtonClickEvent](func(context.Context, sdkgo.TriggerEvent[ButtonClickEvent]) error { return nil }),
		"durable": newMemoryDurableTarget(t, objects, "buttonClicked", "approval-decision", acceptButtonClick),
	} {
		runner := NewButtonClickedTrigger(ButtonClickedTriggerConfig{
			Connection: connection, ConnectionName: "workspace", BindingName: "approval-decision",
			Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}, Target: target,
		})
		require.ErrorIs(t, runner.Run(context.Background()), errButtonClicksRequireDurableInboxes, name)
	}
	require.Zero(t, dials.Load(), "no Socket Mode connection is opened")
}

// TestSocketModeAcknowledgesButtonClickWhileEarlierDeliveryRetries covers Slack's three-second limit: with
// durable inboxes, a root whose target is still retrying must not delay a later click's acknowledgement.
func TestSocketModeAcknowledgesButtonClickWhileEarlierDeliveryRetries(t *testing.T) {
	socket := newBlockingSocketConnection()
	var dials atomic.Int32
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) {
		dials.Add(1)
		return socket, nil
	})
	var mutex sync.Mutex
	order := []string{}
	record := func(entry string) {
		mutex.Lock()
		defer mutex.Unlock()
		order = append(order, entry)
	}
	rootStarted := make(chan struct{})
	releaseRoot := make(chan struct{})
	clicks := make(chan sdkgo.TriggerEvent[ButtonClickEvent], 1)
	objects := testsupport.NewObjectStore()
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C1"},
			Target: newMemoryDurableTarget(t, objects, "channelThreadCreated", "approval-start", sdkgo.TriggerTargetFunc[MessageEvent](
				func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
					close(rootStarted)
					<-releaseRoot
					record("root:" + event.ID)
					return nil
				})),
		}},
		ButtonClickedRoutes: []ButtonClickedTriggerRoute{{
			BindingName: "approval-decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1", ActionIDs: []string{"approve"}},
			Target: newMemoryDurableTarget(t, objects, "buttonClicked", "approval-decision", sdkgo.TriggerTargetFunc[ButtonClickEvent](
				func(_ context.Context, event sdkgo.TriggerEvent[ButtonClickEvent]) error {
					record("click:" + event.Payload.ActionID)
					clicks <- event
					return nil
				})),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()

	socket.envelopes <- messageEnvelope(t, "EvRoot", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "request approval", Timestamp: "1.0"})
	require.Equal(t, "envelope-EvRoot", receiveAcknowledgement(t, socket.acknowledgements))
	select {
	case <-rootStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("root delivery did not start")
	}
	sentAt := time.Now()
	socket.envelopes <- interactiveEnvelope(t, "env-click", buttonClickPayload("C1", "2.0", "1.0", "U2", "approve", "order-1042", "3.0"))
	require.Equal(t, "env-click", receiveAcknowledgementWithin(t, socket.acknowledgements, slackInteractiveAcknowledgementLimit))
	require.Less(t, time.Since(sentAt), time.Second, "the click is acknowledged while the root is still being delivered")
	select {
	case <-clicks:
		t.Fatal("the click overtook the root it was read after")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRoot)
	select {
	case <-clicks:
	case <-time.After(2 * time.Second):
		t.Fatal("button click was not delivered after the root")
	}
	mutex.Lock()
	require.Equal(t, []string{"root:EvRoot", "click:approve"}, order)
	mutex.Unlock()
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
	require.Equal(t, int32(1), dials.Load(), "message and button routes share one Socket Mode connection")
}

// TestSocketModeConnectionDeliversAcknowledgedClicksBeforeReturning covers a connection that ends while a
// click is still waiting for delivery: Run finishes the delivery before it reconnects.
func TestSocketModeConnectionDeliversAcknowledgedClicksBeforeReturning(t *testing.T) {
	releaseClick := make(chan struct{})
	delivered := make(chan string, 2)
	socket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 2), envelopes: []socketEnvelope{
		interactiveEnvelope(t, "env-first", buttonClickPayload("C1", "2.0", "", "U2", "approve", "", "3.0")),
		interactiveEnvelope(t, "env-second", buttonClickPayload("C1", "2.0", "", "U2", "reject", "", "4.0")),
	}}
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return socket, nil })
	objects := testsupport.NewObjectStore()
	// Separate bindings: a durable inbox holds its lock during a delivery attempt, so one binding records
	// its next click only after the attempt.
	route := func(bindingName string, actionID string) buttonClickedTriggerRoute {
		source := connection.client.buttonClickedTriggerSource(connection.reference, ButtonClickedTriggerConfiguration{ChannelID: "C1", ActionIDs: []string{actionID}})
		return buttonClickedTriggerRoute{source: source, target: newMemoryDurableTarget(t, objects, "buttonClicked", bindingName, sdkgo.TriggerTargetFunc[ButtonClickEvent](
			func(_ context.Context, event sdkgo.TriggerEvent[ButtonClickEvent]) error {
				<-releaseClick
				delivered <- event.Payload.ActionTimestamp
				return nil
			},
		))}
	}
	routes := socketModeRoutes{buttons: []buttonClickedTriggerRoute{route("approval-approve", "approve"), route("approval-reject", "reject")}}
	connectionFinished := make(chan error, 1)
	go func() {
		_, err := runSocketModeConnection(context.Background(), routes, false)
		connectionFinished <- err
	}()
	require.Equal(t, "env-first", receiveAcknowledgement(t, socket.acknowledgements))
	require.Equal(t, "env-second", receiveAcknowledgement(t, socket.acknowledgements))
	select {
	case err := <-connectionFinished:
		t.Fatalf("the connection returned before delivering its acknowledged clicks: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseClick)
	select {
	case err := <-connectionFinished:
		require.ErrorContains(t, err, "socket closed")
	case <-time.After(2 * time.Second):
		t.Fatal("the connection did not return after delivery")
	}
	require.Equal(t, "3.0", <-delivered)
	require.Equal(t, "4.0", <-delivered)
}

// TestSocketModeDurableDeliveryQueueStopsReadingAtItsBound covers a stuck delivery during a flood: the reader
// acknowledges only up to the queue bound, pauses, and delivers everything in order after release.
func TestSocketModeDurableDeliveryQueueStopsReadingAtItsBound(t *testing.T) {
	previousCapacity := socketModeDeliveryQueueCapacity
	socketModeDeliveryQueueCapacity = 3
	t.Cleanup(func() { socketModeDeliveryQueueCapacity = previousCapacity })
	socket := newBlockingSocketConnection()
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return socket, nil })
	objects := testsupport.NewObjectStore()
	stuckStarted := make(chan struct{})
	releaseStuck := make(chan struct{})
	delivered := make(chan string, 16)
	route := func(bindingName string, text string) ChannelThreadCreatedTriggerRoute {
		return ChannelThreadCreatedTriggerRoute{
			BindingName:   bindingName,
			Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C1", ThreadTriggerMatcher: MessageMatcher{MessageContains: text}},
			// The flood uses its own binding, because a durable inbox holds its lock during a delivery attempt.
			Target: newMemoryDurableTarget(t, objects, "channelThreadCreated", bindingName, sdkgo.TriggerTargetFunc[MessageEvent](
				func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
					if event.ID == "EvStuck" {
						close(stuckStarted)
						<-releaseStuck
					}
					delivered <- event.ID
					return nil
				})),
		}
	}
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection, ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{route("stuck", "stuck"), route("flood", "flood")},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()

	socket.envelopes <- messageEnvelope(t, "EvStuck", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "stuck", Timestamp: "1.0"})
	require.Equal(t, "envelope-EvStuck", receiveAcknowledgement(t, socket.acknowledgements))
	<-stuckStarted
	const floodSize = 8
	var accepted atomic.Int32
	go func() {
		for index := 1; index <= floodSize; index++ {
			eventID := "EvFlood" + strconv.Itoa(index)
			socket.envelopes <- messageEnvelope(t, eventID, messageEvent{
				Type: "message", Channel: "C1", User: "U1", Text: "flood", Timestamp: strconv.Itoa(index+1) + ".0",
			})
			accepted.Add(1)
		}
	}()
	// Three queued envelopes, plus the one the reader holds while it waits for room.
	held := socketModeDeliveryQueueCapacity + 1
	for index := 1; index <= held; index++ {
		require.Equal(t, "envelope-EvFlood"+strconv.Itoa(index), receiveAcknowledgement(t, socket.acknowledgements))
	}
	select {
	case acknowledgement := <-socket.acknowledgements:
		t.Fatalf("envelope %s was acknowledged beyond the queue bound", acknowledgement["envelope_id"])
	case <-time.After(300 * time.Millisecond):
	}
	require.Eventually(t, func() bool { return accepted.Load() >= int32(held) }, 2*time.Second, 5*time.Millisecond)
	require.Never(t, func() bool { return accepted.Load() > int32(held) }, 300*time.Millisecond, 5*time.Millisecond,
		"the reader stops reading at the bound")
	close(releaseStuck)
	require.Equal(t, "EvStuck", receiveEventID(t, delivered))
	for index := 1; index <= floodSize; index++ {
		if index > held {
			require.Equal(t, "envelope-EvFlood"+strconv.Itoa(index), receiveAcknowledgement(t, socket.acknowledgements))
		}
	}
	for index := 1; index <= floodSize; index++ {
		require.Equal(t, "EvFlood"+strconv.Itoa(index), receiveEventID(t, delivered))
	}
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
}

// fullQueueFixture runs one connection's stuck and flood routes over durable inboxes in memory, shared by
// every runner it builds, as one application's inboxes outlive its processes.
type fullQueueFixture struct {
	t         *testing.T
	objects   *testsupport.ObjectStore
	mutex     sync.Mutex
	delivered []string
}

func newFullQueueFixture(t *testing.T) *fullQueueFixture {
	t.Helper()
	previousCapacity := socketModeDeliveryQueueCapacity
	socketModeDeliveryQueueCapacity = 2
	t.Cleanup(func() { socketModeDeliveryQueueCapacity = previousCapacity })
	return &fullQueueFixture{t: t, objects: testsupport.NewObjectStore()}
}

// newRunner builds a runner whose stuck route delivers through stuckTarget and whose flood route records.
func (fixture *fullQueueFixture) newRunner(dialer socketDialer, stuckTarget func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error) *MessageTriggerRunner {
	fixture.t.Helper()
	record := func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
		fixture.mutex.Lock()
		defer fixture.mutex.Unlock()
		fixture.delivered = append(fixture.delivered, event.ID)
		return nil
	}
	route := func(bindingName string, target func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error) ChannelThreadCreatedTriggerRoute {
		return ChannelThreadCreatedTriggerRoute{
			BindingName:   bindingName,
			Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C1", ThreadTriggerMatcher: MessageMatcher{MessageContains: bindingName}},
			Target:        newMemoryDurableTarget(fixture.t, fixture.objects, "channelThreadCreated", bindingName, sdkgo.TriggerTargetFunc[MessageEvent](target)),
		}
	}
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: newFakeSocketModeConnection(fixture.t, dialer),
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{route("stuck", func(ctx context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
			if err := stuckTarget(ctx, event); err != nil {
				return err
			}
			return record(ctx, event)
		}), route("flood", record)},
	})
	require.NoError(fixture.t, err)
	return runner
}

// fillQueue sends the stuck root and then floods until the reader stops at the bound, and returns the number
// of acknowledged flood envelopes. It stops sending when stop closes.
func (fixture *fullQueueFixture) fillQueue(socket *blockingSocketConnection, stuckStarted <-chan struct{}, stop <-chan struct{}) int {
	t := fixture.t
	t.Helper()
	socket.envelopes <- messageEnvelope(t, "EvStuck", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "stuck", Timestamp: "1.0"})
	require.Equal(t, "envelope-EvStuck", receiveAcknowledgement(t, socket.acknowledgements))
	<-stuckStarted
	go func() {
		for index := 1; index <= 10; index++ {
			select {
			case socket.envelopes <- floodEnvelope(t, index):
			case <-stop:
				return
			case <-socket.closed:
				return
			}
		}
	}()
	held := socketModeDeliveryQueueCapacity + 1
	for index := 1; index <= held; index++ {
		require.Equal(t, "envelope-EvFlood"+strconv.Itoa(index), receiveAcknowledgement(t, socket.acknowledgements))
	}
	select {
	case acknowledgement := <-socket.acknowledgements:
		t.Fatalf("envelope %s was acknowledged beyond the queue bound", acknowledgement["envelope_id"])
	case <-time.After(200 * time.Millisecond):
	}
	return held
}

func (fixture *fullQueueFixture) deliveredEventIDs() []string {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	return append([]string(nil), fixture.delivered...)
}

func (fixture *fullQueueFixture) pendingEventIDs() []string {
	t := fixture.t
	t.Helper()
	eventIDs := []string{}
	for _, bindingName := range []string{"stuck", "flood"} {
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "workspace", TriggerName: "channelThreadCreated", BindingName: bindingName}
		inbox, err := projectconfig.NewTriggerInbox(fixture.objects, projectSlackScope, key)
		require.NoError(t, err)
		pending, err := inbox.Pending(context.Background())
		require.NoError(t, err)
		for _, event := range pending {
			eventIDs = append(eventIDs, event.ID)
		}
	}
	return eventIDs
}

func floodEnvelope(t *testing.T, index int) socketEnvelope {
	return messageEnvelope(t, "EvFlood"+strconv.Itoa(index), messageEvent{
		Type: "message", Channel: "C1", User: "U1", Text: "flood", Timestamp: strconv.Itoa(index+1) + ".0",
	})
}

func floodEventIDs(from int, through int) []string {
	eventIDs := []string{}
	for index := from; index <= through; index++ {
		eventIDs = append(eventIDs, "EvFlood"+strconv.Itoa(index))
	}
	return eventIDs
}

// TestSocketModeFullQueueCancellationReplaysAcknowledgedEventsAfterRestart cancels Run while the reader waits
// for room in a full queue. Run returns, and the next process replays every acknowledged event once, in order.
func TestSocketModeFullQueueCancellationReplaysAcknowledgedEventsAfterRestart(t *testing.T) {
	fixture := newFullQueueFixture(t)
	firstSocket := newBlockingSocketConnection()
	stuckStarted := make(chan struct{})
	firstRunner := fixture.newRunner(func(context.Context, string) (socketConnection, error) { return firstSocket, nil },
		func(ctx context.Context, _ sdkgo.TriggerEvent[MessageEvent]) error {
			close(stuckStarted)
			<-ctx.Done()
			return ctx.Err()
		})
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- firstRunner.Run(ctx) }()
	stopFlood := make(chan struct{})
	defer close(stopFlood)
	held := fixture.fillQueue(firstSocket, stuckStarted, stopFlood)
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled, "Run returns while the reader waits for room")
	require.Empty(t, fixture.deliveredEventIDs())
	acknowledged := append([]string{"EvStuck"}, floodEventIDs(1, held)...)
	require.Equal(t, acknowledged, fixture.pendingEventIDs(), "every acknowledged event stays in its inbox")

	secondSocket := newBlockingSocketConnection()
	secondRunner := fixture.newRunner(func(context.Context, string) (socketConnection, error) { return secondSocket, nil },
		func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil })
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() { runFinished <- secondRunner.Run(ctx) }()
	// Slack resends the first envelope that was never acknowledged.
	secondSocket.envelopes <- floodEnvelope(t, held+1)
	require.Equal(t, "envelope-EvFlood"+strconv.Itoa(held+1), receiveAcknowledgement(t, secondSocket.acknowledgements))
	expected := append(acknowledged, "EvFlood"+strconv.Itoa(held+1))
	require.Eventually(t, func() bool { return len(fixture.deliveredEventIDs()) >= len(expected) }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, expected, fixture.deliveredEventIDs(), "replay delivers every acknowledged event once, in order, before live ones")
	require.Empty(t, fixture.pendingEventIDs())
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
}

// TestSocketModeFullQueueDisconnectDeliversAcknowledgedEventsBeforeReconnecting drops the socket while the
// reader waits for room. After the stuck delivery ends, the connection delivers what it acknowledged, then
// reconnects and receives the resent envelopes, with nothing lost or duplicated.
func TestSocketModeFullQueueDisconnectDeliversAcknowledgedEventsBeforeReconnecting(t *testing.T) {
	shortenSocketModeTimings(t, 10*time.Millisecond, 10*time.Millisecond, 45*time.Second)
	fixture := newFullQueueFixture(t)
	sockets := []*blockingSocketConnection{newBlockingSocketConnection(), newBlockingSocketConnection()}
	var dials atomic.Int32
	stuckStarted := make(chan struct{})
	releaseStuck := make(chan struct{})
	var stuckCalls atomic.Int32
	runner := fixture.newRunner(func(context.Context, string) (socketConnection, error) {
		dial := int(dials.Add(1)) - 1
		if dial >= len(sockets) {
			return nil, errors.New("no more Slack sockets")
		}
		return sockets[dial], nil
	}, func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
		if stuckCalls.Add(1) == 1 {
			close(stuckStarted)
			<-releaseStuck
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	stopFlood := make(chan struct{})
	defer close(stopFlood)
	held := fixture.fillQueue(sockets[0], stuckStarted, stopFlood)
	require.NoError(t, sockets[0].Close())
	close(releaseStuck)
	acknowledged := append([]string{"EvStuck"}, floodEventIDs(1, held)...)
	require.Eventually(t, func() bool { return dials.Load() == 2 }, 2*time.Second, 5*time.Millisecond, "the runner reconnects")
	require.Equal(t, acknowledged, fixture.deliveredEventIDs(), "the connection delivered what it acknowledged before reconnecting")
	require.Empty(t, fixture.pendingEventIDs())
	for index := held + 1; index <= held+2; index++ {
		sockets[1].envelopes <- floodEnvelope(t, index)
		require.Equal(t, "envelope-EvFlood"+strconv.Itoa(index), receiveAcknowledgement(t, sockets[1].acknowledgements))
	}
	expected := append(acknowledged, floodEventIDs(held+1, held+2)...)
	require.Eventually(t, func() bool { return len(fixture.deliveredEventIDs()) >= len(expected) }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, expected, fixture.deliveredEventIDs())
	require.Equal(t, int32(1), stuckCalls.Load(), "the stuck root is delivered once")
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
}

// TestSocketModeWithoutDurableInboxesDeliversEachEventBeforeReadingTheNext keeps main's semantics for plain
// targets: at most one acknowledged event waits for delivery, so a stopped process loses at most one.
func TestSocketModeWithoutDurableInboxesDeliversEachEventBeforeReadingTheNext(t *testing.T) {
	socket := newBlockingSocketConnection()
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return socket, nil })
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	delivered := make(chan string, 2)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C1"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				if event.ID == "EvFirst" {
					close(firstStarted)
					<-releaseFirst
				}
				delivered <- event.ID
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()

	socket.envelopes <- messageEnvelope(t, "EvFirst", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "first", Timestamp: "1.0"})
	require.Equal(t, "envelope-EvFirst", receiveAcknowledgement(t, socket.acknowledgements))
	<-firstStarted
	secondRead := make(chan struct{})
	go func() {
		socket.envelopes <- messageEnvelope(t, "EvSecond", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "second", Timestamp: "2.0"})
		close(secondRead)
	}()
	select {
	case <-secondRead:
		t.Fatal("the reader read a later envelope while an event without a durable inbox awaited delivery")
	case acknowledgement := <-socket.acknowledgements:
		t.Fatalf("envelope %s was acknowledged while an earlier event awaited delivery", acknowledgement["envelope_id"])
	case <-time.After(300 * time.Millisecond):
	}
	close(releaseFirst)
	require.Equal(t, "EvFirst", receiveEventID(t, delivered))
	require.Equal(t, "envelope-EvSecond", receiveAcknowledgement(t, socket.acknowledgements))
	require.Equal(t, "EvSecond", receiveEventID(t, delivered))
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
}

// TestProjectRunnerRecordsButtonClickBeforeAcknowledgingAndReplaysAfterRestart covers a process that stops
// after Slack's acknowledgement but before the click reached the application.
func TestProjectRunnerRecordsButtonClickBeforeAcknowledgingAndReplaysAfterRestart(t *testing.T) {
	fixture := newProjectSlackFixture(t)
	clickEnvelope := interactiveEnvelope(t, "env-click", buttonClickPayload("C1", "2.0", "1.0", "U2", "approve", "order-1042", "3.0"))
	stuck := make(chan struct{})
	firstSocket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{clickEnvelope}}
	firstRunner := fixture.newRunner(t, ProjectMessageTriggerRunnerConfig{ButtonClickedRoutes: []ProjectButtonClickedTriggerRoute{{
		BindingName: "approval-decision",
		Target: sdkgo.TriggerTargetFunc[ButtonClickEvent](func(ctx context.Context, _ sdkgo.TriggerEvent[ButtonClickEvent]) error {
			close(stuck)
			<-ctx.Done()
			return ctx.Err()
		}),
	}}}, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) { return firstSocket, nil }
	})
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- firstRunner.Run(ctx) }()
	require.Equal(t, "env-click", receiveAcknowledgement(t, firstSocket.acknowledgements))
	<-stuck
	const clickID = "T1:C1:2.0:approve:3.0"
	require.Equal(t, []string{clickID}, fixture.pendingButtonEventIDs(t), "the click was recorded before its acknowledgement")
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)

	replayed := make(chan sdkgo.TriggerEvent[ButtonClickEvent], 1)
	secondRunner := fixture.newRunner(t, ProjectMessageTriggerRunnerConfig{ButtonClickedRoutes: []ProjectButtonClickedTriggerRoute{{
		BindingName: "approval-decision",
		Target: sdkgo.TriggerTargetFunc[ButtonClickEvent](func(_ context.Context, event sdkgo.TriggerEvent[ButtonClickEvent]) error {
			replayed <- event
			return nil
		}),
	}}}, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) {
			return nil, errors.New("Slack is unreachable")
		}
	})
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() { runFinished <- secondRunner.Run(ctx) }()
	select {
	case event := <-replayed:
		require.Equal(t, clickID, event.ID)
		require.Equal(t, "order-1042", event.Payload.Value)
	case <-time.After(2 * time.Second):
		t.Fatal("the recorded click was not replayed")
	}
	require.Eventually(t, func() bool { return len(fixture.pendingButtonEventIDs(t)) == 0 }, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
}

func TestRelayedRunnerRefusesButtonClickedRoutes(t *testing.T) {
	host := newRelayHost(t, nil)
	runner := host.newRunner(t, ProjectMessageTriggerRunnerConfig{ButtonClickedRoutes: []ProjectButtonClickedTriggerRoute{{
		BindingName: "approval-decision",
		Target:      sdkgo.TriggerTargetFunc[ButtonClickEvent](func(context.Context, sdkgo.TriggerEvent[ButtonClickEvent]) error { return nil }),
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := runner.Run(ctx)
	require.ErrorContains(t, err, "button clicks reach only Socket Mode")
	require.Zero(t, host.registrationCount(), "the runner does not register a relay that cannot deliver clicks")
}

func TestMessageTriggerRunnerRejectsInvalidButtonRoutes(t *testing.T) {
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return nil, errors.New("unused") })
	plainTarget := sdkgo.TriggerTargetFunc[ButtonClickEvent](func(context.Context, sdkgo.TriggerEvent[ButtonClickEvent]) error { return nil })
	target := newMemoryDurableTarget(t, testsupport.NewObjectStore(), "buttonClicked", "decision", acceptButtonClick)
	_, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ButtonClickedRoutes: []ButtonClickedTriggerRoute{
		{BindingName: "decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}, Target: target},
		{BindingName: "decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}, Target: target},
	}})
	require.ErrorContains(t, err, "duplicated")
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ButtonClickedRoutes: []ButtonClickedTriggerRoute{
		{BindingName: "decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}},
	}})
	require.ErrorContains(t, err, "binding name and target are required")
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ButtonClickedRoutes: []ButtonClickedTriggerRoute{
		{BindingName: "decision", Target: target},
	}})
	require.EqualError(t, err, "Slack channel ID is required")
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ButtonClickedRoutes: []ButtonClickedTriggerRoute{
		{BindingName: "decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}, Target: plainTarget},
	}})
	require.ErrorIs(t, err, errButtonClicksRequireDurableInboxes, "a button route needs a durable inbox")
	plainRoot := []ChannelThreadCreatedTriggerRoute{{
		BindingName: "start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C1"},
		Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil }),
	}}
	durableButton := []ButtonClickedTriggerRoute{{BindingName: "decision", Configuration: ButtonClickedTriggerConfiguration{ChannelID: "C1"}, Target: target}}
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ChannelThreadCreatedRoutes: plainRoot, ButtonClickedRoutes: durableButton})
	require.ErrorIs(t, err, errButtonClicksRequireDurableInboxes, "a message route sharing the click's socket needs a durable inbox too")
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ChannelThreadCreatedRoutes: plainRoot})
	require.NoError(t, err, "message routes alone keep plain targets")
	_, err = NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, ButtonClickedRoutes: durableButton})
	require.NoError(t, err)
}

var acceptButtonClick = sdkgo.TriggerTargetFunc[ButtonClickEvent](func(context.Context, sdkgo.TriggerEvent[ButtonClickEvent]) error { return nil })

// newMemoryDurableTarget wraps target in a durable inbox kept in memory, as NewProjectMessageTriggerRunner does.
func newMemoryDurableTarget[E any](
	t *testing.T, objects *testsupport.ObjectStore, triggerName string, bindingName string, target sdkgo.TriggerTarget[E],
) sdkgo.TriggerTarget[E] {
	t.Helper()
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "workspace", TriggerName: triggerName, BindingName: bindingName}
	inbox, err := projectconfig.NewTriggerInbox(objects, projectSlackScope, key)
	require.NoError(t, err)
	durable, err := provider.NewDurableTriggerTarget(inbox, key, target)
	require.NoError(t, err)
	return durable
}

// buttonClickPayload is a block_actions payload with Slack's documented fields, the assumed thread_ts
// fields, and the fields the connector must never copy into an event or a log record.
func buttonClickPayload(channelID string, messageTimestamp string, threadTimestamp string, userID string, actionID string, value string, actionTimestamp string) map[string]any {
	container := map[string]any{"type": "message", "message_ts": messageTimestamp, "channel_id": channelID, "is_ephemeral": false}
	message := map[string]any{"type": "message", "ts": messageTimestamp, "user": "UBOT", "bot_id": "B1", "text": sentinelText}
	if threadTimestamp != "" {
		container["thread_ts"] = threadTimestamp
		message["thread_ts"] = threadTimestamp
	}
	action := map[string]any{
		"type": "button", "action_id": actionID, "block_id": "decision", "action_ts": actionTimestamp,
		"text": map[string]any{"type": "plain_text", "text": "Approve", "emoji": true},
	}
	if value != "" {
		action["value"] = value
	}
	return map[string]any{
		"type": "block_actions", "token": sentinelVerifyToken, "trigger_id": sentinelTriggerID, "response_url": sentinelResponseURL,
		"api_app_id": "A1", "team": map[string]any{"id": "T1", "domain": "acme"},
		"user":      map[string]any{"id": userID, "username": "approver", "team_id": "T1"},
		"channel":   map[string]any{"id": channelID, "name": "approvals"},
		"container": container, "message": message, "actions": []any{action},
	}
}

func firstAction(payload map[string]any) map[string]any {
	return payload["actions"].([]any)[0].(map[string]any)
}

func interactiveEnvelope(t *testing.T, envelopeID string, payload map[string]any) socketEnvelope {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return socketEnvelope{EnvelopeID: envelopeID, Type: "interactive", Payload: encoded}
}

func decodeBlockActions(t *testing.T, payload map[string]any) blockActionsPayload {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	var decoded blockActionsPayload
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded
}

// blockingSocketConnection reads envelopes as the test sends them and blocks until it is closed, as a quiet
// Slack connection does.
type blockingSocketConnection struct {
	envelopes        chan socketEnvelope
	acknowledgements chan map[string]string
	closed           chan struct{}
	closeOnce        sync.Once
}

func newBlockingSocketConnection() *blockingSocketConnection {
	return &blockingSocketConnection{
		envelopes: make(chan socketEnvelope), acknowledgements: make(chan map[string]string, 8), closed: make(chan struct{}),
	}
}

func (connection *blockingSocketConnection) ReadJSON(destination any) error {
	// A closed connection reads nothing more, even while a test still offers an envelope.
	select {
	case <-connection.closed:
		return errors.New("socket closed")
	default:
	}
	select {
	case <-connection.closed:
		return errors.New("socket closed")
	case envelope := <-connection.envelopes:
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, destination)
	}
}

func (connection *blockingSocketConnection) WriteJSON(value any) error {
	acknowledgement, ok := value.(map[string]string)
	if !ok || strings.TrimSpace(acknowledgement["envelope_id"]) == "" {
		return errors.New("unexpected acknowledgement")
	}
	connection.acknowledgements <- acknowledgement
	return nil
}

func (*blockingSocketConnection) SetReadDeadline(time.Time) error { return nil }

func (connection *blockingSocketConnection) Close() error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

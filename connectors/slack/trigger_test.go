// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestChannelThreadTriggerMatchesChannelPosterAndText(t *testing.T) {
	source := &messageTriggerSource{
		channelID: "C123", matcher: MessageMatcher{MessageContains: "request approval", PosterUserIDs: []string{"U1"}},
	}
	envelope := messageEnvelope(t, "Ev1", messageEvent{Type: "message", Channel: "C123", User: "U1", Text: "Please REQUEST APPROVAL for invoice 42", Timestamp: "1.0"})
	matched, event, err := source.decodeEvent(envelope)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "Ev1", event.ID)
	require.Equal(t, "1.0", event.Payload.ThreadTimestamp)
}

func TestThreadReplyTriggerIgnoresRootBotAndDisallowedPoster(t *testing.T) {
	source := &messageTriggerSource{
		channelID: "C123", requiresThread: true,
		matcher: MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}},
	}
	cases := []messageEvent{
		{Type: "message", Channel: "C123", User: "U2", Text: "approve", Timestamp: "1.0"},
		{Type: "message", Channel: "C123", User: "U2", BotID: "B1", Text: "approve", Timestamp: "2.0", ThreadTS: "1.0"},
		{Type: "message", Channel: "C123", User: "U3", Text: "approve", Timestamp: "2.0", ThreadTS: "1.0"},
	}
	for index, message := range cases {
		matched, _, err := source.decodeEvent(messageEnvelope(t, "Ev", message))
		require.NoError(t, err)
		require.False(t, matched, "case %d", index)
	}
	matched, _, err := source.decodeEvent(messageEnvelope(t, "Ev4", messageEvent{Type: "message", Channel: "C123", User: "U2", Text: "disapprove", Timestamp: "2.0", ThreadTS: "1.0"}))
	require.NoError(t, err)
	require.True(t, matched)
}

func TestThreadReplyConfigurationRequiresApprover(t *testing.T) {
	err := (ThreadReplyCreatedTriggerConfiguration{ChannelID: "C123"}).Validate()
	require.ErrorContains(t, err, "requires at least one")
}

func TestFlowIDByThreadUsesApplicationResolver(t *testing.T) {
	resolver := FlowIDByThread(func(identity ThreadIdentity) (string, error) {
		return identity.TeamID + "/" + identity.ChannelID + "/" + identity.RootTimestamp, nil
	})
	flowID, err := resolver(sdkgo.TriggerEvent[MessageEvent]{Payload: MessageEvent{
		TeamID: "T1", ChannelID: "C1", ThreadTimestamp: "1.0",
	}})
	require.NoError(t, err)
	require.Equal(t, "T1/C1/1.0", flowID)
}

func TestSocketModeReconnectsDeliversAndAcknowledgesMatchedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer app-token", request.Header.Get("Authorization"))
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	defer server.Close()
	first := &fakeSocketConnection{readErr: errors.New("connection lost")}
	second := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{messageEnvelope(t, "Ev2", messageEvent{
		Type: "message", Channel: "C123", User: "U1", Text: "request approval", Timestamp: "1.0",
	})}}
	var dialMu sync.Mutex
	dialCount := 0
	dialer := func(context.Context, string) (socketConnection, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		dialCount++
		if dialCount == 1 {
			return first, nil
		}
		return second, nil
	}
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token")},
	}, func(options *clientOptions) { options.socketDialer = dialer })
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	delivered := make(chan sdkgo.TriggerEvent[MessageEvent], 1)
	runner := NewChannelThreadCreatedTrigger(ChannelThreadCreatedTriggerConfig{
		Connection: connection, ConnectionName: "workspace", BindingName: "approval-start",
		Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123", ThreadTriggerMatcher: MessageMatcher{MessageContains: "request approval"}},
		Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
			delivered <- event
			cancel()
			return nil
		}),
	})
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	select {
	case event := <-delivered:
		require.Equal(t, "Ev2", event.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("matched Slack event was not delivered")
	}
	select {
	case acknowledgement := <-second.acknowledgements:
		require.Equal(t, "envelope-Ev2", acknowledgement["envelope_id"])
	case <-time.After(time.Second):
		t.Fatal("Slack envelope was not acknowledged")
	}
	select {
	case runErr := <-runFinished:
		require.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("trigger runner did not stop")
	}
}

func TestSocketModeAcknowledgesBeforeTargetCompletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	defer server.Close()
	connection := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{messageEnvelope(t, "Ev3", messageEvent{
		Type: "message", Channel: "C123", User: "U1", Text: "request approval", Timestamp: "1.0",
	})}}
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token")},
	}, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) { return connection, nil }
	})
	require.NoError(t, err)
	configuredConnection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	targetStarted := make(chan struct{})
	releaseTarget := make(chan struct{})
	runner := NewChannelThreadCreatedTrigger(ChannelThreadCreatedTriggerConfig{
		Connection: configuredConnection, BindingName: "approval-start",
		Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
		Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
			close(targetStarted)
			<-releaseTarget
			return nil
		}),
	})
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	select {
	case <-targetStarted:
	case <-time.After(time.Second):
		t.Fatal("trigger target did not start")
	}
	select {
	case acknowledgement := <-connection.acknowledgements:
		require.Equal(t, "envelope-Ev3", acknowledgement["envelope_id"])
	case <-time.After(time.Second):
		t.Fatal("Slack envelope was not acknowledged before target completion")
	}
	close(releaseTarget)
	cancel()
	select {
	case <-runFinished:
	case <-time.After(time.Second):
		t.Fatal("trigger runner did not stop")
	}
}

func TestTriggerDeliveryRetriesAfterAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	attempts := 0
	err := deliverTriggerEvent(ctx, sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary Dex failure")
		}
		return nil
	}), sdkgo.TriggerEvent[MessageEvent]{ID: "Ev4"})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
}

type fakeSocketConnection struct {
	envelopes        []socketEnvelope
	readErr          error
	acknowledgements chan map[string]string
}

func (connection *fakeSocketConnection) ReadJSON(destination any) error {
	if len(connection.envelopes) == 0 {
		if connection.readErr != nil {
			return connection.readErr
		}
		return errors.New("socket closed")
	}
	envelope := connection.envelopes[0]
	connection.envelopes = connection.envelopes[1:]
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, destination)
}

func (connection *fakeSocketConnection) WriteJSON(value any) error {
	if connection.acknowledgements == nil {
		connection.acknowledgements = make(chan map[string]string, 1)
	}
	acknowledgement, ok := value.(map[string]string)
	if !ok {
		return errors.New("unexpected acknowledgement")
	}
	connection.acknowledgements <- acknowledgement
	return nil
}

func (*fakeSocketConnection) SetReadDeadline(time.Time) error { return nil }

func (*fakeSocketConnection) Close() error { return nil }

func messageEnvelope(t *testing.T, eventID string, message messageEvent) socketEnvelope {
	t.Helper()
	payload, err := json.Marshal(eventsAPIPayload{EventID: eventID, EventTime: 42, TeamID: "T1", Event: message})
	require.NoError(t, err)
	return socketEnvelope{EnvelopeID: "envelope-" + eventID, Type: "events_api", Payload: payload}
}

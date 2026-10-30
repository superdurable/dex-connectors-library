// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// relayedTriggerPathPrefix is followed by the unpadded base64url connection name.
	relayedTriggerPathPrefix = "/triggers/slack/"
	// relayedTriggerRegistrationPath is the host's registration endpoint on its credential broker's origin.
	relayedTriggerRegistrationPath = "/internal/slack-triggers/register"
	// maxRelayedEventBytes bounds one relayed Events API callback, as the host bounds what Slack sends it.
	maxRelayedEventBytes = 1 << 20
	// relayedEnvelopeID stands in for the Socket Mode envelope ID, which a relayed event has none of.
	relayedEnvelopeID = "relayed"
)

// relayedSourceStartPoll is how often the receiver checks that its source runs before it registers.
var relayedSourceStartPoll = 10 * time.Millisecond

// RelayedTriggerPath returns the path at which an application serves the Slack events that a host relays for
// connectionName: "/triggers/slack/" followed by the connection name in unpadded base64url, such as
// "/triggers/slack/c3VwcG9ydA" for "support". Mount the runner from NewProjectMessageTriggerRunner there on the
// application's HTTP server.
func RelayedTriggerPath(connectionName string) string {
	return relayedTriggerPathPrefix + base64.RawURLEncoding.EncodeToString([]byte(connectionName))
}

// relayedMessageReceiver receives one connection's relayed Events API callbacks and delivers them to the
// runner's routes, as Socket Mode does.
type relayedMessageReceiver struct {
	project  *projectconfig.LoadedProject
	key      projectconfig.ConnectionKey
	routes   []messageTriggerRoute
	endpoint *webhooktrigger.Endpoint[struct{}, []pendingMessageTriggerDelivery]
	source   sdkgo.TriggerSource[[]pendingMessageTriggerDelivery]
}

func newRelayedMessageReceiver(project *projectconfig.LoadedProject, connection Connection, routes []messageTriggerRoute) (*relayedMessageReceiver, error) {
	receiver := &relayedMessageReceiver{
		project: project, routes: routes,
		key: projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name},
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[struct{}, []pendingMessageTriggerDelivery]{
		ConnectorID: ConnectorID, TriggerName: "relayedMessage", Connection: connection.reference,
		// The relay's signature uses the scope token, so the public route never reads a Slack credential.
		Credentials:          sdkgo.StaticCredentialProvider[struct{}]{connection.reference: {}},
		MaxBodyBytes:         maxRelayedEventBytes,
		VerifyRequestContext: receiver.verifyRequest,
		DecodeEvent:          receiver.decodeEvent,
		Now:                  connection.client.now,
		Logger:               connection.client.logger,
	})
	if err != nil {
		return nil, err
	}
	receiver.endpoint = endpoint
	receiver.source = endpoint.NewSource(nil)
	return receiver, nil
}

// selectRelayedMode reads the connection's credential, retrying with the Socket Mode backoff while it is
// unavailable, and reports whether the connection lacks an app_token and so receives relayed events.
func (receiver *relayedMessageReceiver) selectRelayedMode(ctx context.Context) (bool, error) {
	source := receiver.routes[0].source
	for failures := 1; ; failures++ {
		credentials, err := sdkgo.ResolveCredential(ctx, source.client.credentials, source.credentialCall(), source.client.refreshDriver)
		switch {
		case err == nil && credentials.AppToken.Reveal() != "":
			return false, nil
		case err == nil && !receiver.isShared():
			return false, fmt.Errorf("Slack connection %q has no app_token for Socket Mode and is not a shared connection whose host relays its events", receiver.key.ConnectionName)
		case err == nil:
			return true, nil
		case ctx.Err() != nil:
			return false, ctx.Err()
		case errors.Is(err, projectconfig.ErrConnectionNotSelected):
			return false, fmt.Errorf("Slack connection %q: %w", receiver.key.ConnectionName, err)
		}
		delay := socketModeReconnectBackoff(failures)
		// A credential provider's error may quote what it read, so only its absence is reported.
		source.log(ctx, slog.LevelWarn, "slack trigger credentials unavailable; retrying", source.connectionAttrs(),
			slog.Int("attempt", failures), slog.Duration("delay", delay))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func (receiver *relayedMessageReceiver) isShared() bool {
	for _, connection := range receiver.project.Configuration.Connections {
		if connection.ConnectorID == receiver.key.ConnectorID && connection.ConnectionName == receiver.key.ConnectionName {
			return connection.SharedConnectionID != ""
		}
	}
	return false
}

// run replays the routes' inboxes, receives relayed events, and registers the connection name once its source
// runs. It returns a terminal error when the host refuses the registration.
func (receiver *relayedMessageReceiver) run(ctx context.Context) error {
	if err := replayMessageTriggerRoutes(ctx, receiver.routes); err != nil {
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	sourceDone := make(chan error, 1)
	go func() { sourceDone <- receiver.source.Run(runContext, relayedDeliveryTarget{routes: receiver.routes}) }()
	if err := receiver.register(runContext, sourceDone); err != nil {
		cancel()
		<-sourceDone
		return err
	}
	return <-sourceDone
}

// register waits until the source runs, then registers the connection name, retrying while the host is
// unavailable. It returns early with the source's error when the source stops.
func (receiver *relayedMessageReceiver) register(ctx context.Context, sourceDone <-chan error) error {
	source := receiver.routes[0].source
	for receiver.endpoint.RunningSourceCount() == 0 {
		select {
		case err := <-sourceDone:
			return err
		case <-time.After(relayedSourceStartPoll):
		}
	}
	for failures := 1; ; failures++ {
		err := receiver.project.RegisterRelayedTrigger(ctx, receiver.key, relayedTriggerRegistrationPath)
		switch {
		case err == nil:
			source.log(ctx, slog.LevelInfo, "slack relayed trigger registered", source.connectionAttrs())
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, projectconfig.ErrRelayedTriggerNotRelayed):
			return fmt.Errorf("Slack connection %q has no app_token for Socket Mode and its host does not relay its events", receiver.key.ConnectionName)
		case !errors.Is(err, projectconfig.ErrCredentialBrokerUnavailable):
			return fmt.Errorf("register Slack relayed trigger for connection %q: %w", receiver.key.ConnectionName, err)
		}
		delay := socketModeReconnectBackoff(failures)
		source.log(ctx, slog.LevelWarn, "slack relayed trigger registration failed; retrying", source.connectionAttrs(),
			slog.Int("attempt", failures), slog.Duration("delay", delay), slog.String("error", err.Error()))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case err := <-sourceDone:
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

// verifyRequest checks the host's signature. A token that cannot be read is retryable for the host.
func (receiver *relayedMessageReceiver) verifyRequest(ctx context.Context, request webhooktrigger.Request, _ struct{}) error {
	err := receiver.project.VerifyRelayedRequest(ctx, request.Header, request.Body, request.ReceivedAt)
	if err == nil {
		return nil
	}
	// The route is public, so anyone can send a bad signature; only the application's own failure is a warning.
	level, reason := slog.LevelDebug, "invalid_signature"
	if errors.Is(err, projectconfig.ErrCredentialBrokerUnavailable) {
		level, reason = slog.LevelWarn, "verification_unavailable"
		err = fmt.Errorf("%w: %w", webhooktrigger.ErrVerificationUnavailable, err)
	}
	source := receiver.routes[0].source
	source.log(ctx, level, "slack relayed request refused", source.connectionAttrs(), slog.String("reason", reason))
	return err
}

// decodeEvent matches a relayed Events API callback, which has the Socket Mode payload's shape, against the
// routes. An event that no route accepts is acknowledged without being recorded.
func (receiver *relayedMessageReceiver) decodeEvent(request webhooktrigger.Request) (sdkgo.TriggerEvent[[]pendingMessageTriggerDelivery], bool, error) {
	envelope := socketEnvelope{EnvelopeID: relayedEnvelopeID, Type: "events_api", Payload: request.Body}
	// The endpoint passes no context to DecodeEvent; matching only uses it to decide whether to log at DEBUG.
	deliveries, err := decodeMessageTriggerDeliveries(context.Background(), receiver.routes, envelope)
	if err != nil || len(deliveries) == 0 {
		return sdkgo.TriggerEvent[[]pendingMessageTriggerDelivery]{}, false, err
	}
	first := deliveries[0].event
	return sdkgo.TriggerEvent[[]pendingMessageTriggerDelivery]{ID: first.ID, OccurredAt: first.OccurredAt, Payload: deliveries}, true, nil
}

// relayedDeliveryTarget records a relayed event in every matching route's durable inbox, then delivers it to
// those routes inline in route order, so a root reaches Dex before any reply received after it.
type relayedDeliveryTarget struct {
	routes []messageTriggerRoute
}

// PrepareTrigger records the event in every matching route's durable inbox before the host receives 200.
func (target relayedDeliveryTarget) PrepareTrigger(ctx context.Context, event sdkgo.TriggerEvent[[]pendingMessageTriggerDelivery]) error {
	for _, delivery := range event.Payload {
		if err := sdkgo.PrepareTriggerDelivery(ctx, delivery.target, delivery.event); err != nil {
			return err
		}
	}
	return nil
}

// HandleTrigger delivers the event to each matching route in route order.
func (target relayedDeliveryTarget) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[[]pendingMessageTriggerDelivery]) error {
	return deliverMessageTriggerDeliveries(ctx, event.Payload)
}

// ReplayTriggerDeliveries delivers every route's pending inbox events, roots first.
func (target relayedDeliveryTarget) ReplayTriggerDeliveries(ctx context.Context) error {
	return replayMessageTriggerRoutes(ctx, target.routes)
}

// ServeHTTP receives the Slack events that a host relays for the runner's connection, at RelayedTriggerPath. It
// answers 200 once every matching binding's durable inbox recorded the event, 400 for a request without a
// valid host signature or with an invalid body, 413 for a body over 1 MiB, 405 for another method, and 503
// while the runner does not receive relayed events, such as before Run starts, while it replays the inboxes,
// or when the connection uses Socket Mode. A runner built without a project configuration answers 404.
func (runner *MessageTriggerRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if runner.relay == nil {
		http.NotFound(response, request)
		return
	}
	runner.relay.endpoint.ServeHTTP(response, request)
}

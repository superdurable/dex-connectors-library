// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MessageMatcher filters Slack messages by text and posting user.
type MessageMatcher struct {
	// MessageContains requires this case-insensitive substring when non-empty.
	MessageContains string `json:"messageContains,omitempty"`
	// PosterUserIDs limits matches to these Slack user IDs when non-empty.
	PosterUserIDs []string `json:"posterUserIds,omitempty"`
}

// ChannelThreadCreatedTriggerConfiguration configures channel thread created trigger.
type ChannelThreadCreatedTriggerConfiguration struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadTriggerMatcher specifies thread trigger matcher for channel thread created trigger configuration.
	ThreadTriggerMatcher MessageMatcher `json:"threadTriggerMatcher"`
}

// ThreadReplyCreatedTriggerConfiguration configures thread reply created trigger.
type ThreadReplyCreatedTriggerConfiguration struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadReplyMatcher specifies thread reply matcher for thread reply created trigger configuration.
	ThreadReplyMatcher MessageMatcher `json:"threadReplyMatcher"`
}

// MessageEvent is one normalized message provider event.
type MessageEvent struct {
	// TeamID is the team ID returned by Slack.
	TeamID string `json:"teamId"`
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// Timestamp is the Slack message timestamp identifier.
	Timestamp string `json:"timestamp"`
	// ThreadTimestamp is the Slack parent message timestamp.
	ThreadTimestamp string `json:"threadTimestamp"`
	// UserID is the Slack user identifier.
	UserID string `json:"userId"`
	// Text is the text returned by Slack.
	Text string `json:"text"`
}

type socketConnection interface {
	ReadJSON(any) error
	WriteJSON(any) error
	SetReadDeadline(time.Time) error
	Close() error
}

type socketDialer func(context.Context, string) (socketConnection, error)

// triggerBindingIdentity names a Trigger source's connection and binding in credential reads and log records.
type triggerBindingIdentity struct {
	client     *Client
	connection sdkgo.ConnectionRef
	// triggerName and bindingName identify the source in log records. A source built by a generated
	// per-Trigger factory has no binding name.
	triggerName string
	bindingName string
}

type messageTriggerSource struct {
	triggerBindingIdentity
	channelID      string
	matcher        MessageMatcher
	requiresThread bool
}

var (
	// socketModeReconnectDelay is the wait after the first failed Socket Mode connection attempt and after
	// Slack asks the runner to reconnect. The wait doubles with each consecutive failure, up to
	// socketModeReconnectMaxDelay.
	socketModeReconnectDelay    = time.Second
	socketModeReconnectMaxDelay = 30 * time.Second
	// socketModeReadTimeout ends a connection that receives neither an envelope nor a ping for this long.
	// Slack pings idle connections, and every ping extends the read deadline.
	socketModeReadTimeout = 45 * time.Second
	// socketModeControlWriteTimeout bounds the pong that answers a ping.
	socketModeControlWriteTimeout = 10 * time.Second
)

// slackInteractiveAcknowledgementLimit is Slack's interactive acknowledgement deadline; later, the clicker sees an error.
const slackInteractiveAcknowledgementLimit = 3 * time.Second

// socketModeDeliveryQueueCapacity bounds the queue; the reader's held envelope and the one in delivery add two.
var socketModeDeliveryQueueCapacity = 256

type messageTriggerRoute struct {
	source *messageTriggerSource
	target sdkgo.TriggerTarget[MessageEvent]
}

type pendingMessageTriggerDelivery struct {
	source *messageTriggerSource
	target sdkgo.TriggerTarget[MessageEvent]
	event  sdkgo.TriggerEvent[MessageEvent]
}

type socketEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	// Reason explains a disconnect envelope, such as refresh_requested.
	Reason string `json:"reason,omitempty"`
}

// socketModeDisconnect reports that Slack asked the runner to close its connection and open a new one.
type socketModeDisconnect struct {
	reason string
}

// Error returns the safe human-readable failure message.
func (disconnect *socketModeDisconnect) Error() string {
	return "Slack requested a Socket Mode reconnect: " + disconnect.reason
}

type eventsAPIPayload struct {
	EventID   string       `json:"event_id"`
	EventTime int64        `json:"event_time"`
	TeamID    string       `json:"team_id"`
	Event     messageEvent `json:"event"`
}

type messageEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Channel   string `json:"channel"`
	User      string `json:"user"`
	BotID     string `json:"bot_id"`
	Text      string `json:"text"`
	Timestamp string `json:"ts"`
	ThreadTS  string `json:"thread_ts"`
}

type socketOpenResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	URL   string `json:"url"`
}

// Validate returns an error when the value violates its public contract.
func (configuration ChannelThreadCreatedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.ChannelID) == "" {
		return fmt.Errorf("Slack channel ID is required")
	}
	return configuration.ThreadTriggerMatcher.validate(false)
}

// Validate returns an error when the value violates its public contract.
func (configuration ThreadReplyCreatedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.ChannelID) == "" {
		return fmt.Errorf("Slack channel ID is required")
	}
	return configuration.ThreadReplyMatcher.validate(true)
}

func (matcher MessageMatcher) validate(requiresPoster bool) error {
	seen := map[string]bool{}
	for _, userID := range matcher.PosterUserIDs {
		if strings.TrimSpace(userID) == "" || seen[userID] {
			return fmt.Errorf("Slack poster user IDs must be non-empty and unique")
		}
		seen[userID] = true
	}
	if requiresPoster && len(matcher.PosterUserIDs) == 0 {
		return fmt.Errorf("Slack reply matcher requires at least one poster user ID")
	}
	return nil
}

// Run processes provider events until cancellation or an unrecoverable error.
func (source *messageTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent]) error {
	return runSocketModeRoutes(ctx, socketModeRoutes{messages: []messageTriggerRoute{{source: source, target: target}}})
}

// socketModeRoutes share one socket, because Slack sends each envelope to only one app connection.
type socketModeRoutes struct {
	messages []messageTriggerRoute
	buttons  []buttonClickedTriggerRoute
}

// identity returns the first route's identity, which names the shared connection.
func (routes socketModeRoutes) identity() *triggerBindingIdentity {
	if len(routes.messages) > 0 {
		return &routes.messages[0].source.triggerBindingIdentity
	}
	return &routes.buttons[0].source.triggerBindingIdentity
}

func (routes socketModeRoutes) isEmpty() bool {
	return len(routes.messages) == 0 && len(routes.buttons) == 0
}

// hasDurableInboxes reports whether every route's target records and replays events, so queued deliveries lose none.
func (routes socketModeRoutes) hasDurableInboxes() bool {
	for _, route := range routes.messages {
		if !hasDurableInbox(route.target) {
			return false
		}
	}
	for _, route := range routes.buttons {
		if !hasDurableInbox(route.target) {
			return false
		}
	}
	return true
}

// hasDurableInbox reports whether target is a durable inbox, such as provider.NewDurableTriggerTarget builds.
func hasDurableInbox[E any](target sdkgo.TriggerTarget[E]) bool {
	_, isPreparer := target.(sdkgo.TriggerDeliveryPreparer[E])
	_, isReplayer := target.(sdkgo.TriggerDeliveryReplayer)
	return isPreparer && isReplayer
}

// replay delivers pending inbox events: roots, then replies, then button clicks.
func (routes socketModeRoutes) replay(ctx context.Context) error {
	if err := replayMessageTriggerRoutes(ctx, routes.messages); err != nil {
		return err
	}
	return replayButtonClickedTriggerRoutes(ctx, routes.buttons)
}

// decode sends interactive envelopes to button routes, when any, and every other envelope to message routes.
func (routes socketModeRoutes) decode(ctx context.Context, envelope socketEnvelope) (socketModeDeliveries, error) {
	switch {
	case envelope.Type == "interactive" && strings.TrimSpace(envelope.EnvelopeID) != "" && len(routes.buttons) > 0:
		buttons, err := decodeButtonClickedDeliveries(ctx, routes.buttons, envelope)
		return socketModeDeliveries{buttons: buttons}, err
	case len(routes.messages) > 0:
		messages, err := decodeMessageTriggerDeliveries(ctx, routes.messages, envelope)
		return socketModeDeliveries{messages: messages}, err
	}
	if identity := routes.identity(); identity.client.triggerLogger().Enabled(ctx, slog.LevelDebug) {
		identity.log(ctx, slog.LevelDebug, "slack socket mode envelope ignored", identity.connectionAttrs(),
			slog.String("type", envelope.Type), slog.String("envelope_id", envelope.EnvelopeID))
	}
	return socketModeDeliveries{}, nil
}

// socketModeDeliveries are one envelope's matched deliveries; at most one list is non-empty.
type socketModeDeliveries struct {
	messages []pendingMessageTriggerDelivery
	buttons  []pendingButtonClickedDelivery
}

func (deliveries socketModeDeliveries) isEmpty() bool {
	return len(deliveries.messages) == 0 && len(deliveries.buttons) == 0
}

// prepare records the deliveries in their durable inboxes before acknowledgement.
func (deliveries socketModeDeliveries) prepare(ctx context.Context) error {
	for _, delivery := range deliveries.messages {
		if err := sdkgo.PrepareTriggerDelivery(ctx, delivery.target, delivery.event); err != nil {
			return err
		}
	}
	for _, delivery := range deliveries.buttons {
		if err := sdkgo.PrepareTriggerDelivery(ctx, delivery.target, delivery.event); err != nil {
			return err
		}
	}
	return nil
}

// deliver hands the deliveries to their targets in route order until each is consumed.
func (deliveries socketModeDeliveries) deliver(ctx context.Context) error {
	if err := deliverMessageTriggerDeliveries(ctx, deliveries.messages); err != nil {
		return err
	}
	return deliverButtonClickedDeliveries(ctx, deliveries.buttons)
}

func runSocketModeRoutes(ctx context.Context, routes socketModeRoutes) error {
	if routes.isEmpty() {
		return fmt.Errorf("Slack Trigger routes are required")
	}
	source := routes.identity()
	reconnecting := false
	// failures counts the connection attempts that failed since the last one that received an envelope.
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		healthy, err := runSocketModeConnection(ctx, routes, reconnecting)
		reconnecting = true
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if healthy {
			failures = 0
		}
		delay := socketModeReconnectDelay
		var disconnect *socketModeDisconnect
		if errors.As(err, &disconnect) {
			// Slack refreshes connections routinely, so a requested reconnect is not a failure.
			source.log(ctx, slog.LevelInfo, "slack socket mode disconnected; reconnecting", source.connectionAttrs(),
				slog.String("reason", disconnect.reason), slog.Duration("delay", delay))
		} else {
			failures++
			delay = socketModeReconnectBackoff(failures)
			source.log(ctx, slog.LevelWarn, "slack socket mode connection failed; reconnecting", source.connectionAttrs(),
				slog.Int("attempt", failures), slog.Duration("delay", delay), slog.String("error", err.Error()))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// socketModeReconnectBackoff returns the wait after consecutive failed connection attempt number failures,
// starting at 1: socketModeReconnectDelay, doubled for each earlier failure, up to socketModeReconnectMaxDelay.
func socketModeReconnectBackoff(failures int) time.Duration {
	delay := socketModeReconnectDelay
	for completed := 1; completed < failures && delay < socketModeReconnectMaxDelay; completed++ {
		delay *= 2
	}
	return min(delay, socketModeReconnectMaxDelay)
}

// replayMessageTriggerRoutes delivers every route's pending durable inbox events in route order, roots first.
func replayMessageTriggerRoutes(ctx context.Context, routes []messageTriggerRoute) error {
	for _, route := range routes {
		if replayer, ok := route.target.(sdkgo.TriggerDeliveryReplayer); ok {
			if err := replayer.ReplayTriggerDeliveries(ctx); err != nil {
				return fmt.Errorf("replay Slack Trigger deliveries: %w", err)
			}
		}
	}
	return nil
}

// runSocketModeConnection serves one Socket Mode connection until it fails or Slack asks for a
// reconnect, which it reports as a *socketModeDisconnect. It reports whether the connection received an
// envelope, such as Slack's hello, so the caller can count consecutive failed connection attempts.
//
// With durable inboxes, a goroutine delivers a bounded queue in arrival order, so retries delay no
// acknowledgement until it fills; returning waits for it. Otherwise the reader delivers inline.
func runSocketModeConnection(ctx context.Context, routes socketModeRoutes, reconnecting bool) (bool, error) {
	if reconnecting {
		// A connection can end after an event was persisted but before it was delivered, for example when its
		// acknowledgement could not be written. Deliver it before reading newer envelopes, so a reply cannot
		// overtake its root.
		if err := routes.replay(ctx); err != nil {
			return false, err
		}
	}
	source := routes.identity()
	credentials, err := sdkgo.ResolveCredential(ctx, source.client.credentials, source.credentialCall(), source.client.refreshDriver)
	if err != nil {
		// A credential provider's error may quote what it read, so only its absence is reported.
		return false, fmt.Errorf("Slack Socket Mode credentials are unavailable")
	}
	if credentials.AppToken.Reveal() == "" {
		return false, fmt.Errorf("Slack Socket Mode credentials are unavailable: credential app_token is required")
	}
	socketURL, err := source.client.openSocketModeConnection(ctx, credentials.AppToken.Reveal())
	if err != nil {
		return false, err
	}
	connection, err := source.client.socketDialer(ctx, socketURL)
	if err != nil {
		return false, fmt.Errorf("connect Slack Socket Mode: %w", redactSocketURL(err, socketURL))
	}
	defer closeSocketConnection(connection)
	connectionCtx, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()
	// Pings extend the read deadline, so a quiet connection can read for as long as Slack keeps it open.
	// Close it as soon as ctx ends, so Run returns at once instead of waiting for Slack to disconnect.
	stopClosingOnCancel := context.AfterFunc(connectionCtx, func() { closeSocketConnection(connection) })
	defer stopClosingOnCancel()
	source.log(ctx, slog.LevelInfo, "slack socket mode connected", source.connectionAttrs(), slog.Bool("reconnect", reconnecting))
	if !routes.hasDurableInboxes() {
		// Only one acknowledged event can then wait for delivery, so a stopped process loses at most one.
		return readSocketModeEnvelopes(ctx, connection, routes, nil)
	}
	queue := make(chan socketModeDeliveries, socketModeDeliveryQueueCapacity)
	deliveryResult := make(chan error, 1)
	go func() { deliveryResult <- deliverQueuedSocketModeDeliveries(ctx, queue, cancelConnection) }()
	received, readErr := readSocketModeEnvelopes(connectionCtx, connection, routes, queue)
	close(queue)
	if deliveryErr := <-deliveryResult; deliveryErr != nil {
		return received, deliveryErr
	}
	return received, readErr
}

// readSocketModeEnvelopes records and acknowledges envelopes until the connection ends. It queues their
// deliveries, waiting while the queue is full, or delivers them inline when queue is nil.
func readSocketModeEnvelopes(ctx context.Context, connection socketConnection, routes socketModeRoutes, queue chan<- socketModeDeliveries) (bool, error) {
	source := routes.identity()
	received := false
	for {
		if err := connection.SetReadDeadline(time.Now().Add(socketModeReadTimeout)); err != nil {
			return received, fmt.Errorf("set Slack Socket Mode read deadline: %w", err)
		}
		var envelope socketEnvelope
		if err := connection.ReadJSON(&envelope); err != nil {
			return received, fmt.Errorf("read Slack Socket Mode envelope: %w", err)
		}
		receivedAt := time.Now()
		received = true
		if envelope.Type == "disconnect" {
			return true, &socketModeDisconnect{reason: slackCode(envelope.Reason)}
		}
		deliveries, err := routes.decode(ctx, envelope)
		if err != nil {
			source.log(ctx, slog.LevelWarn, "trigger event skipped: undecodable", source.connectionAttrs(),
				slog.String("envelope_id", envelope.EnvelopeID), slog.String("error", err.Error()))
			if acknowledgeErr := acknowledgeEnvelope(connection, envelope.EnvelopeID); acknowledgeErr != nil {
				return true, acknowledgeErr
			}
			continue
		}
		if deliveries.isEmpty() {
			if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
				return true, err
			}
			continue
		}
		if err := deliveries.prepare(ctx); err != nil {
			return true, err
		}
		if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
			return true, err
		}
		if elapsed := time.Since(receivedAt); envelope.Type == "interactive" && elapsed > slackInteractiveAcknowledgementLimit {
			// Slack has already shown the clicker an error, but the recorded click is still delivered.
			source.log(ctx, slog.LevelWarn, "slack interactive envelope acknowledged late", source.connectionAttrs(),
				slog.String("envelope_id", envelope.EnvelopeID), slog.Duration("elapsed", elapsed))
		}
		if queue == nil {
			if err := deliveries.deliver(ctx); err != nil {
				return true, err
			}
			continue
		}
		// A full queue pauses reading; the inboxes hold what was acknowledged, and Slack resends the rest.
		select {
		case queue <- deliveries:
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
}

// deliverQueuedSocketModeDeliveries delivers queued deliveries in order until queue is closed. After a delivery
// error, which is ctx.Err(), it stops the connection and discards the rest, which stay in their inboxes.
func deliverQueuedSocketModeDeliveries(ctx context.Context, queue <-chan socketModeDeliveries, stopConnection context.CancelFunc) error {
	var deliveryErr error
	for deliveries := range queue {
		if deliveryErr != nil {
			continue
		}
		if deliveryErr = deliveries.deliver(ctx); deliveryErr != nil {
			stopConnection()
		}
	}
	return deliveryErr
}

// deliverMessageTriggerDeliveries delivers one event's deliveries inline in route order, so a root is delivered
// before any reply received after it. The channel and thread reach the records of the inbox and the Dex targets
// too, so one thread_ts finds them all.
func deliverMessageTriggerDeliveries(ctx context.Context, deliveries []pendingMessageTriggerDelivery) error {
	for _, delivery := range deliveries {
		deliveryCtx := sdkgo.ContextWithTriggerLogAttrs(ctx,
			slog.String("channel", delivery.event.Payload.ChannelID),
			slog.String("thread_ts", delivery.event.Payload.ThreadTimestamp),
		)
		if err := sdkgo.DeliverTrigger(deliveryCtx, delivery.target, delivery.event, sdkgo.WithTriggerLogger(delivery.source.logger())); err != nil {
			return err
		}
	}
	return nil
}

// credentialCall identifies the source's credential reads. A project connection's provider refuses a call
// without the connector ID.
func (source *triggerBindingIdentity) credentialCall() sdkgo.Call {
	return sdkgo.Call{
		Connection: source.connection, Operation: sdkgo.OperationRef{ConnectorID: ConnectorID, OperationID: source.triggerName},
	}
}

// redactSocketURL removes the Socket Mode URL from a dial error. The URL carries a connection ticket,
// which must not reach a log record.
func redactSocketURL(err error, socketURL string) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return fmt.Errorf("%s Socket Mode URL: %w", urlError.Op, redactSocketURL(urlError.Err, socketURL))
	}
	if socketURL != "" && strings.Contains(err.Error(), socketURL) {
		return errors.New(strings.ReplaceAll(err.Error(), socketURL, "[Socket Mode URL]"))
	}
	return err
}

func decodeMessageTriggerDeliveries(ctx context.Context, routes []messageTriggerRoute, envelope socketEnvelope) ([]pendingMessageTriggerDelivery, error) {
	if envelope.Type != "events_api" || strings.TrimSpace(envelope.EnvelopeID) == "" {
		if source := routes[0].source; source.client.triggerLogger().Enabled(ctx, slog.LevelDebug) {
			source.log(ctx, slog.LevelDebug, "slack socket mode envelope ignored", source.connectionAttrs(),
				slog.String("type", envelope.Type), slog.String("envelope_id", envelope.EnvelopeID))
		}
		return nil, nil
	}
	deliveries := make([]pendingMessageTriggerDelivery, 0, len(routes))
	for _, route := range routes {
		event, ignored, err := route.source.matchEvent(envelope)
		if err != nil {
			return nil, err
		}
		if ignored.reason != "" {
			// Most channel traffic is ignored, so build the record only when DEBUG is enabled.
			if route.source.client.triggerLogger().Enabled(ctx, slog.LevelDebug) {
				route.source.log(ctx, slog.LevelDebug, "trigger event ignored", route.source.bindingAttrs(), ignored.attrs()...)
			}
			continue
		}
		deliveries = append(deliveries, pendingMessageTriggerDelivery{source: route.source, target: route.target, event: event})
	}
	return deliveries, nil
}

// ignoredMessage explains why a source does not match an Events API message. Its fields are IDs and
// Slack enum values, never message text.
type ignoredMessage struct {
	reason  string
	eventID string
	channel string
	subtype string
}

func (ignored ignoredMessage) attrs() []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event_id", ignored.eventID), slog.String("channel", ignored.channel), slog.String("reason", ignored.reason),
	}
	if ignored.subtype != "" {
		attrs = append(attrs, slog.String("subtype", ignored.subtype))
	}
	return attrs
}

func (source *messageTriggerSource) decodeEvent(envelope socketEnvelope) (bool, sdkgo.TriggerEvent[MessageEvent], error) {
	if envelope.Type != "events_api" || strings.TrimSpace(envelope.EnvelopeID) == "" {
		return false, sdkgo.TriggerEvent[MessageEvent]{}, nil
	}
	event, ignored, err := source.matchEvent(envelope)
	return err == nil && ignored.reason == "", event, err
}

// matchEvent decodes one Events API envelope. It returns the event when the message matches the source,
// and otherwise the reason the source ignores it.
func (source *messageTriggerSource) matchEvent(envelope socketEnvelope) (sdkgo.TriggerEvent[MessageEvent], ignoredMessage, error) {
	var payload eventsAPIPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return sdkgo.TriggerEvent[MessageEvent]{}, ignoredMessage{}, err
	}
	message := payload.Event
	isReply := message.ThreadTS != "" && message.ThreadTS != message.Timestamp
	switch {
	case payload.EventID == "":
		return ignoredMessageResult(payload, message, "missing_event_id")
	case message.Type != "message":
		return ignoredMessageResult(payload, message, "not_a_message")
	case message.Subtype != "":
		return ignoredMessageResult(payload, message, "subtype")
	case message.BotID != "":
		return ignoredMessageResult(payload, message, "bot")
	case message.User == "":
		return ignoredMessageResult(payload, message, "missing_user")
	case message.Channel != source.channelID:
		return ignoredMessageResult(payload, message, "channel_mismatch")
	case source.requiresThread && !isReply:
		return ignoredMessageResult(payload, message, "not_a_reply")
	case !source.requiresThread && isReply:
		return ignoredMessageResult(payload, message, "not_a_root")
	case !source.matcher.matches(message.User, message.Text):
		return ignoredMessageResult(payload, message, "matcher_mismatch")
	}
	threadTimestamp := message.ThreadTS
	if !isReply {
		threadTimestamp = message.Timestamp
	}
	return sdkgo.TriggerEvent[MessageEvent]{
		ID: payload.EventID, OccurredAt: time.Unix(payload.EventTime, 0).UTC(),
		Payload: MessageEvent{
			TeamID: payload.TeamID, ChannelID: message.Channel, Timestamp: message.Timestamp,
			ThreadTimestamp: threadTimestamp, UserID: message.User, Text: message.Text,
		},
	}, ignoredMessage{}, nil
}

func ignoredMessageResult(payload eventsAPIPayload, message messageEvent, reason string) (sdkgo.TriggerEvent[MessageEvent], ignoredMessage, error) {
	ignored := ignoredMessage{reason: reason, eventID: payload.EventID, channel: message.Channel}
	if reason == "subtype" {
		ignored.subtype = message.Subtype
	}
	return sdkgo.TriggerEvent[MessageEvent]{}, ignored, nil
}

func closeSocketConnection(connection socketConnection) {
	// Closing is best-effort because a prior read or cancellation usually closed the socket first.
	_ = connection.Close()
}

// logger returns the client's logger, or slog.Default() as of the call, with the source's binding
// attributes.
func (source *triggerBindingIdentity) logger() *slog.Logger {
	logger := source.client.triggerLogger()
	attrs := source.bindingAttrs()
	values := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		values = append(values, attr)
	}
	return logger.With(values...)
}

// connectionAttrs identify the shared Socket Mode connection.
func (source *triggerBindingIdentity) connectionAttrs() []slog.Attr {
	return []slog.Attr{slog.String("connector", ConnectorID), slog.String("connection", source.connection.Name)}
}

// bindingAttrs identify the source's Trigger binding.
func (source *triggerBindingIdentity) bindingAttrs() []slog.Attr {
	attrs := append(source.connectionAttrs(), slog.String("trigger", source.triggerName))
	if source.bindingName != "" {
		attrs = append(attrs, slog.String("binding", source.bindingName))
	}
	return attrs
}

// log writes one record when the client's logger is enabled for level. It builds the record itself, so a
// handler with AddSource reports the function that called log rather than this helper.
func (source *triggerBindingIdentity) log(ctx context.Context, level slog.Level, message string, base []slog.Attr, attrs ...slog.Attr) {
	logger := source.client.triggerLogger()
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	// Skip runtime.Callers and log.
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.AddAttrs(base...)
	record.AddAttrs(attrs...)
	// Logging is best-effort because a handler failure cannot change Trigger delivery state.
	_ = logger.Handler().Handle(ctx, record)
}

func (matcher MessageMatcher) matches(userID string, text string) bool {
	if matcher.MessageContains != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(matcher.MessageContains)) {
		return false
	}
	if len(matcher.PosterUserIDs) == 0 {
		return true
	}
	for _, allowedUserID := range matcher.PosterUserIDs {
		if userID == allowedUserID {
			return true
		}
	}
	return false
}

// slackCode returns a Slack error or reason code such as invalid_auth, or "unknown" when the value is not a
// short snake_case code, so a record never carries arbitrary response text.
func slackCode(code string) string {
	if code == "" || len(code) > 64 {
		return "unknown"
	}
	for _, character := range code {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return "unknown"
		}
	}
	return code
}

func acknowledgeEnvelope(connection socketConnection, envelopeID string) error {
	if envelopeID == "" {
		return nil
	}
	if err := connection.WriteJSON(map[string]string{"envelope_id": envelopeID}); err != nil {
		return fmt.Errorf("acknowledge Slack Socket Mode envelope: %w", err)
	}
	return nil
}

type webSocketConnection struct {
	connection *websocket.Conn
}

func newWebSocketConnection(ctx context.Context, target string) (socketConnection, error) {
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	// Slack's server pings every Socket Mode connection, and its SDKs treat a connection that stops pinging as
	// dead. A ping proves the connection is alive, so the handler extends the read deadline before it answers
	// with a pong. Without this, the read deadline would count only envelopes, and every quiet
	// socketModeReadTimeout would end a healthy connection.
	connection.SetPingHandler(func(appData string) error {
		if err := connection.SetReadDeadline(time.Now().Add(socketModeReadTimeout)); err != nil {
			return err
		}
		err := connection.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(socketModeControlWriteTimeout))
		var netError net.Error
		if errors.Is(err, websocket.ErrCloseSent) || (errors.As(err, &netError) && netError.Timeout()) {
			// Like gorilla's default handler, tolerate a pong that a closing or briefly stalled connection
			// cannot take; a broken connection still fails the read.
			return nil
		}
		return err
	})
	return &webSocketConnection{connection: connection}, nil
}

// ReadJSON decodes the next complete streaming JSON message.
func (connection *webSocketConnection) ReadJSON(value any) error {
	return connection.connection.ReadJSON(value)
}

// WriteJSON encodes and sends one streaming JSON message.
func (connection *webSocketConnection) WriteJSON(value any) error {
	return connection.connection.WriteJSON(value)
}

// SetReadDeadline sets the next streaming read deadline.
func (connection *webSocketConnection) SetReadDeadline(deadline time.Time) error {
	return connection.connection.SetReadDeadline(deadline)
}

// Close releases the underlying streaming response body.
func (connection *webSocketConnection) Close() error { return connection.connection.Close() }

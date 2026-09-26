// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type MessageMatcher struct {
	MessageContains string   `json:"messageContains,omitempty"`
	PosterUserIDs   []string `json:"posterUserIds,omitempty"`
}

type ChannelThreadCreatedTriggerConfiguration struct {
	ChannelID            string         `json:"channelId"`
	ThreadTriggerMatcher MessageMatcher `json:"threadTriggerMatcher"`
}

type ThreadReplyCreatedTriggerConfiguration struct {
	ChannelID          string         `json:"channelId"`
	ThreadReplyMatcher MessageMatcher `json:"threadReplyMatcher"`
}

type MessageEvent struct {
	TeamID          string `json:"teamId"`
	ChannelID       string `json:"channelId"`
	Timestamp       string `json:"timestamp"`
	ThreadTimestamp string `json:"threadTimestamp"`
	UserID          string `json:"userId"`
	Text            string `json:"text"`
}

type socketConnection interface {
	ReadJSON(any) error
	WriteJSON(any) error
	SetReadDeadline(time.Time) error
	Close() error
}

type socketDialer func(context.Context, string) (socketConnection, error)

type messageTriggerSource struct {
	client         *Client
	connection     sdkgo.ConnectionRef
	channelID      string
	matcher        MessageMatcher
	requiresThread bool
	// triggerName and bindingName identify the source in log records. A source built by a generated
	// per-Trigger factory has no binding name.
	triggerName string
	bindingName string
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

func (configuration ChannelThreadCreatedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.ChannelID) == "" {
		return fmt.Errorf("Slack channel ID is required")
	}
	return configuration.ThreadTriggerMatcher.validate(false)
}

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

func (client *Client) channelThreadCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration ChannelThreadCreatedTriggerConfiguration) *messageTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadTriggerMatcher, triggerName: "channelThreadCreated",
	}
}

func (client *Client) threadReplyCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration ThreadReplyCreatedTriggerConfiguration) *messageTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadReplyMatcher, requiresThread: true,
		triggerName: "threadReplyCreated",
	}
}

func (source *messageTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent]) error {
	return runMessageTriggerRoutes(ctx, []messageTriggerRoute{{source: source, target: target}})
}

func runMessageTriggerRoutes(ctx context.Context, routes []messageTriggerRoute) error {
	if len(routes) == 0 {
		return fmt.Errorf("Slack message Trigger routes are required")
	}
	source := routes[0].source
	reconnecting := false
	// failures counts the connection attempts that failed since the last one that received an envelope.
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		healthy, err := runMessageTriggerConnection(ctx, routes, reconnecting)
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

// runMessageTriggerConnection serves one Socket Mode connection until it fails or Slack asks for a
// reconnect, which it reports as a *socketModeDisconnect. It reports whether the connection received an
// envelope, such as Slack's hello, so the caller can count consecutive failed connection attempts.
func runMessageTriggerConnection(ctx context.Context, routes []messageTriggerRoute, reconnecting bool) (bool, error) {
	if reconnecting {
		// A connection can end after an event was persisted but before it was delivered, for example when its
		// acknowledgement could not be written. Deliver it before reading newer envelopes, so a reply cannot
		// overtake its root.
		if err := replayMessageTriggerRoutes(ctx, routes); err != nil {
			return false, err
		}
	}
	source := routes[0].source
	credentials, err := source.client.credentials.Resolve(sdkgo.Call{Connection: source.connection})
	if err != nil {
		// A credential provider's error may quote what it read, so only its absence is reported.
		return false, fmt.Errorf("Slack Socket Mode credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil {
		return false, fmt.Errorf("Slack Socket Mode credentials are unavailable: %w", err)
	}
	socketURL, err := source.client.openSocketModeConnection(ctx, credentials.AppToken.Reveal())
	if err != nil {
		return false, err
	}
	connection, err := source.client.socketDialer(ctx, socketURL)
	if err != nil {
		return false, fmt.Errorf("connect Slack Socket Mode: %w", redactSocketURL(err, socketURL))
	}
	defer connection.Close()
	// Pings extend the read deadline, so a quiet connection can read for as long as Slack keeps it open.
	// Close it as soon as ctx ends, so Run returns at once instead of waiting for Slack to disconnect.
	stopClosingOnCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClosingOnCancel()
	source.log(ctx, slog.LevelInfo, "slack socket mode connected", source.connectionAttrs(), slog.Bool("reconnect", reconnecting))
	received := false
	for {
		if err := connection.SetReadDeadline(time.Now().Add(socketModeReadTimeout)); err != nil {
			return received, fmt.Errorf("set Slack Socket Mode read deadline: %w", err)
		}
		var envelope socketEnvelope
		if err := connection.ReadJSON(&envelope); err != nil {
			return received, fmt.Errorf("read Slack Socket Mode envelope: %w", err)
		}
		received = true
		if envelope.Type == "disconnect" {
			return true, &socketModeDisconnect{reason: slackCode(envelope.Reason)}
		}
		deliveries, err := decodeMessageTriggerDeliveries(ctx, routes, envelope)
		if err != nil {
			source.log(ctx, slog.LevelWarn, "trigger event skipped: undecodable", source.connectionAttrs(),
				slog.String("envelope_id", envelope.EnvelopeID), slog.String("error", err.Error()))
			if acknowledgeErr := acknowledgeEnvelope(connection, envelope.EnvelopeID); acknowledgeErr != nil {
				return true, acknowledgeErr
			}
			continue
		}
		if len(deliveries) == 0 {
			if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
				return true, err
			}
			continue
		}
		for _, delivery := range deliveries {
			if err := sdkgo.PrepareTriggerDelivery(ctx, delivery.target, delivery.event); err != nil {
				return true, err
			}
		}
		if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
			return true, err
		}
		// Deliveries stay inline, so a root is delivered before any reply received after it. The channel and
		// thread reach the records of the inbox and the Dex targets too, so one thread_ts finds them all.
		for _, delivery := range deliveries {
			deliveryCtx := sdkgo.ContextWithTriggerLogAttrs(ctx,
				slog.String("channel", delivery.event.Payload.ChannelID),
				slog.String("thread_ts", delivery.event.Payload.ThreadTimestamp),
			)
			if err := sdkgo.DeliverTrigger(deliveryCtx, delivery.target, delivery.event, sdkgo.WithTriggerLogger(delivery.source.logger())); err != nil {
				return true, err
			}
		}
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
	ignore := func(reason string) (sdkgo.TriggerEvent[MessageEvent], ignoredMessage, error) {
		ignored := ignoredMessage{reason: reason, eventID: payload.EventID, channel: message.Channel}
		if reason == "subtype" {
			ignored.subtype = message.Subtype
		}
		return sdkgo.TriggerEvent[MessageEvent]{}, ignored, nil
	}
	isReply := message.ThreadTS != "" && message.ThreadTS != message.Timestamp
	switch {
	case payload.EventID == "":
		return ignore("missing_event_id")
	case message.Type != "message":
		return ignore("not_a_message")
	case message.Subtype != "":
		return ignore("subtype")
	case message.BotID != "":
		return ignore("bot")
	case message.User == "":
		return ignore("missing_user")
	case message.Channel != source.channelID:
		return ignore("channel_mismatch")
	case source.requiresThread && !isReply:
		return ignore("not_a_reply")
	case !source.requiresThread && isReply:
		return ignore("not_a_root")
	case !source.matcher.matches(message.User, message.Text):
		return ignore("matcher_mismatch")
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

// logger returns the client's logger, or slog.Default() as of the call, with the source's binding
// attributes.
func (source *messageTriggerSource) logger() *slog.Logger {
	logger := source.client.triggerLogger()
	attrs := source.bindingAttrs()
	values := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		values = append(values, attr)
	}
	return logger.With(values...)
}

// connectionAttrs identify the shared Socket Mode connection.
func (source *messageTriggerSource) connectionAttrs() []slog.Attr {
	return []slog.Attr{slog.String("connector", ConnectorID), slog.String("connection", source.connection.Name)}
}

// bindingAttrs identify the source's Trigger binding.
func (source *messageTriggerSource) bindingAttrs() []slog.Attr {
	attrs := append(source.connectionAttrs(), slog.String("trigger", source.triggerName))
	if source.bindingName != "" {
		attrs = append(attrs, slog.String("binding", source.bindingName))
	}
	return attrs
}

// log writes one record when the client's logger is enabled for level. It builds the record itself, so a
// handler with AddSource reports the function that called log rather than this helper.
func (source *messageTriggerSource) log(ctx context.Context, level slog.Level, message string, base []slog.Attr, attrs ...slog.Attr) {
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
	_ = logger.Handler().Handle(ctx, record)
}

// triggerLogger returns the configured logger or, as of the call, slog.Default().
func (client *Client) triggerLogger() *slog.Logger {
	if client != nil && client.logger != nil {
		return client.logger
	}
	return slog.Default()
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

func (client *Client) openSocketModeConnection(ctx context.Context, appToken string) (string, error) {
	target := strings.TrimRight(client.endpoint.String(), "/") + "/apps.connections.open"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return "", fmt.Errorf("build Slack Socket Mode open request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+appToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("open Slack Socket Mode connection: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(contents)) > client.maxResponseBytes {
		return "", fmt.Errorf("read Slack Socket Mode open response")
	}
	var decoded socketOpenResponse
	decodeErr := json.Unmarshal(contents, &decoded)
	if response.StatusCode >= 200 && response.StatusCode < 300 && decodeErr != nil {
		return "", fmt.Errorf("decode Slack Socket Mode open response: %w", decodeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !decoded.OK || decoded.URL == "" {
		// Slack's error code, such as invalid_auth, names the cause without revealing the token.
		return "", fmt.Errorf("Slack rejected the Socket Mode connection: %s (HTTP %d)", slackCode(decoded.Error), response.StatusCode)
	}
	return decoded.URL, nil
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

func (connection *webSocketConnection) ReadJSON(value any) error {
	return connection.connection.ReadJSON(value)
}

func (connection *webSocketConnection) WriteJSON(value any) error {
	return connection.connection.WriteJSON(value)
}

func (connection *webSocketConnection) SetReadDeadline(deadline time.Time) error {
	return connection.connection.SetReadDeadline(deadline)
}

func (connection *webSocketConnection) Close() error { return connection.connection.Close() }

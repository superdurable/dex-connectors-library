// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
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

// ThreadIdentity identifies one Slack thread independently of any application Flow ID scheme.
type ThreadIdentity struct {
	TeamID        string `json:"teamId"`
	ChannelID     string `json:"channelId"`
	RootTimestamp string `json:"rootTimestamp"`
}

type MessageEvent struct {
	TeamID          string `json:"teamId"`
	ChannelID       string `json:"channelId"`
	Timestamp       string `json:"timestamp"`
	ThreadTimestamp string `json:"threadTimestamp"`
	UserID          string `json:"userId"`
	Text            string `json:"text"`
}

// ThreadIdentity returns the stable workspace, channel, and root timestamp for this event.
func (event MessageEvent) ThreadIdentity() ThreadIdentity {
	return ThreadIdentity{TeamID: event.TeamID, ChannelID: event.ChannelID, RootTimestamp: event.ThreadTimestamp}
}

// FlowIDByThread adapts an application-owned Slack thread resolver to the generic Trigger target contract.
func FlowIDByThread(resolve func(ThreadIdentity) (string, error)) connector.FlowIDResolver[MessageEvent] {
	if resolve == nil {
		panic("Slack thread Flow ID resolver is required")
	}
	return func(event connector.TriggerEvent[MessageEvent]) (string, error) {
		return resolve(event.Payload.ThreadIdentity())
	}
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
	connection     connector.ConnectionRef
	channelID      string
	matcher        MessageMatcher
	requiresThread bool
}

type socketEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
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

func (client *Client) channelThreadCreatedTriggerSource(connection connector.ConnectionRef, configuration ChannelThreadCreatedTriggerConfiguration) connector.TriggerSource[MessageEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadTriggerMatcher,
	}
}

func (client *Client) threadReplyCreatedTriggerSource(connection connector.ConnectionRef, configuration ThreadReplyCreatedTriggerConfiguration) connector.TriggerSource[MessageEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadReplyMatcher, requiresThread: true,
	}
}

func (source *messageTriggerSource) Run(ctx context.Context, target connector.TriggerTarget[MessageEvent]) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := source.runConnection(ctx, target); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
	}
}

func (source *messageTriggerSource) runConnection(ctx context.Context, target connector.TriggerTarget[MessageEvent]) error {
	credentials, err := source.client.credentials.Resolve(connector.Call{Connection: source.connection})
	if err != nil || credentials.Validate() != nil {
		return fmt.Errorf("Slack Socket Mode credentials are unavailable")
	}
	socketURL, err := source.client.openSocketModeConnection(ctx, credentials.AppToken.Reveal())
	if err != nil {
		return err
	}
	connection, err := source.client.socketDialer(ctx, socketURL)
	if err != nil {
		return fmt.Errorf("connect Slack Socket Mode: %w", err)
	}
	defer connection.Close()
	for {
		if err := connection.SetReadDeadline(time.Now().Add(45 * time.Second)); err != nil {
			return fmt.Errorf("set Slack Socket Mode read deadline: %w", err)
		}
		var envelope socketEnvelope
		if err := connection.ReadJSON(&envelope); err != nil {
			return fmt.Errorf("read Slack Socket Mode envelope: %w", err)
		}
		matched, event, err := source.decodeEvent(envelope)
		if err != nil {
			if acknowledgeErr := acknowledgeEnvelope(connection, envelope.EnvelopeID); acknowledgeErr != nil {
				return acknowledgeErr
			}
			continue
		}
		if !matched {
			if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
				return err
			}
			continue
		}
		if err := connector.PrepareTriggerDelivery(ctx, target, event); err != nil {
			return err
		}
		if err := acknowledgeEnvelope(connection, envelope.EnvelopeID); err != nil {
			return err
		}
		if err := deliverTriggerEvent(ctx, target, event); err != nil {
			return err
		}
	}
}

func deliverTriggerEvent(ctx context.Context, target connector.TriggerTarget[MessageEvent], event connector.TriggerEvent[MessageEvent]) error {
	for {
		if err := target.HandleTrigger(ctx, event); err == nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (source *messageTriggerSource) decodeEvent(envelope socketEnvelope) (bool, connector.TriggerEvent[MessageEvent], error) {
	if envelope.Type != "events_api" || strings.TrimSpace(envelope.EnvelopeID) == "" {
		return false, connector.TriggerEvent[MessageEvent]{}, nil
	}
	var payload eventsAPIPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return false, connector.TriggerEvent[MessageEvent]{}, err
	}
	message := payload.Event
	if payload.EventID == "" || message.Type != "message" || message.Subtype != "" || message.BotID != "" || message.User == "" || message.Channel != source.channelID {
		return false, connector.TriggerEvent[MessageEvent]{}, nil
	}
	isReply := message.ThreadTS != "" && message.ThreadTS != message.Timestamp
	if source.requiresThread != isReply || !source.matcher.matches(message.User, message.Text) {
		return false, connector.TriggerEvent[MessageEvent]{}, nil
	}
	threadTimestamp := message.ThreadTS
	if !isReply {
		threadTimestamp = message.Timestamp
	}
	return true, connector.TriggerEvent[MessageEvent]{
		ID: payload.EventID, OccurredAt: time.Unix(payload.EventTime, 0).UTC(),
		Payload: MessageEvent{
			TeamID: payload.TeamID, ChannelID: message.Channel, Timestamp: message.Timestamp,
			ThreadTimestamp: threadTimestamp, UserID: message.User, Text: message.Text,
		},
	}, nil
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
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return "", fmt.Errorf("decode Slack Socket Mode open response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !decoded.OK || decoded.URL == "" {
		return "", fmt.Errorf("Slack rejected the Socket Mode connection")
	}
	return decoded.URL, nil
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

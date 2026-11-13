// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ButtonClickedTriggerConfiguration selects the message button clicks that a buttonClicked binding receives.
type ButtonClickedTriggerConfiguration struct {
	// ChannelID is the Slack channel whose message buttons the binding receives.
	ChannelID string `json:"channelId"`
	// ActionIDs limits matches to buttons with these action IDs, the MessageButton.ActionID values that
	// postButtonMessage posted. Empty matches every button in the channel.
	ActionIDs []string `json:"actionIds,omitempty"`
	// ClickerUserIDs limits matches to clicks by these Slack user IDs. Empty matches a click by anyone who can
	// see the message.
	ClickerUserIDs []string `json:"clickerUserIds,omitempty"`
}

// ButtonClickEvent is one click on a message button, decoded from a Slack block_actions payload. It carries
// IDs and the button's value, never the Slack response URL, trigger ID, or verification token.
type ButtonClickEvent struct {
	// TeamID is the Slack workspace ID of the clicked message.
	TeamID string `json:"teamId"`
	// ChannelID is the Slack channel identifier of the clicked message.
	ChannelID string `json:"channelId"`
	// MessageTimestamp is the Slack timestamp identifier of the message that holds the button.
	MessageTimestamp string `json:"messageTimestamp"`
	// ThreadTimestamp is the parent message timestamp of the thread that holds the message, or
	// MessageTimestamp for a top-level message, as MessageEvent reports a thread root.
	ThreadTimestamp string `json:"threadTimestamp"`
	// UserID is the Slack user ID of the member who clicked.
	UserID string `json:"userId"`
	// ActionID is the clicked button's action ID.
	ActionID string `json:"actionId"`
	// Value is the clicked button's value, or empty when the button has none.
	Value string `json:"value,omitempty"`
	// ActionTimestamp is Slack's timestamp of the click; each click has its own.
	ActionTimestamp string `json:"actionTimestamp"`
}

type buttonClickedTriggerSource struct {
	triggerBindingIdentity
	configuration ButtonClickedTriggerConfiguration
}

type buttonClickedTriggerRoute struct {
	source *buttonClickedTriggerSource
	target sdkgo.TriggerTarget[ButtonClickEvent]
}

type pendingButtonClickedDelivery struct {
	source *buttonClickedTriggerSource
	target sdkgo.TriggerTarget[ButtonClickEvent]
	event  sdkgo.TriggerEvent[ButtonClickEvent]
}

type blockActionsPayload struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Container struct {
		Type      string `json:"type"`
		MessageTS string `json:"message_ts"`
		ChannelID string `json:"channel_id"`
		ThreadTS  string `json:"thread_ts"`
	} `json:"container"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		Timestamp string `json:"ts"`
		ThreadTS  string `json:"thread_ts"`
	} `json:"message"`
	Actions []blockAction `json:"actions"`
}

type blockAction struct {
	Type     string `json:"type"`
	ActionID string `json:"action_id"`
	Value    string `json:"value"`
	ActionTS string `json:"action_ts"`
}

// Validate returns an error when the value violates its public contract.
func (configuration ButtonClickedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.ChannelID) == "" {
		return fmt.Errorf("Slack channel ID is required")
	}
	if err := validateUniqueNonEmpty(configuration.ActionIDs); err != nil {
		return fmt.Errorf("Slack button action IDs must be non-empty and unique")
	}
	if err := validateUniqueNonEmpty(configuration.ClickerUserIDs); err != nil {
		return fmt.Errorf("Slack clicker user IDs must be non-empty and unique")
	}
	return nil
}

func validateUniqueNonEmpty(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return fmt.Errorf("values must be non-empty and unique")
		}
		seen[value] = true
	}
	return nil
}

// errButtonClicksRequireDurableInboxes explains why a button click route was refused.
var errButtonClicksRequireDurableInboxes = errors.New("Slack button click Triggers require durable inboxes for every route on their " +
	"Socket Mode connection: add the binding to ButtonClickedRoutes of NewProjectMessageTriggerRunner, or give NewMessageTriggerRunner " +
	"provider.NewDurableTriggerTarget targets")

// Run processes provider events until cancellation or an unrecoverable error. A target without a durable
// inbox, which includes every target that the generated NewButtonClickedTrigger passes, returns an error.
func (source *buttonClickedTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[ButtonClickEvent]) error {
	if !hasDurableInbox(target) {
		return errButtonClicksRequireDurableInboxes
	}
	return runSocketModeRoutes(ctx, socketModeRoutes{buttons: []buttonClickedTriggerRoute{{source: source, target: target}}})
}

// decodeButtonClickedDeliveries matches one interactive envelope against the button routes.
func decodeButtonClickedDeliveries(ctx context.Context, routes []buttonClickedTriggerRoute, envelope socketEnvelope) ([]pendingButtonClickedDelivery, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	var payload blockActionsPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode Slack interactive payload: %w", err)
	}
	deliveries := make([]pendingButtonClickedDelivery, 0, len(routes))
	for _, route := range routes {
		events, ignored := route.source.matchPayload(payload)
		for _, reason := range ignored {
			// Most interactions belong to other bindings, so build the record only at DEBUG.
			if route.source.client.triggerLogger().Enabled(ctx, slog.LevelDebug) {
				route.source.log(ctx, slog.LevelDebug, "trigger event ignored", route.source.bindingAttrs(),
					slog.String("envelope_id", envelope.EnvelopeID), slog.String("channel", blockActionsChannelID(payload)),
					slog.String("reason", reason))
			}
		}
		for _, event := range events {
			deliveries = append(deliveries, pendingButtonClickedDelivery{source: route.source, target: route.target, event: event})
		}
	}
	return deliveries, nil
}

// matchPayload returns an event per accepted button action and a reason per ignored action or payload.
func (source *buttonClickedTriggerSource) matchPayload(payload blockActionsPayload) ([]sdkgo.TriggerEvent[ButtonClickEvent], []string) {
	channelID := blockActionsChannelID(payload)
	messageTimestamp := payload.Container.MessageTS
	if messageTimestamp == "" {
		messageTimestamp = payload.Message.Timestamp
	}
	switch {
	case payload.Type != "block_actions":
		return nil, []string{"not_block_actions"}
	case payload.Container.Type != "message":
		return nil, []string{"not_a_message"}
	case channelID != source.configuration.ChannelID:
		return nil, []string{"channel_mismatch"}
	case payload.User.ID == "":
		return nil, []string{"missing_user"}
	case payload.Team.ID == "" || messageTimestamp == "":
		return nil, []string{"missing_message_identity"}
	case !containsOrEmpty(source.configuration.ClickerUserIDs, payload.User.ID):
		return nil, []string{"clicker_mismatch"}
	}
	threadTimestamp := payload.Container.ThreadTS
	if threadTimestamp == "" {
		threadTimestamp = payload.Message.ThreadTS
	}
	if threadTimestamp == "" {
		threadTimestamp = messageTimestamp
	}
	var events []sdkgo.TriggerEvent[ButtonClickEvent]
	var ignored []string
	for _, action := range payload.Actions {
		switch {
		case action.Type != "button":
			ignored = append(ignored, "not_a_button")
			continue
		case action.ActionID == "" || action.ActionTS == "":
			ignored = append(ignored, "missing_action_identity")
			continue
		case !containsOrEmpty(source.configuration.ActionIDs, action.ActionID):
			ignored = append(ignored, "action_mismatch")
			continue
		}
		events = append(events, sdkgo.TriggerEvent[ButtonClickEvent]{
			// Interactive payloads have no event ID. A click is unique by its message, button, and action time.
			ID:         strings.Join([]string{payload.Team.ID, channelID, messageTimestamp, action.ActionID, action.ActionTS}, ":"),
			OccurredAt: slackTimestampTime(action.ActionTS),
			Payload: ButtonClickEvent{
				TeamID: payload.Team.ID, ChannelID: channelID, MessageTimestamp: messageTimestamp, ThreadTimestamp: threadTimestamp,
				UserID: payload.User.ID, ActionID: action.ActionID, Value: action.Value, ActionTimestamp: action.ActionTS,
			},
		})
	}
	if len(payload.Actions) == 0 {
		ignored = append(ignored, "no_actions")
	}
	return events, ignored
}

func blockActionsChannelID(payload blockActionsPayload) string {
	if payload.Container.ChannelID != "" {
		return payload.Container.ChannelID
	}
	return payload.Channel.ID
}

func containsOrEmpty(allowed []string, value string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, allowedValue := range allowed {
		if value == allowedValue {
			return true
		}
	}
	return false
}

// slackTimestampTime parses a Slack "seconds.micros" timestamp, returning zero time when invalid.
func slackTimestampTime(timestamp string) time.Time {
	seconds, err := strconv.ParseFloat(timestamp, 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) {
		return time.Time{}
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e6))*int64(time.Microsecond)).UTC()
}

// deliverButtonClickedDeliveries delivers one envelope's clicks in route order, with channel and message log attributes.
func deliverButtonClickedDeliveries(ctx context.Context, deliveries []pendingButtonClickedDelivery) error {
	for _, delivery := range deliveries {
		deliveryCtx := sdkgo.ContextWithTriggerLogAttrs(ctx,
			slog.String("channel", delivery.event.Payload.ChannelID),
			slog.String("thread_ts", delivery.event.Payload.ThreadTimestamp),
			slog.String("message_ts", delivery.event.Payload.MessageTimestamp),
		)
		if err := sdkgo.DeliverTrigger(deliveryCtx, delivery.target, delivery.event, sdkgo.WithTriggerLogger(delivery.source.logger())); err != nil {
			return err
		}
	}
	return nil
}

// replayButtonClickedTriggerRoutes delivers each button route's pending inbox events.
func replayButtonClickedTriggerRoutes(ctx context.Context, routes []buttonClickedTriggerRoute) error {
	for _, route := range routes {
		if replayer, ok := route.target.(sdkgo.TriggerDeliveryReplayer); ok {
			if err := replayer.ReplayTriggerDeliveries(ctx); err != nil {
				return fmt.Errorf("replay Slack Trigger deliveries: %w", err)
			}
		}
	}
	return nil
}

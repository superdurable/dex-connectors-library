// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// maximumButtonMessageButtons is Slack's element limit for one actions block.
	maximumButtonMessageButtons = 25
	// maximumButtonMessageTextCharacters is Slack's limit for the section block that shows the message text.
	maximumButtonMessageTextCharacters = 3000
	maximumButtonActionIDCharacters    = 255
	maximumButtonTextCharacters        = 75
	maximumButtonValueCharacters       = 2000
)

// ButtonStyle selects a Slack button's color.
type ButtonStyle string

const (
	// ButtonStylePrimary shows a green button. Slack recommends it for at most one button in a message.
	ButtonStylePrimary ButtonStyle = "primary"
	// ButtonStyleDanger shows a red button for a destructive or rejecting choice.
	ButtonStyleDanger ButtonStyle = "danger"
)

// MessageButton is one Block Kit button in a message that postButtonMessage posts.
type MessageButton struct {
	// ActionID identifies the button in the buttonClicked Trigger's ButtonClickEvent.ActionID and in its
	// ActionIDs filter. It is required, unique within the message, and at most 255 characters.
	ActionID string `json:"actionId"`
	// Text is the required plain-text button label, at most 75 characters; Slack may truncate it near 30.
	Text string `json:"text"`
	// Value is returned unchanged in ButtonClickEvent.Value, such as an order ID, at most 2000 characters.
	// Empty sends no value. Like message text, every member who can see the message can read it, and a click
	// delivers it to the Flow and records it in the durable inbox, so it must never be a secret or token.
	Value string `json:"value,omitempty"`
	// Style is ButtonStylePrimary, ButtonStyleDanger, or empty for Slack's default style.
	Style ButtonStyle `json:"style,omitempty"`
}

// PostButtonMessageInput contains the provider request fields for post button message.
type PostButtonMessageInput struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadTimestamp is the parent message timestamp to reply in that thread; empty posts a top-level message.
	ThreadTimestamp string `json:"threadTimestamp,omitempty"`
	// Text is the required Slack mrkdwn message shown above the buttons and used in notifications, at most
	// 3000 characters and the connection's maxMessageCharacters.
	Text string `json:"text"`
	// Buttons are the 1 through 25 buttons shown below the text, in order.
	Buttons []MessageButton `json:"buttons"`
}

// PostButtonMessageOperation implements the post button message connector operation.
type PostButtonMessageOperation struct{ client *Client }

// slackBlock is a section with the text or an actions block with the buttons.
type slackBlock struct {
	Type     string               `json:"type"`
	Text     *slackTextObject     `json:"text,omitempty"`
	Elements []slackButtonElement `json:"elements,omitempty"`
}

type slackTextObject struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type slackButtonElement struct {
	Type     string          `json:"type"`
	ActionID string          `json:"action_id"`
	Text     slackTextObject `json:"text"`
	Value    string          `json:"value,omitempty"`
	Style    string          `json:"style,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (PostButtonMessageOperation) Definition() sdkgo.MutationDefinition {
	return PostButtonMessageDefinition
}

// IdempotencyKey supplies a stable client message ID; Slack does not document replay deduplication.
func (PostButtonMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PostButtonMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt.
func (operation PostButtonMessageOperation) Invoke(call sdkgo.Call, input PostButtonMessageInput) sdkgo.MutationAttempt[PostMessageOutput] {
	if failure := ValidatePostButtonMessageInput(input); failure != nil {
		return sdkgo.NewMutationBranch(PostButtonMessageBranchDefect, PostMessageOutput{}, failure, sdkgo.Receipt{})
	}
	return operation.client.postMessage(call, "postButtonMessage", input.ChannelID, input.ThreadTimestamp, input.Text, buttonMessageBlocks(input),
		PostButtonMessageDefinition.StepDefaults.ExecuteMethodTimeout, PostButtonMessageBranchSent, PostButtonMessageBranchProviderRejected,
		PostButtonMessageBranchUncertain, PostButtonMessageBranchDefect)
}

// ValidatePostButtonMessageInput returns a VALIDATION Failure when input breaks a Slack Block Kit limit that
// postButtonMessage enforces before sending, and nil otherwise. The connection's maxMessageCharacters is
// checked when the operation runs.
func ValidatePostButtonMessageInput(input PostButtonMessageInput) *sdkgo.Failure {
	invalid := func(message string) *sdkgo.Failure {
		return slackFailurePointer("postButtonMessage", sdkgo.FailureValidation, message)
	}
	switch {
	case strings.TrimSpace(input.ChannelID) == "":
		return invalid("The channel ID is required.")
	case strings.TrimSpace(input.Text) == "" || len([]rune(input.Text)) > maximumButtonMessageTextCharacters:
		return invalid(fmt.Sprintf("The button message text is required and must be at most %d characters.", maximumButtonMessageTextCharacters))
	case len(input.Buttons) == 0 || len(input.Buttons) > maximumButtonMessageButtons:
		return invalid(fmt.Sprintf("A button message needs 1 through %d buttons.", maximumButtonMessageButtons))
	}
	actionIDs := make(map[string]bool, len(input.Buttons))
	for _, button := range input.Buttons {
		switch {
		case strings.TrimSpace(button.ActionID) == "" || len([]rune(button.ActionID)) > maximumButtonActionIDCharacters:
			return invalid(fmt.Sprintf("Each button action ID is required and must be at most %d characters.", maximumButtonActionIDCharacters))
		case actionIDs[button.ActionID]:
			return invalid("Each button action ID must be unique.")
		case strings.TrimSpace(button.Text) == "" || len([]rune(button.Text)) > maximumButtonTextCharacters:
			return invalid(fmt.Sprintf("Each button label is required and must be at most %d characters.", maximumButtonTextCharacters))
		case len([]rune(button.Value)) > maximumButtonValueCharacters:
			return invalid(fmt.Sprintf("Each button value must be at most %d characters.", maximumButtonValueCharacters))
		case button.Style != "" && button.Style != ButtonStylePrimary && button.Style != ButtonStyleDanger:
			return invalid("Each button style must be primary, danger, or empty.")
		}
		actionIDs[button.ActionID] = true
	}
	return nil
}

// buttonMessageBlocks repeats the text in a section, because Slack shows only blocks when a message has them.
func buttonMessageBlocks(input PostButtonMessageInput) []slackBlock {
	elements := make([]slackButtonElement, len(input.Buttons))
	for index, button := range input.Buttons {
		elements[index] = slackButtonElement{
			Type: "button", ActionID: button.ActionID, Value: button.Value, Style: string(button.Style),
			Text: slackTextObject{Type: "plain_text", Text: button.Text, Emoji: true},
		}
	}
	return []slackBlock{
		{Type: "section", Text: &slackTextObject{Type: "mrkdwn", Text: input.Text}},
		{Type: "actions", Elements: elements},
	}
}

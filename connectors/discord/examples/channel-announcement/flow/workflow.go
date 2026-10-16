// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package channelannouncement demonstrates every Discord operation in one Flow
// started from Dex Web: it checks a channel, posts an announcement, reacts to
// it, and reads the channel back to confirm the post.
package channelannouncement

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "DiscordChannelAnnouncement"
	// ConnectionName is the static Dex Web connection that holds the Discord bot token.
	ConnectionName = "discord"

	getAnnouncementChannelStepType      = "GetAnnouncementChannel"
	postAnnouncementMessageStepType     = "PostAnnouncementMessage"
	reactToAnnouncementStepType         = "ReactToAnnouncement"
	readBackAnnouncementChannelStepType = "ReadBackAnnouncementChannel"
	defaultReactionEmoji                = "📣"
	// readBackPageSize bounds the read-back page; the announcement is among the newest messages.
	readBackPageSize = 10
)

var (
	announcementRequestAttribute = dex.DefineAttribute[AnnouncementRequest]("discord-announcement-request")
	announcementChannelAttribute = dex.DefineAttribute[discord.Channel]("discord-announcement-channel")
	announcementMessageAttribute = dex.DefineAttribute[discord.Message]("discord-announcement-message")
	announcementOutcomeAttribute = dex.DefineAttribute[AnnouncementOutcome]("discord-announcement-outcome")
)

// AnnouncementRequest is the typed start input entered in Dex Web Start Flow.
type AnnouncementRequest struct {
	// ChannelID is the 17 to 20 digit ID from Copy Channel ID in Discord's Developer Mode.
	ChannelID string `json:"channelId"`
	// Text is the announcement, 1 to 2000 characters.
	Text string `json:"text"`
	// ReactionEmoji is the bot's reaction on the announcement. Empty selects 📣.
	ReactionEmoji string `json:"reactionEmoji,omitempty"`
}

// AnnouncementOutcome is the Flow completion output.
type AnnouncementOutcome struct {
	ChannelID     string `json:"channelId"`
	ChannelName   string `json:"channelName"`
	MessageID     string `json:"messageId"`
	ReactionEmoji string `json:"reactionEmoji"`
	// IsConfirmed reports that the read-back page held the posted message.
	IsConfirmed bool `json:"isConfirmed"`
}

// Flow posts one announcement to a Discord channel.
type Flow struct {
	dex.FlowDefaults
	connection discord.Connection
}

// NewFlow binds the trusted Discord Connection at registration time.
func NewFlow(connection discord.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordAnnouncementRequest{}),
		dex.DefineStep(discord.NewGetChannelStep(discord.GetChannelStepConfig[AnnouncementRequest]{
			StepType: getAnnouncementChannelStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "announcement-channel", GroupLabel: "Announcement channel", Explanation: "Read the channel to confirm the bot can view it before posting."},
			Connection:          flow.connection,
			MapToOperationInput: MapToGetChannelInput,
			Found:               sdkgo.GoTo(announcementChannelFound{}),
		})),
		dex.DefineStep(announcementChannelFound{}),
		dex.DefineStep(discord.NewCreateMessageStep(discord.CreateMessageStepConfig[AnnouncementRequest]{
			StepType: postAnnouncementMessageStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "announcement-post", GroupLabel: "Announcement post", Explanation: "Post the announcement once, deduplicated by a Discord nonce across retries."},
			Connection:          flow.connection,
			MapToOperationInput: MapToCreateMessageInput,
			Created:             sdkgo.GoTo(announcementPosted{}),
		})),
		dex.DefineStep(announcementPosted{}),
		dex.DefineStep(discord.NewAddReactionStep(discord.AddReactionStepConfig[discord.AddReactionInput]{
			StepType: reactToAnnouncementStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "announcement-reaction", GroupLabel: "Announcement reaction", Explanation: "Add the bot's reaction to the posted announcement."},
			Connection:          flow.connection,
			MapToOperationInput: func(input discord.AddReactionInput) discord.AddReactionInput { return input },
			Added:               sdkgo.GoTo(announcementReacted{}),
		})),
		dex.DefineStep(announcementReacted{}),
		dex.DefineStep(discord.NewListChannelMessagesStep(discord.ListChannelMessagesStepConfig[discord.ListChannelMessagesInput]{
			StepType: readBackAnnouncementChannelStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "announcement-confirmation", GroupLabel: "Announcement confirmation", Explanation: "Read the channel's newest messages back to confirm the announcement."},
			Connection:          flow.connection,
			MapToOperationInput: func(input discord.ListChannelMessagesInput) discord.ListChannelMessagesInput { return input },
			Listed:              sdkgo.GoTo(completeAnnouncement{}),
		})),
		dex.DefineStep(completeAnnouncement{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		announcementRequestAttribute, announcementChannelAttribute, announcementMessageAttribute, announcementOutcomeAttribute,
	}}
}

// dex:field attribute-key:discord-announcement-request value-type:json editable:false description:"Announcement request"
// dex:field attribute-key:discord-announcement-outcome value-type:json editable:false description:"Announcement outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := announcementInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"discord-announcement-request": request,
		"discord-announcement-outcome": outcome,
	}}, nil
}

// dex:field attribute-key:discord-announcement-request value-type:json editable:false description:"Channel, text, and reaction"
// dex:field attribute-key:discord-announcement-outcome value-type:json editable:false description:"Posted message and read-back confirmation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := announcementInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"discord-announcement-request": request,
		"discord-announcement-outcome": outcome,
	}}, nil
}

func announcementInspection(ctx dex.Context) (AnnouncementRequest, AnnouncementOutcome, error) {
	request, err := optionalAttribute(ctx, announcementRequestAttribute)
	if err != nil {
		return AnnouncementRequest{}, AnnouncementOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, announcementOutcomeAttribute)
	if err != nil {
		return AnnouncementRequest{}, AnnouncementOutcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// MapToGetChannelInput reads the requested channel.
func MapToGetChannelInput(request AnnouncementRequest) discord.GetChannelInput {
	return discord.GetChannelInput{ChannelID: request.ChannelID}
}

// MapToCreateMessageInput posts the announcement text to the requested channel.
func MapToCreateMessageInput(request AnnouncementRequest) discord.CreateMessageInput {
	return discord.CreateMessageInput{ChannelID: request.ChannelID, Content: request.Text}
}

// PrepareAnnouncementRequest trims and validates request and applies the default reaction.
func PrepareAnnouncementRequest(request AnnouncementRequest) (AnnouncementRequest, error) {
	request.ChannelID = strings.TrimSpace(request.ChannelID)
	request.ReactionEmoji = strings.TrimSpace(request.ReactionEmoji)
	if length := len(request.ChannelID); length < 17 || length > 20 || strings.Trim(request.ChannelID, "0123456789") != "" {
		return AnnouncementRequest{}, errors.New("channelId must be a Discord channel ID of 17 to 20 digits")
	}
	if strings.TrimSpace(request.Text) == "" || utf8.RuneCountInString(request.Text) > discord.MaximumMessageCharacters {
		return AnnouncementRequest{}, fmt.Errorf("text must hold 1 to %d characters", discord.MaximumMessageCharacters)
	}
	if request.ReactionEmoji == "" {
		request.ReactionEmoji = defaultReactionEmoji
	}
	return request, nil
}

// dex:group group-id:announcement-request group-label:"Announcement request"
// dex:explanation text:"Validate the channel ID and text and persist the request before calling Discord."
type recordAnnouncementRequest struct {
	dex.StepDefaults
}

func (recordAnnouncementRequest) GetStepType() string { return "RecordAnnouncementRequest" }

// WaitFor skips immediately; Dex Web Start Flow invokes the start Step's WaitFor, so StepDefaultsNoWaitFor cannot be used.
func (recordAnnouncementRequest) WaitFor(dex.Context, AnnouncementRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordAnnouncementRequest) Execute(ctx dex.Context, request AnnouncementRequest) (*dex.StepDecision, error) {
	prepared, err := PrepareAnnouncementRequest(request)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := announcementRequestAttribute.Set(ctx, prepared); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[AnnouncementRequest](getAnnouncementChannelStepType), prepared), nil
}

// dex:group group-id:announcement-channel group-label:"Announcement channel"
// dex:explanation text:"Record the visible channel and continue to the post with the recorded request."
type announcementChannelFound struct {
	dex.StepDefaultsNoWaitFor[discord.GetChannelResult]
}

func (announcementChannelFound) GetStepType() string { return "AnnouncementChannelFound" }

func (announcementChannelFound) Execute(ctx dex.Context, result discord.GetChannelResult) (*dex.StepDecision, error) {
	request, err := announcementRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := announcementChannelAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[AnnouncementRequest](postAnnouncementMessageStepType), request), nil
}

// dex:group group-id:announcement-post group-label:"Announcement post"
// dex:explanation text:"Record the posted message and continue to the reaction."
type announcementPosted struct {
	dex.StepDefaultsNoWaitFor[discord.CreateMessageResult]
}

func (announcementPosted) GetStepType() string { return "AnnouncementPosted" }

func (announcementPosted) Execute(ctx dex.Context, result discord.CreateMessageResult) (*dex.StepDecision, error) {
	request, err := announcementRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := announcementMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	reaction := discord.AddReactionInput{ChannelID: request.ChannelID, MessageID: result.Value.ID, Emoji: request.ReactionEmoji}
	return dex.GoTo(sdkgo.StepRef[discord.AddReactionInput](reactToAnnouncementStepType), reaction), nil
}

// dex:group group-id:announcement-reaction group-label:"Announcement reaction"
// dex:explanation text:"Continue to the bounded read-back of the channel's newest messages."
type announcementReacted struct {
	dex.StepDefaultsNoWaitFor[discord.AddReactionResult]
}

func (announcementReacted) GetStepType() string { return "AnnouncementReacted" }

func (announcementReacted) Execute(_ dex.Context, result discord.AddReactionResult) (*dex.StepDecision, error) {
	readBack := discord.ListChannelMessagesInput{ChannelID: result.Value.ChannelID, Limit: readBackPageSize}
	return dex.GoTo(sdkgo.StepRef[discord.ListChannelMessagesInput](readBackAnnouncementChannelStepType), readBack), nil
}

// dex:group group-id:announcement-confirmation group-label:"Announcement confirmation"
// dex:explanation text:"Confirm the announcement is in the read-back page and complete the Flow."
type completeAnnouncement struct {
	dex.StepDefaultsNoWaitFor[discord.ListChannelMessagesResult]
}

func (completeAnnouncement) GetStepType() string { return "CompleteAnnouncement" }

func (completeAnnouncement) Execute(ctx dex.Context, result discord.ListChannelMessagesResult) (*dex.StepDecision, error) {
	request, err := announcementRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	channel, err := announcementChannelAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	message, err := announcementMessageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := AnnouncementOutcome{
		ChannelID: request.ChannelID, ChannelName: channel.Name, MessageID: message.ID, ReactionEmoji: request.ReactionEmoji,
		IsConfirmed: ContainsMessage(result.Value, message.ID),
	}
	if err := announcementOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// ContainsMessage reports whether page holds the message with messageID.
func ContainsMessage(page discord.MessagePage, messageID string) bool {
	for _, message := range page.Messages {
		if message.ID == messageID {
			return true
		}
	}
	return false
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay

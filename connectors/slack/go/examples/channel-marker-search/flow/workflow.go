// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package channelmarkersearch demonstrates the Slack getWorkspaceIdentity and listChannelMessages Steps in a
// runnable Dex Flow that searches recent channel history for marker text the app placed in its own post.
package channelmarkersearch

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex identity for this example.
const FlowType = "SlackChannelMarkerSearch"

// ConnectionName is the static Dex Web connection used by the example.
const ConnectionName = "slack-channel-marker-search"

const (
	workspaceIdentityStepType   = "GetWorkspaceIdentitySlack"
	listChannelMessagesStepType = "ListChannelMessagesSlack"
	defaultMaximumPages         = 3
	maximumMaximumPages         = 10
)

var (
	searchRequestAttribute  = dex.DefineAttribute[SearchRequest]("slack-marker-search-request")
	botIdentityAttribute    = dex.DefineAttribute[slack.GetWorkspaceIdentityOutput]("slack-marker-search-bot")
	searchProgressAttribute = dex.DefineAttribute[SearchProgress]("slack-marker-search-progress")
)

// SearchRequest is entered in Dex Web Start Flow, such as {"channelId":"C0123456789","marker":"ref:order-1042"}.
type SearchRequest struct {
	// ChannelID is the Slack channel to search, such as C0123456789.
	ChannelID string `json:"channelId"`
	// Marker is the literal text the application placed in its own post. Matching is case-sensitive and
	// considers only messages the connection's own bot posted.
	Marker string `json:"marker"`
	// OldestTimestamp limits the search to messages after this Slack timestamp, such as the time the Flow
	// first tried to post. Blank searches back as far as MaximumPages allows.
	OldestTimestamp string `json:"oldestTimestamp,omitempty"`
	// MaximumPages bounds the history pages read: 1 through 10; blank or 0 reads at most 3 pages.
	MaximumPages int `json:"maximumPages,omitempty"`
}

// SearchProgress records how much channel history the Flow has read.
type SearchProgress struct {
	// PagesRead is the number of channel history pages read so far.
	PagesRead int `json:"pagesRead"`
}

// SearchOutput reports whether the marker was found within the bounded history.
type SearchOutput struct {
	// IsFound is true when a top-level message the app posted contains the marker.
	IsFound bool `json:"isFound"`
	// Message is the app's newest message containing the marker; nil when IsFound is false.
	Message *slack.ChannelMessage `json:"message,omitempty"`
	// PagesRead is the number of channel history pages read.
	PagesRead int `json:"pagesRead"`
}

// Flow reads the bot identity, then newest-first channel history pages until it finds the app's marker post
// or reaches the page bound.
type Flow struct {
	dex.FlowDefaults
	connection slack.Connection
}

// NewFlow binds the connection at registration time.
func NewFlow(connection slack.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow type.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the request, bot identity, and progress Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchRequestAttribute, botIdentityAttribute, searchProgressAttribute}}
}

// GetRPCs registers read-only summary and display RPCs for Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored request, bot identity, and search progress when available.
// dex:field attribute-key:slack-marker-search-request value-type:json editable:false description:"Slack marker search request"
// dex:field attribute-key:slack-marker-search-bot value-type:json editable:false description:"Slack bot whose posts are searched"
// dex:field attribute-key:slack-marker-search-progress value-type:json editable:false description:"Slack history pages read"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, bot, progress, err := searchInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-marker-search-request": request, "slack-marker-search-bot": bot, "slack-marker-search-progress": progress,
	}}, nil
}

// GetDexDisplay returns the stored request, bot identity, and search progress when available.
// dex:field attribute-key:slack-marker-search-request value-type:json editable:false description:"Slack marker search request"
// dex:field attribute-key:slack-marker-search-bot value-type:json editable:false description:"Slack bot whose posts are searched"
// dex:field attribute-key:slack-marker-search-progress value-type:json editable:false description:"Slack history pages read"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, bot, progress, err := searchInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"slack-marker-search-request": request, "slack-marker-search-bot": bot, "slack-marker-search-progress": progress,
	}}, nil
}

func searchInspection(ctx dex.Context) (SearchRequest, slack.GetWorkspaceIdentityOutput, SearchProgress, error) {
	request, err := optionalAttribute(ctx, searchRequestAttribute)
	if err != nil {
		return SearchRequest{}, slack.GetWorkspaceIdentityOutput{}, SearchProgress{}, err
	}
	bot, err := optionalAttribute(ctx, botIdentityAttribute)
	if err != nil {
		return SearchRequest{}, slack.GetWorkspaceIdentityOutput{}, SearchProgress{}, err
	}
	progress, err := optionalAttribute(ctx, searchProgressAttribute)
	return request, bot, progress, err
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		var zero T
		return zero, nil
	}
	return value, err
}

// GetSteps registers the start, provider, bot identity, and page inspection Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSearch{}),
		dex.DefineStep(slack.NewGetWorkspaceIdentityStep(slack.GetWorkspaceIdentityStepConfig[SearchRequest]{
			StepType: workspaceIdentityStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "slack", GroupLabel: "Slack", Explanation: "Read the bot identity whose posts the search considers."},
			MapToOperationInput: MapToWorkspaceIdentityInput,
			Read:                sdkgo.GoTo(recordBotIdentity{}),
		})),
		dex.DefineStep(recordBotIdentity{}),
		dex.DefineStep(slack.NewListChannelMessagesStep(slack.ListChannelMessagesStepConfig[slack.ListChannelMessagesInput]{
			StepType: listChannelMessagesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "slack", GroupLabel: "Slack", Explanation: "Read one newest-first page of the channel's top-level messages."},
			MapToOperationInput: MapToListChannelMessagesInput,
			Read:                sdkgo.GoTo(inspectPage{}),
		})),
		dex.DefineStep(inspectPage{}),
	}
}

// MapToWorkspaceIdentityInput ignores the request because the connection determines the bot.
func MapToWorkspaceIdentityInput(SearchRequest) slack.GetWorkspaceIdentityInput {
	return slack.GetWorkspaceIdentityInput{}
}

// MapToListChannelMessagesInput passes the page selection built by the application Steps to Slack.
func MapToListChannelMessagesInput(input slack.ListChannelMessagesInput) slack.ListChannelMessagesInput {
	return input
}

// FindMarkerMessage returns the newest message on a page that bot posted and whose text contains marker.
// A person quoting the marker does not match, so the application never mistakes a quote for its own post.
func FindMarkerMessage(messages []slack.ChannelMessage, marker string, bot slack.GetWorkspaceIdentityOutput) (slack.ChannelMessage, bool) {
	for _, message := range messages {
		if IsPostedByBot(message, bot) && strings.Contains(message.Text, marker) {
			return message, true
		}
	}
	return slack.ChannelMessage{}, false
}

// IsPostedByBot reports whether bot posted message, by its bot ID or its bot user ID.
func IsPostedByBot(message slack.ChannelMessage, bot slack.GetWorkspaceIdentityOutput) bool {
	return (bot.BotID != "" && message.BotID == bot.BotID) || (bot.BotUserID != "" && message.UserID == bot.BotUserID)
}

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Validate and store the search request, then read the bot identity."
type startSearch struct{ dex.StepDefaults }

func (startSearch) GetStepType() string { return "StartSlackChannelMarkerSearch" }
func (startSearch) WaitFor(dex.Context, SearchRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSearch) Execute(ctx dex.Context, request SearchRequest) (*dex.StepDecision, error) {
	if strings.TrimSpace(request.ChannelID) == "" || strings.TrimSpace(request.Marker) == "" || request.MaximumPages < 0 || request.MaximumPages > maximumMaximumPages {
		return dex.ForceFail("Slack channel, non-blank marker, and maximum pages 1 through 10 (blank or 0 reads 3) are required"), nil
	}
	if request.MaximumPages == 0 {
		request.MaximumPages = defaultMaximumPages
	}
	if err := searchRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	if err := searchProgressAttribute.Set(ctx, SearchProgress{}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SearchRequest](workspaceIdentityStepType), request), nil
}

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Store the bot identity, then read the newest history page."
type recordBotIdentity struct {
	dex.StepDefaultsNoWaitFor[slack.GetWorkspaceIdentityResult]
}

func (recordBotIdentity) GetStepType() string { return "RecordSlackMarkerSearchBot" }
func (recordBotIdentity) Execute(ctx dex.Context, result slack.GetWorkspaceIdentityResult) (*dex.StepDecision, error) {
	if err := botIdentityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	request, err := searchRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[slack.ListChannelMessagesInput](listChannelMessagesStepType), historyPageInput(request, "")), nil
}

// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Search the bot's posts on the page for the marker, then read an older page or complete."
type inspectPage struct {
	dex.StepDefaultsNoWaitFor[slack.ListChannelMessagesResult]
}

func (inspectPage) GetStepType() string { return "InspectSlackChannelHistoryPage" }
func (inspectPage) Execute(ctx dex.Context, result slack.ListChannelMessagesResult) (*dex.StepDecision, error) {
	request, err := searchRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	bot, err := botIdentityAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	progress, err := searchProgressAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	progress.PagesRead++
	if err := searchProgressAttribute.Set(ctx, progress); err != nil {
		return nil, err
	}
	if message, isFound := FindMarkerMessage(result.Value.Messages, request.Marker, bot); isFound {
		return dex.GracefulComplete(SearchOutput{IsFound: true, Message: &message, PagesRead: progress.PagesRead}), nil
	}
	if result.Value.NextCursor == "" || progress.PagesRead >= request.MaximumPages {
		return dex.GracefulComplete(SearchOutput{PagesRead: progress.PagesRead}), nil
	}
	return dex.GoTo(sdkgo.StepRef[slack.ListChannelMessagesInput](listChannelMessagesStepType), historyPageInput(request, result.Value.NextCursor)), nil
}

func historyPageInput(request SearchRequest, cursor string) slack.ListChannelMessagesInput {
	return slack.ListChannelMessagesInput{ChannelID: request.ChannelID, OldestTimestamp: request.OldestTimestamp, Cursor: cursor}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay

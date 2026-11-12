// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultChannelPageLimit = 200
	maximumChannelPageLimit = 999
)

// ListChannelsInput selects one page of Slack channels visible to the connection's app.
type ListChannelsInput struct {
	// NameFilter keeps only channels whose name contains this text, ignoring case, surrounding spaces, and one leading #,
	// so "#General" matches general and general-ops. Blank keeps every channel on the page.
	NameFilter string `json:"nameFilter,omitempty"`
	// Visibility is public for public channels only or private for private channels only. Blank lists both.
	Visibility string `json:"visibility,omitempty" connectorEnum:"public,private"`
	// MemberOnly lists only channels the app has joined, which are the channels it can post to. False also lists
	// public channels the app has not joined.
	MemberOnly bool `json:"memberOnly,omitempty"`
	// Limit is the number of channels Slack examines for this page, from 1 through 999, Slack's maximum. Zero uses 200. The name
	// filter runs after Slack returns the page, so fewer channels can be returned.
	Limit int `json:"limit,omitempty"`
	// Cursor is the nextCursor from the previous page. Blank reads the first page.
	Cursor string `json:"cursor,omitempty"`
}

// Channel describes one Slack channel.
type Channel struct {
	// ChannelID is the Slack channel identifier, such as C0123456789, accepted as channelId by the other Slack operations.
	ChannelID string `json:"channelId"`
	// Name is the channel name without the leading #.
	Name string `json:"name"`
	// IsPrivate is true for a private channel.
	IsPrivate bool `json:"isPrivate"`
	// IsMember is true when the app has joined the channel and can post to it.
	IsMember bool `json:"isMember"`
	// Topic is the channel topic; blank when none is set.
	Topic string `json:"topic,omitempty"`
	// Purpose is the channel purpose; blank when none is set.
	Purpose string `json:"purpose,omitempty"`
}

// ListChannelsOutput contains one filtered page of Slack channels.
type ListChannelsOutput struct {
	// Channels are the page's channels that match the input filters, in Slack's order.
	Channels []Channel `json:"channels"`
	// NextCursor continues the listing when non-blank, even if Channels is empty; blank means the listing ended.
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetWorkspaceIdentityInput takes no fields because the connection determines the workspace.
type GetWorkspaceIdentityInput struct{}

// GetWorkspaceIdentityOutput identifies the Slack workspace and bot that the connection is authorized for.
type GetWorkspaceIdentityOutput struct {
	// TeamID is the Slack workspace identifier, such as T0123456789.
	TeamID string `json:"teamId"`
	// TeamName is the workspace name shown in Slack.
	TeamName string `json:"teamName"`
	// WorkspaceURL is the workspace's Slack URL, such as https://acme.slack.com/.
	WorkspaceURL string `json:"workspaceUrl"`
	// EnterpriseID is the Enterprise Grid organization identifier; blank outside Enterprise Grid.
	EnterpriseID string `json:"enterpriseId,omitempty"`
	// BotUserID is the Slack user identifier of the bot that posts messages, such as U0BOT123456.
	BotUserID string `json:"botUserId"`
	// BotUserName is the bot user's Slack name.
	BotUserName string `json:"botUserName"`
	// BotID is the Slack bot identifier, such as B0123456789.
	BotID string `json:"botId"`
}

// ListChannelsOperation implements the list channels connector operation.
type ListChannelsOperation struct{ client *Client }

// GetWorkspaceIdentityOperation implements the get workspace identity connector operation.
type GetWorkspaceIdentityOperation struct{ client *Client }

type slackChannelListResponse struct {
	Channels         []slackChannel `json:"channels"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

type slackChannel struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	IsPrivate bool             `json:"is_private"`
	IsMember  bool             `json:"is_member"`
	Topic     slackChannelText `json:"topic"`
	Purpose   slackChannelText `json:"purpose"`
}

type slackChannelText struct {
	Value string `json:"value"`
}

type slackIdentityResponse struct {
	URL          string `json:"url"`
	Team         string `json:"team"`
	User         string `json:"user"`
	TeamID       string `json:"team_id"`
	UserID       string `json:"user_id"`
	BotID        string `json:"bot_id"`
	EnterpriseID string `json:"enterprise_id"`
}

// Definition returns the immutable connector operation definition.
func (ListChannelsOperation) Definition() sdkgo.QueryDefinition {
	return ListChannelsDefinition
}

// Invoke reads one Slack channel page with the bot token and filters it by name.
func (operation ListChannelsOperation) Invoke(call sdkgo.Call, input ListChannelsInput) sdkgo.QueryAttempt[ListChannelsOutput] {
	const operationName = "listChannels"
	channelTypes, isVisibilityValid := slackChannelTypes(input.Visibility)
	if !isVisibilityValid || input.Limit < 0 || input.Limit > maximumChannelPageLimit {
		return sdkgo.NewQueryBranch(ListChannelsBranchDefect, ListChannelsOutput{}, slackFailurePointer(operationName, sdkgo.FailureValidation, "visibility must be blank, public, or private, and limit must be from 0 through 999"), sdkgo.Receipt{})
	}
	credentials, failure, _ := operation.client.resolveCredentials(call, operationName, botCredentialToken)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListChannelsBranchDefect, ListChannelsOutput{}, failure, sdkgo.Receipt{})
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultChannelPageLimit
	}
	values := url.Values{"exclude_archived": {"true"}, "types": {channelTypes}, "limit": {strconv.Itoa(limit)}}
	if input.Cursor != "" {
		values.Set("cursor", input.Cursor)
	}
	// users.conversations lists only joined channels and omits is_member.
	method := "conversations.list"
	if input.MemberOnly {
		method = "users.conversations"
	}
	response, err := operation.client.get(call, &credentials, botCredentialToken, method, values)
	if attempt, isClassified := classifyQueryResponse(operation.client, call, operationName, response, err, ListChannelsBranchInvalidResponse, ListChannelsBranchProviderRejected, ListChannelsOutput{}, "Slack channels are temporarily unavailable", "Slack rate limited the channel query"); isClassified {
		return attempt
	}
	var decoded slackChannelListResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		return sdkgo.NewQueryBranch(ListChannelsBranchInvalidResponse, ListChannelsOutput{}, slackFailurePointer(operationName, sdkgo.FailureProtocol, "Slack returned an invalid channel list"), operation.client.receipt(call, response, ""))
	}
	nameFilter := channelNameFilter(input.NameFilter)
	channels := make([]Channel, 0, len(decoded.Channels))
	for _, channel := range decoded.Channels {
		if channel.ID == "" {
			return sdkgo.NewQueryBranch(ListChannelsBranchInvalidResponse, ListChannelsOutput{}, slackFailurePointer(operationName, sdkgo.FailureProtocol, "Slack returned a channel without an ID"), operation.client.receipt(call, response, ""))
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(channel.Name), nameFilter) {
			continue
		}
		channels = append(channels, Channel{
			ChannelID: channel.ID, Name: channel.Name, IsPrivate: channel.IsPrivate,
			IsMember: channel.IsMember || input.MemberOnly, Topic: channel.Topic.Value, Purpose: channel.Purpose.Value,
		})
	}
	return sdkgo.NewQueryBranch(ListChannelsBranchListed, ListChannelsOutput{Channels: channels, NextCursor: decoded.ResponseMetadata.NextCursor}, nil, operation.client.receipt(call, response, ""))
}

// Definition returns the immutable connector operation definition.
func (GetWorkspaceIdentityOperation) Definition() sdkgo.QueryDefinition {
	return GetWorkspaceIdentityDefinition
}

// Invoke reads the bot token's workspace and identity with a POST to auth.test, as Slack documents it.
func (operation GetWorkspaceIdentityOperation) Invoke(call sdkgo.Call, _ GetWorkspaceIdentityInput) sdkgo.QueryAttempt[GetWorkspaceIdentityOutput] {
	const operationName = "getWorkspaceIdentity"
	credentials, failure, _ := operation.client.resolveCredentials(call, operationName, botCredentialToken)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetWorkspaceIdentityBranchDefect, GetWorkspaceIdentityOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.postForm(call, &credentials, botCredentialToken, "auth.test", nil)
	if attempt, isClassified := classifyQueryResponse(operation.client, call, operationName, response, err, GetWorkspaceIdentityBranchInvalidResponse, GetWorkspaceIdentityBranchProviderRejected, GetWorkspaceIdentityOutput{}, "Slack workspace identity is temporarily unavailable", "Slack rate limited the identity query"); isClassified {
		return attempt
	}
	var decoded slackIdentityResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil || decoded.UserID == "" || (decoded.TeamID == "" && decoded.EnterpriseID == "") {
		return sdkgo.NewQueryBranch(GetWorkspaceIdentityBranchInvalidResponse, GetWorkspaceIdentityOutput{}, slackFailurePointer(operationName, sdkgo.FailureProtocol, "Slack returned an invalid workspace identity"), operation.client.receipt(call, response, ""))
	}
	return sdkgo.NewQueryBranch(GetWorkspaceIdentityBranchRead, GetWorkspaceIdentityOutput{
		TeamID: decoded.TeamID, TeamName: decoded.Team, WorkspaceURL: decoded.URL, EnterpriseID: decoded.EnterpriseID,
		BotUserID: decoded.UserID, BotUserName: decoded.User, BotID: decoded.BotID,
	}, nil, operation.client.receipt(call, response, decoded.TeamID))
}

// classifyQueryResponse selects the status, transport, and Slack envelope outcome shared by bot-token reads.
// Status comes first because Slack may send 429 or 5xx without a JSON body.
func classifyQueryResponse[Output any](
	client *Client,
	call sdkgo.Call,
	operationName string,
	response providerResponse,
	err error,
	invalidResponse sdkgo.BranchID,
	providerRejected sdkgo.BranchID,
	empty Output,
	unavailableMessage string,
	rateLimitedMessage string,
) (sdkgo.QueryAttempt[Output], bool) {
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](slackFailure(operationName, sdkgo.FailureRateLimit, rateLimitedMessage), retryAfter(response.header)), true
	}
	if response.statusCode >= 500 {
		return sdkgo.NewQueryRetry[Output](slackFailure(operationName, sdkgo.FailureAvailability, unavailableMessage), retryAfter(response.header)), true
	}
	if err != nil {
		if errors.Is(err, errSlackResponseTooLarge) {
			return sdkgo.NewQueryBranch(invalidResponse, empty, slackFailurePointer(operationName, sdkgo.FailureResponseTooLarge, err.Error()), client.receipt(call, response, "")), true
		}
		if errors.Is(err, errSlackResponseInvalid) {
			return sdkgo.NewQueryBranch(invalidResponse, empty, slackFailurePointer(operationName, sdkgo.FailureProtocol, err.Error()), client.receipt(call, response, "")), true
		}
		return sdkgo.NewQueryRetry[Output](slackFailure(operationName, sdkgo.FailureAvailability, unavailableMessage), 0), true
	}
	if failure := classifyResponse(operationName, response); failure != nil {
		return sdkgo.NewQueryBranch(providerRejected, empty, failure, client.receipt(call, response, "")), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

// slackChannelTypes maps a public visibility value to Slack's conversation types parameter.
func slackChannelTypes(visibility string) (string, bool) {
	switch visibility {
	case "":
		return "public_channel,private_channel", true
	case "public":
		return "public_channel", true
	case "private":
		return "private_channel", true
	default:
		return "", false
	}
}

// channelNameFilter trims spaces and one leading # and lowercases the filter.
func channelNameFilter(nameFilter string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(nameFilter), "#"))
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Channel is a Teams channel's non-secret metadata.
type Channel struct {
	// ID is the Graph channel ID.
	ID string `json:"id"`
	// DisplayName is the channel label.
	DisplayName string `json:"displayName"`
	// Description is the optional channel description.
	Description string `json:"description,omitempty"`
	// MembershipType is standard, private, or shared.
	MembershipType string `json:"membershipType,omitempty"`
}

// Chat is a Teams chat's non-secret metadata, without its messages or members.
type Chat struct {
	// ID is the Graph chat ID.
	ID string `json:"id"`
	// ChatType is oneOnOne, group, or meeting.
	ChatType string `json:"chatType"`
	// Topic is the optional group chat topic.
	Topic string `json:"topic,omitempty"`
}

// ListChannelsInput chooses a team's channel page.
type ListChannelsInput struct {
	// TeamID is the team's GUID.
	TeamID string `json:"teamId"`
	// Cursor is the previous page's opaque nextCursor; blank starts at page one.
	Cursor string `json:"cursor,omitempty"`
}

// ListChannelsOutput contains one channel page.
type ListChannelsOutput struct {
	// Channels lists the returned channels.
	Channels []Channel `json:"channels"`
	// NextCursor continues the same channel list, or is blank.
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetChannelInput identifies one channel.
type GetChannelInput struct {
	// TeamID is the team's GUID.
	TeamID string `json:"teamId"`
	// ChannelID is the channel's Graph ID.
	ChannelID string `json:"channelId"`
}

// ListChannelMessagesInput chooses a page of root channel messages.
type ListChannelMessagesInput struct {
	// TeamID is the team's GUID.
	TeamID string `json:"teamId"`
	// ChannelID is the channel's Graph ID.
	ChannelID string `json:"channelId"`
	// PageSize is 1 to 50; zero requests 20.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the previous page's opaque nextCursor; blank starts at page one.
	Cursor string `json:"cursor,omitempty"`
	// MaxTextCharacters bounds each message's text at 1 to 100000 characters; zero selects 4000.
	MaxTextCharacters int `json:"maxTextCharacters,omitempty"`
}

// ListChannelMessagesOutput contains one root-message page.
type ListChannelMessagesOutput struct {
	// Messages lists the returned root messages.
	Messages []Message `json:"messages"`
	// NextCursor continues the same message list, or is blank.
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetChannelMessageInput identifies one root channel message.
type GetChannelMessageInput struct {
	// TeamID is the team's GUID.
	TeamID string `json:"teamId"`
	// ChannelID is the channel's Graph ID.
	ChannelID string `json:"channelId"`
	// MessageID is the decimal Teams message ID.
	MessageID string `json:"messageId"`
	// MaxTextCharacters bounds returned text at 1 to 100000 characters; zero selects 4000.
	MaxTextCharacters int `json:"maxTextCharacters,omitempty"`
}

// ListChatsInput chooses a page of the signed-in user's chats.
type ListChatsInput struct {
	// Cursor is the previous page's opaque nextCursor; blank starts at page one.
	Cursor string `json:"cursor,omitempty"`
}

// ListChatsOutput contains one chat page.
type ListChatsOutput struct {
	// Chats lists the returned chat metadata without messages.
	Chats []Chat `json:"chats"`
	// NextCursor continues the same chat list, or is blank.
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetChatInput identifies one chat.
type GetChatInput struct {
	// ChatID is the chat's Graph ID.
	ChatID string `json:"chatId"`
}

// ListChannelsOperation implements the listChannels query.
type ListChannelsOperation struct{ client *Client }

// GetChannelOperation implements the getChannel query.
type GetChannelOperation struct{ client *Client }

// ListChannelMessagesOperation implements the listChannelMessages query.
type ListChannelMessagesOperation struct{ client *Client }

// GetChannelMessageOperation implements the getChannelMessage query.
type GetChannelMessageOperation struct{ client *Client }

// ListChatsOperation implements the listChats query.
type ListChatsOperation struct{ client *Client }

// GetChatOperation implements the getChat query.
type GetChatOperation struct{ client *Client }

// Definition returns the query definition.
func (ListChannelsOperation) Definition() sdkgo.QueryDefinition { return ListChannelsDefinition }

// Definition returns the query definition.
func (GetChannelOperation) Definition() sdkgo.QueryDefinition { return GetChannelDefinition }

// Definition returns the query definition.
func (ListChannelMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListChannelMessagesDefinition
}

// Definition returns the query definition.
func (GetChannelMessageOperation) Definition() sdkgo.QueryDefinition {
	return GetChannelMessageDefinition
}

// Definition returns the query definition.
func (ListChatsOperation) Definition() sdkgo.QueryDefinition { return ListChatsDefinition }

// Definition returns the query definition.
func (GetChatOperation) Definition() sdkgo.QueryDefinition { return GetChatDefinition }

type teamsReadBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func invokeTeamsRead[T any](client *Client, call sdkgo.Call, operationID, subject, objectID string, request graphRequest, inputError error, branches teamsReadBranches, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	if inputError != nil {
		return sdkgo.NewQueryBranch(branches.defect, zero, failurePointer(operationID, sdkgo.FailureValidation, inputError.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, operationID)
	if failure != nil {
		if failure.isReauthorizationRequired {
			return sdkgo.NewQueryBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[T](failure.failure, 0)
	}
	defer cancel()
	result := client.exchange(session, request)
	classification := client.classifyRead(operationID, subject, result)
	receipt := client.receipt(session, result.response, objectID)
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewQueryRetry[T](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(branches.notFound, zero, &classification.failure, receipt)
	case readRejected:
		return sdkgo.NewQueryBranch(branches.rejected, zero, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(branches.invalid, zero, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(branches.defect, zero, &classification.failure, sdkgo.Receipt{})
	case readSucceeded:
	default:
		return sdkgo.NewQueryBranch(branches.defect, zero, failurePointer(operationID, sdkgo.FailureLocalDefect, "unexpected read outcome"), sdkgo.Receipt{})
	}
	value, err := decode(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(branches.invalid, zero, failurePointer(operationID, sdkgo.FailureProtocol, "Microsoft Graph returned invalid "+subject), receipt)
	}
	return sdkgo.NewQueryBranch(branches.success, value, nil, receipt)
}

func (client *Client) resourcePageRequest(path, cursor string, pageSize int) (graphRequest, error) {
	if cursor != "" {
		if len(cursor) > maximumCursorBytes || !isSafeProviderURL(cursor) {
			return graphRequest{}, errors.New("cursor must be a previous nextCursor")
		}
		parsed, err := url.Parse(cursor)
		if err != nil || !client.isGraphLink(parsed) || !strings.EqualFold(parsed.Path, client.endpointURL.Path+path) {
			return graphRequest{}, errors.New("cursor must continue the same Graph collection")
		}
		return graphRequest{method: http.MethodGet, absoluteURL: cursor}, nil
	}
	request := graphRequest{method: http.MethodGet, path: path}
	if pageSize > 0 {
		request.query = url.Values{"$top": {strconv.Itoa(pageSize)}}
	}
	return request, nil
}

func decodeResourcePage[T any](client *Client, body []byte, path string, validate func(T) bool) ([]T, string, error) {
	var page struct {
		Value    []T    `json:"value"`
		NextLink string `json:"@odata.nextLink"`
	}
	if json.Unmarshal(body, &page) != nil || page.Value == nil {
		return nil, "", errors.New("invalid collection")
	}
	for _, item := range page.Value {
		if !validate(item) {
			return nil, "", errors.New("invalid item")
		}
	}
	if page.NextLink != "" {
		if _, err := client.resourcePageRequest(path, page.NextLink, 0); err != nil {
			return nil, "", err
		}
	}
	return page.Value, page.NextLink, nil
}

// Invoke lists the visible channels of a team.
func (operation ListChannelsOperation) Invoke(call sdkgo.Call, input ListChannelsInput) sdkgo.QueryAttempt[ListChannelsOutput] {
	path := "/teams/" + input.TeamID + "/channels"
	request, err := operation.client.resourcePageRequest(path, input.Cursor, 0)
	if !teamIDPattern.MatchString(input.TeamID) {
		err = errors.New("teamId must be a team GUID")
	}
	return invokeTeamsRead(operation.client, call, "listChannels", "channels", input.TeamID, request, err, teamsReadBranches{ListChannelsBranchListed, ListChannelsBranchNotFound, ListChannelsBranchProviderRejected, ListChannelsBranchInvalidResponse, ListChannelsBranchDefect}, func(body []byte) (ListChannelsOutput, error) {
		items, next, err := decodeResourcePage(operation.client, body, path, func(item Channel) bool { return channelIDPattern.MatchString(item.ID) && item.DisplayName != "" })
		return ListChannelsOutput{Channels: items, NextCursor: next}, err
	})
}

// Invoke reads one channel's metadata.
func (operation GetChannelOperation) Invoke(call sdkgo.Call, input GetChannelInput) sdkgo.QueryAttempt[Channel] {
	err := validateTeamAndChannel(input.TeamID, input.ChannelID)
	return invokeTeamsRead(operation.client, call, "getChannel", "channel", input.ChannelID, graphRequest{method: http.MethodGet, path: "/teams/" + input.TeamID + "/channels/" + input.ChannelID}, err, teamsReadBranches{GetChannelBranchFound, GetChannelBranchNotFound, GetChannelBranchProviderRejected, GetChannelBranchInvalidResponse, GetChannelBranchDefect}, func(body []byte) (Channel, error) {
		var item Channel
		err := json.Unmarshal(body, &item)
		if err == nil && (item.ID != input.ChannelID || item.DisplayName == "") {
			err = errors.New("invalid channel")
		}
		return item, err
	})
}

// Invoke lists root messages of one channel.
func (operation ListChannelMessagesOperation) Invoke(call sdkgo.Call, input ListChannelMessagesInput) sdkgo.QueryAttempt[ListChannelMessagesOutput] {
	err := validateTeamAndChannel(input.TeamID, input.ChannelID)
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if pageSize < 1 || pageSize > 50 {
		err = errors.New("pageSize must be 1 through 50")
	}
	maxText := input.MaxTextCharacters
	if maxText == 0 {
		maxText = defaultMaxTextCharacters
	}
	if maxText < 1 || maxText > maximumMaxTextCharacters {
		err = errors.New("maxTextCharacters must be 1 through 100000")
	}
	path := "/teams/" + input.TeamID + "/channels/" + input.ChannelID + "/messages"
	request, cursorErr := operation.client.resourcePageRequest(path, input.Cursor, pageSize)
	if err == nil {
		err = cursorErr
	}
	return invokeTeamsRead(operation.client, call, "listChannelMessages", "channel messages", input.ChannelID, request, err, teamsReadBranches{ListChannelMessagesBranchListed, ListChannelMessagesBranchNotFound, ListChannelMessagesBranchProviderRejected, ListChannelMessagesBranchInvalidResponse, ListChannelMessagesBranchDefect}, func(body []byte) (ListChannelMessagesOutput, error) {
		var page chatMessageCollection
		if json.Unmarshal(body, &page) != nil || page.Value == nil {
			return ListChannelMessagesOutput{}, errors.New("invalid message page")
		}
		messages := make([]Message, 0, len(page.Value))
		for _, resource := range page.Value {
			message, decodeErr := decodeMessage(resource, maxText)
			if decodeErr != nil {
				return ListChannelMessagesOutput{}, decodeErr
			}
			messages = append(messages, message)
		}
		if page.NextLink != "" {
			if _, cursorErr := operation.client.resourcePageRequest(path, page.NextLink, 0); cursorErr != nil {
				return ListChannelMessagesOutput{}, cursorErr
			}
		}
		return ListChannelMessagesOutput{Messages: messages, NextCursor: page.NextLink}, nil
	})
}

// Invoke reads one root channel message.
func (operation GetChannelMessageOperation) Invoke(call sdkgo.Call, input GetChannelMessageInput) sdkgo.QueryAttempt[Message] {
	err := validateTeamAndChannel(input.TeamID, input.ChannelID)
	if err == nil {
		err = validateMessageID("messageId", input.MessageID)
	}
	maxText := input.MaxTextCharacters
	if maxText == 0 {
		maxText = defaultMaxTextCharacters
	}
	if maxText < 1 || maxText > maximumMaxTextCharacters {
		err = errors.New("maxTextCharacters must be 1 through 100000")
	}
	path := "/teams/" + input.TeamID + "/channels/" + input.ChannelID + "/messages/" + input.MessageID
	return invokeTeamsRead(operation.client, call, "getChannelMessage", "channel message", input.MessageID, graphRequest{method: http.MethodGet, path: path}, err, teamsReadBranches{GetChannelMessageBranchFound, GetChannelMessageBranchNotFound, GetChannelMessageBranchProviderRejected, GetChannelMessageBranchInvalidResponse, GetChannelMessageBranchDefect}, func(body []byte) (Message, error) {
		var resource chatMessageResource
		if err := json.Unmarshal(body, &resource); err != nil {
			return Message{}, err
		}
		message, err := decodeMessage(resource, maxText)
		if err == nil && message.ID != input.MessageID {
			err = errors.New("invalid message ID")
		}
		return message, err
	})
}

// Invoke lists the signed-in user's chats without their messages.
func (operation ListChatsOperation) Invoke(call sdkgo.Call, input ListChatsInput) sdkgo.QueryAttempt[ListChatsOutput] {
	path := "/me/chats"
	request, err := operation.client.resourcePageRequest(path, input.Cursor, 0)
	return invokeTeamsRead(operation.client, call, "listChats", "chats", "", request, err, teamsReadBranches{ListChatsBranchListed, ListChatsBranchProviderRejected, ListChatsBranchProviderRejected, ListChatsBranchInvalidResponse, ListChatsBranchDefect}, func(body []byte) (ListChatsOutput, error) {
		items, next, err := decodeResourcePage(operation.client, body, path, func(item Chat) bool { return chatIDPattern.MatchString(item.ID) && item.ChatType != "" })
		return ListChatsOutput{Chats: items, NextCursor: next}, err
	})
}

// Invoke reads one chat's metadata without its messages.
func (operation GetChatOperation) Invoke(call sdkgo.Call, input GetChatInput) sdkgo.QueryAttempt[Chat] {
	var err error
	if !chatIDPattern.MatchString(input.ChatID) {
		err = errors.New("chatId must be a Teams chat ID")
	}
	return invokeTeamsRead(operation.client, call, "getChat", "chat", input.ChatID, graphRequest{method: http.MethodGet, path: "/me/chats/" + input.ChatID}, err, teamsReadBranches{GetChatBranchFound, GetChatBranchNotFound, GetChatBranchProviderRejected, GetChatBranchInvalidResponse, GetChatBranchDefect}, func(body []byte) (Chat, error) {
		var item Chat
		err := json.Unmarshal(body, &item)
		if err == nil && (item.ID != input.ChatID || item.ChatType == "") {
			err = errors.New("invalid chat")
		}
		return item, err
	})
}

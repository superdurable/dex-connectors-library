// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listThreadRepliesOperationID = "listThreadReplies"
	defaultReplyPageSize         = 20
	defaultMaxTextCharacters     = 4000
	maximumMaxTextCharacters     = 100000
	maximumCursorBytes           = 4096
)

// ListThreadRepliesInput selects one page of replies to a channel message.
type ListThreadRepliesInput struct {
	// TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.
	TeamID string `json:"teamId"`
	// ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2.
	ChannelID string `json:"channelId"`
	// MessageID is the root message's ID, such as the MessageID a postChannelMessage Result returned.
	MessageID string `json:"messageId"`
	// PageSize is 1 to 50 replies; zero reads 20. It is ignored with a Cursor, which keeps its own size.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a previous page's NextCursor; empty reads the first page.
	Cursor string `json:"cursor,omitempty"`
	// MaxTextCharacters bounds each reply's Text from 1 to 100000 characters; zero keeps 4000.
	MaxTextCharacters int `json:"maxTextCharacters,omitempty"`
}

// ListThreadRepliesOutput is one page of replies in the order Graph returned them.
type ListThreadRepliesOutput struct {
	// Replies are the page's replies, including deleted replies and system events, which callers may skip.
	Replies []Message `json:"replies"`
	// NextCursor reads the following page; empty means this is the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListThreadRepliesOperation implements the listThreadReplies Query.
type ListThreadRepliesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListThreadRepliesOperation) Definition() sdkgo.QueryDefinition {
	return ListThreadRepliesDefinition
}

// Invoke reads GET /teams/{team-id}/channels/{channel-id}/messages/{message-id}/replies with $top, or the
// page a Cursor names. Graph supports no other query option on this list, documents no order, and in its
// examples returns the newest reply first. A next-page link is returned as NextCursor only when it stays on
// the endpoint's host and names the same thread. The list needs ChannelMessage.Read.All.
func (operation ListThreadRepliesOperation) Invoke(call sdkgo.Call, input ListThreadRepliesInput) sdkgo.QueryAttempt[ListThreadRepliesOutput] {
	client := operation.client
	request, maxTextCharacters, err := client.buildRepliesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchDefect, ListThreadRepliesOutput{}, failurePointer(listThreadRepliesOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, listThreadRepliesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchDefect, ListThreadRepliesOutput{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, request)
	classification := client.classifyRead(listThreadRepliesOperationID, "thread replies", result)
	receipt := client.receipt(session, result.response, strings.TrimSpace(input.MessageID))
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[ListThreadRepliesOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchNotFound, ListThreadRepliesOutput{}, &classification.failure, receipt)
	case readRejected:
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchProviderRejected, ListThreadRepliesOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchInvalidResponse, ListThreadRepliesOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchDefect, ListThreadRepliesOutput{}, &classification.failure, receipt)
	}
	output, err := client.decodeRepliesPage(result.response.body, input, maxTextCharacters)
	if err != nil {
		return sdkgo.NewQueryBranch(ListThreadRepliesBranchInvalidResponse, ListThreadRepliesOutput{},
			failurePointer(listThreadRepliesOperationID, sdkgo.FailureProtocol, "Microsoft Graph returned an invalid reply page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListThreadRepliesBranchRead, output, nil, receipt)
}

// buildRepliesRequest validates the input and returns the first-page request or the cursor's page.
func (client *Client) buildRepliesRequest(input ListThreadRepliesInput) (graphRequest, int, error) {
	teamID, channelID, messageID := strings.TrimSpace(input.TeamID), strings.TrimSpace(input.ChannelID), strings.TrimSpace(input.MessageID)
	if err := validateTeamAndChannel(teamID, channelID); err != nil {
		return graphRequest{}, 0, err
	}
	if err := validateMessageID("messageId", messageID); err != nil {
		return graphRequest{}, 0, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultReplyPageSize
	}
	if pageSize < 1 || pageSize > readBackPageSize {
		return graphRequest{}, 0, fmt.Errorf("pageSize must be from 1 to %d", readBackPageSize)
	}
	maxTextCharacters := input.MaxTextCharacters
	if maxTextCharacters == 0 {
		maxTextCharacters = defaultMaxTextCharacters
	}
	if maxTextCharacters < 1 || maxTextCharacters > maximumMaxTextCharacters {
		return graphRequest{}, 0, fmt.Errorf("maxTextCharacters must be from 1 to %d", maximumMaxTextCharacters)
	}
	repliesPath := threadRepliesPath(teamID, channelID, messageID)
	if input.Cursor == "" {
		return graphRequest{method: http.MethodGet, path: repliesPath, query: url.Values{"$top": {strconv.Itoa(pageSize)}}}, maxTextCharacters, nil
	}
	if !client.isThreadRepliesLink(input.Cursor, repliesPath) {
		return graphRequest{}, 0, errors.New("cursor must be a nextCursor returned for the same thread")
	}
	return graphRequest{method: http.MethodGet, absoluteURL: input.Cursor}, maxTextCharacters, nil
}

func (client *Client) decodeRepliesPage(body []byte, input ListThreadRepliesInput, maxTextCharacters int) (ListThreadRepliesOutput, error) {
	var collection chatMessageCollection
	if err := json.Unmarshal(body, &collection); err != nil || collection.Value == nil {
		return ListThreadRepliesOutput{}, errors.New("the body is not a chatMessage collection")
	}
	output := ListThreadRepliesOutput{Replies: make([]Message, 0, len(collection.Value))}
	for index, resource := range collection.Value {
		message, err := decodeMessage(resource, maxTextCharacters)
		if err != nil {
			return ListThreadRepliesOutput{}, fmt.Errorf("reply %d: %w", index, err)
		}
		output.Replies = append(output.Replies, message)
	}
	if collection.NextLink != "" {
		repliesPath := threadRepliesPath(strings.TrimSpace(input.TeamID), strings.TrimSpace(input.ChannelID), strings.TrimSpace(input.MessageID))
		if !client.isThreadRepliesLink(collection.NextLink, repliesPath) {
			return ListThreadRepliesOutput{}, errors.New("the next-page link does not name this thread on the Graph endpoint")
		}
		output.NextCursor = collection.NextLink
	}
	return output, nil
}

// isThreadRepliesLink accepts an absolute link on the endpoint's scheme and host whose path is this thread's replies.
func (client *Client) isThreadRepliesLink(link string, repliesPath string) bool {
	if len(link) > maximumCursorBytes || !isSafeProviderURL(link) {
		return false
	}
	parsed, err := url.Parse(link)
	if err != nil || !client.isGraphLink(parsed) {
		return false
	}
	// Graph IDs compare without case; a GUID can come back in another case than the request used.
	return strings.EqualFold(parsed.Path, client.endpointURL.Path+repliesPath)
}

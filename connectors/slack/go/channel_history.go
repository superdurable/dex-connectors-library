// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Slack caps conversations.history at 15 messages for non-Marketplace apps installed since May 2025.
const (
	defaultChannelHistoryPageSize = 15
	maximumChannelHistoryPageSize = 15
)

var slackTimestampPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// ListChannelMessagesInput selects one page of a Slack channel's top-level message history.
type ListChannelMessagesInput struct {
	// ChannelID is the Slack channel identifier, such as C0123456789, from listChannels or the channel picker.
	ChannelID string `json:"channelId"`
	// OldestTimestamp keeps only messages after this Slack message timestamp, such as 1710000000.000100.
	// Blank reads from the start of the channel's history.
	OldestTimestamp string `json:"oldestTimestamp,omitempty"`
	// LatestTimestamp keeps only messages before this Slack message timestamp, such as 1710000099.000100.
	// Blank reads up to the newest message.
	LatestTimestamp string `json:"latestTimestamp,omitempty"`
	// IsInclusive also keeps messages whose timestamp equals OldestTimestamp or LatestTimestamp.
	// Slack ignores it when both timestamps are blank.
	IsInclusive bool `json:"isInclusive,omitempty"`
	// PageSize is the maximum number of messages on this page: 1 through 15; blank or 0 reads 15, the most
	// Slack returns to a non-Marketplace app. Slack may return fewer messages while NextCursor continues the history.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the nextCursor from the previous page. Blank reads the newest page.
	Cursor string `json:"cursor,omitempty"`
}

// ChannelMessage is one top-level message from a Slack channel's history.
type ChannelMessage struct {
	// ChannelID is the Slack channel identifier that was read.
	ChannelID string `json:"channelId"`
	// Timestamp is the Slack message timestamp, which identifies the message within its channel.
	Timestamp string `json:"timestamp"`
	// ThreadTimestamp is the parent timestamp of the message's thread: equal to Timestamp for a thread parent,
	// different for a thread reply that was also sent to the channel, and blank for a message without a thread.
	ThreadTimestamp string `json:"threadTimestamp,omitempty"`
	// UserID is the Slack user identifier of the poster, such as the bot user for the app's own posts.
	// Blank for messages posted by integrations without a user.
	UserID string `json:"userId,omitempty"`
	// BotID is the Slack bot identifier when an app or integration posted the message; blank for people.
	BotID string `json:"botId,omitempty"`
	// Text is the message text in Slack's markup.
	Text string `json:"text"`
	// ReplyCount is the number of thread replies when the message starts a thread; zero otherwise.
	ReplyCount int `json:"replyCount,omitempty"`
}

// ListChannelMessagesOutput contains one newest-first page of a channel's top-level messages.
type ListChannelMessagesOutput struct {
	// Messages are the page's top-level messages, newest first. A thread reply appears only when its poster
	// also sent it to the channel; read the other replies with ListThreadMessages.
	Messages []ChannelMessage `json:"messages"`
	// NextCursor continues to older messages when non-blank; blank means the requested range ended.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListChannelMessagesOperation implements the list channel messages connector operation.
type ListChannelMessagesOperation struct{ client *Client }

type slackChannelHistoryResponse struct {
	Messages []slackChannelHistoryMessage `json:"messages"`
}

type slackChannelHistoryMessage struct {
	Timestamp  string `json:"ts"`
	ThreadTS   string `json:"thread_ts"`
	User       string `json:"user"`
	BotID      string `json:"bot_id"`
	Text       string `json:"text"`
	ReplyCount int    `json:"reply_count"`
}

// Definition returns the immutable connector operation definition.
func (ListChannelMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListChannelMessagesDefinition
}

// Invoke reads one conversations.history page with the bot token, which can read every channel the app posts to.
func (operation ListChannelMessagesOperation) Invoke(call sdkgo.Call, input ListChannelMessagesInput) sdkgo.QueryAttempt[ListChannelMessagesOutput] {
	const operationName = "listChannelMessages"
	if !isValidChannelHistoryInput(input) {
		return sdkgo.NewQueryBranch(ListChannelMessagesBranchDefect, ListChannelMessagesOutput{}, slackFailurePointer(operationName, sdkgo.FailureValidation, "channel, valid Slack message timestamps, and page size 1 through 15 (blank or 0 reads 15) are required"), sdkgo.Receipt{})
	}
	credentials, failure, _ := operation.client.resolveCredentials(call, operationName, botCredentialToken)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListChannelMessagesBranchDefect, ListChannelMessagesOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultChannelHistoryPageSize
	}
	values := url.Values{"channel": {input.ChannelID}, "limit": {strconv.Itoa(pageSize)}}
	if input.OldestTimestamp != "" {
		values.Set("oldest", input.OldestTimestamp)
	}
	if input.LatestTimestamp != "" {
		values.Set("latest", input.LatestTimestamp)
	}
	if input.IsInclusive {
		values.Set("inclusive", "true")
	}
	if input.Cursor != "" {
		values.Set("cursor", input.Cursor)
	}
	response, err := operation.client.get(call, &credentials, botCredentialToken, "conversations.history", values)
	if attempt, isClassified := classifyQueryResponse(operation.client, call, operationName, response, err, ListChannelMessagesBranchInvalidResponse, ListChannelMessagesBranchProviderRejected, ListChannelMessagesOutput{}, "Slack channel history is temporarily unavailable", "Slack rate limited the channel history query"); isClassified {
		return attempt
	}
	var decoded slackChannelHistoryResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		return sdkgo.NewQueryBranch(ListChannelMessagesBranchInvalidResponse, ListChannelMessagesOutput{}, slackFailurePointer(operationName, sdkgo.FailureProtocol, "Slack returned an invalid channel history"), operation.client.receipt(call, response, ""))
	}
	messages := make([]ChannelMessage, 0, len(decoded.Messages))
	for _, message := range decoded.Messages {
		if message.Timestamp == "" {
			return sdkgo.NewQueryBranch(ListChannelMessagesBranchInvalidResponse, ListChannelMessagesOutput{}, slackFailurePointer(operationName, sdkgo.FailureProtocol, "Slack returned a channel message without a timestamp"), operation.client.receipt(call, response, ""))
		}
		messages = append(messages, ChannelMessage{
			ChannelID: input.ChannelID, Timestamp: message.Timestamp, ThreadTimestamp: message.ThreadTS,
			UserID: message.User, BotID: message.BotID, Text: message.Text, ReplyCount: message.ReplyCount,
		})
	}
	return sdkgo.NewQueryBranch(ListChannelMessagesBranchRead, ListChannelMessagesOutput{Messages: messages, NextCursor: response.decoded.ResponseMetadata.NextCursor}, nil, operation.client.receipt(call, response, ""))
}

// isValidChannelHistoryInput checks the channel, optional timestamp shapes, and page size before any request.
func isValidChannelHistoryInput(input ListChannelMessagesInput) bool {
	if strings.TrimSpace(input.ChannelID) == "" || input.PageSize < 0 || input.PageSize > maximumChannelHistoryPageSize {
		return false
	}
	for _, timestamp := range []string{input.OldestTimestamp, input.LatestTimestamp} {
		if timestamp != "" && !slackTimestampPattern.MatchString(timestamp) {
			return false
		}
	}
	return true
}

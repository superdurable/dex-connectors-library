// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaximumMessagePageSize is Discord's largest page of listed messages.
	MaximumMessagePageSize = 100
	// defaultMessagePageSize is Discord's page size when the request sets no limit.
	defaultMessagePageSize = 50
)

// ListChannelMessagesInput selects one page of a channel's messages.
//
// Discord returns messages newest first. Set at most one of BeforeMessageID
// and AfterMessageID: blank both reads the newest page, BeforeMessageID pages
// toward older messages, and AfterMessageID reads messages newer than it.
type ListChannelMessagesInput struct {
	// ChannelID is the channel's snowflake ID, 17 to 20 digits.
	ChannelID string `json:"channelId"`
	// Limit is the most messages returned, 1 to 100; zero returns up to 50.
	Limit int `json:"limit,omitempty"`
	// BeforeMessageID lists messages older than this message ID, such as the previous page's OldestMessageID.
	BeforeMessageID string `json:"beforeMessageId,omitempty"`
	// AfterMessageID lists messages newer than this message ID.
	AfterMessageID string `json:"afterMessageId,omitempty"`
}

// MessagePage is one page of listed messages, newest first.
type MessagePage struct {
	// Messages holds at most the requested limit of messages; an empty page is an empty list.
	Messages []Message `json:"messages"`
	// OldestMessageID is the last message's ID, to pass as BeforeMessageID for the next older page; blank when empty.
	OldestMessageID string `json:"oldestMessageId,omitempty"`
	// IsFull reports that the page holds the requested limit, so an older page may exist.
	IsFull bool `json:"isFull"`
}

// ListChannelMessagesOperation implements the listChannelMessages connector Query.
type ListChannelMessagesOperation struct{ client *Client }

var listChannelMessagesBranches = operationBranches{
	notFound: ListChannelMessagesBranchNotFound, rejected: ListChannelMessagesBranchProviderRejected,
	invalidResponse: ListChannelMessagesBranchInvalidResponse, defect: ListChannelMessagesBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListChannelMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListChannelMessagesDefinition
}

// Invoke sends one message list request and classifies its attempt. Invalid
// input or credentials select defect before any request.
func (operation ListChannelMessagesOperation) Invoke(call sdkgo.Call, input ListChannelMessagesInput) sdkgo.QueryAttempt[MessagePage] {
	query, limit, err := encodeListChannelMessagesQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListChannelMessagesBranchDefect, MessagePage{}, providerFailurePointer("listChannelMessages", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "listChannelMessages", providerRequest{
		method: http.MethodGet, path: "/channels/" + input.ChannelID + "/messages?" + query.Encode(),
	}, input.ChannelID)
	if outcome != nil {
		return queryAttemptForOutcome[MessagePage](*outcome, listChannelMessagesBranches)
	}
	receipt := operation.client.receipt(call, input.ChannelID)
	page, err := decodeMessagePage(response.body, input.ChannelID, limit)
	if err != nil {
		return sdkgo.NewQueryBranch(ListChannelMessagesBranchInvalidResponse, MessagePage{}, providerFailurePointer("listChannelMessages", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListChannelMessagesBranchListed, page, nil, receipt)
}

func encodeListChannelMessagesQuery(input ListChannelMessagesInput) (url.Values, int, error) {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return nil, 0, err
	}
	if input.Limit < 0 || input.Limit > MaximumMessagePageSize {
		return nil, 0, errors.New("limit must be from 1 through 100, or zero for Discord's default of 50")
	}
	if input.BeforeMessageID != "" && input.AfterMessageID != "" {
		return nil, 0, errors.New("set at most one of beforeMessageId and afterMessageId")
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultMessagePageSize
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if input.BeforeMessageID != "" {
		if err := validateSnowflake("beforeMessageId", input.BeforeMessageID); err != nil {
			return nil, 0, err
		}
		query.Set("before", input.BeforeMessageID)
	}
	if input.AfterMessageID != "" {
		if err := validateSnowflake("afterMessageId", input.AfterMessageID); err != nil {
			return nil, 0, err
		}
		query.Set("after", input.AfterMessageID)
	}
	return query, limit, nil
}

func decodeMessagePage(body []byte, channelID string, limit int) (MessagePage, error) {
	var decoded []providerMessage
	if err := decodeResponse(body, &decoded); err != nil {
		return MessagePage{}, err
	}
	if decoded == nil {
		return MessagePage{}, errors.New("Discord returned a message page that is not a list")
	}
	if len(decoded) > limit {
		return MessagePage{}, errors.New("Discord returned more messages than the limit")
	}
	page := MessagePage{Messages: make([]Message, 0, len(decoded)), IsFull: len(decoded) == limit}
	for _, providerMessage := range decoded {
		message, err := convertProviderMessage(providerMessage, channelID)
		if err != nil {
			return MessagePage{}, err
		}
		page.Messages = append(page.Messages, message)
	}
	if len(page.Messages) > 0 {
		page.OldestMessageID = page.Messages[len(page.Messages)-1].ID
	}
	return page, nil
}

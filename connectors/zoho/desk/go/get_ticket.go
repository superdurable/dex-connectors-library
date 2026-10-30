// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultThreadLimit is the number of threads getTicket returns when ThreadLimit is zero.
	DefaultThreadLimit = 5
	// MaxThreadLimit is the largest accepted ThreadLimit.
	MaxThreadLimit = 20
	// DefaultCommentLimit is the number of comments getTicket returns when CommentLimit is zero.
	DefaultCommentLimit = 5
	// MaxCommentLimit is the largest accepted CommentLimit.
	MaxCommentLimit = 20

	getTicketOperation = "getTicket"
)

// GetTicketInput names one ticket and how many of its newest threads and comments to return.
type GetTicketInput struct {
	// TicketID is the Zoho Desk ticket ID, a decimal string such as 1892000000042034.
	TicketID string `json:"ticketId"`
	// ThreadLimit is 1 to MaxThreadLimit; zero uses DefaultThreadLimit.
	ThreadLimit int `json:"threadLimit,omitempty"`
	// CommentLimit is 1 to MaxCommentLimit; zero uses DefaultCommentLimit.
	CommentLimit int `json:"commentLimit,omitempty"`
}

// TicketDetails is one ticket with its contact, newest threads, and newest comments.
type TicketDetails struct {
	// Ticket is the ticket, including its HTML description.
	Ticket Ticket `json:"ticket"`
	// Contact is the contact who raised the ticket, or nil when Zoho Desk embedded none.
	Contact *TicketContact `json:"contact,omitempty"`
	// Threads lists the ticket's newest threads, newest first: emails, replies, and other
	// conversation messages, each with Zoho Desk's plain-text summary.
	Threads []TicketThread `json:"threads"`
	// HasMoreThreads reports that the ticket has older threads than the last one returned.
	HasMoreThreads bool `json:"hasMoreThreads,omitempty"`
	// Comments lists the ticket's newest comments, newest first, public and private alike.
	Comments []TicketComment `json:"comments"`
	// HasMoreComments reports that the ticket has older comments than the last one returned.
	HasMoreComments bool `json:"hasMoreComments,omitempty"`
}

// TicketThread is one message in a ticket's conversation, such as a customer email or an agent
// reply. Zoho Desk's thread list carries a summary, not the full content.
type TicketThread struct {
	// ID is the Zoho Desk thread ID.
	ID string `json:"id"`
	// Channel is the thread's channel, such as EMAIL, FORUMS, or FEEDBACK.
	Channel string `json:"channel,omitempty"`
	// Direction is in for a message from the customer and out for one to the customer.
	Direction string `json:"direction,omitempty"`
	// Visibility is public or private.
	Visibility string `json:"visibility,omitempty"`
	// Status is Zoho Desk's delivery status, such as SUCCESS, FAILED, or DRAFT.
	Status string `json:"status,omitempty"`
	// Summary is Zoho Desk's plain-text summary of the thread.
	Summary string `json:"summary,omitempty"`
	// IsSummaryTruncated reports that Summary was cut at MaxTextBytes.
	IsSummaryTruncated bool `json:"isSummaryTruncated,omitempty"`
	// IsDescriptionThread reports the thread that holds the ticket's original description.
	IsDescriptionThread bool `json:"isDescriptionThread,omitempty"`
	// AuthorName is the author's display name.
	AuthorName string `json:"authorName,omitempty"`
	// AuthorType is AGENT or END_USER.
	AuthorType string `json:"authorType,omitempty"`
	// CreatedAt is when the thread was created.
	CreatedAt time.Time `json:"createdAt"`
}

// TicketComment is one public or private comment on a ticket.
type TicketComment struct {
	// ID is the Zoho Desk comment ID.
	ID string `json:"id"`
	// IsPublic is true for a comment the customer can see in the help center and false for a
	// private comment only agents see.
	IsPublic bool `json:"isPublic"`
	// Content is the comment as Zoho Desk returned it, in ContentType.
	Content string `json:"content"`
	// ContentType is plainText or html.
	ContentType string `json:"contentType,omitempty"`
	// IsContentTruncated reports that Content was cut at MaxTextBytes.
	IsContentTruncated bool `json:"isContentTruncated,omitempty"`
	// CommenterID is the agent or user who commented.
	CommenterID string `json:"commenterId,omitempty"`
	// CommenterName is the commenter's display name.
	CommenterName string `json:"commenterName,omitempty"`
	// CommenterType is AGENT or END_USER.
	CommenterType string `json:"commenterType,omitempty"`
	// CommentedAt is when the comment was added.
	CommentedAt time.Time `json:"commentedAt"`
	// ModifiedAt is when the comment was last edited, or nil.
	ModifiedAt *time.Time `json:"modifiedAt,omitempty"`
}

// GetTicketOperation is the getTicket Query.
type GetTicketOperation struct {
	client *Client
}

type deskThreadWire struct {
	ID                  zohoID      `json:"id"`
	Channel             string      `json:"channel"`
	Direction           string      `json:"direction"`
	Visibility          string      `json:"visibility"`
	Status              string      `json:"status"`
	Summary             string      `json:"summary"`
	IsDescriptionThread zohoBoolean `json:"isDescriptionThread"`
	CreatedTime         string      `json:"createdTime"`
	Author              *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"author"`
}

type deskCommentWire struct {
	ID           zohoID      `json:"id"`
	IsPublic     zohoBoolean `json:"isPublic"`
	Content      string      `json:"content"`
	PlainText    *string     `json:"plainText"`
	ContentType  string      `json:"contentType"`
	CommenterID  zohoID      `json:"commenterId"`
	CommentedAt  string      `json:"commentedTime"`
	ModifiedTime string      `json:"modifiedTime"`
	Commenter    *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"commenter"`
}

// Definition returns the immutable connector operation definition.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads the ticket with its contact, then its newest threads and newest comments.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[TicketDetails] {
	if err := validateGetTicketInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, deskFailurePointer(sdkgo.FailureValidation, getTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, getTicketOperation)
	if failure != nil {
		return queryAttemptForSession[TicketDetails](failure, GetTicketBranchProviderRejected, GetTicketBranchDefect)
	}
	defer cancel()
	ticketResult := operation.client.exchange(session, getTicketOperation, deskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID), query: url.Values{"include": {"contacts"}},
	})
	receipt := operation.client.receipt(call, ticketResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(ticketResult, receipt); isTerminal {
		return attempt
	}
	ticket, contact, err := decodeTicketBody(ticketResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, deskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Zoho Desk returned an invalid ticket: "+err.Error()), receipt)
	}
	details := TicketDetails{Ticket: ticket, Contact: contactFromWire(contact)}

	threadLimit := limitOrDefault(input.ThreadLimit, DefaultThreadLimit)
	threadResult := operation.client.exchange(session, getTicketOperation, deskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID) + "/threads",
		query: url.Values{"from": {"0"}, "limit": {strconv.Itoa(threadLimit)}, "sortBy": {"-sendDateTime"}},
	})
	receipt = operation.client.receipt(call, threadResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(threadResult, receipt); isTerminal {
		return attempt
	}
	if details.Threads, err = decodeThreadPage(threadResult.response, threadLimit); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, deskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Zoho Desk returned an invalid thread list: "+err.Error()), receipt)
	}
	details.HasMoreThreads = ticket.ThreadCount > len(details.Threads)

	commentLimit := limitOrDefault(input.CommentLimit, DefaultCommentLimit)
	commentResult := operation.client.exchange(session, getTicketOperation, deskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID) + "/comments",
		query: url.Values{"from": {"0"}, "limit": {strconv.Itoa(commentLimit)}, "sortBy": {"-commentedTime"}},
	})
	receipt = operation.client.receipt(call, commentResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(commentResult, receipt); isTerminal {
		return attempt
	}
	if details.Comments, err = decodeCommentPage(commentResult.response, commentLimit); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, deskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Zoho Desk returned an invalid comment list: "+err.Error()), receipt)
	}
	details.HasMoreComments = ticket.CommentCount > len(details.Comments)
	return sdkgo.NewQueryBranch(GetTicketBranchFound, details, nil, receipt)
}

// getTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func getTicketAttemptForExchange(result deskExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[TicketDetails], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[TicketDetails]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[TicketDetails](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetTicketBranchNotFound, TicketDetails{}, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(GetTicketBranchProviderRejected, TicketDetails{}, &result.failure, receipt), true
	}
}

func validateGetTicketInput(input GetTicketInput) error {
	if err := validateZohoID("ticketId", input.TicketID); err != nil {
		return err
	}
	if input.ThreadLimit < 0 || input.ThreadLimit > MaxThreadLimit {
		return fmt.Errorf("threadLimit must be between 1 and %d, or zero for %d", MaxThreadLimit, DefaultThreadLimit)
	}
	if input.CommentLimit < 0 || input.CommentLimit > MaxCommentLimit {
		return fmt.Errorf("commentLimit must be between 1 and %d, or zero for %d", MaxCommentLimit, DefaultCommentLimit)
	}
	return nil
}

func limitOrDefault(limit int, defaultLimit int) int {
	if limit == 0 {
		return defaultLimit
	}
	return limit
}

// decodeListData reads Zoho Desk's {data: [...]} list; a 204 is an empty list.
func decodeListData[T any](response deskResponse, limit int) ([]T, error) {
	if response.statusCode == http.StatusNoContent {
		return nil, nil
	}
	var page struct {
		Data *[]T `json:"data"`
	}
	if err := json.Unmarshal(response.body, &page); err != nil || page.Data == nil {
		return nil, errors.New("list is not a JSON object with a data array")
	}
	if len(*page.Data) > limit {
		return nil, errors.New("list is larger than requested")
	}
	return *page.Data, nil
}

// decodeThreadPage orders threads newest first, even if Zoho Desk returned another order.
func decodeThreadPage(response deskResponse, limit int) ([]TicketThread, error) {
	wires, err := decodeListData[deskThreadWire](response, limit)
	if err != nil {
		return nil, err
	}
	threads := make([]TicketThread, 0, len(wires))
	for index, wire := range wires {
		if wire.ID == "" {
			return nil, fmt.Errorf("thread %d has no ID", index)
		}
		createdAt, err := parseZohoTimestamp("createdTime", wire.CreatedTime)
		if err != nil {
			return nil, fmt.Errorf("thread %d: %w", index, err)
		}
		summary, isSummaryTruncated := truncateUTF8(wire.Summary, MaxTextBytes)
		thread := TicketThread{
			ID: string(wire.ID), Channel: wire.Channel, Direction: wire.Direction, Visibility: wire.Visibility, Status: wire.Status,
			Summary: summary, IsSummaryTruncated: isSummaryTruncated, IsDescriptionThread: bool(wire.IsDescriptionThread), CreatedAt: createdAt,
		}
		if wire.Author != nil {
			thread.AuthorName, thread.AuthorType = wire.Author.Name, wire.Author.Type
		}
		threads = append(threads, thread)
	}
	sort.SliceStable(threads, func(left, right int) bool { return threads[left].CreatedAt.After(threads[right].CreatedAt) })
	return threads, nil
}

// decodeCommentPage orders comments newest first, even if Zoho Desk returned another order.
func decodeCommentPage(response deskResponse, limit int) ([]TicketComment, error) {
	wires, err := decodeListData[deskCommentWire](response, limit)
	if err != nil {
		return nil, err
	}
	comments := make([]TicketComment, 0, len(wires))
	for index, wire := range wires {
		comment, err := decodeCommentWire(wire)
		if err != nil {
			return nil, fmt.Errorf("comment %d: %w", index, err)
		}
		comments = append(comments, comment)
	}
	sort.SliceStable(comments, func(left, right int) bool { return comments[left].CommentedAt.After(comments[right].CommentedAt) })
	return comments, nil
}

// decodeCommentWire prefers Zoho Desk's plainText rendering when the response carries one.
func decodeCommentWire(wire deskCommentWire) (TicketComment, error) {
	if wire.ID == "" {
		return TicketComment{}, errors.New("comment has no ID")
	}
	commentedAt, err := parseZohoTimestamp("commentedTime", wire.CommentedAt)
	if err != nil {
		return TicketComment{}, err
	}
	content, contentType := wire.Content, wire.ContentType
	if wire.PlainText != nil && !strings.EqualFold(contentType, "plainText") {
		content, contentType = *wire.PlainText, "plainText"
	}
	content, isContentTruncated := truncateUTF8(content, MaxTextBytes)
	comment := TicketComment{
		ID: string(wire.ID), IsPublic: bool(wire.IsPublic), Content: content, ContentType: contentType,
		IsContentTruncated: isContentTruncated, CommenterID: string(wire.CommenterID), CommentedAt: commentedAt,
	}
	if wire.Commenter != nil {
		comment.CommenterName, comment.CommenterType = wire.Commenter.Name, wire.Commenter.Type
	}
	if comment.ModifiedAt, err = parseOptionalZohoTimestamp("modifiedTime", wire.ModifiedTime); err != nil {
		return TicketComment{}, err
	}
	return comment, nil
}

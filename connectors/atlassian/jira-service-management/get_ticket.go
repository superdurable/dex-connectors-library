// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getTicketOperationID = "getTicket"
	// DefaultCommentLimit is the number of comments a zero CommentLimit reads.
	DefaultCommentLimit = 10
	// MaxCommentLimit is the largest CommentLimit getTicket accepts.
	MaxCommentLimit = 50
	// MaxSLACompletedCycles bounds the completed cycles kept for one SLA, the first ones the provider lists.
	MaxSLACompletedCycles = 20
	slaPageSize           = 50
)

// GetTicketInput identifies one request and how many of its comments to read.
type GetTicketInput struct {
	// IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID. Jira also finds a moved request
	// by its old key and returns its current key.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// CommentLimit is 1 to MaxCommentLimit comments to read from the start of the request's comments;
	// zero reads DefaultCommentLimit.
	CommentLimit int `json:"commentLimit,omitempty"`
}

// TicketDetails is one request with its first comments and its SLAs.
type TicketDetails struct {
	// Ticket is the request with its plain-text description.
	Ticket Ticket `json:"ticket"`
	// Comments lists public replies and internal notes in the order Jira Service Management returns them.
	Comments []TicketComment `json:"comments"`
	// HasMoreComments reports that the request has comments beyond Comments.
	HasMoreComments bool `json:"hasMoreComments,omitempty"`
	// SLAs lists the request's service level agreements, such as Time to first response.
	SLAs []TicketSLA `json:"slas"`
}

// TicketComment is one public reply or internal note on a request.
type TicketComment struct {
	// ID is the comment ID.
	ID string `json:"id"`
	// IsPublic is true for a reply the customer can see and false for an internal note only agents see.
	IsPublic bool `json:"isPublic"`
	// Body is the comment's stored text, in which Jira Service Management renders wiki markup.
	Body string `json:"body"`
	// IsBodyTruncated reports that Body stopped at 32767 characters.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Author is the agent or customer who wrote the comment, or nil when Jira hides it.
	Author *AccountReference `json:"author,omitempty"`
	// CreatedAt is when the comment was added.
	CreatedAt time.Time `json:"createdAt"`
}

// TicketSLA is one service level agreement on a request, with Jira Service Management's own SLA name.
type TicketSLA struct {
	// ID is the SLA ID.
	ID string `json:"id"`
	// Name is the SLA name, such as Time to resolution.
	Name string `json:"name"`
	// OngoingCycle is the running cycle, or nil when the SLA is not running.
	OngoingCycle *SLACycle `json:"ongoingCycle,omitempty"`
	// CompletedCycles lists up to MaxSLACompletedCycles finished cycles.
	CompletedCycles []SLACycle `json:"completedCycles,omitempty"`
}

// SLACycle is one SLA cycle. Durations are milliseconds; RemainingMillis is negative after a breach.
type SLACycle struct {
	// StartedAt is when the cycle started.
	StartedAt time.Time `json:"startedAt"`
	// BreachAt is when the cycle breaches or breached its goal, or nil when the provider gives none.
	BreachAt *time.Time `json:"breachAt,omitempty"`
	// StoppedAt is when a completed cycle stopped; it is nil for the ongoing cycle.
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`
	// IsBreached reports that the cycle exceeded its goal.
	IsBreached bool `json:"isBreached"`
	// IsPaused reports an ongoing cycle whose clock is paused, such as while waiting for the customer.
	IsPaused bool `json:"isPaused,omitempty"`
	// IsWithinCalendarHours reports an ongoing cycle whose clock runs only in the SLA's working hours.
	IsWithinCalendarHours bool `json:"isWithinCalendarHours,omitempty"`
	// GoalDurationMillis is the goal in milliseconds.
	GoalDurationMillis int64 `json:"goalDurationMillis"`
	// ElapsedMillis is the clock time used in milliseconds.
	ElapsedMillis int64 `json:"elapsedMillis"`
	// RemainingMillis is the clock time left in milliseconds.
	RemainingMillis int64 `json:"remainingMillis"`
}

// GetTicketOperation implements the getTicket Query.
type GetTicketOperation struct{ client *Client }

type serviceDeskComment struct {
	ID      string           `json:"id"`
	Body    string           `json:"body"`
	Public  *bool            `json:"public"`
	Author  *serviceDeskUser `json:"author"`
	Created *serviceDeskDate `json:"created"`
}

type serviceDeskSLA struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	OngoingCycle    *serviceDeskSLACycle  `json:"ongoingCycle"`
	CompletedCycles []serviceDeskSLACycle `json:"completedCycles"`
}

type serviceDeskSLACycle struct {
	StartTime           *serviceDeskDate     `json:"startTime"`
	BreachTime          *serviceDeskDate     `json:"breachTime"`
	StopTime            *serviceDeskDate     `json:"stopTime"`
	Breached            bool                 `json:"breached"`
	Paused              bool                 `json:"paused"`
	WithinCalendarHours bool                 `json:"withinCalendarHours"`
	GoalDuration        *serviceDeskDuration `json:"goalDuration"`
	ElapsedTime         *serviceDeskDuration `json:"elapsedTime"`
	RemainingTime       *serviceDeskDuration `json:"remainingTime"`
}

// Definition returns the immutable connector operation definition.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads the request's Jira issue, then its first comments and its SLAs from the request API.
// Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[TicketDetails] {
	client := operation.client
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	commentLimit := input.CommentLimit
	switch {
	case commentLimit == 0:
		commentLimit = DefaultCommentLimit
	case commentLimit < 1 || commentLimit > MaxCommentLimit:
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureValidation, "commentLimit must be between 1 and 50"), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, getTicketOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[TicketDetails](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	details := TicketDetails{Comments: []TicketComment{}, SLAs: []TicketSLA{}}
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: jiraPlatformAPI, path: issuePath(issueIDOrKey),
		query: url.Values{"fields": {strings.Join(append(append([]string(nil), ticketFieldIDs...), "description"), ",")}},
	})
	receipt := client.receipt(session, result.response, issueIDOrKey)
	if classification := client.classifyRead(getTicketOperationID, "request", result); classification.outcome != readSucceeded {
		return getTicketReadFailureAttempt(classification, receipt)
	}
	var resource issueResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureProtocol, "Jira returned an invalid request issue"), receipt)
	}
	if details.Ticket, err = decodeTicket(resource, true); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureProtocol, "Jira returned an invalid request issue: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = details.Ticket.Key
	if attempt, isTerminal := operation.readComments(session, &details, commentLimit, receipt); isTerminal {
		return attempt
	}
	if attempt, isTerminal := operation.readSLAs(session, &details, receipt); isTerminal {
		return attempt
	}
	return sdkgo.NewQueryBranch(GetTicketBranchFound, details, nil, receipt)
}

// readComments reads the first comment page, public replies and internal notes alike.
func (operation GetTicketOperation) readComments(session *operationSession, details *TicketDetails, commentLimit int, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[TicketDetails], bool) {
	client := operation.client
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: serviceDeskAPI, path: requestPath(details.Ticket.Key) + "/comment",
		query: url.Values{"public": {"true"}, "internal": {"true"}, "start": {"0"}, "limit": {strconv.Itoa(commentLimit)}},
	})
	if classification := client.classifyRead(getTicketOperationID, "request comments", result); classification.outcome != readSucceeded {
		return getTicketReadFailureAttempt(classification, receipt), true
	}
	page, err := decodeServiceDeskPage[serviceDeskComment](result.response.body)
	if err == nil {
		for _, comment := range page.Values {
			var converted TicketComment
			if converted, err = convertComment(comment); err != nil {
				break
			}
			details.Comments = append(details.Comments, converted)
		}
	}
	if err != nil || len(details.Comments) > commentLimit {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureProtocol, "Jira Service Management returned an invalid comment page"), receipt), true
	}
	details.HasMoreComments = !page.IsLastPage
	return sdkgo.QueryAttempt[TicketDetails]{}, false
}

// readSLAs reads the request's SLA page, which only an agent of the service desk may read.
func (operation GetTicketOperation) readSLAs(session *operationSession, details *TicketDetails, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[TicketDetails], bool) {
	client := operation.client
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: serviceDeskAPI, path: requestPath(details.Ticket.Key) + "/sla",
		query: url.Values{"start": {"0"}, "limit": {strconv.Itoa(slaPageSize)}},
	})
	if classification := client.classifyRead(getTicketOperationID, "request SLAs", result); classification.outcome != readSucceeded {
		return getTicketReadFailureAttempt(classification, receipt), true
	}
	page, err := decodeServiceDeskPage[serviceDeskSLA](result.response.body)
	if err == nil {
		for _, sla := range page.Values {
			var converted TicketSLA
			if converted, err = convertSLA(sla); err != nil {
				break
			}
			details.SLAs = append(details.SLAs, converted)
		}
	}
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, failurePointer(getTicketOperationID, sdkgo.FailureProtocol, "Jira Service Management returned an invalid SLA page"), receipt), true
	}
	return sdkgo.QueryAttempt[TicketDetails]{}, false
}

func getTicketReadFailureAttempt(classification readClassification, receipt sdkgo.Receipt) sdkgo.QueryAttempt[TicketDetails] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewQueryRetry[TicketDetails](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(GetTicketBranchNotFound, TicketDetails{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetTicketBranchProviderRejected, TicketDetails{}, &classification.failure, receipt)
	}
}

func convertComment(comment serviceDeskComment) (TicketComment, error) {
	createdAt, hasCreatedAt := comment.Created.time()
	if !isSafeProviderToken(comment.ID) || comment.Public == nil || !hasCreatedAt {
		return TicketComment{}, errors.New("comment ID, visibility, or creation time is missing")
	}
	body, isTruncated := truncatePlainText(comment.Body)
	return TicketComment{
		ID: comment.ID, IsPublic: *comment.Public, Body: body, IsBodyTruncated: isTruncated,
		Author: comment.Author.view(), CreatedAt: createdAt,
	}, nil
}

func convertSLA(sla serviceDeskSLA) (TicketSLA, error) {
	if !isSafeProviderToken(sla.ID) {
		return TicketSLA{}, errors.New("SLA ID is missing")
	}
	converted := TicketSLA{ID: sla.ID, Name: sla.Name}
	if sla.OngoingCycle != nil {
		cycle, err := convertSLACycle(*sla.OngoingCycle)
		if err != nil {
			return TicketSLA{}, err
		}
		cycle.StoppedAt = nil
		converted.OngoingCycle = &cycle
	}
	for index, completed := range sla.CompletedCycles {
		if index == MaxSLACompletedCycles {
			break
		}
		cycle, err := convertSLACycle(completed)
		if err != nil {
			return TicketSLA{}, err
		}
		converted.CompletedCycles = append(converted.CompletedCycles, cycle)
	}
	return converted, nil
}

func convertSLACycle(cycle serviceDeskSLACycle) (SLACycle, error) {
	startedAt, hasStartedAt := cycle.StartTime.time()
	if !hasStartedAt {
		return SLACycle{}, errors.New("SLA cycle start time is missing")
	}
	return SLACycle{
		StartedAt: startedAt, BreachAt: cycle.BreachTime.timePointer(), StoppedAt: cycle.StopTime.timePointer(),
		IsBreached: cycle.Breached, IsPaused: cycle.Paused, IsWithinCalendarHours: cycle.WithinCalendarHours,
		GoalDurationMillis: cycle.GoalDuration.milliseconds(), ElapsedMillis: cycle.ElapsedTime.milliseconds(),
		RemainingMillis: cycle.RemainingTime.milliseconds(),
	}, nil
}

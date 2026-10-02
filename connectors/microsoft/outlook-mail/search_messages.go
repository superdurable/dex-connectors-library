// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchMessagesOperationID = "searchMessages"
	// DefaultSearchLimit is the page size searchMessages uses when the input leaves limit zero.
	DefaultSearchLimit = 10
	// MaxSearchLimit bounds one page so a Result stays small; Graph itself allows up to 1000.
	MaxSearchLimit = 50
	// MaxSubjectFilterCharacters bounds the subjectContains filter.
	MaxSubjectFilterCharacters = 255

	odataTimeLayout = "2006-01-02T15:04:05Z"
)

// earliestReceivedFilter starts every filter, because Graph requires an ordered property to be filtered first.
var earliestReceivedFilter = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// SearchMessagesInput filters one page of messages in one folder. Every filter is optional, and filters combine.
type SearchMessagesInput struct {
	// Folder is a folder ID from the mail folder picker or a well-known name such as inbox, archive, or
	// sentitems; blank searches DefaultFolder. Subfolders are not searched.
	Folder string `json:"folder,omitempty"`
	// From keeps messages whose From address equals this bare address, such as jane@acme.example.com.
	From string `json:"from,omitempty"`
	// SubjectContains keeps messages whose subject contains this text.
	SubjectContains string `json:"subjectContains,omitempty"`
	// ReceivedAfter keeps messages received at or after this instant, to the second.
	ReceivedAfter *time.Time `json:"receivedAfter,omitempty"`
	// ReceivedBefore keeps messages received strictly before this instant, to the second.
	ReceivedBefore *time.Time `json:"receivedBefore,omitempty"`
	// IsUnread keeps only messages that are not marked read.
	IsUnread bool `json:"isUnread,omitempty"`
	// Limit is 1 to MaxSearchLimit messages per page; zero uses DefaultSearchLimit.
	Limit int `json:"limit,omitempty"`
	// PageCursor is nextPageCursor from an earlier Result. It carries that search's filters and page
	// size, so when it is set every field except Folder is ignored.
	PageCursor string `json:"pageCursor,omitempty"`
}

// SearchMessagesOutput is one page of matching summaries, newest first by received time.
type SearchMessagesOutput struct {
	// Folder is the searched folder as requested, a well-known name or an ID.
	Folder string `json:"folder"`
	// Messages lists at most the page size summaries, newest first.
	Messages []MessageSummary `json:"messages"`
	// HasMore reports that NextPageCursor continues the search.
	HasMore bool `json:"hasMore"`
	// NextPageCursor is the opaque continuation to pass as pageCursor, or empty on the last page.
	NextPageCursor string `json:"nextPageCursor,omitempty"`
}

// SearchMessagesOperation implements the searchMessages Query.
type SearchMessagesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SearchMessagesOperation) Definition() sdkgo.QueryDefinition { return SearchMessagesDefinition }

// Invoke lists one page of GET /mailFolders/{folder}/messages ordered by receivedDateTime descending.
func (operation SearchMessagesOperation) Invoke(call sdkgo.Call, input SearchMessagesInput) sdkgo.QueryAttempt[SearchMessagesOutput] {
	request, folder, err := buildSearchMessagesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchMessagesBranchDefect, SearchMessagesOutput{},
			graphFailurePointer(sdkgo.FailureValidation, searchMessagesOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, failed := operation.client.openSession(call, searchMessagesOperationID)
	if failed != nil {
		attempt, _ := queryAttemptForExchange[SearchMessagesOutput](*failed, sdkgo.Receipt{}, searchMessagesBranches)
		return attempt
	}
	result := session.exchange(request)
	receipt := session.receipt(result, "")
	if attempt, isTerminal := queryAttemptForExchange[SearchMessagesOutput](result, receipt, searchMessagesBranches); isTerminal {
		return attempt
	}
	page, err := decodeMessagePage(result.body, folder)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchMessagesBranchInvalidResponse, SearchMessagesOutput{},
			graphFailurePointer(sdkgo.FailureProtocol, searchMessagesOperationID, "Microsoft Graph returned an invalid message page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchMessagesBranchSearched, page, nil, receipt)
}

var searchMessagesBranches = queryBranches{
	providerRejected: SearchMessagesBranchProviderRejected, invalidResponse: SearchMessagesBranchInvalidResponse,
	defect: SearchMessagesBranchDefect,
}

// buildSearchMessagesRequest validates the filters and returns the request and the folder as requested.
func buildSearchMessagesRequest(input SearchMessagesInput) (graphRequest, string, error) {
	folder, err := resolveFolderSegment("folder", input.Folder, DefaultFolder)
	if err != nil {
		return graphRequest{}, "", err
	}
	if input.PageCursor != "" {
		if err := validateNextPageLink(input.PageCursor); err != nil {
			return graphRequest{}, "", fmt.Errorf("pageCursor must be nextPageCursor from an earlier searchMessages Result: %w", err)
		}
		return graphRequest{method: http.MethodGet, absoluteURL: input.PageCursor}, folder, nil
	}
	filter, err := buildMessageFilter(input)
	if err != nil {
		return graphRequest{}, "", err
	}
	limit := input.Limit
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	if limit < 1 || limit > MaxSearchLimit {
		return graphRequest{}, "", fmt.Errorf("limit must be between 1 and %d", MaxSearchLimit)
	}
	return graphRequest{
		method: http.MethodGet, path: "/mailFolders/" + url.PathEscape(folder) + "/messages",
		query: graphQuery{
			{name: "$filter", value: filter}, {name: "$orderby", value: "receivedDateTime desc"},
			{name: "$top", value: strconv.Itoa(limit)}, {name: "$select", value: strings.Join(summaryProperties, ",")},
		},
	}, folder, nil
}

// buildMessageFilter writes receivedDateTime first, which Graph requires for $orderby=receivedDateTime.
func buildMessageFilter(input SearchMessagesInput) (string, error) {
	receivedAfter := earliestReceivedFilter
	if input.ReceivedAfter != nil {
		receivedAfter = input.ReceivedAfter.UTC().Truncate(time.Second)
	}
	clauses := []string{"receivedDateTime ge " + receivedAfter.Format(odataTimeLayout)}
	if input.ReceivedBefore != nil {
		receivedBefore := input.ReceivedBefore.UTC().Truncate(time.Second)
		if !receivedBefore.After(receivedAfter) {
			return "", errors.New("receivedBefore must be later than receivedAfter")
		}
		clauses = append(clauses, "receivedDateTime lt "+receivedBefore.Format(odataTimeLayout))
	}
	if sender := strings.TrimSpace(input.From); sender != "" {
		if err := validateBareAddresses("from", []string{sender}); err != nil {
			return "", err
		}
		clauses = append(clauses, "from/emailAddress/address eq "+quoteODataString(sender))
	}
	if input.IsUnread {
		clauses = append(clauses, "isRead eq false")
	}
	if subject := strings.TrimSpace(input.SubjectContains); subject != "" {
		if err := validateSingleLine("subjectContains", subject, MaxSubjectFilterCharacters); err != nil {
			return "", err
		}
		clauses = append(clauses, "contains(subject,"+quoteODataString(subject)+")")
	}
	return strings.Join(clauses, " and "), nil
}

// decodeMessagePage reads Graph's collection envelope and validates its next-page link.
func decodeMessagePage(body []byte, folder string) (SearchMessagesOutput, error) {
	var envelope struct {
		Value    *[]json.RawMessage `json:"value"`
		NextLink string             `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Value == nil {
		return SearchMessagesOutput{}, errors.New("response has no value list")
	}
	page := SearchMessagesOutput{Folder: folder, Messages: []MessageSummary{}}
	for _, item := range *envelope.Value {
		if len(page.Messages) == MaxSearchLimit {
			break
		}
		message, err := decodeGraphMessage(item)
		if err != nil {
			return SearchMessagesOutput{}, err
		}
		page.Messages = append(page.Messages, message.summarize())
	}
	if envelope.NextLink != "" {
		if err := validateNextPageLink(envelope.NextLink); err != nil {
			return SearchMessagesOutput{}, err
		}
		page.HasMore, page.NextPageCursor = true, envelope.NextLink
	}
	return page, nil
}

// quoteODataString writes an OData string literal, doubling single quotes.
func quoteODataString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

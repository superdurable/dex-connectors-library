// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultSearchPageSize is the page size a zero SearchConversationsInput.PageSize requests.
	DefaultSearchPageSize = 25
	// MaxSearchPageSize is Front's largest page.
	MaxSearchPageSize = 100
	// MaxSearchTextBytes bounds SearchConversationsInput.Text.
	MaxSearchTextBytes = 512
	searchPathPrefix   = "/conversations/search/"
)

// SearchStatusFilter is one value of Front's is: search filter. Several combine with AND, so open with
// unassigned lists open conversations without an assignee.
type SearchStatusFilter string

const (
	// SearchStatusOpen matches conversations in the Open tab.
	SearchStatusOpen SearchStatusFilter = "open"
	// SearchStatusArchived matches conversations in the Archived tab.
	SearchStatusArchived SearchStatusFilter = "archived"
	// SearchStatusSnoozed matches snoozed conversations, which Front reopens later.
	SearchStatusSnoozed SearchStatusFilter = "snoozed"
	// SearchStatusTrashed matches conversations in the trash, which Front otherwise omits.
	SearchStatusTrashed SearchStatusFilter = "trashed"
	// SearchStatusAssigned matches conversations with an assignee.
	SearchStatusAssigned SearchStatusFilter = "assigned"
	// SearchStatusUnassigned matches conversations without an assignee.
	SearchStatusUnassigned SearchStatusFilter = "unassigned"
	// SearchStatusUnreplied matches conversations whose last message was inbound.
	SearchStatusUnreplied SearchStatusFilter = "unreplied"
	// SearchStatusWaiting matches ticket statuses in the waiting category.
	SearchStatusWaiting SearchStatusFilter = "waiting"
	// SearchStatusResolved matches ticket statuses in the resolved category.
	SearchStatusResolved SearchStatusFilter = "resolved"
)

// SearchStatusFilters returns every is: value Front documents.
func SearchStatusFilters() []SearchStatusFilter {
	return []SearchStatusFilter{
		SearchStatusOpen, SearchStatusArchived, SearchStatusSnoozed, SearchStatusTrashed, SearchStatusAssigned,
		SearchStatusUnassigned, SearchStatusUnreplied, SearchStatusWaiting, SearchStatusResolved,
	}
}

// bareEmailPattern admits an address that cannot end a search filter early or start another one.
var bareEmailPattern = regexp.MustCompile("^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]{1,64}@[A-Za-z0-9.-]{1,253}$")

// SearchConversationsInput selects one page of Front's conversation search. Every set field is one
// filter, and Front combines them with AND. At least one field other than the page fields is required.
type SearchConversationsInput struct {
	// Text is Front search text matched in subjects and bodies: words, which all must match, and quoted
	// phrases. It is sent unchanged, so Front also applies any filter it contains; quote user-supplied text.
	Text string `json:"text,omitempty"`
	// InboxID keeps conversations in one inbox, such as inb_41w25.
	InboxID string `json:"inboxId,omitempty"`
	// TagID keeps conversations with one tag, such as tag_13o8r1.
	TagID string `json:"tagId,omitempty"`
	// Statuses are is: filters such as open and unassigned. Blank omits trashed conversations, Front's default.
	Statuses []SearchStatusFilter `json:"statuses,omitempty"`
	// RecipientEmail keeps conversations where the address is a from, to, cc, or bcc handle of any message.
	RecipientEmail string `json:"recipientEmail,omitempty"`
	// AssigneeID keeps conversations assigned to one teammate, such as tea_2thf.
	AssigneeID string `json:"assigneeId,omitempty"`
	// ActivityAfter keeps conversations with a message or comment created after this time, in whole seconds.
	ActivityAfter *time.Time `json:"activityAfter,omitempty"`
	// PageSize is 1 to MaxSearchPageSize; zero requests DefaultSearchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is a previous Result's NextPageToken; send it with the same filters.
	PageToken string `json:"pageToken,omitempty"`
}

// SearchConversationsOutput is one page of conversations, ordered by last activity, newest first.
type SearchConversationsOutput struct {
	// Conversations is the page; it may hold fewer than PageSize while more remain.
	Conversations []Conversation `json:"conversations"`
	// TotalCount is Front's count of every matching conversation.
	TotalCount int `json:"totalCount"`
	// NextPageToken requests the next page, or is empty on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// Query is the Front search query the connector sent, for review.
	Query string `json:"query"`
}

// SearchConversationsOperation implements the searchConversations Query. Build it with
// Client.SearchConversations.
type SearchConversationsOperation struct{ client *Client }

// Definition returns the immutable searchConversations operation definition.
func (SearchConversationsOperation) Definition() sdkgo.QueryDefinition {
	return SearchConversationsDefinition
}

// Invoke validates the filters, sends GET /conversations/search/{query}, and returns one page.
func (operation SearchConversationsOperation) Invoke(call sdkgo.Call, input SearchConversationsInput) sdkgo.QueryAttempt[SearchConversationsOutput] {
	operationID := SearchConversationsDefinition.Operation.OperationID
	client := operation.client
	query, err := BuildConversationSearchQuery(input)
	output := SearchConversationsOutput{Conversations: []Conversation{}, Query: query}
	if err == nil {
		err = validateSearchPage(input)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, output, failure, sdkgo.Receipt{})
	}
	parameters := url.Values{"limit": {strconv.Itoa(cmp.Or(input.PageSize, DefaultSearchPageSize))}}
	if input.PageToken != "" {
		parameters.Set("page_token", input.PageToken)
	}
	result := client.exchange(call.Context, credentials, operationID, frontRequest{
		method: http.MethodGet, path: searchPathPrefix + escapeSearchQuery(query), query: parameters,
	})
	receipt := client.receipt(call, "")
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[SearchConversationsOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(SearchConversationsBranchProviderRejected, output, &result.failure, receipt)
	}
	if err := client.decodeSearchPage(result.response.body, &output); err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, SearchConversationsOutput{Conversations: []Conversation{}, Query: query},
			frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid search page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchConversationsBranchSearched, output, nil, receipt)
}

func (client *Client) decodeSearchPage(body []byte, output *SearchConversationsOutput) error {
	var page frontPageWire[frontConversationWire]
	if err := json.Unmarshal(body, &page); err != nil || page.Results == nil {
		return errors.New("the page is not a conversation list")
	}
	for _, wire := range page.Results {
		conversation, err := convertConversation(wire)
		if err != nil {
			return err
		}
		output.Conversations = append(output.Conversations, conversation)
	}
	if page.Total != nil && *page.Total >= 0 {
		output.TotalCount = *page.Total
	}
	token, err := client.nextPageToken(page.Pagination.Next, searchPathPrefix)
	output.NextPageToken = token
	return err
}

// BuildConversationSearchQuery returns the Front search query for input, such as
// "inbox:inb_41w25 is:open recipient:jane@acme.example.com", or an error for an invalid filter.
func BuildConversationSearchQuery(input SearchConversationsInput) (string, error) {
	// The typed fields add at most 14 filters, within Front's limit of 15.
	var terms []string
	if text := strings.TrimSpace(input.Text); text != "" {
		if len(text) > MaxSearchTextBytes || strings.ContainsFunc(text, unicode.IsControl) {
			return "", fmt.Errorf("text must be at most %d bytes without control characters", MaxSearchTextBytes)
		}
		terms = append(terms, text)
	}
	for _, field := range [][2]string{{"inboxId", input.InboxID}, {"tagId", input.TagID}, {"assigneeId", input.AssigneeID}} {
		if field[1] != "" {
			if err := validateSearchID(field[0], field[1]); err != nil {
				return "", err
			}
		}
	}
	if input.InboxID != "" {
		terms = append(terms, "inbox:"+input.InboxID)
	}
	if input.TagID != "" {
		terms = append(terms, "tag:"+input.TagID)
	}
	seenStatuses := []SearchStatusFilter{}
	for _, status := range input.Statuses {
		if !slices.Contains(SearchStatusFilters(), status) {
			return "", fmt.Errorf("statuses must be Front is: values such as open, archived, or unassigned, not %q", status)
		}
		if !slices.Contains(seenStatuses, status) {
			seenStatuses = append(seenStatuses, status)
			terms = append(terms, "is:"+string(status))
		}
	}
	if input.RecipientEmail != "" {
		if err := validateBareEmail("recipientEmail", input.RecipientEmail); err != nil {
			return "", err
		}
		terms = append(terms, "recipient:"+input.RecipientEmail)
	}
	if input.AssigneeID != "" {
		terms = append(terms, "assignee:"+input.AssigneeID)
	}
	if input.ActivityAfter != nil {
		if input.ActivityAfter.Unix() < 1 {
			return "", errors.New("activityAfter must be after 1970")
		}
		terms = append(terms, "after:"+strconv.FormatInt(input.ActivityAfter.Unix(), 10))
	}
	if len(terms) == 0 {
		return "", errors.New("set text or at least one filter; Front has no empty search")
	}
	return strings.Join(terms, " "), nil
}

func validateSearchID(field string, value string) error {
	switch field {
	case "inboxId":
		return validateResourceID(field, value, inboxIDPattern, "inb_41w25")
	case "tagId":
		return validateResourceID(field, value, tagIDPattern, "tag_13o8r1")
	default:
		return validateResourceID(field, value, teammateIDPattern, "tea_2thf")
	}
}

func validateSearchPage(input SearchConversationsInput) error {
	switch {
	case input.PageSize < 0 || input.PageSize > MaxSearchPageSize:
		return fmt.Errorf("pageSize must be 1 to %d, or zero for %d", MaxSearchPageSize, DefaultSearchPageSize)
	case input.PageToken != "" && !pageTokenPattern.MatchString(input.PageToken):
		return errors.New("pageToken must be a nextPageToken from an earlier search")
	}
	return nil
}

// validateBareEmail accepts one address without a display name, quotes, spaces, or parentheses.
func validateBareEmail(field string, value string) error {
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" || !bareEmailPattern.MatchString(value) {
		return fmt.Errorf("%s must be one bare email address such as jane@acme.example.com", field)
	}
	return nil
}

// escapeSearchQuery percent-encodes the query as one path segment, spaces as %20, as Front's examples do.
func escapeSearchQuery(query string) string {
	return strings.ReplaceAll(url.QueryEscape(query), "+", "%20")
}

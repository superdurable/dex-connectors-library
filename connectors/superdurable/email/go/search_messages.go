// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchMessagesOperation = "searchMessages"

	// DefaultSearchLimit is the number of summaries returned when SearchMessagesInput.Limit is zero.
	DefaultSearchLimit = 10
	// MaxSearchLimit is the largest SearchMessagesInput.Limit.
	MaxSearchLimit = 50
	// MaxSearchTextBytes bounds each text filter.
	MaxSearchTextBytes = 256

	searchDateLayout = "2006-01-02"
)

// SearchMessagesInput selects messages in one mailbox. Every set filter must match. Text filters use
// IMAP SEARCH, which matches a case-insensitive substring of the header; servers with full-text indexes,
// such as Dovecot with FTS, may match whole words instead. Confirm exact values, such as the sender
// address, in the Flow.
type SearchMessagesInput struct {
	// Mailbox is the mailbox to search; blank uses DefaultMailbox.
	Mailbox string `json:"mailbox,omitempty"`
	// From matches the From header, such as jane@example.com or a domain such as @example.com.
	From string `json:"from,omitempty"`
	// To matches the To header.
	To string `json:"to,omitempty"`
	// SubjectContains matches the Subject header.
	SubjectContains string `json:"subjectContains,omitempty"`
	// SinceDate keeps messages that arrived on or after this date, written YYYY-MM-DD. IMAP compares
	// the server's internal arrival date in the server's time zone and ignores the time of day.
	SinceDate string `json:"sinceDate,omitempty"`
	// BeforeDate keeps messages that arrived before this date, written YYYY-MM-DD.
	BeforeDate string `json:"beforeDate,omitempty"`
	// IsUnseen keeps only messages without the \Seen flag; false matches seen and unseen messages.
	IsUnseen bool `json:"isUnseen,omitempty"`
	// OlderThanUID continues a search: only messages with a smaller UID match. Set it from the previous
	// Result's NextOlderThanUID together with UIDValidity.
	OlderThanUID uint32 `json:"olderThanUid,omitempty"`
	// UIDValidity is required with OlderThanUID; a changed UIDVALIDITY selects providerRejected.
	UIDValidity uint32 `json:"uidValidity,omitempty"`
	// Limit is the number of summaries to return, from 1 through MaxSearchLimit; zero uses DefaultSearchLimit.
	Limit int `json:"limit,omitempty"`
}

// SearchMessagesOutput is one bounded, newest-first page of matching messages.
type SearchMessagesOutput struct {
	// Mailbox is the searched mailbox.
	Mailbox string `json:"mailbox"`
	// UIDValidity is the mailbox UIDVALIDITY that every returned UID belongs to.
	UIDValidity uint32 `json:"uidValidity"`
	// Messages holds at most Limit summaries, highest UID first; a higher UID arrived in the mailbox later.
	Messages []MessageSummary `json:"messages"`
	// TotalMatched is the number of messages that matched, including those not returned.
	TotalMatched int `json:"totalMatched"`
	// HasMore reports matches older than the last returned summary.
	HasMore bool `json:"hasMore"`
	// NextOlderThanUID is the OlderThanUID that continues the search, or zero when HasMore is false.
	NextOlderThanUID uint32 `json:"nextOlderThanUid,omitempty"`
}

// SearchMessagesOperation is the searchMessages Query.
type SearchMessagesOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (SearchMessagesOperation) Definition() sdkgo.QueryDefinition { return SearchMessagesDefinition }

// Invoke examines the mailbox read-only, runs UID SEARCH, and fetches the envelopes of the newest matches.
func (operation SearchMessagesOperation) Invoke(call sdkgo.Call, input SearchMessagesInput) sdkgo.QueryAttempt[SearchMessagesOutput] {
	mailbox := mailboxOrDefault(input.Mailbox)
	criteria, limit, err := buildSearchCriteria(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchMessagesBranchDefect, SearchMessagesOutput{},
			emailFailurePointer(sdkgo.FailureValidation, searchMessagesOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchMessagesOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchMessagesBranchDefect, SearchMessagesOutput{}, failure, sdkgo.Receipt{})
	}
	ctx, cancel := context.WithTimeout(call.Context, imapOperationTimeout)
	defer cancel()
	session, sessionFailure := operation.client.openIMAPSession(ctx, credentials, searchMessagesOperation)
	if sessionFailure != nil {
		return searchMessagesAttemptForFailure(sessionFailure)
	}
	defer session.close()
	selected, sessionFailure := session.selectMailbox(mailbox, true, 0)
	if sessionFailure != nil {
		return searchMessagesAttemptForFailure(sessionFailure)
	}
	if input.UIDValidity != 0 && input.UIDValidity != selected.UIDValidity {
		return sdkgo.NewQueryBranch(SearchMessagesBranchProviderRejected, SearchMessagesOutput{},
			emailFailurePointer(sdkgo.FailureConflict, searchMessagesOperation, "the mailbox's UIDVALIDITY changed, so the search cannot continue; start it again"),
			sdkgo.Receipt{})
	}
	searchData, err := session.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return searchMessagesAttemptForFailure(session.classify("SEARCH", err))
	}
	matched, isStatic := searchData.All.(imap.UIDSet)
	if searchData.All != nil && (!isStatic || matched.Dynamic()) {
		return searchMessagesAttemptForFailure(&serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol,
			searchMessagesOperation, "the IMAP server answered SEARCH with a dynamic UID set")})
	}
	uids, _ := matched.Nums()
	slices.SortFunc(uids, func(first imap.UID, second imap.UID) int { return cmp.Compare(second, first) })
	uids = slices.Compact(uids)
	output := SearchMessagesOutput{Mailbox: mailbox, UIDValidity: selected.UIDValidity, Messages: []MessageSummary{}, TotalMatched: len(uids)}
	page := uids[:min(limit, len(uids))]
	if len(page) > 0 {
		fetched, sessionFailure := session.fetchMessages(page, summaryFetchOptions())
		if sessionFailure != nil {
			return searchMessagesAttemptForFailure(sessionFailure)
		}
		summaries, err := buildOrderedSummaries(mailbox, selected.UIDValidity, page, fetched)
		if err != nil {
			return searchMessagesAttemptForFailure(&serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol,
				searchMessagesOperation, err.Error())})
		}
		output.Messages = summaries
	}
	if len(uids) > len(page) {
		output.HasMore, output.NextOlderThanUID = true, uint32(page[len(page)-1])
	}
	return sdkgo.NewQueryBranch(SearchMessagesBranchSearched, output, nil, sdkgo.Receipt{})
}

// buildSearchCriteria validates input and returns the IMAP criteria and page size.
func buildSearchCriteria(input SearchMessagesInput) (*imap.SearchCriteria, int, error) {
	if input.Mailbox != "" {
		if err := validateMailboxName("mailbox", input.Mailbox); err != nil {
			return nil, 0, err
		}
	}
	limit := input.Limit
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	if limit < 1 || limit > MaxSearchLimit {
		return nil, 0, fmt.Errorf("limit must be from 1 through %d", MaxSearchLimit)
	}
	criteria := &imap.SearchCriteria{}
	for _, filter := range []struct{ field, header, value string }{
		{"from", "From", input.From}, {"to", "To", input.To}, {"subjectContains", "Subject", input.SubjectContains},
	} {
		if filter.value == "" {
			continue
		}
		if err := validateSearchText(filter.field, filter.value); err != nil {
			return nil, 0, err
		}
		criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: filter.header, Value: filter.value})
	}
	since, err := parseSearchDate("sinceDate", input.SinceDate)
	if err != nil {
		return nil, 0, err
	}
	before, err := parseSearchDate("beforeDate", input.BeforeDate)
	if err != nil {
		return nil, 0, err
	}
	if !since.IsZero() && !before.IsZero() && !before.After(since) {
		return nil, 0, errors.New("beforeDate must be later than sinceDate")
	}
	criteria.Since, criteria.Before = since, before
	if input.IsUnseen {
		criteria.NotFlag = append(criteria.NotFlag, imap.FlagSeen)
	}
	if input.OlderThanUID != 0 {
		if input.UIDValidity == 0 {
			return nil, 0, errors.New("uidValidity is required with olderThanUid; copy both from the previous result")
		}
		if input.OlderThanUID == 1 {
			return nil, 0, errors.New("olderThanUid must be greater than 1")
		}
		var older imap.UIDSet
		older.AddRange(1, imap.UID(input.OlderThanUID-1))
		criteria.UID = append(criteria.UID, older)
	} else if input.UIDValidity != 0 {
		return nil, 0, errors.New("uidValidity is used only with olderThanUid")
	}
	return criteria, limit, nil
}

func validateSearchText(field string, value string) error {
	if strings.TrimSpace(value) != value || len(value) > MaxSearchTextBytes || !utf8.ValidString(value) ||
		strings.ContainsFunc(value, isControlCharacter) {
		return fmt.Errorf("%s must be at most %d bytes of text without control characters or surrounding spaces", field, MaxSearchTextBytes)
	}
	return nil
}

func parseSearchDate(field string, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(searchDateLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a calendar date written YYYY-MM-DD", field)
	}
	return parsed, nil
}

// summaryFetchOptions fetches everything a MessageSummary needs and no message content.
func summaryFetchOptions() *imap.FetchOptions {
	return &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, InternalDate: true, RFC822Size: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}
}

// buildOrderedSummaries keeps the order of uids; a message expunged between SEARCH and FETCH is skipped.
func buildOrderedSummaries(mailbox string, uidValidity uint32, uids []imap.UID, fetched []*imapclient.FetchMessageBuffer) ([]MessageSummary, error) {
	byUID := make(map[imap.UID]*imapclient.FetchMessageBuffer, len(fetched))
	for _, message := range fetched {
		byUID[message.UID] = message
	}
	summaries := make([]MessageSummary, 0, len(uids))
	for _, uid := range uids {
		message, isFetched := byUID[uid]
		if !isFetched {
			continue
		}
		summary, err := buildMessageSummary(mailbox, uidValidity, message)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func searchMessagesAttemptForFailure(failure *serverFailure) sdkgo.QueryAttempt[SearchMessagesOutput] {
	switch failure.outcome {
	case outcomeRetryable:
		return sdkgo.NewQueryRetry[SearchMessagesOutput](failure.failure, 0)
	case outcomeInvalidResponse:
		return sdkgo.NewQueryBranch(SearchMessagesBranchInvalidResponse, SearchMessagesOutput{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(SearchMessagesBranchProviderRejected, SearchMessagesOutput{}, &failure.failure, sdkgo.Receipt{})
	}
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listConversationsOperation = "listConversations"

// ListConversationsInput selects one page of a shared inbox's conversations. Hiver documents
// no status, assignee, or date filter for this list, so a Flow filters the returned page.
type ListConversationsInput struct {
	// InboxID is the Hiver shared inbox ID, as listInboxes returns it.
	InboxID string `json:"inboxId"`
	// PageToken is NextPageToken from the previous page, or empty for the first page.
	PageToken string `json:"pageToken,omitempty"`
	// PageSize is the number of conversations per page, from 10 to 100; zero uses DefaultPageSize.
	PageSize int `json:"pageSize,omitempty"`
}

// ListConversationsOutput is one page of conversations.
type ListConversationsOutput struct {
	// Conversations are the conversations of this page in Hiver's order, without message IDs.
	Conversations []Conversation `json:"conversations"`
	// NextPageToken reads the next page, or is empty after the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListConversationsOperation is the listConversations Query.
type ListConversationsOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (ListConversationsOperation) Definition() sdkgo.QueryDefinition {
	return ListConversationsDefinition
}

// Invoke sends GET /v1/inboxes/{inbox_id}/conversations with limit and next_page.
func (operation ListConversationsOperation) Invoke(call sdkgo.Call, input ListConversationsInput) sdkgo.QueryAttempt[ListConversationsOutput] {
	if err := errors.Join(validateHiverID("inboxId", input.InboxID), validatePageInput(input.PageToken, input.PageSize)); err != nil {
		return sdkgo.NewQueryBranch(ListConversationsBranchDefect, ListConversationsOutput{}, hiverFailurePointer(sdkgo.FailureValidation, listConversationsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, listConversationsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListConversationsBranchDefect, ListConversationsOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, listConversationsOperation, hiverRequest{
		method: http.MethodGet, path: inboxPath(input.InboxID) + "/conversations", query: pageQuery(input.PageToken, input.PageSize),
	})
	receipt := operation.client.receipt(call, result.response, input.InboxID)
	if attempt, isTerminal := queryAttemptForExchange[ListConversationsOutput](result, receipt, queryBranches{
		notFound: ListConversationsBranchNotFound, providerRejected: ListConversationsBranchProviderRejected,
		invalidResponse: ListConversationsBranchInvalidResponse, defect: ListConversationsBranchDefect,
	}); isTerminal {
		return attempt
	}
	results, nextPageToken, err := decodeListPage(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListConversationsBranchInvalidResponse, ListConversationsOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, listConversationsOperation, "Hiver returned an invalid conversation page: "+err.Error()), receipt)
	}
	output := ListConversationsOutput{Conversations: make([]Conversation, 0, len(results)), NextPageToken: nextPageToken}
	for index, raw := range results {
		details, err := decodeConversation(raw, input.InboxID)
		if err != nil {
			return sdkgo.NewQueryBranch(ListConversationsBranchInvalidResponse, ListConversationsOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, listConversationsOperation,
				fmt.Sprintf("Hiver returned an invalid conversation at index %d: %s", index, err.Error())), receipt)
		}
		output.Conversations = append(output.Conversations, details.Conversation)
	}
	return sdkgo.NewQueryBranch(ListConversationsBranchListed, output, nil, receipt)
}

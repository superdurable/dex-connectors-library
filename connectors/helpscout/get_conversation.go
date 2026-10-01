// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultThreadLimit is the number of newest threads getConversation returns when ThreadLimit is zero.
	DefaultThreadLimit = 10
	// MaxThreadLimit is the largest ThreadLimit; it fits Help Scout's first page of threads.
	MaxThreadLimit = 25

	// maxThreadsPerPage rejects a thread page far larger than Help Scout documents.
	maxThreadsPerPage = 100
)

// GetConversationInput names one conversation and how many of its newest threads to return.
type GetConversationInput struct {
	// ConversationID is the Help Scout conversation ID, not the conversation number shown to users.
	ConversationID int64 `json:"conversationId"`
	// ThreadLimit is 1 to MaxThreadLimit newest threads; zero returns DefaultThreadLimit.
	ThreadLimit int `json:"threadLimit,omitempty"`
}

// ConversationDetails is one conversation with its newest threads, newest first.
type ConversationDetails struct {
	// Conversation is the conversation, empty on the merged branch.
	Conversation Conversation `json:"conversation"`
	// Threads lists up to ThreadLimit of the newest threads, customer messages, replies, notes, and
	// line items alike; each Body is cut at MaxThreadBodyBytes.
	Threads []Thread `json:"threads"`
	// HasOlderThreads reports that the conversation has threads older than the last one returned.
	HasOlderThreads bool `json:"hasOlderThreads,omitempty"`
	// MergedIntoConversationID names the conversation this one was merged into, on the merged branch only.
	MergedIntoConversationID int64 `json:"mergedIntoConversationId,omitempty"`
}

// GetConversationOperation implements the getConversation Query. Build it with Client.GetConversation.
type GetConversationOperation struct{ client *Client }

type threadPageWire struct {
	Embedded struct {
		Threads []helpScoutThreadWire `json:"threads"`
	} `json:"_embedded"`
	Links helpScoutPageLinks `json:"_links"`
}

// Definition returns the immutable getConversation operation definition.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition { return GetConversationDefinition }

// Invoke reads GET /v2/conversations/{id} and then the first page of GET /v2/conversations/{id}/threads,
// which Help Scout sorts newest first. A 301 is Help Scout's answer for a conversation merged within the
// last 60 days and selects merged with the Location's conversation ID.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[ConversationDetails] {
	operationID := GetConversationDefinition.Operation.OperationID
	client := operation.client
	if err := validateGetConversationInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{},
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[ConversationDetails](operationID, GetConversationBranchDefect, err)
	}
	branches := failureBranches{
		notFound: GetConversationBranchNotFound, providerRejected: GetConversationBranchProviderRejected, invalidResponse: GetConversationBranchInvalidResponse,
	}
	response, err := client.send(call.Context, call, &credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(input.ConversationID)}, nil)
	if err == nil && response.statusCode == http.StatusMovedPermanently {
		return client.mergedConversationAttempt(call, input.ConversationID, response)
	}
	if attempt, isTerminal := classifyQueryExchange[ConversationDetails](client, call, operationID, credentials, response, err, branches, input.ConversationID); isTerminal {
		return attempt
	}
	conversation, err := decodeConversationBody(response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{},
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid conversation: "+err.Error()),
			client.receipt(call, input.ConversationID, ""))
	}
	response, err = client.send(call.Context, call, &credentials, helpScoutRequest{
		method: http.MethodGet, path: conversationPath(input.ConversationID) + "/threads", query: url.Values{"page": {"1"}},
	}, nil)
	if attempt, isTerminal := classifyQueryExchange[ConversationDetails](client, call, operationID, credentials, response, err, branches, input.ConversationID); isTerminal {
		return attempt
	}
	threads, hasOlderThreads, err := decodeNewestThreads(response.body, threadLimit(input))
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{},
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid thread page: "+err.Error()),
			client.receipt(call, input.ConversationID, ""))
	}
	return sdkgo.NewQueryBranch(GetConversationBranchFound, ConversationDetails{
		Conversation: conversation, Threads: threads, HasOlderThreads: hasOlderThreads,
	}, nil, client.receipt(call, input.ConversationID, ""))
}

// mergedConversationAttempt reads the merged-into conversation ID from the 301's Location header.
func (client *Client) mergedConversationAttempt(call sdkgo.Call, conversationID int64, response helpScoutResponse) sdkgo.QueryAttempt[ConversationDetails] {
	operationID := GetConversationDefinition.Operation.OperationID
	mergedIntoID, err := parseConversationLocation(response.header.Get("Location"))
	if err != nil || mergedIntoID == conversationID {
		return sdkgo.NewQueryBranch(GetConversationBranchProviderRejected, ConversationDetails{},
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout answered 301 without a conversation Location"),
			client.receipt(call, conversationID, ""))
	}
	return sdkgo.NewQueryBranch(GetConversationBranchMerged, ConversationDetails{Threads: []Thread{}, MergedIntoConversationID: mergedIntoID},
		helpScoutFailurePointer(operationID, sdkgo.FailureNotFound, fmt.Sprintf("conversation %d was merged into conversation %d", conversationID, mergedIntoID)),
		client.receipt(call, mergedIntoID, ""))
}

// parseConversationLocation accepts /v2/conversations/{id} on api.helpscout.net or as a relative path.
func parseConversationLocation(location string) (int64, error) {
	parsed, err := url.Parse(location)
	if err != nil || (parsed.Host != "" && (parsed.Scheme != "https" || parsed.Host != "api.helpscout.net")) {
		return 0, errors.New("location is not a Help Scout API URL")
	}
	identifier, isCut := strings.CutPrefix(parsed.Path, "/v2/conversations/")
	conversationID, err := strconv.ParseInt(identifier, 10, 64)
	if !isCut || err != nil || conversationID < 1 {
		return 0, errors.New("location does not name a conversation")
	}
	return conversationID, nil
}

func validateGetConversationInput(input GetConversationInput) error {
	if input.ConversationID < 1 {
		return errors.New("conversationId must be a positive Help Scout conversation ID")
	}
	if input.ThreadLimit < 0 || input.ThreadLimit > MaxThreadLimit {
		return fmt.Errorf("threadLimit must be from 1 through %d, or zero for %d", MaxThreadLimit, DefaultThreadLimit)
	}
	return nil
}

func threadLimit(input GetConversationInput) int {
	if input.ThreadLimit == 0 {
		return DefaultThreadLimit
	}
	return input.ThreadLimit
}

// decodeNewestThreads keeps the first limit threads of Help Scout's newest-first page.
func decodeNewestThreads(body []byte, limit int) ([]Thread, bool, error) {
	var page threadPageWire
	if err := decodeHelpScoutJSON(body, &page); err != nil {
		return nil, false, errors.New("page is not a JSON object")
	}
	if len(page.Embedded.Threads) > maxThreadsPerPage {
		return nil, false, errors.New("page holds more threads than a conversation can")
	}
	threads := make([]Thread, 0, min(limit, len(page.Embedded.Threads)))
	for index, threadWire := range page.Embedded.Threads {
		if index == limit {
			break
		}
		thread, err := decodeThread(threadWire)
		if err != nil {
			return nil, false, fmt.Errorf("thread %d: %w", index, err)
		}
		threads = append(threads, thread)
	}
	hasOlderThreads := len(page.Embedded.Threads) > limit || page.Links.Next != nil
	return threads, hasOlderThreads, nil
}

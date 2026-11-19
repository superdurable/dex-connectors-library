// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MessageIdentity contains the IDs returned by Gmail's message list.
type MessageIdentity struct {
	// ID is the Gmail message ID.
	ID string `json:"id"`
	// ThreadID identifies the containing conversation.
	ThreadID string `json:"threadId"`
}

// ListMessagesInput selects one page of Gmail message identities.
type ListMessagesInput struct {
	// Query uses Gmail search syntax, such as from:ada@example.com is:unread.
	Query string `json:"query,omitempty"`
	// LabelIDs requires every named label on a returned message.
	LabelIDs []string `json:"labelIds,omitempty"`
	// MaxResults is 1 through 500; zero uses Gmail's default of 100.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the cursor returned by the preceding page.
	PageToken string `json:"pageToken,omitempty"`
	// IncludeSpamTrash also searches Spam and Trash when true.
	IncludeSpamTrash bool `json:"includeSpamTrash,omitempty"`
}

// MessagesPage contains one message list page.
type MessagesPage struct {
	// Messages contains IDs; call getMessage to read bodies.
	Messages []MessageIdentity `json:"messages"`
	// NextPageToken is blank on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// ResultSizeEstimate is Gmail's approximate total match count.
	ResultSizeEstimate int `json:"resultSizeEstimate"`
}

// ThreadIdentity contains the identity and snippet returned by a thread list.
type ThreadIdentity struct {
	// ID is the Gmail thread ID.
	ID string `json:"id"`
	// Snippet is Gmail's short preview.
	Snippet string `json:"snippet,omitempty"`
}

// ListThreadsInput selects one page of Gmail thread identities.
type ListThreadsInput struct {
	// Query uses Gmail search syntax.
	Query string `json:"query,omitempty"`
	// LabelIDs requires each named label on a matching thread.
	LabelIDs []string `json:"labelIds,omitempty"`
	// MaxResults is 1 through 500; zero uses Gmail's default.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the cursor returned by a previous page.
	PageToken string `json:"pageToken,omitempty"`
	// IncludeSpamTrash also searches Spam and Trash when true.
	IncludeSpamTrash bool `json:"includeSpamTrash,omitempty"`
}

// ThreadsPage contains one thread list page.
type ThreadsPage struct {
	// Threads contains IDs; call getThread to read messages.
	Threads []ThreadIdentity `json:"threads"`
	// NextPageToken is blank on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// ResultSizeEstimate is Gmail's approximate total match count.
	ResultSizeEstimate int `json:"resultSizeEstimate"`
}

// GetThreadInput identifies one Gmail conversation.
type GetThreadInput struct {
	// ThreadID is the Gmail thread ID.
	ThreadID string `json:"threadId"`
}

// Thread contains the decoded messages in one Gmail conversation.
type Thread struct {
	// ID is the Gmail thread ID.
	ID string `json:"id"`
	// Snippet is Gmail's short preview.
	Snippet string `json:"snippet,omitempty"`
	// Messages are decoded in Gmail's order.
	Messages []Message `json:"messages"`
}

// ListLabelsInput requests all labels visible to the connected account.
type ListLabelsInput struct{}

// Label is a Gmail label with its message and thread counts.
type Label struct {
	// ID is the immutable Gmail label ID.
	ID string `json:"id"`
	// Name is the display name in Gmail.
	Name string `json:"name"`
	// Type is system or user.
	Type string `json:"type,omitempty"`
	// MessagesTotal counts messages carrying the label.
	MessagesTotal int `json:"messagesTotal,omitempty"`
	// MessagesUnread counts unread messages carrying the label.
	MessagesUnread int `json:"messagesUnread,omitempty"`
	// ThreadsTotal counts threads carrying the label.
	ThreadsTotal int `json:"threadsTotal,omitempty"`
	// ThreadsUnread counts unread threads carrying the label.
	ThreadsUnread int `json:"threadsUnread,omitempty"`
}

// LabelsList contains all labels returned by Gmail.
type LabelsList struct {
	// Labels contains system and user labels.
	Labels []Label `json:"labels"`
}

// GetLabelInput identifies one Gmail label.
type GetLabelInput struct {
	// LabelID is the immutable Gmail label ID.
	LabelID string `json:"labelId"`
}

// ListMessagesOperation searches one page of messages.
type ListMessagesOperation struct{ client *Client }

// Definition returns the generated message listing contract.
func (ListMessagesOperation) Definition() sdkgo.QueryDefinition { return ListMessagesDefinition }

// ListMessages binds the listing Query to the client.
func (client *Client) ListMessages() ListMessagesOperation { return ListMessagesOperation{client} }

// Invoke queries one page of message IDs.
func (operation ListMessagesOperation) Invoke(call sdkgo.Call, input ListMessagesInput) sdkgo.QueryAttempt[MessagesPage] {
	if !validMailboxPageInput(input.Query, input.LabelIDs, input.MaxResults, input.PageToken) {
		return mailboxBranch(ListMessagesBranchDefect, MessagesPage{}, sdkgo.FailureValidation, "Gmail message search input is invalid")
	}
	query := mailboxListQuery(input.Query, input.LabelIDs, input.MaxResults, input.PageToken, input.IncludeSpamTrash)
	response, failure := operation.client.getMailboxResource(call, "/users/me/messages?"+query.Encode())
	if failure != nil {
		return mailboxRequestFailure[MessagesPage](*failure, ListMessagesBranchDefect, ListMessagesBranchInvalidResponse)
	}
	if response.statusCode != http.StatusOK {
		return mailboxStatus[MessagesPage](response, ListMessagesBranchProviderRejected, "listMessages")
	}
	var page MessagesPage
	if err := json.Unmarshal(response.body, &page); err != nil || page.ResultSizeEstimate < 0 || !validMailboxToken(page.NextPageToken) {
		return mailboxBranch(ListMessagesBranchInvalidResponse, MessagesPage{}, sdkgo.FailureProtocol, "Gmail returned invalid message list JSON")
	}
	for _, item := range page.Messages {
		if !validMailboxID(item.ID) || !validMailboxID(item.ThreadID) {
			return mailboxBranch(ListMessagesBranchInvalidResponse, MessagesPage{}, sdkgo.FailureProtocol, "Gmail returned invalid message IDs")
		}
	}
	if page.Messages == nil {
		page.Messages = []MessageIdentity{}
	}
	return sdkgo.NewQueryBranch(ListMessagesBranchListed, page, nil, sdkgo.Receipt{})
}

// ListThreadsOperation searches one page of threads.
type ListThreadsOperation struct{ client *Client }

// Definition returns the generated thread listing contract.
func (ListThreadsOperation) Definition() sdkgo.QueryDefinition { return ListThreadsDefinition }

// ListThreads binds the listing Query to the client.
func (client *Client) ListThreads() ListThreadsOperation { return ListThreadsOperation{client} }

// Invoke queries one page of thread IDs.
func (operation ListThreadsOperation) Invoke(call sdkgo.Call, input ListThreadsInput) sdkgo.QueryAttempt[ThreadsPage] {
	if !validMailboxPageInput(input.Query, input.LabelIDs, input.MaxResults, input.PageToken) {
		return mailboxBranch(ListThreadsBranchDefect, ThreadsPage{}, sdkgo.FailureValidation, "Gmail thread search input is invalid")
	}
	query := mailboxListQuery(input.Query, input.LabelIDs, input.MaxResults, input.PageToken, input.IncludeSpamTrash)
	response, failure := operation.client.getMailboxResource(call, "/users/me/threads?"+query.Encode())
	if failure != nil {
		return mailboxRequestFailure[ThreadsPage](*failure, ListThreadsBranchDefect, ListThreadsBranchInvalidResponse)
	}
	if response.statusCode != http.StatusOK {
		return mailboxStatus[ThreadsPage](response, ListThreadsBranchProviderRejected, "listThreads")
	}
	var page ThreadsPage
	if err := json.Unmarshal(response.body, &page); err != nil || page.ResultSizeEstimate < 0 || !validMailboxToken(page.NextPageToken) {
		return mailboxBranch(ListThreadsBranchInvalidResponse, ThreadsPage{}, sdkgo.FailureProtocol, "Gmail returned invalid thread list JSON")
	}
	for _, item := range page.Threads {
		if !validMailboxID(item.ID) {
			return mailboxBranch(ListThreadsBranchInvalidResponse, ThreadsPage{}, sdkgo.FailureProtocol, "Gmail returned invalid thread IDs")
		}
	}
	if page.Threads == nil {
		page.Threads = []ThreadIdentity{}
	}
	return sdkgo.NewQueryBranch(ListThreadsBranchListed, page, nil, sdkgo.Receipt{})
}

// GetThreadOperation reads one conversation.
type GetThreadOperation struct{ client *Client }

// Definition returns the generated thread read contract.
func (GetThreadOperation) Definition() sdkgo.QueryDefinition { return GetThreadDefinition }

// GetThread binds the read Query to the client.
func (client *Client) GetThread() GetThreadOperation { return GetThreadOperation{client} }

// Invoke reads and decodes all messages in the named thread.
func (operation GetThreadOperation) Invoke(call sdkgo.Call, input GetThreadInput) sdkgo.QueryAttempt[Thread] {
	if !validMailboxID(input.ThreadID) {
		return mailboxBranch(GetThreadBranchDefect, Thread{}, sdkgo.FailureValidation, "Gmail thread ID is invalid")
	}
	response, failure := operation.client.getMailboxResource(call, "/users/me/threads/"+url.PathEscape(input.ThreadID)+"?format=full")
	if failure != nil {
		return mailboxRequestFailure[Thread](*failure, GetThreadBranchDefect, GetThreadBranchInvalidResponse)
	}
	if response.statusCode == http.StatusNotFound {
		return mailboxBranch(GetThreadBranchNotFound, Thread{}, sdkgo.FailureNotFound, "Gmail thread was not found")
	}
	if response.statusCode != http.StatusOK {
		return mailboxStatus[Thread](response, GetThreadBranchProviderRejected, "getThread")
	}
	var resource struct {
		ID       string                 `json:"id"`
		Snippet  string                 `json:"snippet"`
		Messages []gmailMessageResource `json:"messages"`
	}
	if err := json.Unmarshal(response.body, &resource); err != nil || resource.ID != input.ThreadID || len(resource.Messages) == 0 {
		return mailboxBranch(GetThreadBranchInvalidResponse, Thread{}, sdkgo.FailureProtocol, "Gmail returned invalid thread JSON")
	}
	thread := Thread{ID: resource.ID, Snippet: resource.Snippet, Messages: make([]Message, 0, len(resource.Messages))}
	for _, item := range resource.Messages {
		message, err := decodeGmailMessage(item)
		if err != nil || message.ThreadID != thread.ID {
			return mailboxBranch(GetThreadBranchInvalidResponse, Thread{}, sdkgo.FailureProtocol, "Gmail returned invalid thread message JSON")
		}
		thread.Messages = append(thread.Messages, message)
	}
	return sdkgo.NewQueryBranch(GetThreadBranchFound, thread, nil, sdkgo.Receipt{Provider: "gmail", ProviderObjectID: thread.ID, ObservedAt: operation.client.now().UTC()})
}

// ListLabelsOperation reads the mailbox's labels.
type ListLabelsOperation struct{ client *Client }

// Definition returns the generated label listing contract.
func (ListLabelsOperation) Definition() sdkgo.QueryDefinition { return ListLabelsDefinition }

// ListLabels binds the listing Query to the client.
func (client *Client) ListLabels() ListLabelsOperation { return ListLabelsOperation{client} }

// Invoke lists Gmail's labels.
func (operation ListLabelsOperation) Invoke(call sdkgo.Call, _ ListLabelsInput) sdkgo.QueryAttempt[LabelsList] {
	response, failure := operation.client.getMailboxResource(call, "/users/me/labels")
	if failure != nil {
		return mailboxRequestFailure[LabelsList](*failure, ListLabelsBranchDefect, ListLabelsBranchInvalidResponse)
	}
	if response.statusCode != http.StatusOK {
		return mailboxStatus[LabelsList](response, ListLabelsBranchProviderRejected, "listLabels")
	}
	var list LabelsList
	if err := json.Unmarshal(response.body, &list); err != nil {
		return mailboxBranch(ListLabelsBranchInvalidResponse, LabelsList{}, sdkgo.FailureProtocol, "Gmail returned invalid labels JSON")
	}
	for _, label := range list.Labels {
		if !validMailboxID(label.ID) || label.Name == "" {
			return mailboxBranch(ListLabelsBranchInvalidResponse, LabelsList{}, sdkgo.FailureProtocol, "Gmail returned invalid label identity")
		}
	}
	if list.Labels == nil {
		list.Labels = []Label{}
	}
	return sdkgo.NewQueryBranch(ListLabelsBranchListed, list, nil, sdkgo.Receipt{})
}

// GetLabelOperation reads one label and its counts.
type GetLabelOperation struct{ client *Client }

// Definition returns the generated label read contract.
func (GetLabelOperation) Definition() sdkgo.QueryDefinition { return GetLabelDefinition }

// GetLabel binds the read Query to the client.
func (client *Client) GetLabel() GetLabelOperation { return GetLabelOperation{client} }

// Invoke reads one Gmail label.
func (operation GetLabelOperation) Invoke(call sdkgo.Call, input GetLabelInput) sdkgo.QueryAttempt[Label] {
	if !validMailboxID(input.LabelID) {
		return mailboxBranch(GetLabelBranchDefect, Label{}, sdkgo.FailureValidation, "Gmail label ID is invalid")
	}
	response, failure := operation.client.getMailboxResource(call, "/users/me/labels/"+url.PathEscape(input.LabelID))
	if failure != nil {
		return mailboxRequestFailure[Label](*failure, GetLabelBranchDefect, GetLabelBranchInvalidResponse)
	}
	if response.statusCode == http.StatusNotFound {
		return mailboxBranch(GetLabelBranchNotFound, Label{}, sdkgo.FailureNotFound, "Gmail label was not found")
	}
	if response.statusCode != http.StatusOK {
		return mailboxStatus[Label](response, GetLabelBranchProviderRejected, "getLabel")
	}
	var label Label
	if err := json.Unmarshal(response.body, &label); err != nil || label.ID != input.LabelID || label.Name == "" {
		return mailboxBranch(GetLabelBranchInvalidResponse, Label{}, sdkgo.FailureProtocol, "Gmail returned invalid label JSON")
	}
	return sdkgo.NewQueryBranch(GetLabelBranchFound, label, nil, sdkgo.Receipt{Provider: "gmail", ProviderObjectID: label.ID, ObservedAt: operation.client.now().UTC()})
}

func validMailboxID(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "/\r\n")
}
func validMailboxToken(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "\r\n")
}
func validMailboxPageInput(query string, labels []string, maxResults int, token string) bool {
	if len(query) > 4096 || strings.ContainsAny(query, "\r\n") || maxResults < 0 || maxResults > 500 || !validMailboxToken(token) {
		return false
	}
	for _, label := range labels {
		if !validMailboxID(label) {
			return false
		}
	}
	return true
}
func mailboxListQuery(query string, labels []string, maxResults int, token string, includeSpamTrash bool) url.Values {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	for _, label := range labels {
		values.Add("labelIds", label)
	}
	if maxResults > 0 {
		values.Set("maxResults", strconv.Itoa(maxResults))
	}
	if token != "" {
		values.Set("pageToken", token)
	}
	if includeSpamTrash {
		values.Set("includeSpamTrash", "true")
	}
	return values
}

func (client *Client) getMailboxResource(call sdkgo.Call, path string) (gmailHTTPResult, *sdkgo.Failure) {
	credentials, err := client.resolveCredentials(call.Context, call)
	if err != nil {
		return gmailHTTPResult{}, mailboxFailurePointer(sdkgo.FailureAuthentication, "Gmail credentials are unavailable")
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	response, _, err := client.doAuthenticatedRequest(call.Context, call, credentials, http.MethodGet, target, nil, "")
	if err != nil {
		return gmailHTTPResult{}, mailboxFailurePointer(sdkgo.FailureTransport, "Gmail read failed before a response was received")
	}
	defer response.Body.Close()
	result := gmailHTTPResult{statusCode: response.StatusCode, header: response.Header.Clone()}
	limit := client.maxResponseBytes
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limit = 64 << 10
	}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return gmailHTTPResult{}, mailboxFailurePointer(sdkgo.FailureTransport, "Gmail response was interrupted")
	}
	if int64(len(result.body)) > limit && response.StatusCode >= 200 && response.StatusCode < 300 {
		return gmailHTTPResult{}, mailboxFailurePointer(sdkgo.FailureResponseTooLarge, "Gmail response exceeded the configured limit")
	}
	return result, nil
}

func mailboxBranch[T any](branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := mailboxFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func mailboxRequestFailure[T any](failure sdkgo.Failure, defectBranch, invalidBranch sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if failure.Kind == sdkgo.FailureAuthentication {
		return sdkgo.NewQueryBranch(defectBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	if failure.Kind == sdkgo.FailureResponseTooLarge {
		return sdkgo.NewQueryBranch(invalidBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryRetry[T](failure, 0)
}
func mailboxStatus[T any](response gmailHTTPResult, rejectedBranch sdkgo.BranchID, operationID string) sdkgo.QueryAttempt[T] {
	status := response.statusCode
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		delay, _ := retryAfter(response.header)
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "gmail", Operation: operationID, Message: "Gmail read is temporarily unavailable"}, delay)
	}
	return mailboxBranch(rejectedBranch, *new(T), statusFailureKind(status), "Gmail rejected the read with HTTP "+strconv.Itoa(status))
}
func mailboxFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "gmail", Operation: "mailboxRead", Message: message}
}
func mailboxFailurePointer(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := mailboxFailure(kind, message)
	return &failure
}

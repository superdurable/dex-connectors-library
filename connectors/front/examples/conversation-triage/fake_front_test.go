// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// fakeAPIToken is deliberately not shaped like a Front JSON Web Token.
	fakeAPIToken = "front-example-test-token-0123456789"
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so Dex dispatches a backup attempt.
	slowResponseDelay = 9 * time.Second
	// providerSentinel is Front message text the connector must never repeat.
	providerSentinel = "SENTINEL-front-message-text"
	fakeTriageTagID  = "tag_triaged"
	fakeTeammateID   = "tea_leela"
	fakeInboxID      = "inb_support"
)

// fakeFront is a scripted, stateful stand-in for the Front Core API endpoints the example calls.
type fakeFront struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping or held, so a test can assert their final effect.
	delayedRequests sync.WaitGroup
	nextID          int
	conversations   map[string]*fakeConversation
	contacts        map[string]string
	requests        []fakeRecordedRequest

	shouldDelayFirstComment    bool
	holdsFirstComment          chan struct{}
	shouldRejectComment        bool
	shouldRateLimitFirstSearch bool
	shouldDelayFirstTagWrite   bool
}

type fakeConversation struct {
	id, subject, inboxID, status, assigneeID string
	tagIDs                                   []string
	messages                                 []fakeMessage
	comments                                 []fakeComment
	updatedAt                                int64
}

type fakeMessage struct {
	id, fromHandle, body string
	isInbound            bool
	createdAt            int64
}

type fakeComment struct {
	id, body string
	postedAt int64
}

type fakeRecordedRequest struct {
	name, method, path, body string
	at                       time.Time
}

func newFakeFront(t *testing.T) *fakeFront {
	t.Helper()
	provider := &fakeFront{t: t, conversations: map[string]*fakeConversation{}, contacts: map[string]string{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeFront) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	if request.Header.Get("Authorization") != "Bearer "+fakeAPIToken {
		provider.writeError(response, http.StatusUnauthorized)
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodGet && len(segments) == 3 && segments[0] == "conversations" && segments[1] == "search":
		provider.search(response, request, segments[2])
	case len(segments) == 2 && segments[0] == "contacts" && strings.HasPrefix(segments[1], "alt:email:"):
		provider.record("contact", request, body)
		provider.findContact(response, strings.TrimPrefix(segments[1], "alt:email:"))
	case len(segments) >= 2 && segments[0] == "conversations":
		provider.serveConversation(response, request, body, segments[1], strings.Join(segments[2:], "/"))
	default:
		provider.writeError(response, http.StatusNotFound)
	}
}

func (provider *fakeFront) serveConversation(response http.ResponseWriter, request *http.Request, body []byte, conversationID string, resource string) {
	route := request.Method + " " + resource
	names := map[string]string{
		"GET ": "read", "PATCH ": "patch", "GET messages": "messages", "POST messages": "reply",
		"GET comments": "comments", "POST comments": "comment", "POST tags": "tagAdd", "DELETE tags": "tagRemove",
	}
	name, isKnown := names[route]
	if !isKnown {
		provider.writeError(response, http.StatusNotFound)
		return
	}
	attempt := provider.record(name, request, body)
	provider.mutex.Lock()
	_, isKnownConversation := provider.conversations[conversationID]
	provider.mutex.Unlock()
	if !isKnownConversation {
		provider.writeError(response, http.StatusNotFound)
		return
	}
	switch name {
	case "read":
		provider.writeConversation(response, conversationID)
	case "messages":
		provider.listMessages(response, request, conversationID)
	case "comments":
		provider.listComments(response, conversationID)
	case "comment":
		provider.addComment(response, body, conversationID, attempt)
	case "patch":
		provider.patchConversation(response, body, conversationID)
	case "tagAdd", "tagRemove":
		provider.changeTags(response, body, conversationID, name == "tagAdd", attempt)
	default:
		provider.writeError(response, http.StatusForbidden)
	}
}

// search supports the is:open, recipient:, and inbox: filters the example sends.
func (provider *fakeFront) search(response http.ResponseWriter, request *http.Request, query string) {
	attempt := provider.record("search", request, nil)
	if attempt == 1 && provider.shouldRateLimitFirstSearch {
		response.Header().Set("Retry-After", "1")
		provider.writeError(response, http.StatusTooManyRequests)
		return
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	results := []any{}
	for _, conversation := range provider.sortedConversations() {
		isMatch := true
		for _, term := range strings.Fields(query) {
			name, value, _ := strings.Cut(term, ":")
			switch name {
			case "is":
				isMatch = isMatch && value == "open" && (conversation.status == "assigned" || conversation.status == "unassigned")
			case "inbox":
				isMatch = isMatch && conversation.inboxID == value
			case "recipient":
				isMatch = isMatch && slices.ContainsFunc(conversation.messages, func(message fakeMessage) bool { return message.fromHandle == value })
			default:
				provider.writeErrorLocked(response, http.StatusBadRequest)
				return
			}
		}
		if isMatch {
			results = append(results, provider.conversationJSON(conversation))
		}
	}
	provider.writeValueLocked(response, http.StatusOK, map[string]any{"_pagination": map[string]any{"next": nil}, "_total": len(results), "_results": results})
}

func (provider *fakeFront) findContact(response http.ResponseWriter, email string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	contactID, hasContact := provider.contacts[email]
	if !hasContact {
		provider.writeErrorLocked(response, http.StatusNotFound)
		return
	}
	provider.writeValueLocked(response, http.StatusOK, map[string]any{
		"id": contactID, "name": "Jane Smith", "is_private": false, "updated_at": 1767225600,
		"handles": []any{map[string]any{"handle": email, "source": "email"}}, "lists": []any{map[string]any{"name": "Customers"}},
	})
}

func (provider *fakeFront) writeConversation(response http.ResponseWriter, conversationID string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.writeValueLocked(response, http.StatusOK, provider.conversationJSON(provider.conversations[conversationID]))
}

// listMessages answers newest first with a company-host next link, as Front does.
func (provider *fakeFront) listMessages(response http.ResponseWriter, request *http.Request, conversationID string) {
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	require.NoError(provider.t, err)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	messages := provider.conversations[conversationID].messages
	results := []any{}
	for index := len(messages) - 1; index >= 0 && len(results) < limit; index-- {
		message := messages[index]
		document := map[string]any{
			"id": message.id, "type": "email", "is_inbound": message.isInbound, "draft_mode": nil, "subject": "Re: order 88213",
			"blurb": message.body, "body": "<p>" + message.body + "</p>", "created_at": message.createdAt,
			"recipients": []any{map[string]any{"handle": message.fromHandle, "role": "from", "name": nil}},
		}
		if !message.isInbound {
			document["author"] = map[string]any{"id": fakeTeammateID, "email": "leela@planet-express.example.com"}
		}
		results = append(results, document)
	}
	var next any
	if len(messages) > limit {
		next = "https://acme.api.frontapp.com/conversations/" + conversationID + "/messages?limit=" + strconv.Itoa(limit) + "&page_token=fake0123"
	}
	provider.writeValueLocked(response, http.StatusOK, map[string]any{"_pagination": map[string]any{"next": next}, "_results": results})
}

func (provider *fakeFront) listComments(response http.ResponseWriter, conversationID string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	comments := provider.conversations[conversationID].comments
	results := []any{}
	for index := len(comments) - 1; index >= 0; index-- {
		results = append(results, map[string]any{
			"id": comments[index].id, "body": comments[index].body, "is_pinned": false, "posted_at": comments[index].postedAt,
			"author": map[string]any{"id": fakeTeammateID, "email": "leela@planet-express.example.com"},
		})
	}
	provider.writeValueLocked(response, http.StatusOK, map[string]any{"_results": results})
}

// addComment applies a comment; the scripted first attempt answers late or is held until the test releases it.
func (provider *fakeFront) addComment(response http.ResponseWriter, body []byte, conversationID string, attempt int) {
	var comment struct {
		Body string `json:"body"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &comment))
	if provider.shouldRejectComment {
		provider.writeError(response, http.StatusForbidden)
		return
	}
	if attempt == 1 && (provider.shouldDelayFirstComment || provider.holdsFirstComment != nil) {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		if provider.holdsFirstComment != nil {
			<-provider.holdsFirstComment
		} else {
			time.Sleep(slowResponseDelay)
		}
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := provider.conversations[conversationID]
	provider.nextID++
	created := fakeComment{id: fmt.Sprintf("com_%d", provider.nextID), body: comment.Body, postedAt: time.Now().Unix()}
	conversation.comments = append(conversation.comments, created)
	provider.writeValueLocked(response, http.StatusCreated, map[string]any{"id": created.id, "body": created.body, "is_pinned": false, "posted_at": created.postedAt})
}

func (provider *fakeFront) patchConversation(response http.ResponseWriter, body []byte, conversationID string) {
	var patch map[string]json.RawMessage
	require.NoError(provider.t, json.Unmarshal(body, &patch))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := provider.conversations[conversationID]
	if raw, hasAssignee := patch["assignee_id"]; hasAssignee {
		conversation.assigneeID = ""
		require.NoError(provider.t, json.Unmarshal(raw, &conversation.assigneeID))
		if conversation.status == "assigned" || conversation.status == "unassigned" {
			conversation.status = map[bool]string{true: "assigned", false: "unassigned"}[conversation.assigneeID != ""]
		}
	}
	response.WriteHeader(http.StatusNoContent)
}

// changeTags applies tag IDs as a set; the scripted first write answers after slowResponseDelay.
func (provider *fakeFront) changeTags(response http.ResponseWriter, body []byte, conversationID string, isAdd bool, attempt int) {
	var tags struct {
		TagIDs []string `json:"tag_ids"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &tags))
	if attempt == 1 && provider.shouldDelayFirstTagWrite {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := provider.conversations[conversationID]
	for _, tagID := range tags.TagIDs {
		switch {
		case isAdd && !slices.Contains(conversation.tagIDs, tagID):
			conversation.tagIDs = append(conversation.tagIDs, tagID)
		case !isAdd:
			conversation.tagIDs = slices.DeleteFunc(conversation.tagIDs, func(existing string) bool { return existing == tagID })
		}
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeFront) conversationJSON(conversation *fakeConversation) map[string]any {
	var assignee, recipient any
	if conversation.assigneeID != "" {
		assignee = map[string]any{"id": conversation.assigneeID, "email": "leela@planet-express.example.com", "first_name": "Leela"}
	}
	for index := len(conversation.messages) - 1; index >= 0; index-- {
		if message := conversation.messages[index]; message.isInbound {
			links := map[string]any{}
			if contactID, hasContact := provider.contacts[message.fromHandle]; hasContact {
				links["related"] = map[string]any{"contact": "https://acme.api.frontapp.com/contacts/" + contactID}
			}
			recipient = map[string]any{"_links": links, "handle": message.fromHandle, "role": "from"}
			break
		}
	}
	tags := []any{}
	for _, tagID := range conversation.tagIDs {
		tags = append(tags, map[string]any{"id": tagID, "name": strings.TrimPrefix(tagID, "tag_")})
	}
	return map[string]any{
		"_links": map[string]any{"self": "https://acme.api.frontapp.com/conversations/" + conversation.id},
		"id":     conversation.id, "type": "conversation", "subject": conversation.subject, "status": conversation.status,
		"ticket_ids": []string{}, "assignee": assignee, "recipient": recipient, "tags": tags, "links": []any{},
		"custom_fields": map[string]any{}, "is_private": false, "scheduled_reminders": []any{}, "metadata": map[string]any{},
		"created_at": conversation.messages[0].createdAt, "updated_at": conversation.updatedAt,
	}
}

// sortedConversations orders by last activity, newest first, as Front's search does.
func (provider *fakeFront) sortedConversations() []*fakeConversation {
	conversations := make([]*fakeConversation, 0, len(provider.conversations))
	for _, conversation := range provider.conversations {
		conversations = append(conversations, conversation)
	}
	slices.SortFunc(conversations, func(left, right *fakeConversation) int { return int(right.updatedAt - left.updatedAt) })
	return conversations
}

func (provider *fakeFront) seedContact(email string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	contactID := fmt.Sprintf("crd_%d", provider.nextID)
	provider.contacts[email] = contactID
	return contactID
}

// seedConversation adds a conversation with one inbound message from fromEmail, last active createdAgo.
func (provider *fakeFront) seedConversation(fromEmail string, inboxID string, status string, createdAgo time.Duration, body string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	conversationID := fmt.Sprintf("cnv_%d", provider.nextID)
	createdAt := time.Now().Add(-createdAgo).Unix()
	provider.conversations[conversationID] = &fakeConversation{
		id: conversationID, subject: "About order 88213", inboxID: inboxID, status: status, updatedAt: createdAt,
		messages: []fakeMessage{{id: fmt.Sprintf("msg_%d", provider.nextID), fromHandle: fromEmail, body: body, isInbound: true, createdAt: createdAt}},
	}
	return conversationID
}

func (provider *fakeFront) seedComment(conversationID string, body string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	conversation := provider.conversations[conversationID]
	conversation.comments = append(conversation.comments, fakeComment{id: fmt.Sprintf("com_%d", provider.nextID), body: body, postedAt: time.Now().Unix()})
}

// record keeps the request and returns how many requests of that name arrived, this one included.
func (provider *fakeFront) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests = append(provider.requests, fakeRecordedRequest{
		name: name, method: request.Method, path: request.URL.RequestURI(), body: string(body), at: time.Now(),
	})
	return provider.countLocked(name)
}

func (provider *fakeFront) writeError(response http.ResponseWriter, status int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.writeErrorLocked(response, status)
}

func (provider *fakeFront) writeErrorLocked(response http.ResponseWriter, status int) {
	provider.writeValueLocked(response, status, map[string]any{"_error": map[string]any{"status": status, "title": "Error", "message": providerSentinel}})
}

func (provider *fakeFront) writeValueLocked(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(encoded)
	require.NoError(provider.t, err)
}

// waitForDelayedRequests waits until every slow or held handler applied its effect.
func (provider *fakeFront) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * slowResponseDelay):
		t.Fatal("a delayed fake Front request did not finish")
	}
}

func (provider *fakeFront) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.countLocked(name)
}

func (provider *fakeFront) countLocked(name string) int {
	count := 0
	for _, request := range provider.requests {
		if request.name == name {
			count++
		}
	}
	return count
}

func (provider *fakeFront) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *fakeFront) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for index := len(provider.requests) - 1; index >= 0; index-- {
		if provider.requests[index].name == name {
			return provider.requests[index]
		}
	}
	provider.t.Fatalf("no %s request was recorded", name)
	return fakeRecordedRequest{}
}

func (provider *fakeFront) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	times := []time.Time{}
	for _, request := range provider.requests {
		if request.name == name {
			times = append(times, request.at)
		}
	}
	return times
}

// conversation returns a copy of the conversation's current state.
func (provider *fakeFront) conversation(conversationID string) fakeConversation {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := *provider.conversations[conversationID]
	conversation.tagIDs = slices.Clone(conversation.tagIDs)
	conversation.comments = slices.Clone(conversation.comments)
	return conversation
}

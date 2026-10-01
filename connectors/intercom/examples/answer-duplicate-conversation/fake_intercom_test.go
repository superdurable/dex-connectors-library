// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	fakeAccessToken  = "intercomIntegrationAccessToken0123456789"
	fakeClientSecret = "SENTINEL-INTERCOM-CLIENT-SECRET"
	fakeAdminID      = "5017691"
	fakeWorkspaceID  = "ecahpwf5"
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so Dex dispatches a backup attempt.
	slowResponseDelay = 9 * time.Second
)

var fakeHTMLTagPattern = regexp.MustCompile(`<[^>]*>`)

// fakeIntercom is a stateful, credential-safe Intercom API fake. It requires the bearer token and
// Intercom-Version 2.16, evaluates the documented search filters, and records conversation parts.
type fakeIntercom struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping or held, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	nextID        int64
	contacts      []fakeContact
	conversations map[string]*fakeConversation
	counts        map[string]int
	requests      map[string][]fakeRecordedRequest

	delaysFirstReply              bool
	losesFirstReplyResponse       bool
	failsFirstReplyBeforeApplying bool
	rejectsReply                  bool
	holdsFirstReply               chan struct{}
	delaysFirstClose              bool
	rejectsRedundantClose         bool
	rateLimitsFirstSearch         bool
}

type fakeContact struct {
	id    string
	role  string
	email string
	name  string
}

type fakeConversation struct {
	id           string
	state        string
	snoozedUntil int64
	createdAt    int64
	updatedAt    int64
	author       fakeContact
	contactIDs   []string
	sourceBody   string
	parts        []fakePart
}

type fakePart struct {
	id         string
	partType   string
	htmlBody   string
	authorType string
	authorID   string
	createdAt  int64
}

type fakeRecordedRequest struct {
	at     time.Time
	path   string
	header http.Header
	body   string
}

func newFakeIntercom(t *testing.T) *fakeIntercom {
	t.Helper()
	// Time-based IDs keep conversation-derived Flow IDs unique across runs against one Dex server.
	provider := &fakeIntercom{
		t: t, nextID: time.Now().UnixMicro(), conversations: map[string]*fakeConversation{},
		counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeIntercom) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+fakeAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"type":"error.list","request_id":"fake-request","errors":[{"code":"unauthorized","message":"SENTINEL Access Token Invalid"}]}`)
		return
	}
	if request.Header.Get("Intercom-Version") != "2.16" {
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"intercom_version_invalid"}]}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"client_error"}]}`)
		return
	}
	segments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/admins":
		provider.record("admins", request, body)
		provider.writeValue(response, http.StatusOK, map[string]any{"type": "admin.list", "admins": []any{
			map[string]any{"type": "admin", "id": fakeAdminID, "name": "Ada Support", "email": "ada@acme.example.com"},
		}})
	case request.Method == http.MethodPost && request.URL.Path == "/contacts/search":
		provider.searchContacts(response, request, body)
	case request.Method == http.MethodPost && request.URL.Path == "/conversations/search":
		provider.searchConversations(response, request, body)
	case len(segments) == 2 && segments[0] == "conversations" && request.Method == http.MethodGet:
		provider.readConversation(response, request, body, segments[1])
	case len(segments) == 3 && segments[0] == "conversations" && segments[2] == "reply" && request.Method == http.MethodPost:
		provider.replyToConversation(response, request, body, segments[1])
	case len(segments) == 3 && segments[0] == "conversations" && segments[2] == "parts" && request.Method == http.MethodPost:
		provider.manageConversation(response, request, body, segments[1])
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"type":"error.list","errors":[{"code":"not_found"}]}`)
	}
}

func (provider *fakeIntercom) searchContacts(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("contactSearch", request, body)
	var search struct {
		Query struct {
			Field    string `json:"field"`
			Operator string `json:"operator"`
			Value    string `json:"value"`
		} `json:"query"`
		Pagination struct {
			PerPage int `json:"per_page"`
		} `json:"pagination"`
	}
	if json.Unmarshal(body, &search) != nil || search.Query.Field != "email" || search.Query.Operator != "=" || search.Pagination.PerPage < 1 {
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"parameter_invalid"}]}`)
		return
	}
	provider.mutex.Lock()
	contacts := []any{}
	for _, contact := range provider.contacts {
		if contact.email == search.Query.Value && len(contacts) < search.Pagination.PerPage {
			contacts = append(contacts, map[string]any{
				"type": "contact", "id": contact.id, "role": contact.role, "email": contact.email, "name": contact.name,
				"external_id": nil, "created_at": time.Now().Add(-48 * time.Hour).Unix(), "updated_at": time.Now().Add(-time.Hour).Unix(),
			})
		}
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{"type": "list", "data": contacts, "total_count": len(contacts), "pages": map[string]any{"type": "pages", "page": 1}})
}

func (provider *fakeIntercom) searchConversations(response http.ResponseWriter, request *http.Request, body []byte) {
	isFirst := provider.record("conversationSearch", request, body) == 1
	if provider.rateLimitsFirstSearch && isFirst {
		response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(2*time.Second).Unix(), 10))
		provider.writeJSON(response, http.StatusTooManyRequests, `{"type":"error.list","errors":[{"code":"rate_limit_exceeded","message":"SENTINEL slow down"}]}`)
		return
	}
	var search struct {
		Query      json.RawMessage `json:"query"`
		Pagination struct {
			PerPage int `json:"per_page"`
		} `json:"pagination"`
	}
	if json.Unmarshal(body, &search) != nil || search.Pagination.PerPage < 1 || search.Pagination.PerPage > 150 {
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"parameter_invalid"}]}`)
		return
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	matches := []any{}
	for _, conversation := range provider.sortedConversations() {
		isMatch, err := provider.matchesFilter(conversation, search.Query)
		if err != nil {
			provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"parameter_invalid"}]}`)
			return
		}
		if isMatch && len(matches) < search.Pagination.PerPage {
			matches = append(matches, provider.conversationJSON(conversation, false, false))
		}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"type": "conversation.list", "conversations": matches, "total_count": len(matches),
		"pages": map[string]any{"type": "pages", "page": 1, "per_page": search.Pagination.PerPage, "total_pages": 1},
	})
}

// matchesFilter evaluates the documented AND, OR, =, and > filters on the fields the connector sends; it requires provider.mutex.
func (provider *fakeIntercom) matchesFilter(conversation *fakeConversation, raw json.RawMessage) (bool, error) {
	var filter struct {
		Field    string          `json:"field"`
		Operator string          `json:"operator"`
		Value    json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &filter); err != nil {
		return false, err
	}
	if filter.Operator == "AND" || filter.Operator == "OR" {
		var children []json.RawMessage
		if err := json.Unmarshal(filter.Value, &children); err != nil || len(children) < 2 || len(children) > 15 {
			return false, fmt.Errorf("invalid group")
		}
		for _, child := range children {
			isMatch, err := provider.matchesFilter(conversation, child)
			if err != nil {
				return false, err
			}
			if isMatch && filter.Operator == "OR" {
				return true, nil
			}
			if !isMatch && filter.Operator == "AND" {
				return false, nil
			}
		}
		return filter.Operator == "AND", nil
	}
	var text string
	switch {
	case filter.Field == "updated_at" && filter.Operator == ">":
		var since int64
		if err := json.Unmarshal(filter.Value, &since); err != nil {
			return false, err
		}
		return conversation.updatedAt >= since, nil
	case filter.Operator != "=" || json.Unmarshal(filter.Value, &text) != nil:
		return false, fmt.Errorf("unsupported filter")
	case filter.Field == "state":
		return conversation.state == text, nil
	case filter.Field == "contact_ids":
		return slices.Contains(conversation.contactIDs, text), nil
	case filter.Field == "source.author.email":
		return conversation.author.email == text, nil
	default:
		return false, fmt.Errorf("unsupported field")
	}
}

func (provider *fakeIntercom) readConversation(response http.ResponseWriter, request *http.Request, body []byte, conversationID string) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation, found := provider.conversations[conversationID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"type":"error.list","errors":[{"code":"not_found","message":"SENTINEL Resource Not Found"}]}`)
		return
	}
	provider.writeValue(response, http.StatusOK, provider.conversationJSON(conversation, true, request.URL.Query().Get("display_as") == "plaintext"))
}

func (provider *fakeIntercom) replyToConversation(response http.ResponseWriter, request *http.Request, body []byte, conversationID string) {
	attempt := provider.record("reply", request, body)
	if provider.rejectsReply {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"type":"error.list","request_id":"fake-request","errors":[{"code":"parameter_invalid","field":"admin_id","message":"SENTINEL Admin not found"}]}`)
		return
	}
	if attempt == 1 && provider.failsFirstReplyBeforeApplying {
		provider.writeJSON(response, http.StatusServiceUnavailable, `{"type":"error.list","errors":[{"code":"server_error","message":"SENTINEL unavailable"}]}`)
		return
	}
	if attempt == 1 && provider.holdsFirstReply != nil {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstReply
	}
	if attempt == 1 && provider.delaysFirstReply {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var reply struct {
		MessageType string `json:"message_type"`
		Type        string `json:"type"`
		AdminID     string `json:"admin_id"`
		Body        string `json:"body"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &reply))
	if reply.Type != "admin" || reply.AdminID != fakeAdminID || (reply.MessageType != "comment" && reply.MessageType != "note") || reply.Body == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"parameter_invalid"}]}`)
		return
	}
	provider.mutex.Lock()
	conversation, found := provider.conversations[conversationID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"type":"error.list","errors":[{"code":"not_found"}]}`)
		return
	}
	provider.addPart(conversation, reply.MessageType, reply.Body, "admin", reply.AdminID)
	value := provider.conversationJSON(conversation, true, false)
	provider.mutex.Unlock()
	if attempt == 1 && provider.losesFirstReplyResponse {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeIntercom) manageConversation(response http.ResponseWriter, request *http.Request, body []byte, conversationID string) {
	attempt := provider.record("manage", request, body)
	if attempt == 1 && provider.delaysFirstClose {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var change struct {
		MessageType  string `json:"message_type"`
		AdminID      string `json:"admin_id"`
		SnoozedUntil int64  `json:"snoozed_until"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &change))
	if change.AdminID != fakeAdminID {
		provider.writeJSON(response, http.StatusNotFound, `{"type":"error.list","errors":[{"code":"admin_not_found"}]}`)
		return
	}
	provider.mutex.Lock()
	conversation, found := provider.conversations[conversationID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"type":"error.list","errors":[{"code":"not_found"}]}`)
		return
	}
	target := map[string]string{"close": "closed", "open": "open", "snoozed": "snoozed"}[change.MessageType]
	if target == "" {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"parameter_invalid"}]}`)
		return
	}
	if target == conversation.state && provider.rejectsRedundantClose {
		provider.counts["redundantManage"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"conflict","message":"SENTINEL already in that state"}]}`)
		return
	}
	conversation.state, conversation.snoozedUntil = target, change.SnoozedUntil
	provider.addPart(conversation, change.MessageType, "", "admin", change.AdminID)
	provider.counts["appliedManage"]++
	value := provider.conversationJSON(conversation, true, false)
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, value)
}

// addPart requires provider.mutex.
func (provider *fakeIntercom) addPart(conversation *fakeConversation, partType string, htmlBody string, authorType string, authorID string) {
	provider.nextID++
	now := time.Now().Unix()
	conversation.parts = append(conversation.parts, fakePart{
		id: strconv.FormatInt(provider.nextID, 10), partType: partType, htmlBody: htmlBody, authorType: authorType, authorID: authorID, createdAt: now,
	})
	conversation.updatedAt = now
}

func (provider *fakeIntercom) seedContact(email string, role string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	id := fmt.Sprintf("5ba682d23d7cf92bef%06d", provider.nextID%1000000)
	provider.contacts = append(provider.contacts, fakeContact{id: id, role: role, email: email, name: "Jane Smith"})
	return id
}

// seedConversation stores a conversation a contact started createdAgo before now.
func (provider *fakeIntercom) seedConversation(contactID string, state string, createdAgo time.Duration, message string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	var author fakeContact
	for _, contact := range provider.contacts {
		if contact.id == contactID {
			author = contact
		}
	}
	createdAt := time.Now().Add(-createdAgo).Unix()
	conversation := &fakeConversation{
		id: strconv.FormatInt(provider.nextID, 10), state: state, createdAt: createdAt, updatedAt: createdAt,
		author: author, contactIDs: []string{contactID}, sourceBody: "<p>" + html.EscapeString(message) + "</p>",
	}
	provider.conversations[conversation.id] = conversation
	return conversation.id
}

func (provider *fakeIntercom) addAdminComment(conversationID string, body string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.addPart(provider.conversations[conversationID], "comment", "<p>"+html.EscapeString(body)+"</p>", "admin", fakeAdminID)
}

// conversationJSON requires provider.mutex.
func (provider *fakeIntercom) conversationJSON(conversation *fakeConversation, includeParts bool, isPlainText bool) map[string]any {
	render := func(body string) string {
		if !isPlainText {
			return body
		}
		text := strings.ReplaceAll(strings.ReplaceAll(body, "<br>", "\n"), "</p><p>", "\n\n")
		return html.UnescapeString(fakeHTMLTagPattern.ReplaceAllString(text, ""))
	}
	var snoozedUntil any
	if conversation.state == "snoozed" {
		snoozedUntil = conversation.snoozedUntil
	}
	contacts := []any{}
	for _, contactID := range conversation.contactIDs {
		contacts = append(contacts, map[string]any{"type": "contact", "id": contactID, "external_id": nil})
	}
	value := map[string]any{
		"type": "conversation", "id": conversation.id, "title": nil, "created_at": conversation.createdAt, "updated_at": conversation.updatedAt,
		"waiting_since": conversation.createdAt, "snoozed_until": snoozedUntil, "open": conversation.state != "closed",
		"state": conversation.state, "read": false, "priority": "none", "admin_assignee_id": 0, "team_assignee_id": 0,
		"source": map[string]any{
			"type": "conversation", "id": "source-" + conversation.id, "delivered_as": "customer_initiated", "subject": "",
			"body":   render(conversation.sourceBody),
			"author": map[string]any{"type": conversation.author.role, "id": conversation.author.id, "name": conversation.author.name, "email": conversation.author.email},
		},
		"contacts":   map[string]any{"type": "contact.list", "contacts": contacts},
		"tags":       map[string]any{"type": "tag.list", "tags": []any{}},
		"statistics": map[string]any{"type": "conversation_statistics", "count_conversation_parts": len(conversation.parts)},
	}
	if includeParts {
		parts := []any{}
		for _, part := range conversation.parts {
			parts = append(parts, map[string]any{
				"type": "conversation_part", "id": part.id, "part_type": part.partType, "body": render(part.htmlBody),
				"created_at": part.createdAt, "updated_at": part.createdAt, "notified_at": part.createdAt,
				"author":           map[string]any{"type": part.authorType, "id": part.authorID, "name": "Ada Support", "email": "ada@acme.example.com"},
				"app_package_code": "dex-intercom-test", "redacted": false,
			})
		}
		value["conversation_parts"] = map[string]any{"type": "conversation_part.list", "conversation_parts": parts, "total_count": len(parts)}
	}
	return value
}

// sortedConversations requires provider.mutex and orders conversations by creation.
func (provider *fakeIntercom) sortedConversations() []*fakeConversation {
	conversations := make([]*fakeConversation, 0, len(provider.conversations))
	for _, conversation := range provider.conversations {
		conversations = append(conversations, conversation)
	}
	slices.SortFunc(conversations, func(left, right *fakeConversation) int { return strings.Compare(left.id, right.id) })
	return conversations
}

// dropConnection closes the connection after Intercom applied the request, as a lost response would.
func (provider *fakeIntercom) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeIntercom) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{
		at: time.Now(), path: request.URL.RequestURI(), header: request.Header.Clone(), body: string(body),
	})
	return provider.counts[name]
}

func (provider *fakeIntercom) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeIntercom) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Intercom response write failed: %v", err)
	}
}

func (provider *fakeIntercom) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * slowResponseDelay):
		t.Fatal("a delayed fake Intercom request did not finish")
	}
}

func (provider *fakeIntercom) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeIntercom) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeIntercom) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeIntercom) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

// conversation returns a copy of a stored conversation for assertions.
func (provider *fakeIntercom) conversation(conversationID string) fakeConversation {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := *provider.conversations[conversationID]
	conversation.parts = slices.Clone(conversation.parts)
	return conversation
}

func (conversation fakeConversation) partsOfType(partType string) []fakePart {
	var parts []fakePart
	for _, part := range conversation.parts {
		if part.partType == partType {
			parts = append(parts, part)
		}
	}
	return parts
}

// signedNotification returns a conversation webhook notification and its X-Hub-Signature under secret.
func (provider *fakeIntercom) signedNotification(notificationID string, topic string, conversationID string, secret string) ([]byte, string) {
	provider.mutex.Lock()
	item := provider.conversationJSON(provider.conversations[conversationID], true, true)
	provider.mutex.Unlock()
	body, err := json.Marshal(map[string]any{
		"type": "notification_event", "app_id": fakeWorkspaceID, "id": notificationID, "topic": topic,
		"data":  map[string]any{"type": "notification_event_data", "item": item},
		"links": map[string]any{}, "delivery_status": "pending", "delivery_attempts": 1, "delivered_at": 0,
		"first_sent_at": time.Now().Unix(), "created_at": time.Now().Unix(), "self": nil,
	})
	require.NoError(provider.t, err)
	return body, hubSignature(body, secret)
}

func hubSignature(body []byte, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body) // A hash write never fails.
	return "sha1=" + hex.EncodeToString(mac.Sum(nil))
}

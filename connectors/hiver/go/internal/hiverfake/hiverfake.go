// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hiverfake serves the Hiver API v1 inbox and conversation lists that the conversationReceived
// Trigger polls: bearer authentication, limit and opaque next_page tokens, newest-first or oldest-first
// order, page tokens that expire with a 400, and scripted 429 answers with Retry-After. It also serves one
// conversation read, with one message ID, for the example Flow's getConversation Step.
package hiverfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIKey is the bearer token every request must carry.
const APIKey = "hiver-fake-api-key"

// Server is a Hiver API stand-in; its URL is the WithAPIBaseURL value.
type Server struct {
	*httptest.Server
	mutex          sync.Mutex
	inboxes        map[string]*inbox
	isOldestFirst  bool
	tokenEpoch     int
	rateLimits     []time.Duration
	listRequests   int
	pageTokenUsage []string
}

type inbox struct {
	email         string
	conversations []string
}

// New starts a fake with one shared inbox; Close it with the returned Server's Close.
func New(inboxID string, email string) *Server {
	server := &Server{inboxes: map[string]*inbox{inboxID: {email: email}}}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serve))
	return server
}

// AddInbox adds another shared inbox with no conversations.
func (server *Server) AddInbox(inboxID string, email string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.inboxes[inboxID] = &inbox{email: email}
}

// AddConversations appends conversations, which newest-first order lists before older ones.
func (server *Server) AddConversations(inboxID string, conversationIDs ...string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.inboxes[inboxID].conversations = append(server.inboxes[inboxID].conversations, conversationIDs...)
}

// DeleteConversation removes a conversation from the list.
func (server *Server) DeleteConversation(inboxID string, conversationID string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	target := server.inboxes[inboxID]
	target.conversations = slices.DeleteFunc(target.conversations, func(id string) bool { return id == conversationID })
}

// ListOldestFirst makes later lists return the oldest conversation first, so new ones land on the last page.
func (server *Server) ListOldestFirst() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.isOldestFirst = true
}

// ExpirePageTokens makes every next_page token issued so far answer 400.
func (server *Server) ExpirePageTokens() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.tokenEpoch++
}

// RateLimitNext answers the next list request with 429 and, when retryAfter is positive, Retry-After seconds.
func (server *Server) RateLimitNext(retryAfter time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.rateLimits = append(server.rateLimits, retryAfter)
}

// ConversationListRequests counts conversation-list requests, including rejected ones.
func (server *Server) ConversationListRequests() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.listRequests
}

// PageTokensUsed returns the next_page value of every conversation-list request, "" for a first page.
func (server *Server) PageTokensUsed() []string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return slices.Clone(server.pageTokenUsage)
}

func (server *Server) serve(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+APIKey {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"errors": []any{map[string]any{"message": "unauthorized"}}})
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "inboxes":
		server.serveInboxes(writer)
	case len(parts) == 3 && parts[0] == "inboxes" && parts[2] == "conversations":
		server.serveConversations(writer, request, parts[1])
	case len(parts) == 4 && parts[0] == "inboxes" && parts[2] == "conversations":
		server.serveConversation(writer, parts[1], parts[3])
	default:
		writeJSON(writer, http.StatusNotFound, map[string]any{"Message": "not found"})
	}
}

func (server *Server) serveInboxes(writer http.ResponseWriter) {
	results := []any{}
	for id, target := range server.inboxes {
		results = append(results, map[string]any{"id": id, "display_name": "Support", "email": target.email, "channel_type": "email", "is_authorised": true})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": map[string]any{"results": results, "pagination": map[string]any{"next_page": nil}}})
}

func (server *Server) serveConversations(writer http.ResponseWriter, request *http.Request, inboxID string) {
	server.listRequests++
	pageToken := request.URL.Query().Get("next_page")
	server.pageTokenUsage = append(server.pageTokenUsage, pageToken)
	if len(server.rateLimits) != 0 {
		retryAfter := server.rateLimits[0]
		server.rateLimits = server.rateLimits[1:]
		if retryAfter > 0 {
			writer.Header().Set("Retry-After", strconv.Itoa(int(retryAfter/time.Second)))
		}
		writeJSON(writer, http.StatusTooManyRequests, map[string]any{"Message": "Too Many Requests"})
		return
	}
	target, isKnown := server.inboxes[inboxID]
	if !isKnown {
		writeJSON(writer, http.StatusNotFound, map[string]any{"Message": "inbox not found"})
		return
	}
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil || limit < 10 || limit > 100 {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"Message": "limit must be 10 to 100"})
		return
	}
	offset := 0
	if pageToken != "" {
		var epoch int
		if _, err := fmt.Sscanf(pageToken, "e%d.o%d", &epoch, &offset); err != nil || epoch != server.tokenEpoch || offset < 0 {
			writeJSON(writer, http.StatusBadRequest, map[string]any{"errors": []any{map[string]any{"message": "invalid page token"}}})
			return
		}
	}
	ordered := slices.Clone(target.conversations)
	if !server.isOldestFirst {
		slices.Reverse(ordered)
	}
	end := min(offset+limit, len(ordered))
	results := []any{}
	for _, id := range ordered[min(offset, len(ordered)):end] {
		results = append(results, conversationJSON(id))
	}
	var nextPage any
	if end < len(ordered) {
		nextPage = fmt.Sprintf("e%d.o%d", server.tokenEpoch, end)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": map[string]any{"results": results, "pagination": map[string]any{"next_page": nextPage}}})
}

func (server *Server) serveConversation(writer http.ResponseWriter, inboxID string, conversationID string) {
	target, isKnown := server.inboxes[inboxID]
	if !isKnown || !slices.Contains(target.conversations, conversationID) {
		writeJSON(writer, http.StatusNotFound, map[string]any{"Message": "conversation not found"})
		return
	}
	conversation := conversationJSON(conversationID)
	conversation["message_ids"] = []any{map[string]any{"hiver_message_id": "8" + conversationID, "gmail_message_id": "19cfm" + conversationID}}
	writeJSON(writer, http.StatusOK, map[string]any{"data": []any{conversation}})
}

func conversationJSON(conversationID string) map[string]any {
	return map[string]any{
		"id": conversationID, "status": "open", "tag_ids": []any{}, "gmail_thread_id": "19cf" + conversationID,
		"private_permalink": "https://app.hiverhq.com/conversation/" + conversationID,
	}
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	// The client reads the status first; a failed write surfaces there as a read error.
	_ = json.NewEncoder(writer).Encode(body)
}

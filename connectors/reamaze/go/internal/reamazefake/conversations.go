// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package reamazefake serves the Re:amaze conversation list and read API that the conversationCreated
// Trigger and getConversation use, over HTTP Basic, for tests and runnable examples.
package reamazefake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// PageSize is the number of conversations Re:amaze lists per page.
const PageSize = 30

// Conversation is one stored conversation.
type Conversation struct {
	Slug      string
	Subject   string
	Channel   string
	Status    int
	CreatedAt time.Time
	Body      string
}

// Provider is a Re:amaze stand-in. It is safe for concurrent use.
type Provider struct {
	// URL is the API base URL to pass to reamaze.WithAPIBaseURL, ending in /api/v1.
	URL string

	email, token string
	mutex        sync.Mutex
	stored       []Conversation
	// hidden marks stored conversations not listed yet; Hide and Reveal model a conversation listed late.
	hidden         map[string]bool
	isAscending    bool
	rateLimits     []string
	listRequests   int
	listedPages    []int
	listedFilters  []string
	listedChannels []string
}

// New starts a provider that accepts email and token as HTTP Basic credentials.
func New(t testing.TB, email string, token string) *Provider {
	t.Helper()
	provider := &Provider{email: email, token: token, hidden: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(server.Close)
	provider.URL = server.URL + "/api/v1"
	return provider
}

// Insert stores a conversation; a zero CreatedAt is set to now, truncated to milliseconds.
func (provider *Provider) Insert(conversation Conversation) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if conversation.CreatedAt.IsZero() {
		conversation.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	}
	if conversation.Body == "" {
		conversation.Body = "Opening message of " + conversation.Subject
	}
	provider.stored = append(provider.stored, conversation)
}

// Hide keeps a stored conversation out of lists until Reveal, as Re:amaze's delayed listing does.
func (provider *Provider) Hide(slug string) { provider.setHidden(slug, true) }

// Reveal lists a hidden conversation.
func (provider *Provider) Reveal(slug string) { provider.setHidden(slug, false) }

func (provider *Provider) setHidden(slug string, isHidden bool) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.hidden[slug] = isHidden
}

// ListOldestFirst switches the list order to ascending creation time.
func (provider *Provider) ListOldestFirst() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.isAscending = true
}

// RateLimitNextList answers the next list request with HTTP 429 and retryAfter, which may be empty.
func (provider *Provider) RateLimitNextList(retryAfter string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.rateLimits = append(provider.rateLimits, retryAfter)
}

// ListRequests returns the number of list requests, including rate-limited ones.
func (provider *Provider) ListRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.listRequests
}

// ListedPages returns the page parameter of every answered list request, in order.
func (provider *Provider) ListedPages() []int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]int(nil), provider.listedPages...)
}

// ListedQueries returns the filter and category parameters of every answered list request.
func (provider *Provider) ListedQueries() (filters []string, channels []string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.listedFilters...), append([]string(nil), provider.listedChannels...)
}

func (provider *Provider) serve(writer http.ResponseWriter, request *http.Request) {
	email, token, ok := request.BasicAuth()
	if !ok || email != provider.email || token != provider.token {
		http.Error(writer, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1")
	switch {
	case request.Method != http.MethodGet:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	case path == "/conversations":
		provider.serveList(writer, request)
	case strings.HasPrefix(path, "/conversations/") && strings.HasSuffix(path, "/messages"):
		provider.serveMessages(writer, strings.TrimSuffix(strings.TrimPrefix(path, "/conversations/"), "/messages"))
	case strings.HasPrefix(path, "/conversations/"):
		provider.serveConversation(writer, strings.TrimPrefix(path, "/conversations/"))
	default:
		http.NotFound(writer, request)
	}
}

// serveList lists by creation time, newest first unless ListOldestFirst; filter all includes archived (4).
func (provider *Provider) serveList(writer http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.listRequests++
	if len(provider.rateLimits) != 0 {
		retryAfter := provider.rateLimits[0]
		provider.rateLimits = provider.rateLimits[1:]
		if retryAfter != "" {
			writer.Header().Set("Retry-After", retryAfter)
		}
		http.Error(writer, `{"error":"rate limited"}`, http.StatusTooManyRequests)
		return
	}
	query := request.URL.Query()
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	provider.listedPages = append(provider.listedPages, page)
	provider.listedFilters = append(provider.listedFilters, query.Get("filter"))
	provider.listedChannels = append(provider.listedChannels, query.Get("category"))
	var matching []Conversation
	for _, conversation := range provider.stored {
		isArchivedHidden := conversation.Status == 4 && query.Get("filter") != "all"
		isOtherChannel := query.Get("category") != "" && conversation.Channel != query.Get("category")
		if !provider.hidden[conversation.Slug] && !isArchivedHidden && !isOtherChannel {
			matching = append(matching, conversation)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool {
		if provider.isAscending {
			return matching[i].CreatedAt.Before(matching[j].CreatedAt)
		}
		return matching[i].CreatedAt.After(matching[j].CreatedAt)
	})
	pageCount := (len(matching) + PageSize - 1) / PageSize
	start := min((page-1)*PageSize, len(matching))
	end := min(start+PageSize, len(matching))
	entries := make([]map[string]any, 0, end-start)
	for _, conversation := range matching[start:end] {
		entries = append(entries, conversationJSON(conversation))
	}
	writeJSON(writer, map[string]any{"page_size": PageSize, "page_count": pageCount, "total_count": len(matching), "conversations": entries})
}

func (provider *Provider) serveConversation(writer http.ResponseWriter, slug string) {
	conversation, ok := provider.find(slug)
	if !ok {
		http.Error(writer, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(writer, conversationJSON(conversation))
}

func (provider *Provider) serveMessages(writer http.ResponseWriter, slug string) {
	conversation, ok := provider.find(slug)
	if !ok {
		http.Error(writer, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(writer, map[string]any{"page_size": PageSize, "page_count": 1, "total_count": 1, "messages": []map[string]any{{
		"body": conversation.Body, "visibility": 0, "origin": 1, "created_at": conversation.CreatedAt.Format(time.RFC3339Nano),
		"user": map[string]any{"name": "Jane Smith", "email": "jane@acme.example.com"},
	}}})
}

func (provider *Provider) find(slug string) (Conversation, bool) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, conversation := range provider.stored {
		if conversation.Slug == slug {
			return conversation, true
		}
	}
	return Conversation{}, false
}

func conversationJSON(conversation Conversation) map[string]any {
	channel := conversation.Channel
	if channel == "" {
		channel = "support"
	}
	return map[string]any{
		"slug": conversation.Slug, "subject": conversation.Subject, "status": conversation.Status,
		"created_at": conversation.CreatedAt.Format("2006-01-02T15:04:05.000Z07:00"), "tag_list": []string{},
		"message":  map[string]any{"body": conversation.Body},
		"author":   map[string]any{"name": "Jane Smith", "email": "jane@acme.example.com"},
		"category": map[string]any{"name": strings.ToUpper(channel[:1]) + channel[1:], "slug": channel, "channel": 1},
	}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	// An encoding failure means the client went away; the test sees the request fail.
	_ = json.NewEncoder(writer).Encode(value)
}

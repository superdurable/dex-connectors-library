// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package testconnectors provides connectors and a fake provider for tests of
// the server. Production code never imports it.
package testconnectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProviderToken is the bearer token the fake provider accepts.
const ProviderToken = "test-provider-token"

// ScriptedResponse overrides the fake provider's answer to one request.
type ScriptedResponse struct {
	// Status is the HTTP status to answer; zero keeps the normal answer.
	Status int
	// RetryAfter is sent as the Retry-After header in whole seconds.
	RetryAfter time.Duration
	// IsHeld counts the request, then holds it until the client leaves or Release.
	IsHeld bool
	// IsHeldAfterEffect applies the write, then holds: the answer may be lost.
	IsHeldAfterEffect bool
	// IsEffectAppliedFirst applies the write, then answers Status anyway.
	IsEffectAppliedFirst bool
}

// FakeProvider counts requests; notes are not idempotent, and records
// deduplicate by Idempotency-Key.
type FakeProvider struct {
	server *httptest.Server

	mu                sync.Mutex
	noteRequests      int
	createdNotes      map[string]string
	recordRequests    int
	recordsByKey      map[string]string
	appliedRecordKeys map[string]int
	readRequests      int
	scripts           map[string][]ScriptedResponse
	heldRequests      chan string
	release           chan struct{}
}

// NewFakeProvider starts a fake provider on a loopback port.
func NewFakeProvider() *FakeProvider {
	provider := &FakeProvider{
		createdNotes:      map[string]string{},
		recordsByKey:      map[string]string{},
		appliedRecordKeys: map[string]int{},
		scripts:           map[string][]ScriptedResponse{},
		heldRequests:      make(chan string, 16),
		release:           make(chan struct{}),
	}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

// URL is the provider's base URL.
func (provider *FakeProvider) URL() string { return provider.server.URL }

// Close stops the provider and releases held requests.
func (provider *FakeProvider) Close() {
	provider.Release()
	provider.server.CloseClientConnections()
	provider.server.Close()
}

// Script queues responses for the next requests of one operation: getNote,
// postNote, saveRecord, or readRecord.
func (provider *FakeProvider) Script(operationID string, responses ...ScriptedResponse) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.scripts[operationID] = append(provider.scripts[operationID], responses...)
}

// HeldRequests reports the operation ID of each request that arrives held.
func (provider *FakeProvider) HeldRequests() <-chan string { return provider.heldRequests }

// Release lets every held request finish with its normal answer.
func (provider *FakeProvider) Release() {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	select {
	case <-provider.release:
	default:
		close(provider.release)
	}
}

// NoteRequests counts the postNote requests that reached the provider.
func (provider *FakeProvider) NoteRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.noteRequests
}

// CreatedNotes counts the notes the provider created.
func (provider *FakeProvider) CreatedNotes() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return len(provider.createdNotes)
}

// RecordRequests counts the saveRecord requests that reached the provider.
func (provider *FakeProvider) RecordRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.recordRequests
}

// AppliedRecordWrites returns how often the record write of an idempotency
// key took effect; deduplication keeps it at most one.
func (provider *FakeProvider) AppliedRecordWrites(idempotencyKey string) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.appliedRecordKeys[idempotencyKey]
}

// MaximumAppliedRecordWrites returns the highest applied count of any key.
func (provider *FakeProvider) MaximumAppliedRecordWrites() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	maximum := 0
	for _, count := range provider.appliedRecordKeys {
		maximum = max(maximum, count)
	}
	return maximum
}

// ReadRequests counts getNote requests.
func (provider *FakeProvider) ReadRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.readRequests
}

func (provider *FakeProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+ProviderToken {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/notes/"):
		provider.serveGetNote(response, request, strings.TrimPrefix(request.URL.Path, "/notes/"))
	case request.Method == http.MethodPost && request.URL.Path == "/notes":
		provider.servePostNote(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/records/"):
		provider.serveReadRecord(response, request, strings.TrimPrefix(request.URL.Path, "/records/"))
	case request.Method == http.MethodPut && request.URL.Path == "/records":
		provider.serveSaveRecord(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (provider *FakeProvider) serveGetNote(response http.ResponseWriter, request *http.Request, noteID string) {
	provider.mu.Lock()
	provider.readRequests++
	provider.mu.Unlock()
	if provider.answerScripted(response, request, "getNote") {
		return
	}
	provider.mu.Lock()
	text, isFound := provider.createdNotes[noteID]
	provider.mu.Unlock()
	if !isFound && noteID != "welcome" {
		http.Error(response, "no such note", http.StatusNotFound)
		return
	}
	if noteID == "welcome" {
		text = "Welcome <to> the notes & more"
	}
	writeJSON(response, map[string]string{"id": noteID, "text": text})
}

func (provider *FakeProvider) servePostNote(response http.ResponseWriter, request *http.Request) {
	provider.mu.Lock()
	provider.noteRequests++
	provider.mu.Unlock()
	if provider.answerScripted(response, request, "postNote") {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(request.Body).Decode(&body) != nil {
		http.Error(response, "bad body", http.StatusBadRequest)
		return
	}
	provider.mu.Lock()
	noteID := "n-" + strconv.Itoa(len(provider.createdNotes)+1)
	provider.createdNotes[noteID] = body.Text
	provider.mu.Unlock()
	if provider.answerAfterEffect(response, request, "postNote") {
		return
	}
	writeJSON(response, map[string]string{"id": noteID})
}

func (provider *FakeProvider) serveReadRecord(response http.ResponseWriter, request *http.Request, idempotencyKey string) {
	if provider.answerScripted(response, request, "readRecord") {
		return
	}
	provider.mu.Lock()
	value, isFound := provider.recordsByKey[idempotencyKey]
	provider.mu.Unlock()
	if !isFound {
		http.Error(response, "no such record", http.StatusNotFound)
		return
	}
	writeJSON(response, map[string]string{"value": value})
}

func (provider *FakeProvider) serveSaveRecord(response http.ResponseWriter, request *http.Request) {
	provider.mu.Lock()
	provider.recordRequests++
	provider.mu.Unlock()
	if provider.answerScripted(response, request, "saveRecord") {
		return
	}
	idempotencyKey := request.Header.Get("Idempotency-Key")
	var body struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if idempotencyKey == "" || json.NewDecoder(request.Body).Decode(&body) != nil {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}
	if body.Name == "forbidden" {
		http.Error(response, "this record name is refused", http.StatusBadRequest)
		return
	}
	provider.mu.Lock()
	if _, isKnown := provider.recordsByKey[idempotencyKey]; !isKnown {
		provider.recordsByKey[idempotencyKey] = body.Value
		provider.appliedRecordKeys[idempotencyKey]++
	}
	value := provider.recordsByKey[idempotencyKey]
	provider.mu.Unlock()
	if provider.answerAfterEffect(response, request, "saveRecord") {
		return
	}
	writeJSON(response, map[string]string{"value": value})
}

// answerScripted answers the next scripted response and reports whether it did.
func (provider *FakeProvider) answerScripted(response http.ResponseWriter, request *http.Request, operationID string) bool {
	provider.mu.Lock()
	queue := provider.scripts[operationID]
	if len(queue) == 0 {
		provider.mu.Unlock()
		return false
	}
	scripted := queue[0]
	if scripted.IsHeldAfterEffect || scripted.IsEffectAppliedFirst {
		provider.mu.Unlock()
		return false
	}
	provider.scripts[operationID] = queue[1:]
	release := provider.release
	provider.mu.Unlock()
	if scripted.IsHeld {
		provider.heldRequests <- operationID
		select {
		case <-request.Context().Done():
			return true
		case <-release:
		}
	}
	if scripted.Status == 0 {
		return false
	}
	if scripted.RetryAfter > 0 {
		response.Header().Set("Retry-After", strconv.Itoa(int(scripted.RetryAfter/time.Second)))
	}
	http.Error(response, fmt.Sprintf("scripted status %d", scripted.Status), scripted.Status)
	return true
}

// answerAfterEffect consumes an after-effect script entry, holding or
// answering its status, and reports whether it answered.
func (provider *FakeProvider) answerAfterEffect(response http.ResponseWriter, request *http.Request, operationID string) bool {
	provider.mu.Lock()
	queue := provider.scripts[operationID]
	if len(queue) == 0 || (!queue[0].IsHeldAfterEffect && !queue[0].IsEffectAppliedFirst) {
		provider.mu.Unlock()
		return false
	}
	scripted := queue[0]
	provider.scripts[operationID] = queue[1:]
	release := provider.release
	provider.mu.Unlock()
	if scripted.IsEffectAppliedFirst {
		http.Error(response, fmt.Sprintf("scripted status %d after the effect", scripted.Status), scripted.Status)
		return true
	}
	provider.heldRequests <- operationID
	select {
	case <-request.Context().Done():
		return true
	case <-release:
		return false
	}
}

func writeJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		http.Error(response, "encode", http.StatusInternalServerError)
	}
}

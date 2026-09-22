// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mockprovider supplies deterministic provider behavior for connector tests.
package mockprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type Action struct {
	CallID     string `json:"callId"`
	CustomerID string `json:"customerId"`
	Credits    int    `json:"credits"`
	Status     string `json:"status"`
}

type Provider struct {
	server  *httptest.Server
	mu      sync.Mutex
	actions map[string]Action
}

func Start() *Provider {
	provider := &Provider{actions: map[string]Action{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

func (provider *Provider) URL() string { return provider.server.URL }

func (provider *Provider) Close() { provider.server.Close() }

func (provider *Provider) Action(callID string) (Action, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	action, ok := provider.actions[callID]
	return action, ok
}

func (provider *Provider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Request-Id", "mock-request-1")
	response.Header().Set("Set-Cookie", "session=provider-secret")
	if request.Header.Get("X-Mock-Api-Key") != "test-key" {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/profiles/"):
		customerID := strings.TrimPrefix(request.URL.Path, "/profiles/")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"customerId": customerID,
			"name":       "Ada Lovelace",
			"headline":   "AI platform builder",
		})
	case request.Method == http.MethodPost && (request.URL.Path == "/credits" || request.URL.Path == "/credits-unknown"):
		callID := request.Header.Get("Idempotency-Key")
		var input Action
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		provider.mu.Lock()
		action, exists := provider.actions[callID]
		if !exists {
			action = Action{CallID: callID, CustomerID: input.CustomerID, Credits: input.Credits, Status: "succeeded"}
			provider.actions[callID] = action
		}
		provider.mu.Unlock()
		if request.URL.Path == "/credits-unknown" {
			hijacker, ok := response.(http.Hijacker)
			if !ok {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			connection, _, err := hijacker.Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		response.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(response).Encode(action)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/actions/"):
		callID := strings.TrimPrefix(request.URL.Path, "/actions/")
		action, ok := provider.Action(callID)
		if !ok {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(action)
	case request.Method == http.MethodGet && request.URL.Path == "/rate-limit":
		response.Header().Set("Retry-After", "3")
		response.WriteHeader(http.StatusTooManyRequests)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

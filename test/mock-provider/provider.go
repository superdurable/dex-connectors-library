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

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type Mutation struct {
	CallID     string `json:"callId"`
	CustomerID string `json:"customerId"`
	Credits    int    `json:"credits"`
	Status     string `json:"status"`
}

type Provider struct {
	server                 *httptest.Server
	mu                     sync.Mutex
	mutations              map[string]Mutation
	mutationExecutions     map[string]int
	mutationAttempts       map[string]int
	profileRequests        int
	profileFailuresLeft    int
	mutationRateLimitsLeft int
	recoveryFailuresLeft   int
	recoveryRequests       int
}

type Options struct {
	ProfileFailures    int
	MutationRateLimits int
	RecoveryFailures   int
}

func Start() *Provider {
	return StartWithOptions(Options{})
}

func StartWithOptions(options Options) *Provider {
	provider := &Provider{
		mutations: map[string]Mutation{}, mutationExecutions: map[string]int{}, mutationAttempts: map[string]int{},
		profileFailuresLeft: options.ProfileFailures, mutationRateLimitsLeft: options.MutationRateLimits,
		recoveryFailuresLeft: options.RecoveryFailures,
	}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

func (provider *Provider) URL() string { return provider.server.URL }

func (provider *Provider) Close() { provider.server.Close() }

func (provider *Provider) Mutation(callID string) (Mutation, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	mutation, ok := provider.mutations[callID]
	return mutation, ok
}

func (provider *Provider) MutationCount(callID connector.CallID) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.mutationExecutions[string(callID)]
}

func (provider *Provider) MutationAttempts(callID connector.CallID) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.mutationAttempts[string(callID)]
}

func (provider *Provider) ProfileRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.profileRequests
}

func (provider *Provider) RecoveryRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.recoveryRequests
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
		provider.mu.Lock()
		provider.profileRequests++
		shouldFail := provider.profileFailuresLeft > 0
		if shouldFail {
			provider.profileFailuresLeft--
		}
		provider.mu.Unlock()
		if shouldFail {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		customerID := strings.TrimPrefix(request.URL.Path, "/profiles/")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"customerId": customerID,
			"name":       "Ada Lovelace",
			"headline":   "AI platform builder",
		})
	case request.Method == http.MethodPost && (request.URL.Path == "/credits" || request.URL.Path == "/credits-unknown"):
		callID := request.Header.Get("Idempotency-Key")
		provider.mu.Lock()
		provider.mutationAttempts[callID]++
		shouldRateLimit := provider.mutationRateLimitsLeft > 0
		if shouldRateLimit {
			provider.mutationRateLimitsLeft--
		}
		provider.mu.Unlock()
		if shouldRateLimit {
			response.Header().Set("Retry-After", "1")
			response.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var input Mutation
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		provider.mu.Lock()
		mutation, exists := provider.mutations[callID]
		if !exists {
			mutation = Mutation{CallID: callID, CustomerID: input.CustomerID, Credits: input.Credits, Status: "succeeded"}
			provider.mutations[callID] = mutation
			provider.mutationExecutions[callID]++
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
		_ = json.NewEncoder(response).Encode(mutation)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/mutations/"):
		provider.mu.Lock()
		provider.recoveryRequests++
		shouldFail := provider.recoveryFailuresLeft > 0
		if shouldFail {
			provider.recoveryFailuresLeft--
		}
		provider.mu.Unlock()
		if shouldFail {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		callID := strings.TrimPrefix(request.URL.Path, "/mutations/")
		mutation, ok := provider.Mutation(callID)
		if !ok {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(mutation)
	case request.Method == http.MethodGet && request.URL.Path == "/rate-limit":
		response.Header().Set("Retry-After", "3")
		response.WriteHeader(http.StatusTooManyRequests)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

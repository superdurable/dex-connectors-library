// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"
)

// changeLogProvider stands in for a provider API with GET /latest and GET /changes?after=&limit=.
type changeLogProvider struct {
	mutex   sync.Mutex
	changes []providerChange
	// nextStatus, when set, answers the next /changes request with that status and Retry-After value.
	nextStatus     int
	nextRetryAfter string
}

type providerChange struct {
	ItemID    string    `json:"itemId"`
	Sequence  int64     `json:"sequence"`
	Title     string    `json:"title"`
	ChangedAt time.Time `json:"changedAt"`
}

func (provider *changeLogProvider) addChange(itemID, title string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.changes = append(provider.changes, providerChange{
		ItemID: itemID, Sequence: int64(len(provider.changes) + 1), Title: title, ChangedAt: time.Now().UTC(),
	})
}

// ServeHTTP answers the fake provider API.
func (provider *changeLogProvider) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if request.URL.Path == "/changes" && provider.nextStatus != 0 {
		writer.Header().Set("Retry-After", provider.nextRetryAfter)
		writer.WriteHeader(provider.nextStatus)
		provider.nextStatus, provider.nextRetryAfter = 0, ""
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	switch request.URL.Path {
	case "/latest":
		// The example reads every response; an encoding failure surfaces as a decode error there.
		_ = json.NewEncoder(writer).Encode(map[string]int64{"latest": int64(len(provider.changes))})
	case "/changes":
		after, err := strconv.Atoi(request.URL.Query().Get("after"))
		limit, limitErr := strconv.Atoi(request.URL.Query().Get("limit"))
		if err != nil || limitErr != nil || after < 0 || after > len(provider.changes) {
			writer.WriteHeader(http.StatusGone)
			return
		}
		end := min(after+limit, len(provider.changes))
		_ = json.NewEncoder(writer).Encode(changeListing{Changes: provider.changes[after:end], HasMore: end < len(provider.changes)})
	default:
		writer.WriteHeader(http.StatusNotFound)
	}
}

func startChangeLogProvider() (*changeLogProvider, *httptest.Server) {
	provider := &changeLogProvider{}
	return provider, httptest.NewServer(provider)
}

type changeListing struct {
	Changes []providerChange `json:"changes"`
	HasMore bool             `json:"hasMore"`
}

// changeLogClient is the connector's provider client.
type changeLogClient struct {
	baseURL    string
	httpClient *http.Client
}

// changeLogStatusError is a provider response other than 200, with its raw Retry-After header.
type changeLogStatusError struct {
	Code       int
	RetryAfter string
}

// Error describes the failure.
func (e *changeLogStatusError) Error() string { return "change log responded " + strconv.Itoa(e.Code) }

func (client *changeLogClient) latestSequence(ctx context.Context) (int64, error) {
	var latest struct {
		Latest int64 `json:"latest"`
	}
	err := client.get(ctx, "/latest", &latest)
	return latest.Latest, err
}

func (client *changeLogClient) listChanges(ctx context.Context, after int64, limit int) (changeListing, error) {
	var listing changeListing
	err := client.get(ctx, fmt.Sprintf("/changes?after=%d&limit=%d", after, limit), &listing)
	return listing, err
}

func (client *changeLogClient) get(ctx context.Context, path string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &changeLogStatusError{Code: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

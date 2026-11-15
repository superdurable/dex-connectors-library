// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asanaeventsfake

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (fake *Project) serve(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	status, body := fake.route(request)
	fake.requests = append(fake.requests, Request{Path: request.URL.Path, Query: request.URL.Query(), Status: status})
	writer.Header().Set("Content-Type", "application/json")
	if status == http.StatusTooManyRequests && fake.retryAfter != "" {
		writer.Header().Set("Retry-After", fake.retryAfter)
	}
	writer.WriteHeader(status)
	// A test client that hung up cannot be answered; the recorded request already holds the status.
	_ = json.NewEncoder(writer).Encode(body)
}

func (fake *Project) route(request *http.Request) (int, any) {
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		return http.StatusUnauthorized, errorBody("Not Authorized")
	}
	if request.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, errorBody("method not allowed")
	}
	switch {
	case request.URL.Path == "/events":
		return fake.serveEvents(request)
	case strings.HasPrefix(request.URL.Path, "/tasks/"):
		return fake.serveTask(strings.TrimPrefix(request.URL.Path, "/tasks/"))
	default:
		return http.StatusNotFound, errorBody("Unknown object")
	}
}

func (fake *Project) serveEvents(request *http.Request) (int, any) {
	query := request.URL.Query()
	if query.Get("resource") != fake.projectID {
		return http.StatusNotFound, errorBody("Unknown object")
	}
	if fake.rateLimited > 0 {
		fake.rateLimited--
		return http.StatusTooManyRequests, errorBody("Rate limit exceeded")
	}
	fresh := map[string]any{
		"errors": []map[string]string{{"message": "Sync token invalid or too old."}},
		"sync":   syncToken(len(fake.events)),
	}
	sequenceText, hasPrefix := strings.CutPrefix(query.Get("sync"), "sync-")
	sequence, err := strconv.Atoi(sequenceText)
	if !hasPrefix || err != nil || sequence < fake.oldestValidToken || sequence > len(fake.events) {
		return http.StatusPreconditionFailed, fresh
	}
	end := min(sequence+pageSize, len(fake.events))
	return http.StatusOK, map[string]any{
		"data": fake.events[sequence:end], "sync": syncToken(end), "has_more": end < len(fake.events),
	}
}

func (fake *Project) serveTask(taskID string) (int, any) {
	task, ok := fake.tasks[taskID]
	if !ok {
		return http.StatusNotFound, errorBody("Unknown object")
	}
	return http.StatusOK, map[string]any{"data": map[string]any{
		"gid": taskID, "name": task.name, "resource_subtype": "default_task", "completed": task.completed,
		"memberships": []map[string]any{{"project": map[string]any{"gid": fake.projectID, "name": "Facilities"}}},
		"created_at":  baseTime.Format("2006-01-02T15:04:05.000Z"), "modified_at": baseTime.Format("2006-01-02T15:04:05.000Z"),
	}}
}

func errorBody(message string) map[string]any {
	return map[string]any{"errors": []map[string]string{{"message": message}}}
}

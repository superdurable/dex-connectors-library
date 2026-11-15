// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package asanaeventsfake serves one Asana project's Events API stream and its tasks over HTTP for
// taskChanged tests. Sync tokens are "sync-<sequence>"; a request without one, or with an expired one,
// answers 412 with a fresh token, and each page holds at most 100 events, as Asana documents.
package asanaeventsfake

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// AccessToken is the personal access token the fake accepts.
const AccessToken = "asana-fake-token"

// pageSize is Asana's documented cap of events per sync token response.
const pageSize = 100

var baseTime = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// Request is one recorded request.
type Request struct {
	// Path is the request path, such as /events or /tasks/1204000000000001.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Project is a stateful single-project fake that is safe for concurrent use.
type Project struct {
	*httptest.Server
	projectID        string
	mutex            sync.Mutex
	events           []map[string]any
	tasks            map[string]*storedTask
	oldestValidToken int
	rateLimited      int
	retryAfter       string
	requests         []Request
}

type storedTask struct {
	name      string
	completed bool
}

// New starts a fake serving projectID; the test cleanup closes it.
func New(t *testing.T, projectID string) *Project {
	t.Helper()
	fake := &Project{projectID: projectID, tasks: map[string]*storedTask{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

// AddTask creates a task in the project and records its added event, with the project as parent.
func (fake *Project) AddTask(taskID string, name string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.tasks[taskID] = &storedTask{name: name}
	fake.appendEvent(taskID, "added", map[string]any{"gid": fake.projectID, "resource_type": "project", "name": "Facilities"}, nil)
}

// RenameTask changes a task's name and records a changed event for the name field.
func (fake *Project) RenameTask(taskID string, name string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.tasks[taskID].name = name
	fake.appendEvent(taskID, "changed", nil, map[string]any{"field": "name", "action": "changed", "new_value": name})
}

// CompleteTask marks a task completed and records a changed event for the completed field.
func (fake *Project) CompleteTask(taskID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.tasks[taskID].completed = true
	fake.appendEvent(taskID, "changed", nil, map[string]any{"field": "completed", "action": "changed", "new_value": true})
}

// DeleteTask moves a task to the trash and records a deleted event.
func (fake *Project) DeleteTask(taskID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	delete(fake.tasks, taskID)
	fake.appendEvent(taskID, "deleted", nil, nil)
}

// AddStory records a comment story event, which a taskChanged Trigger must ignore.
func (fake *Project) AddStory(storyID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.events = append(fake.events, map[string]any{
		"action": "added", "created_at": fake.eventTime(), "user": user(),
		"resource": map[string]any{"gid": storyID, "resource_type": "story", "resource_subtype": "comment_added"},
	})
}

// ExpireSyncTokens makes every token older than the latest event answer 412, as Asana does after 24
// hours; the fresh token a 412 returns stays valid.
func (fake *Project) ExpireSyncTokens() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.oldestValidToken = len(fake.events)
}

// RateLimitNext answers the next count /events requests with 429 and the given Retry-After header.
func (fake *Project) RateLimitNext(count int, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.retryAfter = count, retryAfter
}

// SyncToken returns the token that follows every event recorded so far.
func (fake *Project) SyncToken() string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return syncToken(len(fake.events))
}

// Requests returns a copy of the recorded requests.
func (fake *Project) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

func (fake *Project) appendEvent(taskID string, action string, parent map[string]any, change map[string]any) {
	event := map[string]any{
		"action": action, "created_at": fake.eventTime(), "user": user(), "parent": parent,
		"resource": map[string]any{"gid": taskID, "resource_type": "task", "resource_subtype": "default_task", "name": fake.taskName(taskID)},
	}
	if change != nil {
		event["change"] = change
	}
	fake.events = append(fake.events, event)
}

func (fake *Project) taskName(taskID string) string {
	if task, ok := fake.tasks[taskID]; ok {
		return task.name
	}
	return ""
}

func (fake *Project) eventTime() string {
	return baseTime.Add(time.Duration(len(fake.events)) * time.Second).Format(time.RFC3339Nano)
}

func user() map[string]any {
	return map[string]any{"gid": "1203000000000001", "resource_type": "user", "name": "Ada Lovelace"}
}

func syncToken(sequence int) string { return "sync-" + strconv.Itoa(sequence) }

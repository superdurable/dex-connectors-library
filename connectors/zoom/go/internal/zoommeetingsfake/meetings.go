// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zoommeetingsfake serves one Zoom user's GET /users/me/meetings and GET /meetings/{id} over HTTP
// for meetingChanged tests. Listing pages follow Zoom's documented shape: page_size up to 300, an opaque
// next_page_token, and meetings in creation order.
package zoommeetingsfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// AccessToken is the bearer token the fake accepts.
const AccessToken = "zoom-trigger-token"

var baseTime = time.Date(2036, 2, 23, 17, 0, 0, 0, time.UTC)

// Request is one recorded request.
type Request struct {
	// Path is the request path.
	Path string
	// Query is the request query string.
	Query url.Values
	// Status is the HTTP status the fake answered.
	Status int
}

// Meetings is a stateful single-user fake that is safe for concurrent use.
type Meetings struct {
	*httptest.Server
	hostID             string
	mutex              sync.Mutex
	created            int
	meetings           map[int64]*meeting
	deleted            map[int64]*meeting
	rejectedPageTokens int
	rateLimited        int
	rateLimitIsDaily   bool
	retryAfter         string
	issuedPageTokens   map[string]int
	requests           []Request
}

type meeting struct {
	id        int64
	topic     string
	startTime time.Time
	duration  int
	createdAt int
}

// New starts a fake for hostID; the test cleanup closes it.
func New(t *testing.T, hostID string) *Meetings {
	t.Helper()
	fake := &Meetings{hostID: hostID, meetings: map[int64]*meeting{}, deleted: map[int64]*meeting{}, issuedPageTokens: map[string]int{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

// Schedule creates a 30-minute meeting starting days after the base time.
func (fake *Meetings) Schedule(meetingID int64, topic string, days int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.created++
	fake.meetings[meetingID] = &meeting{id: meetingID, topic: topic, startTime: baseTime.AddDate(0, 0, days), duration: 30, createdAt: fake.created}
}

// Reschedule moves an existing meeting's start by hours.
func (fake *Meetings) Reschedule(meetingID int64, hours int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.meetings[meetingID].startTime = fake.meetings[meetingID].startTime.Add(time.Duration(hours) * time.Hour)
}

// Rename changes an existing meeting's topic.
func (fake *Meetings) Rename(meetingID int64, topic string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.meetings[meetingID].topic = topic
}

// SetHostID makes every meeting belong to hostID, as when the connection is reauthorized as another user.
func (fake *Meetings) SetHostID(hostID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.hostID = hostID
}

// Delete moves a meeting to Recently Deleted, where Recover can restore it.
func (fake *Meetings) Delete(meetingID int64) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.deleted[meetingID] = fake.meetings[meetingID]
	delete(fake.meetings, meetingID)
}

// Recover restores a deleted meeting unchanged, as Zoom's Recently Deleted list does within 7 days.
func (fake *Meetings) Recover(meetingID int64) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.meetings[meetingID] = fake.deleted[meetingID]
	delete(fake.deleted, meetingID)
}

// RejectNextPageTokens answers the next count requests that carry a page token with 400, as Zoom does
// after a token's 15-minute expiry.
func (fake *Meetings) RejectNextPageTokens(count int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rejectedPageTokens = count
}

// RateLimitNext answers the next count listing requests with 429. isDaily selects Zoom's documented
// daily-limit message; retryAfter is sent as the Retry-After header when not blank.
func (fake *Meetings) RateLimitNext(count int, isDaily bool, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.rateLimitIsDaily, fake.retryAfter = count, isDaily, retryAfter
}

// Requests returns the recorded requests.
func (fake *Meetings) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

// ListingRequestCount counts listing requests.
func (fake *Meetings) ListingRequestCount() int {
	count := 0
	for _, request := range fake.Requests() {
		if strings.HasSuffix(request.Path, "/users/me/meetings") {
			count++
		}
	}
	return count
}

func (fake *Meetings) serve(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	status, body := fake.answer(request, response.Header())
	fake.requests = append(fake.requests, Request{Path: request.URL.Path, Query: request.URL.Query(), Status: status})
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body) // A failed write only fails the client's read.
}

func (fake *Meetings) answer(request *http.Request, header http.Header) (int, any) {
	if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+AccessToken {
		return http.StatusUnauthorized, map[string]any{"code": 124, "message": "Invalid access token."}
	}
	if strings.HasPrefix(request.URL.Path, "/meetings/") {
		return fake.answerMeeting(strings.TrimPrefix(request.URL.Path, "/meetings/"))
	}
	if request.URL.Path != "/users/me/meetings" {
		return http.StatusNotFound, map[string]any{"code": 1001, "message": "User does not exist."}
	}
	if fake.rateLimited > 0 {
		fake.rateLimited--
		if fake.retryAfter != "" {
			header.Set("Retry-After", fake.retryAfter)
		}
		message := "You have reached the maximum per-second rate limit for this API. Try again later."
		if fake.rateLimitIsDaily {
			message = "You have reached the maximum daily rate limit for this API. Refer to the response header for details on when you can make another call."
		}
		return http.StatusTooManyRequests, map[string]any{"code": 429, "message": message}
	}
	return fake.answerListing(request.URL.Query())
}

func (fake *Meetings) answerListing(query url.Values) (int, any) {
	if query.Get("type") != "scheduled" {
		return http.StatusBadRequest, map[string]any{"code": 300, "message": "Unexpected type."}
	}
	pageSize, err := strconv.Atoi(query.Get("page_size"))
	if err != nil || pageSize < 1 || pageSize > 300 {
		return http.StatusBadRequest, map[string]any{"code": 300, "message": "Invalid page_size."}
	}
	offset := 0
	if token := query.Get("next_page_token"); token != "" {
		if fake.rejectedPageTokens > 0 {
			fake.rejectedPageTokens--
			return http.StatusBadRequest, map[string]any{"code": 300, "message": "The next page token is invalid or expired."}
		}
		issued, isIssued := fake.issuedPageTokens[token]
		if !isIssued {
			return http.StatusBadRequest, map[string]any{"code": 300, "message": "The next page token is invalid or expired."}
		}
		offset = issued
	}
	ordered := make([]*meeting, 0, len(fake.meetings))
	for _, stored := range fake.meetings {
		ordered = append(ordered, stored)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].createdAt < ordered[j].createdAt })
	end := min(offset+pageSize, len(ordered))
	items := make([]map[string]any, 0, end-offset)
	for _, stored := range ordered[min(offset, end):end] {
		items = append(items, fake.resource(stored))
	}
	nextPageToken := ""
	if end < len(ordered) {
		nextPageToken = fmt.Sprintf("page-%d-%d", len(fake.issuedPageTokens)+1, end)
		fake.issuedPageTokens[nextPageToken] = end
	}
	return http.StatusOK, map[string]any{
		"next_page_token": nextPageToken, "page_size": pageSize, "total_records": len(ordered), "meetings": items,
	}
}

func (fake *Meetings) answerMeeting(rawID string) (int, any) {
	meetingID, err := strconv.ParseInt(rawID, 10, 64)
	stored, isStored := fake.meetings[meetingID]
	if err != nil || !isStored {
		return http.StatusNotFound, map[string]any{"code": 3001, "message": "Meeting does not exist."}
	}
	resource := fake.resource(stored)
	resource["settings"] = map[string]any{"waiting_room": true}
	return http.StatusOK, resource
}

func (fake *Meetings) resource(stored *meeting) map[string]any {
	return map[string]any{
		"id": stored.id, "uuid": fmt.Sprintf("uuid-%d==", stored.id), "host_id": fake.hostID, "topic": stored.topic, "type": 2,
		"start_time": stored.startTime.Format(time.RFC3339), "duration": stored.duration, "timezone": "America/Los_Angeles",
		"join_url":   fmt.Sprintf("https://us05web.zoom.us/j/%d", stored.id),
		"created_at": baseTime.Add(-time.Duration(stored.createdAt) * time.Hour).Format(time.RFC3339),
	}
}

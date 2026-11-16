// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zendeskexportfake

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (fake *Account) serveExport(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if fake.rateLimited > 0 {
		fake.rateLimited--
		if fake.retryAfter != "" {
			writer.Header().Set("Retry-After", fake.retryAfter)
		}
		fake.answerLocked(writer, request, http.StatusTooManyRequests, map[string]any{"error": "APIRateLimitExceeded"})
		return
	}
	if fake.isAgentOnly {
		fake.answerLocked(writer, request, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	query := request.URL.Query()
	perPage := 1000
	if value := query.Get("per_page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 1000 {
			fake.answerLocked(writer, request, http.StatusBadRequest, map[string]any{"error": "InvalidPaginationParameter"})
			return
		}
		perPage = parsed
	}
	perPage = min(perPage, fake.maximumPerPage)
	after, isCursor, status := fake.exportStart(query)
	if status != http.StatusOK {
		fake.answerLocked(writer, request, status, map[string]any{"error": "InvalidPaginationParameter"})
		return
	}
	isExcludingDeleted := query.Get("exclude_deleted") == "true"
	var matches []*storedTicket
	for _, ticket := range fake.tickets {
		isAfter := ticket.sequence > after
		if !isCursor {
			isAfter = ticket.updatedAt.Unix() >= int64(after)
		}
		if isAfter && !(isExcludingDeleted && ticket.status == "deleted") {
			matches = append(matches, ticket)
		}
	}
	sort.Slice(matches, func(left, right int) bool { return matches[left].sequence < matches[right].sequence })
	nextSequence := fake.sequence
	isEnd := len(matches) <= perPage
	if !isEnd {
		matches = matches[:perPage]
	}
	tickets := make([]map[string]any, 0, len(matches))
	for _, ticket := range matches {
		tickets = append(tickets, ticketJSON(ticket))
		if !isEnd {
			nextSequence = ticket.sequence
		}
	}
	fake.answerLocked(writer, request, http.StatusOK, map[string]any{
		"tickets": tickets, "after_cursor": fake.epochCursor(nextSequence), "before_cursor": nil, "end_of_stream": isEnd,
	})
}

// exportStart returns the sequence after which a cursor request lists changes, or the start_time of a time
// request, and the HTTP status for an unusable request.
func (fake *Account) exportStart(query map[string][]string) (int, bool, int) {
	if cursors := query["cursor"]; len(cursors) == 1 {
		epoch, sequence, ok := strings.Cut(strings.TrimPrefix(cursors[0], "c-"), "-")
		parsed, err := strconv.Atoi(sequence)
		if !ok || err != nil || epoch != strconv.Itoa(fake.cursorEpoch) {
			return 0, true, http.StatusUnprocessableEntity
		}
		return parsed, true, http.StatusOK
	}
	startTimes := query["start_time"]
	if len(startTimes) != 1 {
		return 0, false, http.StatusBadRequest
	}
	startTime, err := strconv.ParseInt(startTimes[0], 10, 64)
	if err != nil || startTime > time.Now().Add(-time.Minute).Unix() {
		// Zendesk serves no data for the most recent minute.
		return 0, false, http.StatusBadRequest
	}
	return int(startTime), false, http.StatusOK
}

// epochCursor names a position in the current epoch, so InvalidateCursors rejects every earlier cursor.
func (fake *Account) epochCursor(sequence int) string {
	return "c-" + strconv.Itoa(fake.cursorEpoch) + "-" + strconv.Itoa(sequence)
}

func (fake *Account) serveTicket(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	ticket, ok := fake.readableTicket(request)
	if !ok {
		fake.answerLocked(writer, request, http.StatusNotFound, map[string]any{"error": "RecordNotFound"})
		return
	}
	fake.answerLocked(writer, request, http.StatusOK, map[string]any{
		"ticket": ticketJSON(ticket),
		"users":  []map[string]any{{"id": 7701, "name": "Jane Smith", "email": "jane@acme.example.com", "role": "end-user"}},
	})
}

func (fake *Account) serveComments(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	ticket, ok := fake.readableTicket(request)
	if !ok {
		fake.answerLocked(writer, request, http.StatusNotFound, map[string]any{"error": "RecordNotFound"})
		return
	}
	fake.answerLocked(writer, request, http.StatusOK, map[string]any{
		"comments": []map[string]any{{
			"id": ticket.id * 10, "author_id": 7701, "public": true, "plain_body": "Opened: " + ticket.subject,
			"created_at": ticket.createdAt.Format(time.RFC3339),
		}},
		"meta": map[string]any{"has_more": false},
	})
}

func (fake *Account) readableTicket(request *http.Request) (*storedTicket, bool) {
	ticketID, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	ticket := fake.tickets[ticketID]
	return ticket, err == nil && ticket != nil && ticket.status != "deleted"
}

func ticketJSON(ticket *storedTicket) map[string]any {
	return map[string]any{
		"id": ticket.id, "subject": ticket.subject, "description": "Opened: " + ticket.subject, "status": ticket.status,
		"requester_id": 7701, "tags": []string{"dex-fake"}, "via": map[string]any{"channel": "api"},
		"created_at": ticket.createdAt.Format(time.RFC3339), "updated_at": ticket.updatedAt.Format(time.RFC3339),
		"generated_timestamp": ticket.updatedAt.Unix(),
	}
}

func (fake *Account) answer(writer http.ResponseWriter, request *http.Request, status int, body any) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.answerLocked(writer, request, status, body)
}

func (fake *Account) answerLocked(writer http.ResponseWriter, request *http.Request, status int, body any) {
	path := strings.TrimPrefix(request.URL.Path, "/api/v2")
	fake.requests = append(fake.requests, Request{Path: path, Query: request.URL.Query(), Status: status})
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	// A write error means the client went away; the request is already recorded.
	_ = json.NewEncoder(writer).Encode(body)
}

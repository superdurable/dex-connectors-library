// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package twiliofake serves a credential-safe stand-in for the Twilio Messages list API, so tests can
// drive the messageReceived Trigger without a live account.
package twiliofake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Messages is a fake Twilio account whose Messages list Twilio's documented filters and paging serve:
// DateSent> and DateSent< (inclusive, to the second), To, PageSize, and next_page_uri with a PageToken.
// Results are newest first. It accepts only the Basic credential it was created with.
type Messages struct {
	// URL is the endpoint to configure, including the 2010-04-01 version segment.
	URL string

	server     *httptest.Server
	accountSID string
	username   string
	password   string
	// nonce keeps SIDs, and so Flow IDs, unique across test runs against one Dex server.
	nonce string

	mutex        sync.Mutex
	messages     []message
	listRequests []url.Values
	throttles    []time.Duration
	next         int
	isDateOnly   bool
}

type message struct {
	SID         string  `json:"sid"`
	AccountSID  string  `json:"account_sid"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Body        string  `json:"body"`
	Status      string  `json:"status"`
	Direction   string  `json:"direction"`
	NumSegments string  `json:"num_segments"`
	NumMedia    string  `json:"num_media"`
	DateCreated string  `json:"date_created"`
	DateUpdated string  `json:"date_updated"`
	DateSent    *string `json:"date_sent"`
	sentAt      time.Time
}

// New starts the fake for accountSID; the test closes it on cleanup.
func New(t *testing.T, accountSID string, username string, password string) *Messages {
	t.Helper()
	fake := &Messages{accountSID: accountSID, username: username, password: password, nonce: strconv.FormatInt(time.Now().UnixNano(), 10)}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	fake.URL = fake.server.URL + "/2010-04-01"
	return fake
}

// Receive adds an inbound message dated sentAt and returns its SID. Status is received unless the
// caller passes receiving.
func (fake *Messages) Receive(from string, to string, body string, sentAt time.Time, status string) string {
	return fake.add(from, to, body, sentAt, status, "inbound")
}

// Send adds an outbound-api message, which the Trigger must ignore.
func (fake *Messages) Send(from string, to string, body string, sentAt time.Time) string {
	return fake.add(from, to, body, sentAt, "delivered", "outbound-api")
}

// SetStatus changes a message's status, as Twilio does when processing finishes.
func (fake *Messages) SetStatus(messageSID string, status string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	for index := range fake.messages {
		if fake.messages[index].SID == messageSID {
			fake.messages[index].Status = status
		}
	}
}

// UseDateOnlyFilters makes DateSent> and DateSent< compare whole GMT dates, the precision Twilio's
// reference documents, so a listing can include the whole day around a time-of-day bound.
func (fake *Messages) UseDateOnlyFilters() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.isDateOnly = true
}

// ThrottleNextList makes the next list request answer 429 with Retry-After seconds; zero omits the header.
func (fake *Messages) ThrottleNextList(retryAfter time.Duration) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.throttles = append(fake.throttles, retryAfter)
}

// ListRequests returns the query of every list request served, including throttled ones.
func (fake *Messages) ListRequests() []url.Values {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return slices.Clone(fake.listRequests)
}

func (fake *Messages) add(from string, to string, body string, sentAt time.Time, status string, direction string) string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.next++
	digest := sha256.Sum256([]byte(fake.nonce + fake.accountSID + strconv.Itoa(fake.next) + to + body))
	if status == "" {
		status = "received"
	}
	created := sentAt.UTC().Format(time.RFC1123Z)
	dateSent := created
	entry := message{
		SID: "SM" + hex.EncodeToString(digest[:16]), AccountSID: fake.accountSID, From: from, To: to, Body: body,
		Status: status, Direction: direction, NumSegments: "1", NumMedia: "0",
		DateCreated: created, DateUpdated: created, DateSent: &dateSent, sentAt: sentAt.UTC().Truncate(time.Second),
	}
	fake.messages = append(fake.messages, entry)
	return entry.SID
}

func (fake *Messages) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	listPath := "/2010-04-01/Accounts/" + fake.accountSID + "/Messages.json"
	username, password, hasBasic := request.BasicAuth()
	switch {
	case !hasBasic || username != fake.username || password != fake.password:
		writeError(writer, http.StatusUnauthorized, 20003)
		return
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, strings.TrimSuffix(listPath, ".json")+"/"):
		fake.serveMessage(writer, strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, strings.TrimSuffix(listPath, ".json")+"/"), ".json"))
		return
	case request.Method != http.MethodGet || request.URL.Path != listPath:
		writeError(writer, http.StatusNotFound, 20404)
		return
	}
	query := request.URL.Query()
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.listRequests = append(fake.listRequests, query)
	if len(fake.throttles) > 0 {
		retryAfter := fake.throttles[0]
		fake.throttles = fake.throttles[1:]
		if retryAfter > 0 {
			writer.Header().Set("Retry-After", strconv.Itoa(int(retryAfter/time.Second)))
		}
		writeError(writer, http.StatusTooManyRequests, 20429)
		return
	}
	matched, ok := fake.filter(query)
	if !ok {
		writeError(writer, http.StatusBadRequest, 20001)
		return
	}
	pageSize, err := strconv.Atoi(query.Get("PageSize"))
	if err != nil || pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}
	offset := 0
	if token := query.Get("PageToken"); token != "" {
		if offset, err = strconv.Atoi(strings.TrimPrefix(token, "PA")); err != nil || offset < 0 || offset > len(matched) {
			writeError(writer, http.StatusBadRequest, 20001)
			return
		}
	}
	end := min(offset+pageSize, len(matched))
	response := map[string]any{"messages": matched[offset:end], "page_size": pageSize, "next_page_uri": nil}
	if end < len(matched) {
		next := url.Values{}
		for name, values := range query {
			next[name] = values
		}
		next.Set("PageToken", "PA"+strconv.Itoa(end))
		next.Set("Page", strconv.Itoa(end/pageSize))
		response["next_page_uri"] = listPath + "?" + next.Encode()
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response) // A failed write only fails the client's read.
}

// serveMessage answers getMessage for one SID.
func (fake *Messages) serveMessage(writer http.ResponseWriter, messageSID string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	for _, entry := range fake.messages {
		if entry.SID == messageSID {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(entry) // See serveHTTP.
			return
		}
	}
	writeError(writer, http.StatusNotFound, 20404)
}

// filter applies To and the inclusive DateSent bounds, newest first.
func (fake *Messages) filter(query url.Values) ([]message, bool) {
	bounds := map[string]time.Time{}
	for _, name := range []string{"DateSent>", "DateSent<"} {
		if value := query.Get(name); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, false
			}
			if fake.isDateOnly {
				parsed = parsed.UTC().Truncate(24 * time.Hour)
				if name == "DateSent<" {
					parsed = parsed.Add(24*time.Hour - time.Second)
				}
			}
			bounds[name] = parsed
		}
	}
	matched := []message{}
	for _, entry := range fake.messages {
		if to := query.Get("To"); to != "" && entry.To != to {
			continue
		}
		if since, ok := bounds["DateSent>"]; ok && entry.sentAt.Before(since) {
			continue
		}
		if until, ok := bounds["DateSent<"]; ok && entry.sentAt.After(until) {
			continue
		}
		matched = append(matched, entry)
	}
	slices.SortStableFunc(matched, func(left, right message) int {
		if !left.sentAt.Equal(right.sentAt) {
			return right.sentAt.Compare(left.sentAt)
		}
		return strings.Compare(right.SID, left.SID)
	})
	return matched, true
}

func writeError(writer http.ResponseWriter, status int, code int) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"code": code, "status": status}) // See serveHTTP.
}

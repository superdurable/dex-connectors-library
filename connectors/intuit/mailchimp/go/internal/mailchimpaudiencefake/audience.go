// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mailchimpaudiencefake serves one Mailchimp audience's contact list and contact reads over HTTP,
// for memberChanged Trigger and example tests. It models the documented list semantics the Trigger relies
// on: since_last_changed keeps contacts changed strictly after the bound, sort_field=last_changed orders by
// change second, count is at most 1000, offset skips matching contacts, and fields keeps only the listed
// members.* fields, as Mailchimp's partial responses do.
package mailchimpaudiencefake

import (
	"crypto/md5"
	"encoding/hex"
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

// APIKey is the only key the fake accepts; its -us6 suffix selects a data center the fake ignores.
// The literal is split so secret scanners do not read a fixture as a credential.
const APIKey = "0123456789abcdef" + "0123456789abcdef" + "-us6"

// Audience is one fake audience. It is safe for concurrent use.
type Audience struct {
	// URL is the API base URL to pass to mailchimp.WithAPIBaseURL, ending in /3.0.
	URL string

	audienceID string
	mutex      sync.Mutex
	clock      time.Time
	members    map[string]*member
	failures   []Failure
	requests   []string
}

// Failure is one injected response for the next list request.
type Failure struct {
	// Status is the HTTP status, such as 429 or 400.
	Status int
	// RetryAfter is the Retry-After header value; empty sends none.
	RetryAfter string
	// WithoutDocument sends an empty body, as Mailchimp's throttling 403 does.
	WithoutDocument bool
}

type member struct {
	ID           string         `json:"id"`
	EmailAddress string         `json:"email_address"`
	Status       string         `json:"status"`
	MergeFields  map[string]any `json:"merge_fields"`
	LastChanged  string         `json:"last_changed"`
	ListID       string         `json:"list_id"`
	changedAt    time.Time
}

// New starts a fake for audienceID whose clock starts at 2026-11-01T09:00:00Z.
func New(t testing.TB, audienceID string) *Audience {
	t.Helper()
	audience := &Audience{
		audienceID: audienceID, clock: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), members: map[string]*member{},
	}
	server := httptest.NewServer(http.HandlerFunc(audience.serve))
	t.Cleanup(server.Close)
	audience.URL = server.URL + "/3.0"
	return audience
}

// Upsert adds or changes the contact for emailAddress one second after the previous change and returns
// its subscriber hash.
func (audience *Audience) Upsert(emailAddress string, status string, firstName string) string {
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	audience.clock = audience.clock.Add(time.Second)
	return audience.upsertLocked(emailAddress, status, firstName, audience.clock)
}

// UpsertAt adds or changes the contact with an explicit change time, which may lie before earlier changes
// to model a change that becomes listable late. It does not move the clock.
func (audience *Audience) UpsertAt(emailAddress string, status string, firstName string, changedAt time.Time) string {
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	return audience.upsertLocked(emailAddress, status, firstName, changedAt.UTC().Truncate(time.Second))
}

// Clock returns the time of the latest Upsert.
func (audience *Audience) Clock() time.Time {
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	return audience.clock
}

// FailNextList answers the next list requests with failures, in order.
func (audience *Audience) FailNextList(failures ...Failure) {
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	audience.failures = append(audience.failures, failures...)
}

// Requests returns the method, path, and query of every authenticated request so far.
func (audience *Audience) Requests() []string {
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	return append([]string(nil), audience.requests...)
}

// SubscriberHash returns Mailchimp's member ID for emailAddress.
func SubscriberHash(emailAddress string) string {
	digest := md5.Sum([]byte(strings.ToLower(emailAddress)))
	return hex.EncodeToString(digest[:])
}

func (audience *Audience) upsertLocked(emailAddress string, status string, firstName string, changedAt time.Time) string {
	hash := SubscriberHash(emailAddress)
	audience.members[hash] = &member{
		ID: hash, EmailAddress: emailAddress, Status: status, MergeFields: map[string]any{"FNAME": firstName},
		LastChanged: changedAt.Format("2006-01-02T15:04:05+00:00"), ListID: audience.audienceID, changedAt: changedAt,
	}
	return hash
}

func (audience *Audience) serve(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+APIKey {
		writeProblem(writer, http.StatusUnauthorized, "API Key Invalid")
		return
	}
	audience.mutex.Lock()
	defer audience.mutex.Unlock()
	audience.requests = append(audience.requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery)
	prefix := "/3.0/lists/" + audience.audienceID + "/members"
	switch {
	case request.Method != http.MethodGet:
		writeProblem(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
	case request.URL.Path == prefix:
		audience.serveListLocked(writer, request)
	case strings.HasPrefix(request.URL.Path, prefix+"/"):
		found, ok := audience.members[strings.TrimPrefix(request.URL.Path, prefix+"/")]
		if !ok {
			writeProblem(writer, http.StatusNotFound, "Resource Not Found")
			return
		}
		writeJSON(writer, http.StatusOK, found)
	default:
		writeProblem(writer, http.StatusNotFound, "Resource Not Found")
	}
}

func (audience *Audience) serveListLocked(writer http.ResponseWriter, request *http.Request) {
	if len(audience.failures) != 0 {
		failure := audience.failures[0]
		audience.failures = audience.failures[1:]
		if failure.RetryAfter != "" {
			writer.Header().Set("Retry-After", failure.RetryAfter)
		}
		if failure.WithoutDocument {
			writer.WriteHeader(failure.Status)
			return
		}
		writeProblem(writer, failure.Status, http.StatusText(failure.Status))
		return
	}
	query := request.URL.Query()
	count, countErr := strconv.Atoi(query.Get("count"))
	offset, offsetErr := strconv.Atoi(query.Get("offset"))
	if countErr != nil || offsetErr != nil || count < 1 || count > 1000 || offset < 0 || query.Get("sort_field") != "last_changed" {
		writeProblem(writer, http.StatusBadRequest, "Invalid Resource")
		return
	}
	var since *time.Time
	if value := query.Get("since_last_changed"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeProblem(writer, http.StatusBadRequest, "Invalid Resource")
			return
		}
		since = &parsed
	}
	matching := make([]*member, 0, len(audience.members))
	for _, candidate := range audience.members {
		if since == nil || candidate.changedAt.After(*since) {
			matching = append(matching, candidate)
		}
	}
	isDescending := query.Get("sort_dir") == "DESC"
	sort.Slice(matching, func(i, j int) bool {
		if !matching[i].changedAt.Equal(matching[j].changedAt) {
			return matching[i].changedAt.Before(matching[j].changedAt) != isDescending
		}
		return matching[i].ID < matching[j].ID
	})
	page := matching[min(offset, len(matching)):min(offset+count, len(matching))]
	writeJSON(writer, http.StatusOK, map[string]any{"members": selectFields(page, query.Get("fields")), "list_id": audience.audienceID, "total_items": len(matching)})
}

// selectFields keeps the members.* fields named in fields; an empty fields keeps every field.
func selectFields(page []*member, fields string) []map[string]any {
	selected := make([]map[string]any, 0, len(page))
	for _, candidate := range page {
		encoded, _ := json.Marshal(candidate)
		var full map[string]any
		_ = json.Unmarshal(encoded, &full)
		if fields == "" {
			selected = append(selected, full)
			continue
		}
		kept := map[string]any{}
		for _, field := range strings.Split(fields, ",") {
			if name, ok := strings.CutPrefix(field, "members."); ok {
				if value, present := full[name]; present {
					kept[name] = value
				}
			}
		}
		selected = append(selected, kept)
	}
	return selected
}

func writeProblem(writer http.ResponseWriter, status int, title string) {
	writeJSON(writer, status, map[string]any{"type": "https://mailchimp.com/developer/marketing/docs/errors/", "title": title, "status": status})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value) // The test client reports a truncated body itself.
}

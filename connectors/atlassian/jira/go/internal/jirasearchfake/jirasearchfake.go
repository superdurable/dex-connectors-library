// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package jirasearchfake serves one Jira Cloud site's enhanced JQL search and issue reads over HTTP for
// issueChanged tests. It understands exactly the JQL the issueChanged Trigger sends: an optional project
// clause, a relative updated bound in minutes, and an updated order.
package jirasearchfake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// CloudID is the fake site's cloudId.
const CloudID = "1324a887-45db-1bf4-1e99-ef0ff456d421"

var (
	triggerJQLPattern = regexp.MustCompile(`^(?:project = ("?[A-Z0-9_]+"?) AND )?updated >= -([0-9]+)m ORDER BY updated (ASC|DESC)$`)
	// jiraZone formats times with a non-UTC offset, as Jira does in the account's time zone.
	jiraZone = time.FixedZone("CET", 3600)
)

// Project is one fake project.
type Project struct {
	// ID is the numeric project ID.
	ID string
	// Key is the project key.
	Key string
	// Name is the project name.
	Name string
}

// Operations and HumanResources are the fake site's projects.
var (
	Operations     = Project{ID: "10000", Key: "OPS", Name: "Operations"}
	HumanResources = Project{ID: "10001", Key: "HR", Name: "Human Resources"}
)

// Request is one recorded search request.
type Request struct {
	// JQL is the request's query.
	JQL string
	// NextPageToken is the request's page token.
	NextPageToken string
	// MaxResults is the requested page size.
	MaxResults int
	// Status is the HTTP status the fake answered.
	Status int
}

// Site is a stateful fake that is safe for concurrent use.
type Site struct {
	*httptest.Server
	mutex             sync.Mutex
	now               func() time.Time
	issues            map[string]*storedIssue
	nextIssueNumber   int
	rateLimited       int
	rateLimitStatus   int
	retryAfter        string
	rejectsPageTokens bool
	requests          []Request
}

type storedIssue struct {
	id, key, summary string
	project          Project
	created, updated time.Time
	isUnindexed      bool
}

// New starts a fake whose clock is time.Now; the test cleanup closes it.
func New(t *testing.T) *Site {
	t.Helper()
	site := &Site{now: time.Now, issues: map[string]*storedIssue{}, nextIssueNumber: 1}
	site.Server = httptest.NewServer(http.HandlerFunc(site.serve))
	t.Cleanup(site.Close)
	return site
}

// SetClock replaces the fake's clock, which stamps created and updated times and evaluates relative JQL.
func (site *Site) SetClock(now func() time.Time) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.now = now
}

// Create adds an issue to project and returns its key, such as OPS-1.
func (site *Site) Create(project Project, summary string) string {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	number := site.nextIssueNumber
	site.nextIssueNumber++
	now := site.now().Truncate(time.Millisecond)
	issue := &storedIssue{
		id: strconv.Itoa(20000 + number), key: project.Key + "-" + strconv.Itoa(number), summary: summary,
		project: project, created: now, updated: now,
	}
	site.issues[issue.key] = issue
	return issue.key
}

// Update changes an issue's summary and moves its updated time to the fake's clock.
func (site *Site) Update(key string, summary string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	issue := site.issues[key]
	issue.summary, issue.updated = summary, site.now().Truncate(time.Millisecond)
}

// SetIndexed hides an issue from search, as Jira's index lag does, or shows it again.
func (site *Site) SetIndexed(key string, isIndexed bool) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.issues[key].isUnindexed = !isIndexed
}

// IssueID returns the numeric ID of key.
func (site *Site) IssueID(key string) string {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return site.issues[key].id
}

// RateLimitNextRequests answers the next count search requests with 429 and the Retry-After value.
func (site *Site) RateLimitNextRequests(count int, retryAfter string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.rateLimited, site.rateLimitStatus, site.retryAfter = count, http.StatusTooManyRequests, retryAfter
}

// OverloadNextRequests answers the next count search requests with 503 and the Retry-After value, as
// Jira does for some transient failures.
func (site *Site) OverloadNextRequests(count int, retryAfter string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.rateLimited, site.rateLimitStatus, site.retryAfter = count, http.StatusServiceUnavailable, retryAfter
}

// RejectPageTokens answers every search that carries a page token with 400, as for an expired token.
func (site *Site) RejectPageTokens(isRejected bool) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.rejectsPageTokens = isRejected
}

// Requests returns the recorded search requests in arrival order.
func (site *Site) Requests() []Request {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return append([]Request(nil), site.requests...)
}

func (site *Site) serve(response http.ResponseWriter, request *http.Request) {
	prefix := "/ex/jira/" + CloudID + "/rest/api/3"
	if request.Header.Get("Authorization") == "" || !strings.HasPrefix(request.URL.Path, prefix) {
		http.Error(response, `{"errorMessages":["unauthorized"]}`, http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, prefix)
	switch {
	case request.Method == http.MethodPost && path == "/search/jql":
		site.serveSearch(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/issue/"):
		site.serveIssue(response, strings.TrimPrefix(path, "/issue/"))
	default:
		http.Error(response, `{"errorMessages":["not found"]}`, http.StatusNotFound)
	}
}

func (site *Site) serveSearch(response http.ResponseWriter, request *http.Request) {
	var body struct {
		JQL           string `json:"jql"`
		MaxResults    int    `json:"maxResults"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, `{"errorMessages":["invalid body"]}`, http.StatusBadRequest)
		return
	}
	site.mutex.Lock()
	defer site.mutex.Unlock()
	// Jira stamps every response with its own clock, which also evaluates relative JQL.
	response.Header().Set("Date", site.now().UTC().Format(http.TimeFormat))
	record := Request{JQL: body.JQL, NextPageToken: body.NextPageToken, MaxResults: body.MaxResults}
	status, payload := site.search(body.JQL, body.MaxResults, body.NextPageToken, response.Header())
	record.Status = status
	site.requests = append(site.requests, record)
	writeJSON(response, status, payload)
}

// search answers one request; a page token encodes "<offset>:<jql>", so it is valid only for its query.
func (site *Site) search(jql string, maxResults int, pageToken string, header http.Header) (int, any) {
	if site.rateLimited > 0 {
		site.rateLimited--
		header.Set("Retry-After", site.retryAfter)
		return site.rateLimitStatus, map[string]any{"errorMessages": []string{"rate limited"}}
	}
	match := triggerJQLPattern.FindStringSubmatch(jql)
	offset := 0
	if pageToken != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(pageToken)
		parsedOffset, query, hasSeparator := strings.Cut(string(decoded), ":")
		parsed, err := strconv.Atoi(parsedOffset)
		if site.rejectsPageTokens || decodeErr != nil || !hasSeparator || err != nil || query != jql {
			return http.StatusBadRequest, map[string]any{"errorMessages": []string{"invalid nextPageToken"}}
		}
		offset = parsed
	}
	if match == nil || maxResults < 1 || maxResults > 100 {
		return http.StatusBadRequest, map[string]any{"errorMessages": []string{"unsupported JQL"}}
	}
	minutes, _ := strconv.Atoi(match[2]) // The pattern admits only digits.
	since := site.now().Add(-time.Duration(minutes) * time.Minute).Truncate(time.Minute)
	var matched []*storedIssue
	for _, issue := range site.issues {
		project := strings.Trim(match[1], `"`)
		if issue.isUnindexed || issue.updated.Before(since) || (project != "" && project != issue.project.ID && project != issue.project.Key) {
			continue
		}
		matched = append(matched, issue)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].updated.Equal(matched[j].updated) {
			return matched[i].updated.Before(matched[j].updated) == (match[3] == "ASC")
		}
		return matched[i].id < matched[j].id
	})
	end := min(offset+maxResults, len(matched))
	page := map[string]any{"issues": []any{}}
	issues := []any{}
	for _, issue := range matched[min(offset, end):end] {
		issues = append(issues, issue.resource())
	}
	page["issues"] = issues
	if end < len(matched) {
		page["nextPageToken"] = base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d:%s", end, jql))
	}
	page["isLast"] = end >= len(matched)
	return http.StatusOK, page
}

func (site *Site) serveIssue(response http.ResponseWriter, idOrKey string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	for _, issue := range site.issues {
		if issue.key == idOrKey || issue.id == idOrKey {
			writeJSON(response, http.StatusOK, issue.resource())
			return
		}
	}
	writeJSON(response, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist"}})
}

func (issue *storedIssue) resource() map[string]any {
	return map[string]any{"id": issue.id, "key": issue.key, "fields": map[string]any{
		"summary":   issue.summary,
		"status":    map[string]any{"id": "1", "name": "To Do", "statusCategory": map[string]any{"key": "new"}},
		"issuetype": map[string]any{"id": "10001", "name": "Task"},
		"project":   map[string]any{"id": issue.project.ID, "key": issue.project.Key, "name": issue.project.Name},
		"labels":    []string{},
		"created":   issue.created.In(jiraZone).Format("2006-01-02T15:04:05.000-0700"),
		"updated":   issue.updated.In(jiraZone).Format("2006-01-02T15:04:05.000-0700"),
	}}
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// The test client reads the body; a failed write surfaces there as a transport failure.
	_ = json.NewEncoder(response).Encode(payload)
}

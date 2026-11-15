// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package confluencepagefake is an in-process Confluence Cloud site for pageChanged Trigger tests. It
// serves the accessible-resources lookup, CQL content search over pages with the subset of CQL the
// Trigger sends, and page reads in storage format. It is credential-safe: it checks a bearer token but
// never echoes it.
package confluencepagefake

import (
	"encoding/json"
	"fmt"
	"html"
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

// AccessToken is the bearer token the fake accepts.
const AccessToken = "confluence-fake-access-token"

const siteBase = "https://ops.atlassian.net/wiki"

var (
	spaceClausePattern        = regexp.MustCompile(`space = "([^"]+)"`)
	lastModifiedClausePattern = regexp.MustCompile(`lastmodified >= "(\d{4}-\d{2}-\d{2} \d{2}:\d{2})"`)
)

// Site is one fake Confluence site. Every method is safe for concurrent use.
type Site struct {
	// URL is the Atlassian gateway base URL to configure as the connector endpoint.
	URL     string
	cloudID string
	server  *httptest.Server

	mutex             sync.Mutex
	pages             map[string]*page
	nextPageID        int
	cursorGeneration  int
	throttledSearches int
	retryAfterSeconds int
	searchRequests    int
	cqlQueries        []string
}

type page struct {
	id             string
	spaceKey       string
	title          string
	body           string
	version        int
	lastModifiedAt time.Time
	createdAt      time.Time
}

// New starts a site with the given cloud ID and stops it when the test ends.
func New(t *testing.T, cloudID string) *Site {
	t.Helper()
	site := &Site{cloudID: cloudID, pages: map[string]*page{}, nextPageID: 600000}
	site.server = httptest.NewServer(http.HandlerFunc(site.serveHTTP))
	site.URL = site.server.URL
	t.Cleanup(site.server.Close)
	return site
}

// Publish creates a page at version 1 and returns its ID.
func (site *Site) Publish(spaceKey string, title string, body string) string {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.nextPageID++
	now := time.Now().UTC()
	created := &page{id: strconv.Itoa(site.nextPageID), spaceKey: spaceKey, title: title, body: body, version: 1, lastModifiedAt: now, createdAt: now}
	site.pages[created.id] = created
	return created.id
}

// Edit saves the next version of a page with a new title and body.
func (site *Site) Edit(pageID string, title string, body string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	edited := site.pages[pageID]
	edited.title, edited.body, edited.version, edited.lastModifiedAt = title, body, edited.version+1, time.Now().UTC()
}

// ThrottleSearches makes the next count searches answer HTTP 429 with Retry-After in seconds.
func (site *Site) ThrottleSearches(count int, retryAfterSeconds int) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.throttledSearches, site.retryAfterSeconds = count, retryAfterSeconds
}

// ExpireSearchCursors makes every search cursor issued so far answer HTTP 400.
func (site *Site) ExpireSearchCursors() {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.cursorGeneration++
}

// SearchRequests returns how many content searches the site answered, including throttled ones.
func (site *Site) SearchRequests() int {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return site.searchRequests
}

// CQLQueries returns every CQL query searched, in order.
func (site *Site) CQLQueries() []string {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	return append([]string(nil), site.cqlQueries...)
}

func (site *Site) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		writeJSON(response, http.StatusUnauthorized, map[string]any{"message": "Unauthorized"})
		return
	}
	prefix := "/ex/confluence/" + site.cloudID + "/wiki"
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/oauth/token/accessible-resources":
		writeJSON(response, http.StatusOK, []map[string]any{{"id": site.cloudID, "name": "Ops", "url": "https://ops.atlassian.net", "scopes": []string{"search:confluence", "read:page:confluence"}}})
	case request.Method == http.MethodGet && request.URL.Path == prefix+"/rest/api/content/search":
		site.serveSearch(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, prefix+"/api/v2/pages/"):
		site.servePage(response, strings.TrimPrefix(request.URL.Path, prefix+"/api/v2/pages/"))
	default:
		writeJSON(response, http.StatusNotFound, map[string]any{"message": "Not Found"})
	}
}

func (site *Site) serveSearch(response http.ResponseWriter, request *http.Request) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	site.searchRequests++
	if site.throttledSearches > 0 {
		site.throttledSearches--
		response.Header().Set("Retry-After", strconv.Itoa(site.retryAfterSeconds))
		writeJSON(response, http.StatusTooManyRequests, map[string]any{"message": "Rate limit exceeded"})
		return
	}
	cql := request.URL.Query().Get("cql")
	site.cqlQueries = append(site.cqlQueries, cql)
	matches, err := site.matchingPages(cql)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]any{"message": err.Error()})
		return
	}
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		limit = 25
	}
	offset := 0
	if cursor := request.URL.Query().Get("cursor"); cursor != "" {
		if offset, err = site.decodeCursor(cursor, cql); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]any{"message": "Invalid cursor"})
			return
		}
	}
	end := min(offset+limit, len(matches))
	results := []map[string]any{}
	for _, match := range matches[min(offset, end):end] {
		results = append(results, map[string]any{
			"id": match.id, "type": "page", "status": "current", "title": match.title,
			"space":   map[string]any{"id": 98306, "key": match.spaceKey},
			"version": map[string]any{"number": match.version, "when": match.lastModifiedAt.Format(time.RFC3339Nano)},
			"_links":  map[string]any{"webui": "/spaces/" + match.spaceKey + "/pages/" + match.id},
		})
	}
	links := map[string]any{"base": siteBase}
	if end < len(matches) {
		links["next"] = "/rest/api/content/search?cql=" + strings.ReplaceAll(cql, " ", "+") + "&cursor=" + site.encodeCursor(end, cql)
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": results, "_links": links})
}

// matchingPages applies the CQL subset the Trigger sends; the site's time zone is UTC.
func (site *Site) matchingPages(cql string) ([]*page, error) {
	if !strings.HasPrefix(cql, "type = page") || !strings.HasSuffix(cql, "ORDER BY lastmodified DESC") {
		return nil, fmt.Errorf("unsupported CQL %q", cql)
	}
	spaceKey := ""
	if match := spaceClausePattern.FindStringSubmatch(cql); match != nil {
		spaceKey = match[1]
	}
	var lowerBound time.Time
	if match := lastModifiedClausePattern.FindStringSubmatch(cql); match != nil {
		parsed, err := time.Parse("2006-01-02 15:04", match[1])
		if err != nil {
			return nil, err
		}
		lowerBound = parsed
	}
	var matches []*page
	for _, candidate := range site.pages {
		if (spaceKey == "" || candidate.spaceKey == spaceKey) && !candidate.lastModifiedAt.Before(lowerBound) {
			matches = append(matches, candidate)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if !matches[i].lastModifiedAt.Equal(matches[j].lastModifiedAt) {
			return matches[i].lastModifiedAt.After(matches[j].lastModifiedAt)
		}
		return matches[i].id < matches[j].id
	})
	return matches, nil
}

func (site *Site) encodeCursor(offset int, cql string) string {
	return fmt.Sprintf("g%d-o%d-q%d", site.cursorGeneration, offset, len(cql))
}

func (site *Site) decodeCursor(cursor string, cql string) (int, error) {
	var generation, offset, length int
	if _, err := fmt.Sscanf(cursor, "g%d-o%d-q%d", &generation, &offset, &length); err != nil {
		return 0, err
	}
	if generation != site.cursorGeneration || length != len(cql) {
		return 0, fmt.Errorf("cursor expired")
	}
	return offset, nil
}

func (site *Site) servePage(response http.ResponseWriter, pageID string) {
	site.mutex.Lock()
	defer site.mutex.Unlock()
	found, ok := site.pages[pageID]
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"id": found.id, "status": "current", "title": found.title, "spaceId": "98306",
		"createdAt": found.createdAt.Format(time.RFC3339Nano),
		"version":   map[string]any{"number": found.version, "createdAt": found.lastModifiedAt.Format(time.RFC3339Nano)},
		"body":      map[string]any{"storage": map[string]any{"representation": "storage", "value": "<p>" + html.EscapeString(found.body) + "</p>"}},
		"_links":    map[string]any{"webui": "/spaces/" + found.spaceKey + "/pages/" + found.id, "base": siteBase},
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// A failed write leaves the client with a truncated body, which the connector must classify anyway.
	_ = json.NewEncoder(response).Encode(value)
}

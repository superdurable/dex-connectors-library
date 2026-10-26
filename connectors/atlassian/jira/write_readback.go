// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const writeMarkerPropertyKey = "dex.connector.call"

type writeMarkerProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type writeMarkerPropertyResponse struct {
	Value string `json:"value"`
}

type issueReadbackPage struct {
	Issues []struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
		} `json:"fields"`
	} `json:"issues"`
}

type commentReadbackPage struct {
	Comments []commentResource `json:"comments"`
}

func writeMarker(call sdkgo.Call) writeMarkerProperty {
	digest := sha256.Sum256([]byte(call.ID))
	return writeMarkerProperty{Key: writeMarkerPropertyKey, Value: hex.EncodeToString(digest[:])}
}

func (client *Client) exchangeWrite(session *operationSession, request jiraRequest) exchangeResult {
	deadline, hasDeadline := session.context.Deadline()
	if !hasDeadline {
		return client.exchange(session, request)
	}
	remaining := time.Until(deadline)
	if remaining < time.Second {
		return exchangeResult{err: context.DeadlineExceeded}
	}
	postContext, cancel := context.WithTimeout(session.context, remaining*3/4)
	defer cancel()
	postSession := *session
	postSession.context = postContext
	return client.exchange(&postSession, request)
}

func (client *Client) findCreatedIssue(session *operationSession, requested CreateIssueOutput, marker writeMarkerProperty) (createdIssueResource, bool) {
	project := requested.ProjectKey
	if project == "" {
		project = requested.ProjectID
	}
	quotedProject, err := QuoteJQLString(project)
	if err != nil {
		return createdIssueResource{}, false
	}
	summaryClause, err := buildSummaryPhraseClause(requested.Summary)
	if err != nil {
		return createdIssueResource{}, false
	}
	search := searchRequestBody{
		JQL:    "project = " + quotedProject + " AND " + summaryClause + " ORDER BY created DESC",
		Fields: []string{"summary"}, MaxResults: 50,
	}
	result := client.exchange(session, jiraRequest{method: http.MethodPost, path: enhancedSearchPath, payload: search})
	if result.err != nil || !isSuccessStatus(result.response.statusCode) || result.response.hasBodyReadFailed || result.response.isBodyTooLarge || result.response.reflectsSecret {
		return createdIssueResource{}, false
	}
	var page issueReadbackPage
	if json.Unmarshal(result.response.body, &page) != nil {
		return createdIssueResource{}, false
	}
	for _, candidate := range page.Issues {
		if candidate.Fields.Summary != requested.Summary || !numericIDPattern.MatchString(candidate.ID) || !issueKeyPattern.MatchString(candidate.Key) {
			continue
		}
		propertyPath := issuePath(candidate.ID) + "/properties/" + writeMarkerPropertyKey
		if client.hasMatchingWriteMarker(session, propertyPath, marker.Value) {
			return createdIssueResource{ID: candidate.ID, Key: candidate.Key}, true
		}
	}
	return createdIssueResource{}, false
}

func (client *Client) findAddedComment(session *operationSession, issueIDOrKey string, marker writeMarkerProperty) (commentResource, bool) {
	query := url.Values{"maxResults": {"50"}, "orderBy": {"-created"}}
	result := client.exchange(session, jiraRequest{method: http.MethodGet, path: issuePath(issueIDOrKey) + "/comment", query: query})
	if result.err != nil || !isSuccessStatus(result.response.statusCode) || result.response.hasBodyReadFailed || result.response.isBodyTooLarge || result.response.reflectsSecret {
		return commentResource{}, false
	}
	var page commentReadbackPage
	if json.Unmarshal(result.response.body, &page) != nil {
		return commentResource{}, false
	}
	for _, candidate := range page.Comments {
		if !numericIDPattern.MatchString(candidate.ID) {
			continue
		}
		propertyPath := "/comment/" + url.PathEscape(candidate.ID) + "/properties/" + writeMarkerPropertyKey
		if client.hasMatchingWriteMarker(session, propertyPath, marker.Value) {
			return candidate, true
		}
	}
	return commentResource{}, false
}

func (client *Client) hasMatchingWriteMarker(session *operationSession, path string, expected string) bool {
	result := client.exchange(session, jiraRequest{method: http.MethodGet, path: path})
	if result.err != nil || result.response.statusCode != http.StatusOK || result.response.hasBodyReadFailed || result.response.isBodyTooLarge || result.response.reflectsSecret {
		return false
	}
	var property writeMarkerPropertyResponse
	return json.Unmarshal(result.response.body, &property) == nil && strings.EqualFold(property.Value, expected)
}

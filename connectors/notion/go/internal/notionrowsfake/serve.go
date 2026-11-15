// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notionrowsfake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Token is the only API token the fake accepts; it is test data, not a credential.
const Token = "ntn_fake_rows_token"

const notionVersion = "2026-03-11"

func (fake *DataSource) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	recorded := Request{Method: request.Method, Path: request.URL.Path}
	status, body := fake.answer(request, &recorded)
	recorded.Status = status
	fake.requests = append(fake.requests, recorded)
	fake.mutex.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	if status == http.StatusTooManyRequests {
		writer.Header().Set("Retry-After", fake.retryAfter)
	}
	writer.WriteHeader(status)
	// The test client reads the whole body; a write failure only fails that request.
	_, _ = io.WriteString(writer, body)
}

func (fake *DataSource) answer(request *http.Request, recorded *Request) (int, string) {
	if request.Header.Get("Authorization") != "Bearer "+Token || request.Header.Get("Notion-Version") != notionVersion {
		return http.StatusUnauthorized, notionError(401, "unauthorized")
	}
	if fake.rateLimited > 0 {
		fake.rateLimited--
		return http.StatusTooManyRequests, notionError(429, "rate_limited")
	}
	queryPath := "/v1/data_sources/" + fake.dataSourceID + "/query"
	switch {
	case request.Method == http.MethodPost && request.URL.Path == queryPath:
		var body QueryBody
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return http.StatusBadRequest, notionError(400, "invalid_json")
		}
		recorded.Body = &body
		return fake.answerQuery(body)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/query"):
		return http.StatusNotFound, notionError(404, "object_not_found")
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/pages/"):
		row, exists := fake.rows[strings.TrimPrefix(request.URL.Path, "/v1/pages/")]
		if !exists {
			return http.StatusNotFound, notionError(404, "object_not_found")
		}
		return http.StatusOK, fake.pageResource(row)
	default:
		return http.StatusNotFound, notionError(404, "object_not_found")
	}
}

func (fake *DataSource) answerQuery(body QueryBody) (int, string) {
	if len(body.Sorts) != 1 || body.Sorts[0].Timestamp != "last_edited_time" || body.PageSize < 1 || body.PageSize > 100 {
		return http.StatusBadRequest, notionError(400, "validation_error")
	}
	offset := 0
	if body.StartCursor != "" {
		parsed, err := strconv.Atoi(strings.TrimPrefix(body.StartCursor, "rows-"))
		if fake.isStartCursorStale || err != nil || !strings.HasPrefix(body.StartCursor, "rows-") {
			fake.isStartCursorStale = false
			return http.StatusBadRequest, notionError(400, "validation_error")
		}
		offset = parsed
	}
	var lowerBound time.Time
	if body.Filter != nil {
		parsed, err := time.Parse(time.RFC3339Nano, body.Filter.LastEditedTime.OnOrAfter)
		if body.Filter.Timestamp != "last_edited_time" || err != nil {
			return http.StatusBadRequest, notionError(400, "validation_error")
		}
		lowerBound = parsed
	}
	var matched []*storedRow
	for _, row := range fake.rows {
		if !row.isInTrash && !fake.hiddenRows[row.id] && !row.lastEdited.Before(lowerBound) {
			matched = append(matched, row)
		}
	}
	isDescending := body.Sorts[0].Direction == "descending"
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].lastEdited.Equal(matched[j].lastEdited) {
			return matched[i].lastEdited.Before(matched[j].lastEdited) != isDescending
		}
		return matched[i].id < matched[j].id
	})
	end := min(offset+body.PageSize, len(matched))
	offset = min(offset, end)
	results := make([]string, 0, end-offset)
	for _, row := range matched[offset:end] {
		results = append(results, fake.pageResource(row))
	}
	nextCursor, hasMore := "null", end < len(matched)
	if hasMore {
		nextCursor = strconv.Quote("rows-" + strconv.Itoa(end))
	}
	return http.StatusOK, fmt.Sprintf(`{"object":"list","results":[%s],"next_cursor":%s,"has_more":%t,"type":"page_or_data_source"}`,
		strings.Join(results, ","), nextCursor, hasMore)
}

func (fake *DataSource) pageResource(row *storedRow) string {
	return fmt.Sprintf(`{"object":"page","id":%q,"created_time":%q,"last_edited_time":%q,"in_trash":%t,"is_archived":false,`+
		`"url":"https://www.notion.so/%s","parent":{"type":"data_source_id","data_source_id":%q,"database_id":%q},`+
		`"properties":{"Name":{"id":"title","type":"title","title":[{"type":"text","text":{"content":%q},"plain_text":%q}]}}}`,
		row.id, notionTime(row.created), notionTime(row.lastEdited), row.isInTrash, strings.ReplaceAll(row.id, "-", ""),
		fake.dataSourceID, fake.databaseID, row.title, row.title)
}

func notionTime(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000Z") }

func notionError(status int, code string) string {
	return fmt.Sprintf(`{"object":"error","status":%d,"code":%q,"request_id":"fake-request"}`, status, code)
}

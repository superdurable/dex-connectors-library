// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	databaseRowTriggerOperationID = "databaseRowChanged"
	databaseRowTriggerSubject     = "databaseRowChanged data source query"
	lastEditedTimeTimestamp       = "last_edited_time"
)

// errStaleStartCursor reports a 400 validation_error for a stored start cursor; the query restarts.
var errStaleStartCursor = errors.New("Notion rejected the stored query start cursor")

// databaseRowQuery is one data source query sorted by last edited time.
type databaseRowQuery struct {
	editedOnOrAfter *time.Time
	isNewestFirst   bool
	startCursor     string
	pageSize        int
}

// queriedDatabaseRow is one decoded row with its parsed last edited time.
type queriedDatabaseRow struct {
	page           Page
	lastEditedTime time.Time
}

// queriedDatabaseRows is one page of rows; nextCursor is empty on the last page and at Notion's
// 10,000-row query limit.
type queriedDatabaseRows struct {
	rows       []queriedDatabaseRow
	nextCursor string
}

type databaseRowQueryBody struct {
	Filter      map[string]any      `json:"filter,omitempty"`
	Sorts       []map[string]string `json:"sorts"`
	StartCursor string              `json:"start_cursor,omitempty"`
	PageSize    int                 `json:"page_size"`
	ResultType  string              `json:"result_type"`
}

// queryDatabaseRowsForTrigger sends one data source query for a poller. A 429 or 529 is marked rate
// limited with Notion's Retry-After seconds; every other failure is a plain error the kit backs off on.
func (client *Client) queryDatabaseRowsForTrigger(ctx context.Context, call sdkgo.Call, dataSourceID string, query databaseRowQuery) (queriedDatabaseRows, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return queriedDatabaseRows{}, err
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return queriedDatabaseRows{}, errors.New("Notion databaseRowChanged credentials are unavailable; save the connection's API token again")
	}
	session := &operationSession{context: ctx, call: call, credentials: credentials}
	result := client.exchange(session, notionRequest{
		method: http.MethodPost, path: "/data_sources/" + url.PathEscape(dataSourceID) + "/query", payload: query.requestBody(),
	})
	if query.startCursor != "" && result.err == nil && result.response.statusCode == http.StatusBadRequest &&
		readErrorToken(result.response.body, "/code") == "validation_error" {
		return queriedDatabaseRows{}, errStaleStartCursor
	}
	classification := client.classifyRead(databaseRowTriggerOperationID, databaseRowTriggerSubject, result)
	switch {
	case classification.outcome == readSucceeded:
	case classification.outcome == readRetry && classification.failure.Kind == sdkgo.FailureRateLimit:
		return queriedDatabaseRows{}, pollingtrigger.MarkRateLimited(errors.New(classification.failure.Message), classification.retryAfter)
	case classification.outcome == readNotFound:
		return queriedDatabaseRows{}, errors.New("Notion could not find the watched data source; share its database with the connection or pick another data source")
	default:
		return queriedDatabaseRows{}, errors.New(classification.failure.Message)
	}
	return decodeDatabaseRowsPage(result.response.body, query.pageSize)
}

func (query databaseRowQuery) requestBody() databaseRowQueryBody {
	direction := "ascending"
	if query.isNewestFirst {
		direction = "descending"
	}
	body := databaseRowQueryBody{
		Sorts:       []map[string]string{{"timestamp": lastEditedTimeTimestamp, "direction": direction}},
		StartCursor: query.startCursor, PageSize: query.pageSize, ResultType: "page",
	}
	if query.editedOnOrAfter != nil {
		body.Filter = map[string]any{
			"timestamp":             lastEditedTimeTimestamp,
			lastEditedTimeTimestamp: map[string]string{"on_or_after": query.editedOnOrAfter.UTC().Format(time.RFC3339Nano)},
		}
	}
	return body
}

func decodeDatabaseRowsPage(body []byte, pageSize int) (queriedDatabaseRows, error) {
	var page queryResponseBody
	if err := json.Unmarshal(body, &page); err != nil || len(page.Results) > pageSize {
		return queriedDatabaseRows{}, errors.New("Notion returned an invalid databaseRowChanged query page")
	}
	nextCursor, err := validateCursor(dereferenceText(page.NextCursor))
	if err != nil {
		return queriedDatabaseRows{}, errors.New("Notion returned an invalid databaseRowChanged query cursor")
	}
	decoded := queriedDatabaseRows{rows: make([]queriedDatabaseRow, 0, len(page.Results))}
	for index, resource := range page.Results {
		row, err := decodePage(resource)
		if err != nil {
			return queriedDatabaseRows{}, fmt.Errorf("Notion returned an invalid row %d in the databaseRowChanged query page", index)
		}
		lastEditedTime, err := time.Parse(time.RFC3339Nano, row.LastEditedTime)
		if err != nil {
			return queriedDatabaseRows{}, fmt.Errorf("Notion returned row %d without a valid last_edited_time", index)
		}
		decoded.rows = append(decoded.rows, queriedDatabaseRow{page: row, lastEditedTime: lastEditedTime.UTC()})
	}
	if page.HasMore {
		decoded.nextCursor = nextCursor
	}
	return decoded, nil
}

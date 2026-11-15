// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// recordChangedFilterLookback widens the server-side filter, so formula date rounding never hides a
	// change; the fetcher drops already processed records itself.
	recordChangedFilterLookback = time.Second
	// maximumRecordChangedScanPages bounds the pages one fetch follows past already processed records;
	// the open scan resumes on the next fetch.
	maximumRecordChangedScanPages = 20
	// lastModifiedTimeFieldType is Airtable's field type name for a Last modified time field.
	lastModifiedTimeFieldType = "lastModifiedTime"
)

// errListOffsetExpired reports 422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE for a page offset.
var errListOffsetExpired = errors.New("Airtable no longer recognizes the list offset")

// recordChangedFetcher lists one table's records by its Last modified time field, oldest change first.
type recordChangedFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration RecordChangedTriggerConfiguration
	// fieldName is the watched field's current name, resolved from the base schema; blank until resolved.
	fieldName string
}

func (client *Client) newRecordChangedFetcher(binding sdkgo.TriggerBindingRef, configuration RecordChangedTriggerConfiguration) *recordChangedFetcher {
	return &recordChangedFetcher{
		client: client, configuration: configuration,
		call: pollingtrigger.NewTriggerCredentialCall(binding.Connection, binding.Trigger),
	}
}

// SeedCursor positions the cursor at the table's most recent change, so existing records are not delivered.
func (fetcher *recordChangedFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	if err := fetcher.resolveFieldName(ctx); err != nil {
		return pollingtrigger.Cursor{}, err
	}
	records, nextOffset, err := fetcher.listRecords(ctx, time.Time{}, SortDirectionDescending, "")
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	position := recordChangedPosition{fieldID: fetcher.configuration.LastModifiedTimeFieldID}
	if nextOffset != "" && len(records) > 0 {
		// Older existing records than this page, including its oldest time's unlisted ties, stay hidden.
		oldest := records[0].modifiedAt
		for _, changed := range records {
			if changed.modifiedAt.Before(oldest) {
				oldest = changed.modifiedAt
			}
		}
		position.floor = oldest.Truncate(time.Millisecond).Add(time.Millisecond)
	}
	for _, changed := range records {
		position.deliver(changed)
	}
	return encodeRecordChangedCursor(position)
}

// FetchChanges lists records modified since the cursor's lower bound, oldest first, and returns those not
// yet delivered. An open scan resumes from its stored Airtable offset.
func (fetcher *recordChangedFetcher) FetchChanges(
	ctx context.Context,
	cursor pollingtrigger.Cursor,
) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[RecordChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[RecordChangedEvent]]
	position, err := decodeRecordChangedCursor(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.fieldID != fetcher.configuration.LastModifiedTimeFieldID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("the binding now watches a different Last modified time field"))
	}
	if err := fetcher.resolveFieldName(ctx); err != nil {
		return page, err
	}
	since, offset := position.filterSince(), ""
	if position.scan != nil {
		since, offset = mustParseCursorTime(position.scan.Since), position.scan.Offset
	}
	hasRestartedScan := false
	for scannedPages := 0; scannedPages < maximumRecordChangedScanPages; scannedPages++ {
		records, nextOffset, err := fetcher.listRecords(ctx, since, SortDirectionAscending, offset)
		if errors.Is(err, errListOffsetExpired) && offset != "" && !hasRestartedScan {
			// Airtable iterators time out; the scan starts again, and remembered records stay skipped.
			hasRestartedScan, position.scan = true, nil
			since, offset = position.filterSince(), ""
			continue
		}
		if err != nil {
			return page, err
		}
		for _, changed := range records {
			if position.isDelivered(changed) {
				continue
			}
			page.Changes = append(page.Changes, fetcher.newEvent(changed))
			position.deliver(changed)
		}
		position.scan = nil
		if nextOffset == "" {
			position.closeBurst()
		} else {
			position.scan = &recordChangedScan{Since: formatCursorTime(since), Offset: nextOffset}
		}
		if len(page.Changes) > 0 || nextOffset == "" {
			page.HasMorePages = nextOffset != ""
			page.Next, err = encodeRecordChangedCursor(position)
			return page, err
		}
		offset = nextOffset
	}
	// A long run of delivered records: commit the open scan so the next fetch continues it.
	page.HasMorePages = true
	page.Next, err = encodeRecordChangedCursor(position)
	return page, err
}

// filterSince is the listRecords filter bound: the lower bound minus the formula rounding lookback.
func (position *recordChangedPosition) filterSince() time.Time {
	lowerBound := position.lowerBound()
	if lowerBound.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return lowerBound.Add(-recordChangedFilterLookback).Truncate(time.Millisecond)
}

// mustParseCursorTime parses a time decodeRecordChangedCursor already validated.
func mustParseCursorTime(text string) time.Time {
	at, err := parseCursorTime(text)
	if err != nil {
		panic(err)
	}
	return at
}

// changedRecord is one listed record with its parsed Last modified time.
type changedRecord struct {
	record     Record
	modifiedAt time.Time
}

func (fetcher *recordChangedFetcher) newEvent(changed changedRecord) sdkgo.TriggerEvent[RecordChangedEvent] {
	return sdkgo.TriggerEvent[RecordChangedEvent]{
		ID: recordChangedEventID(changed.record.ID, changed.modifiedAt), OccurredAt: changed.modifiedAt,
		Payload: RecordChangedEvent{
			BaseID: fetcher.configuration.BaseID, TableID: fetcher.configuration.TableID,
			LastModifiedTime: changed.modifiedAt, Record: changed.record,
			IsCreatedRecord: changed.modifiedAt.Sub(changed.record.CreatedTime).Abs() <= time.Second,
		},
	}
}

// listRecords reads one page sorted by the watched field, of records whose field value is after since.
func (fetcher *recordChangedFetcher) listRecords(
	ctx context.Context, since time.Time, direction SortDirection, offset string,
) ([]changedRecord, string, error) {
	if since.IsZero() {
		since = time.Unix(0, 0).UTC()
	}
	fieldID := fetcher.configuration.LastModifiedTimeFieldID
	body, err := json.Marshal(listRecordsRequestBody{
		PageSize: fetcher.client.pollPageSize, Offset: offset,
		FilterByFormula: `IS_AFTER({` + fieldID + `}, DATETIME_PARSE("` + since.Format("2006-01-02T15:04:05.000Z") + `"))`,
		Sort:            []RecordSort{{Field: fieldID, Direction: direction}},
	})
	if err != nil {
		return nil, "", err
	}
	response, err := fetcher.exchange(ctx, providerRequest{
		method: http.MethodPost, path: recordPath(fetcher.configuration.BaseID, fetcher.configuration.TableID) + "/listRecords",
		baseID: fetcher.configuration.BaseID, body: body,
	})
	if err != nil {
		return nil, "", err
	}
	page, err := decodeRecordPage(response.body, fetcher.client.pollPageSize)
	if err != nil {
		return nil, "", err
	}
	records := make([]changedRecord, 0, len(page.Records))
	for _, record := range page.Records {
		modifiedAt, err := fetcher.readLastModifiedTime(record)
		if err != nil {
			return nil, "", err
		}
		records = append(records, changedRecord{record: record, modifiedAt: modifiedAt})
	}
	return records, page.Offset, nil
}

// readLastModifiedTime parses the watched field of a record the filter matched, so the field has a value.
func (fetcher *recordChangedFetcher) readLastModifiedTime(record Record) (time.Time, error) {
	value, isPresent := record.Fields[fetcher.fieldName]
	if !isPresent {
		// The field was renamed since the schema was read; the next poll reads the schema again.
		fetcher.fieldName = ""
		return time.Time{}, errors.New("Airtable returned a changed record without the Last modified time field; the field name is read again")
	}
	text, isText := value.Text()
	modifiedAt, err := time.Parse(time.RFC3339Nano, text)
	if !isText || err != nil {
		return time.Time{}, errors.New("Airtable returned a Last modified time value without a time of day")
	}
	return modifiedAt.UTC(), nil
}

// tablesSchema is the part of GET /v0/meta/bases/{baseId}/tables the fetcher reads.
type tablesSchema struct {
	Tables []struct {
		ID     string `json:"id"`
		Fields []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Options struct {
				Result *struct {
					Type string `json:"type"`
				} `json:"result"`
			} `json:"options"`
		} `json:"fields"`
	} `json:"tables"`
}

// resolveFieldName reads the base schema once, and again after a rename, to find the watched field's
// current name, since listed records key their fields by name.
func (fetcher *recordChangedFetcher) resolveFieldName(ctx context.Context) error {
	if fetcher.fieldName != "" {
		return nil
	}
	response, err := fetcher.exchange(ctx, providerRequest{
		method: http.MethodGet, path: "/v0/meta/bases/" + url.PathEscape(fetcher.configuration.BaseID) + "/tables",
		baseID: fetcher.configuration.BaseID,
	})
	if err != nil {
		return err
	}
	var schema tablesSchema
	if err := decodeResponse(response.body, &schema); err != nil {
		return err
	}
	for _, table := range schema.Tables {
		if table.ID != fetcher.configuration.TableID {
			continue
		}
		for _, field := range table.Fields {
			if field.ID != fetcher.configuration.LastModifiedTimeFieldID {
				continue
			}
			if field.Type != lastModifiedTimeFieldType || field.Name == "" {
				return errors.New("the Airtable recordChanged field is not a Last modified time field")
			}
			if field.Options.Result != nil && field.Options.Result.Type != "dateTime" {
				return errors.New("the Airtable recordChanged Last modified time field must include a time of day")
			}
			fetcher.fieldName = field.Name
			return nil
		}
		return errors.New("the Airtable table has no field with the recordChanged field ID")
	}
	return errors.New("the Airtable base schema has no table with the recordChanged table ID")
}

// exchange waits on the runner's request limiter, then sends one paced request through the Client.
func (fetcher *recordChangedFetcher) exchange(ctx context.Context, request providerRequest) (providerResponse, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return providerResponse{}, err
	}
	response, outcome := fetcher.client.exchangeWithContext(ctx, fetcher.call, RecordChangedTriggerDefinition.Trigger.TriggerName, request, request.baseID)
	if outcome == nil {
		return response, nil
	}
	if outcome.disposition == outcomeOffsetExpired {
		return response, errListOffsetExpired
	}
	err := errors.New(outcome.failure.Message)
	if outcome.disposition == outcomeRetry && outcome.failure.Kind == sdkgo.FailureRateLimit {
		// 429 and a full per-base pacing queue pause every binding of the connection.
		return response, pollingtrigger.MarkRateLimited(err, outcome.retryAfter)
	}
	return response, err
}

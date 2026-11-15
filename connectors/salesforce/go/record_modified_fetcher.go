// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	recordModifiedPolledTriggerID = "recordModifiedPolled"
	// modstampSettleDelay leaves recent SystemModstamp values unread, so a transaction that commits a few
	// seconds after its stamp is still read after the cursor passed earlier records.
	modstampSettleDelay = 15 * time.Second
	// serverClockTolerance absorbs the HTTP Date header's whole-second truncation.
	serverClockTolerance = time.Second
)

// recordPollFetcher reads one object's records in (SystemModstamp, Id) order for the poll Trigger kit.
type recordPollFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration RecordModifiedTriggerConfiguration

	// serverClockOffset is how far Salesforce's clock was behind this host's at the last response, or zero.
	serverClockMutex  sync.Mutex
	serverClockOffset time.Duration
}

// recordPollCursor is the committed Trigger cursor: every record ordered at or before (SystemModstamp,
// RecordID) was read. A blank RecordID means every record stamped at or before SystemModstamp was read.
type recordPollCursor struct {
	SObjectType    string `json:"sObjectType"`
	SystemModstamp string `json:"systemModstamp"`
	RecordID       string `json:"recordId,omitempty"`
}

func newRecordPollFetcher(client *Client, connection sdkgo.ConnectionRef, configuration RecordModifiedTriggerConfiguration) *recordPollFetcher {
	return &recordPollFetcher{
		client:        client,
		call:          pollingtrigger.NewTriggerCredentialCall(connection, RecordModifiedPolledTriggerDefinition.Trigger),
		configuration: configuration,
	}
}

// SeedCursor positions the cursor at the newest record stamped before the settle horizon, or at the
// horizon for an empty object, so existing records become the baseline and are never delivered.
func (fetcher *recordPollFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	horizon := fetcher.settleHorizon()
	cursor, err := fetcher.seedCursorAt(ctx, horizon)
	if err != nil {
		return cursor, err
	}
	// A Salesforce clock behind this host's must not seed past seconds Salesforce has not stamped yet.
	if serverHorizon := fetcher.settleHorizon(); serverHorizon.Before(horizon) {
		return fetcher.seedCursorAt(ctx, serverHorizon)
	}
	return cursor, nil
}

func (fetcher *recordPollFetcher) seedCursorAt(ctx context.Context, horizon time.Time) (pollingtrigger.Cursor, error) {
	statement, err := renderSOQL("SELECT Id, SystemModstamp FROM :object WHERE SystemModstamp <= :horizon ORDER BY SystemModstamp DESC, Id DESC LIMIT 1",
		map[string]SOQLValue{"object": SOQLIdentifier(fetcher.configuration.SObjectType), "horizon": SOQLDateTime(horizon)})
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("Salesforce baseline query could not be built: %w", err)
	}
	page, err := fetcher.query(ctx, statement)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	position := recordPollCursor{SObjectType: fetcher.configuration.SObjectType, SystemModstamp: horizon.Format(time.RFC3339)}
	if len(page.Records) > 0 {
		modstamp, err := polledRecordModstamp(page.Records[0])
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		position.SystemModstamp, position.RecordID = modstamp.Format(time.RFC3339), page.Records[0].ID
	}
	return encodeRecordPollCursor(position)
}

// FetchChanges reads up to pollPageSize records ordered after cursor and stamped before the settle
// horizon. An undecodable cursor, or one for another object, is marked invalid so the kit reseeds.
func (fetcher *recordPollFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[RecordModifiedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[RecordModifiedEvent]]
	position, after, err := decodeRecordPollCursor(cursor)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.SObjectType != fetcher.configuration.SObjectType {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Salesforce poll cursor belongs to another object"))
	}
	horizon := fetcher.settleHorizon()
	if !after.Before(horizon) {
		page.Next = cursor
		return page, nil
	}
	statement, err := fetcher.changesStatement(position, after, horizon)
	if err != nil {
		return page, fmt.Errorf("Salesforce poll query could not be built: %w", err)
	}
	listed, err := fetcher.query(ctx, statement)
	if err != nil {
		return page, err
	}
	if len(listed.Records) > fetcher.client.pollPageSize {
		return page, fmt.Errorf("Salesforce poll page has %d records, more than the SOQL LIMIT %d", len(listed.Records), fetcher.client.pollPageSize)
	}
	// The response may show Salesforce's clock behind; later records wait for a poll past its horizon.
	serverHorizon := fetcher.settleHorizon()
	next := position
	for _, record := range listed.Records {
		modstamp, err := polledRecordModstamp(record)
		if err != nil {
			return page, err
		}
		if !isAfterCursor(modstamp, record.ID, after, next.RecordID) {
			return page, errors.New("Salesforce poll page is not ordered by SystemModstamp after the cursor")
		}
		if modstamp.After(serverHorizon) {
			page.Next, err = encodeRecordPollCursor(next)
			return page, err
		}
		eventID, event := newRecordModifiedTriggerEvent(fetcher.configuration.SObjectType, record, modstamp, recordChangeKind(record))
		page.Changes = append(page.Changes, sdkgo.TriggerEvent[RecordModifiedEvent]{ID: eventID, OccurredAt: modstamp, Payload: event})
		after, next.SystemModstamp, next.RecordID = modstamp, modstamp.Format(time.RFC3339), record.ID
	}
	// A full page, or a page Salesforce split, may hide more records; the next page continues from next.
	page.HasMorePages = len(listed.Records) == fetcher.client.pollPageSize || !listed.IsDone
	page.Next, err = encodeRecordPollCursor(next)
	return page, err
}

// changesStatement selects the records ordered after the cursor; Id breaks ties within one second.
func (fetcher *recordPollFetcher) changesStatement(position recordPollCursor, after time.Time, horizon time.Time) (string, error) {
	bindings := map[string]SOQLValue{
		"object": SOQLIdentifier(fetcher.configuration.SObjectType), "after": SOQLDateTime(after), "horizon": SOQLDateTime(horizon),
	}
	condition := "SystemModstamp > :after"
	if position.RecordID != "" {
		condition = "(SystemModstamp > :after OR (SystemModstamp = :after AND Id > :recordId))"
		bindings["recordId"] = SOQLString(position.RecordID)
	}
	// Field names are validated API names, so joining them cannot inject an expression.
	return renderSOQL("SELECT "+strings.Join(fetcher.configuration.selectedFields(), ", ")+
		" FROM :object WHERE SystemModstamp <= :horizon AND "+condition+
		" ORDER BY SystemModstamp ASC, Id ASC LIMIT "+strconv.Itoa(fetcher.client.pollPageSize), bindings)
}

// query sends one SOQL request. REQUEST_LIMIT_EXCEEDED or 429 pauses the connection through the kit.
func (fetcher *recordPollFetcher) query(ctx context.Context, statement string) (QueryRecordsOutput, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return QueryRecordsOutput{}, err
	}
	client := fetcher.client
	credential, err := sdkgo.ResolveCredential(ctx, client.credentials, fetcher.call, client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return QueryRecordsOutput{}, errors.New("Salesforce authorization is revoked or expired; reauthorize the connection")
	}
	if err != nil || validateResolvedCredentials(credential) != nil {
		return QueryRecordsOutput{}, errors.New("Salesforce Trigger credentials are unavailable")
	}
	request := salesforceRequest{
		method: http.MethodGet, path: client.dataPath("query"), query: url.Values{"q": {statement}},
		responseLimit: client.maxResponseBytes,
		header:        http.Header{queryOptionsHeader: {"batchSize=" + strconv.Itoa(max(minimumQueryBatchSize, client.pollPageSize))}},
	}
	if len(request.query.Encode()) > maxEncodedQueryBytes {
		return QueryRecordsOutput{}, errors.New("Salesforce poll query is too long; select fewer fields")
	}
	response, err := client.sendRequest(ctx, fetcher.call, &credential, request)
	if err != nil {
		return QueryRecordsOutput{}, fmt.Errorf("Salesforce poll request failed: %s", err.Error())
	}
	fetcher.observeServerClock(response.header.Get("Date"))
	if response.status < 200 || response.status >= 300 {
		outcome := client.classifyFailedResponse(operationBranches{operationID: recordModifiedPolledTriggerID}, response)
		failure := fmt.Errorf("Salesforce poll request: %s", outcome.failure.Message)
		if outcome.failure.Kind == sdkgo.FailureRateLimit {
			// The daily API allocation and concurrent request limit both report REQUEST_LIMIT_EXCEEDED.
			return QueryRecordsOutput{}, pollingtrigger.MarkRateLimited(failure, outcome.retryAfter)
		}
		return QueryRecordsOutput{}, failure
	}
	output, err := client.decodeQueryResponse(response)
	if err != nil {
		return QueryRecordsOutput{}, errors.New("Salesforce returned an invalid poll query page")
	}
	return output, nil
}

// settleHorizon is the settle delay before the earlier of this host's and Salesforce's clocks.
func (fetcher *recordPollFetcher) settleHorizon() time.Time {
	fetcher.serverClockMutex.Lock()
	offset := fetcher.serverClockOffset
	fetcher.serverClockMutex.Unlock()
	return fetcher.client.now().UTC().Add(offset - modstampSettleDelay).Truncate(time.Second)
}

// observeServerClock records how far Salesforce's clock, from a response Date header, is behind this host's.
func (fetcher *recordPollFetcher) observeServerClock(dateHeader string) {
	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		return
	}
	offset := serverTime.Sub(fetcher.client.now())
	if offset > -serverClockTolerance {
		offset = 0
	}
	fetcher.serverClockMutex.Lock()
	fetcher.serverClockOffset = offset
	fetcher.serverClockMutex.Unlock()
}

// polledRecordModstamp requires an ID and a whole-second SystemModstamp, which SOQL dateTime literals match.
func polledRecordModstamp(record Record) (time.Time, error) {
	text, isPresent := record.StringField("SystemModstamp")
	if !isRecordID(record.ID) || !isPresent {
		return time.Time{}, errors.New("Salesforce poll record lacks Id or SystemModstamp")
	}
	modstamp, err := parseSalesforceDateTime(text)
	if err != nil || !modstamp.Equal(modstamp.Truncate(time.Second)) {
		return time.Time{}, errors.New("Salesforce poll record SystemModstamp is not a whole-second dateTime")
	}
	return modstamp, nil
}

// isAfterCursor checks the order SOQL promises without assuming how Salesforce collates Id values, so a
// page that repeats the cursor's record cannot make the poll loop.
func isAfterCursor(modstamp time.Time, recordID string, after time.Time, afterRecordID string) bool {
	return modstamp.After(after) || (modstamp.Equal(after) && afterRecordID != "" && !isSameRecordID(recordID, afterRecordID))
}

func encodeRecordPollCursor(position recordPollCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Salesforce poll cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func decodeRecordPollCursor(cursor pollingtrigger.Cursor) (recordPollCursor, time.Time, error) {
	var position recordPollCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil {
		return recordPollCursor{}, time.Time{}, errors.New("Salesforce poll cursor is not decodable")
	}
	after, err := time.Parse(time.RFC3339, position.SystemModstamp)
	if err != nil || (position.RecordID != "" && !isRecordID(position.RecordID)) {
		return recordPollCursor{}, time.Time{}, errors.New("Salesforce poll cursor is not decodable")
	}
	return position, after.UTC(), nil
}

//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refunddecision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "patINTEGRATION.0123456789abcdef"
	integrationBaseID      = "appRefundBase0001"
	integrationPolicyTable = "tblPolicies000001"
	integrationLogTable    = "tblDecisionLog001"
	sentinelMessage        = "SENTINEL provider message text"

	standardPolicyKey   = "refund-standard"
	duplicatedPolicyKey = "refund-duplicated"
	slowPolicyKey       = "refund-slow"
	limitedPolicyKey    = "refund-rate-limited"
	rejectedPolicyKey   = "refund-rejected"

	standardPolicyRecordID = "recPolicyStandard"
	slowPolicyRecordID     = "recPolicySlowRow1"
	limitedPolicyRecordID  = "recPolicyLimited1"
	slowCaseID             = "case-slow"

	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay = 9 * time.Second
)

var integrationSettings = Settings{
	PolicyTable: TableSelection{BaseID: integrationBaseID, BaseName: "Refunds", TableID: integrationPolicyTable, TableName: "Policies"},
	LogTable:    TableSelection{BaseID: integrationBaseID, BaseName: "Refunds", TableID: integrationLogTable, TableName: "Decision Log"},
}

func TestApprovedRefundIsLoggedLinkedStampedAndReadBackWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := uniqueFlowID("approved")
	requestID := "start-" + flowID
	options := dex.StartFlowOptions{RequestID: &requestID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}
	input := Input{CaseID: "case-approved", Customer: "Jane Doe", AmountUSD: 120, PolicyKey: standardPolicyKey}

	runID, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, options)
	require.NoError(t, err)
	repeatedRunID, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, options)
	require.NoError(t, err, "a retry of the same logical start attaches to the existing run")
	require.Equal(t, runID, repeatedRunID)

	decision := waitForCompletedDecision(t, ctx, harness, flowID)
	require.Equal(t, PhaseCompleted, decision.Phase)
	require.Equal(t, DecisionApproved, decision.Decision)
	require.Equal(t, standardPolicyRecordID, decision.PolicyRecordID)
	require.Equal(t, 250.0, decision.ApprovalLimitUSD, "the typed number survived Dex persistence of the list Result")
	require.True(t, decision.IsLogRecordCreated)
	require.True(t, decision.IsPolicyStamped)

	logRows := provider.logRowsForCase("case-approved")
	require.Len(t, logRows, 1)
	require.Equal(t, decision.LogRecordID, logRows[0].id)
	require.JSONEq(t, `{"Case ID":"case-approved","Customer":"Jane Doe","Amount USD":120,"Decision":"approved","Policy":["recPolicyStandard"]}`,
		logRows[0].fieldsJSON(t))
	require.JSONEq(t, `"case-approved"`, string(provider.policy(standardPolicyRecordID).fields[LastDecidedCaseField]))
	require.Equal(t, 1, provider.count("list "+standardPolicyKey), "the duplicate start ran the Flow once")
	require.Equal(t, 1, provider.count("upsert case-approved"))
	require.Equal(t, 1, provider.count("update "+standardPolicyRecordID))
	require.Equal(t, 1, provider.count("get "+decision.LogRecordID))
}

func TestRerunOfOneCaseUpdatesItsLogRowWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, time.Minute)
	first := waitForCompletedDecision(t, ctx, harness, startDecision(t, ctx, harness, "rerun-first",
		Input{CaseID: "case-rerun", Customer: "Ada", AmountUSD: 100, PolicyKey: standardPolicyKey}))
	second := waitForCompletedDecision(t, ctx, harness, startDecision(t, ctx, harness, "rerun-second",
		Input{CaseID: "case-rerun", Customer: "Ada", AmountUSD: 900, PolicyKey: standardPolicyKey}))

	require.Equal(t, DecisionApproved, first.Decision)
	require.True(t, first.IsLogRecordCreated)
	require.Equal(t, DecisionEscalated, second.Decision)
	require.False(t, second.IsLogRecordCreated, "the merge on Case ID found the first Flow's row")
	require.Equal(t, first.LogRecordID, second.LogRecordID)
	logRows := provider.logRowsForCase("case-rerun")
	require.Len(t, logRows, 1)
	require.JSONEq(t, `"escalated"`, string(logRows[0].fields[DecisionField]))
	require.JSONEq(t, `900`, string(logRows[0].fields[AmountField]))
}

func TestDuplicatePolicyKeyIsLoggedForReviewWithoutALinkWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, time.Minute)
	decision := waitForCompletedDecision(t, ctx, harness, startDecision(t, ctx, harness, "duplicated",
		Input{CaseID: "case-duplicated", Customer: "Grace", AmountUSD: 10, PolicyKey: duplicatedPolicyKey}))

	require.Equal(t, DecisionNeedsReview, decision.Decision)
	require.Equal(t, 2, decision.MatchingPolicyCount)
	require.Empty(t, decision.PolicyRecordID)
	require.False(t, decision.IsPolicyStamped)
	logRows := provider.logRowsForCase("case-duplicated")
	require.Len(t, logRows, 1)
	require.JSONEq(t, `[]`, string(logRows[0].fields[PolicyLinkField]))
	require.Zero(t, provider.countPrefix("update "), "no single policy is stamped")
}

// TestSlowWritesSendOneUpsertAndConvergeRepeatedUpdatesWithRealDex proves both write durability choices.
// The fake matches upserts on arrival, so concurrent duplicates would both create a row.
func TestSlowWritesSendOneUpsertAndConvergeRepeatedUpdatesWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, 2*time.Minute)
	decision := waitForCompletedDecision(t, ctx, harness, startDecision(t, ctx, harness, "slow",
		Input{CaseID: slowCaseID, Customer: "Katherine", AmountUSD: 40, PolicyKey: slowPolicyKey}))

	t.Logf("Flow decided %s with %d upsert and %d update dispatches", decision.Decision,
		provider.count("upsert "+slowCaseID), provider.count("update "+slowPolicyRecordID))
	require.Equal(t, DecisionApproved, decision.Decision)
	require.Equal(t, 1, provider.count("upsert "+slowCaseID), "sync durability sent the slow upsert once")
	require.Len(t, provider.logRowsForCase(slowCaseID), 1)
	require.GreaterOrEqual(t, provider.count("update "+slowPolicyRecordID), 2, "Dex's async fallback dispatched the slow update again")
	require.JSONEq(t, `"case-slow"`, string(provider.policy(slowPolicyRecordID).fields[LastDecidedCaseField]), "the repeated absolute update converged")
	require.True(t, decision.IsPolicyStamped)
}

func TestRateLimitedPolicyReadWaitsAirtablesCooldownWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, 2*time.Minute)
	decision := waitForCompletedDecision(t, ctx, harness, startDecision(t, ctx, harness, "rate-limited",
		Input{CaseID: "case-rate-limited", Customer: "Lin", AmountUSD: 10, PolicyKey: limitedPolicyKey}))

	require.Equal(t, DecisionApproved, decision.Decision)
	attempts := provider.times("list " + limitedPolicyKey)
	require.Len(t, attempts, 2, "the 429 read nothing, so Dex retried the list once")
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), 29*time.Second, "Dex waited Airtable's 30-second cooldown")
}

func TestRejectedPolicyReadFailsTheFlowWithoutProviderTextWithRealDex(t *testing.T) {
	provider, harness := newRefundDecisionHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := startDecision(t, ctx, harness, "rejected", Input{CaseID: "case-rejected", AmountUSD: 10, PolicyKey: rejectedPolicyKey})

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, sentinelMessage)
	require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	require.Equal(t, 1, provider.count("list "+rejectedPolicyKey), "a rejected formula is not retried")
	require.Zero(t, provider.countPrefix("upsert "))
}

func startDecision(t *testing.T, ctx context.Context, harness *refundDecisionHarness, scenario string, input Input) string {
	t.Helper()
	flowID := uniqueFlowID(scenario)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForCompletedDecision(t *testing.T, ctx context.Context, harness *refundDecisionHarness, flowID string) RefundDecision {
	t.Helper()
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var decision RefundDecision
	require.NoError(t, result.DecodeSingleOutput(&decision))
	return decision
}

func waitForTerminalFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not reach a terminal status", flowID)
		return result
	}
}

func uniqueFlowID(scenario string) string {
	return "airtable-refund-decision-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func integrationContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// fakeAirtable is a stateful fake of the Airtable record API for the policy and decision log tables.
type fakeAirtable struct {
	*httptest.Server
	t            *testing.T
	mutex        sync.Mutex
	tables       map[string][]*fakeRecord
	nextRecordID int
	calls        map[string][]time.Time
}

type fakeRecord struct {
	id     string
	fields map[string]json.RawMessage
}

func (record *fakeRecord) fieldsJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal(record.fields)
	require.NoError(t, err)
	return string(encoded)
}

func newFakeAirtable(t *testing.T) *fakeAirtable {
	t.Helper()
	provider := &fakeAirtable{t: t, tables: map[string][]*fakeRecord{integrationPolicyTable: nil, integrationLogTable: nil}, calls: map[string][]time.Time{}}
	for _, seed := range []struct{ recordID, key, limit string }{
		{standardPolicyRecordID, standardPolicyKey, "250"},
		{"recPolicyDupOne01", duplicatedPolicyKey, "100"},
		{"recPolicyDupTwo01", duplicatedPolicyKey, "500"},
		{slowPolicyRecordID, slowPolicyKey, "50"},
		{limitedPolicyRecordID, limitedPolicyKey, "50"},
	} {
		provider.tables[integrationPolicyTable] = append(provider.tables[integrationPolicyTable], &fakeRecord{id: seed.recordID, fields: map[string]json.RawMessage{
			PolicyKeyField: json.RawMessage(strconv.Quote(seed.key)), ApprovalLimitField: json.RawMessage(seed.limit),
		}})
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeAirtable) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":{"type":"AUTHENTICATION_REQUIRED","message":"`+sentinelMessage+`"}}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":{"type":"INVALID_REQUEST_UNKNOWN"}}`)
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/v0/"), "/")
	if len(segments) < 2 || segments[0] != integrationBaseID {
		provider.writeJSON(response, http.StatusForbidden, `{"error":{"type":"INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND"}}`)
		return
	}
	tableID := segments[1]
	if _, exists := provider.tables[tableID]; !exists {
		provider.writeJSON(response, http.StatusForbidden, `{"error":{"type":"INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND"}}`)
		return
	}
	switch {
	case request.Method == http.MethodPost && len(segments) == 3 && segments[2] == "listRecords":
		provider.listRecords(response, tableID, body)
	case request.Method == http.MethodPatch && len(segments) == 2:
		provider.writeRecords(response, tableID, body)
	case request.Method == http.MethodGet && len(segments) == 3:
		provider.getRecord(response, tableID, segments[2])
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"error":"NOT_FOUND"}`)
	}
}

func (provider *fakeAirtable) listRecords(response http.ResponseWriter, tableID string, body []byte) {
	var request struct {
		PageSize        int      `json:"pageSize"`
		FilterByFormula string   `json:"filterByFormula"`
		Fields          []string `json:"fields"`
	}
	const formulaPrefix = "{" + PolicyKeyField + `} = "`
	if json.Unmarshal(body, &request) != nil || !strings.HasPrefix(request.FilterByFormula, formulaPrefix) || !strings.HasSuffix(request.FilterByFormula, `"`) {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_FILTER_BY_FORMULA"}}`)
		return
	}
	policyKey := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(request.FilterByFormula, formulaPrefix), `"`), `\"`, `"`)
	attempt := provider.record("list " + policyKey)
	switch {
	case policyKey == rejectedPolicyKey:
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_FILTER_BY_FORMULA","message":"`+sentinelMessage+`"}}`)
		return
	case policyKey == limitedPolicyKey && attempt == 1:
		provider.writeJSON(response, http.StatusTooManyRequests, `{"error":{"type":"RATE_LIMIT_REACHED","message":"`+sentinelMessage+`"}}`)
		return
	}
	provider.mutex.Lock()
	var encoded []string
	for _, record := range provider.tables[tableID] {
		if len(encoded) < request.PageSize && string(record.fields[PolicyKeyField]) == strconv.Quote(policyKey) {
			encoded = append(encoded, encodeFakeRecord(provider.t, record, request.Fields))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"records":[`+strings.Join(encoded, ",")+`]}`)
}

// writeRecords serves PATCH: an upsert with performUpsert, or an update by record ID.
func (provider *fakeAirtable) writeRecords(response http.ResponseWriter, tableID string, body []byte) {
	var request struct {
		PerformUpsert *struct {
			FieldsToMergeOn []string `json:"fieldsToMergeOn"`
		} `json:"performUpsert"`
		Records []struct {
			ID     string                     `json:"id"`
			Fields map[string]json.RawMessage `json:"fields"`
		} `json:"records"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Records) == 0 || len(request.Records) > airtable.MaximumRecordsPerWrite {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_RECORDS"}}`)
		return
	}
	if request.PerformUpsert == nil {
		provider.updateRecords(response, tableID, request.Records[0].ID, request.Records[0].Fields)
		return
	}
	mergeFields := request.PerformUpsert.FieldsToMergeOn
	recordFields := request.Records[0].Fields
	var caseID string
	if json.Unmarshal(recordFields[CaseIDField], &caseID) != nil {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_VALUE_FOR_COLUMN"}}`)
		return
	}
	provider.record("upsert " + caseID)
	// Like a slow provider, the merge is evaluated on arrival and committed only when the response is sent.
	matches := provider.matchingRecords(tableID, mergeFields, recordFields)
	if caseID == slowCaseID {
		time.Sleep(slowResponseDelay)
	}
	if len(matches) > 1 {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_MULTIPLE_MATCHES","message":"`+sentinelMessage+`"}}`)
		return
	}
	provider.mutex.Lock()
	var record *fakeRecord
	created, updated := "[]", "[]"
	if len(matches) == 0 {
		provider.nextRecordID++
		record = &fakeRecord{id: fmt.Sprintf("recFake%010d", provider.nextRecordID), fields: map[string]json.RawMessage{}}
		provider.tables[tableID] = append(provider.tables[tableID], record)
		created = `["` + record.id + `"]`
	} else {
		record = matches[0]
		updated = `["` + record.id + `"]`
	}
	for name, value := range recordFields {
		record.fields[name] = value
	}
	encoded := encodeFakeRecord(provider.t, record, nil)
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"records":[`+encoded+`],"createdRecords":`+created+`,"updatedRecords":`+updated+`}`)
}

func (provider *fakeAirtable) updateRecords(response http.ResponseWriter, tableID string, recordID string, fields map[string]json.RawMessage) {
	provider.record("update " + recordID)
	provider.mutex.Lock()
	record := provider.findRecord(tableID, recordID)
	if record == nil {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"error":"NOT_FOUND"}`)
		return
	}
	for name, value := range fields {
		record.fields[name] = value
	}
	encoded := encodeFakeRecord(provider.t, record, nil)
	provider.mutex.Unlock()
	if recordID == slowPolicyRecordID {
		time.Sleep(slowResponseDelay)
	}
	provider.writeJSON(response, http.StatusOK, `{"records":[`+encoded+`]}`)
}

func (provider *fakeAirtable) getRecord(response http.ResponseWriter, tableID string, recordID string) {
	provider.record("get " + recordID)
	provider.mutex.Lock()
	record := provider.findRecord(tableID, recordID)
	encoded := ""
	if record != nil {
		encoded = encodeFakeRecord(provider.t, record, nil)
	}
	provider.mutex.Unlock()
	if record == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"NOT_FOUND"}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, encoded)
}

func (provider *fakeAirtable) matchingRecords(tableID string, mergeFields []string, fields map[string]json.RawMessage) []*fakeRecord {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var matches []*fakeRecord
	for _, record := range provider.tables[tableID] {
		isMatch := true
		for _, mergeField := range mergeFields {
			isMatch = isMatch && compactJSON(provider.t, record.fields[mergeField]) == compactJSON(provider.t, fields[mergeField])
		}
		if isMatch {
			matches = append(matches, record)
		}
	}
	return matches
}

// findRecord returns the record or nil; the caller holds the mutex.
func (provider *fakeAirtable) findRecord(tableID string, recordID string) *fakeRecord {
	for _, record := range provider.tables[tableID] {
		if record.id == recordID {
			return record
		}
	}
	return nil
}

func (provider *fakeAirtable) record(call string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.calls[call] = append(provider.calls[call], time.Now())
	return len(provider.calls[call])
}

func (provider *fakeAirtable) count(call string) int {
	return len(provider.times(call))
}

func (provider *fakeAirtable) countPrefix(prefix string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for call, times := range provider.calls {
		if strings.HasPrefix(call, prefix) {
			total += len(times)
		}
	}
	return total
}

func (provider *fakeAirtable) times(call string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.calls[call]...)
}

func (provider *fakeAirtable) logRowsForCase(caseID string) []fakeRecord {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var rows []fakeRecord
	for _, record := range provider.tables[integrationLogTable] {
		if string(record.fields[CaseIDField]) == strconv.Quote(caseID) {
			rows = append(rows, fakeRecord{id: record.id, fields: copyFields(record.fields)})
		}
	}
	return rows
}

func (provider *fakeAirtable) policy(recordID string) fakeRecord {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	record := provider.findRecord(integrationPolicyTable, recordID)
	require.NotNil(provider.t, record, recordID)
	return fakeRecord{id: record.id, fields: copyFields(record.fields)}
}

func (provider *fakeAirtable) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Airtable response write failed: %v", err)
	}
}

func encodeFakeRecord(t *testing.T, record *fakeRecord, onlyFields []string) string {
	fields := map[string]json.RawMessage{}
	for name, value := range record.fields {
		isRequested := len(onlyFields) == 0 || containsString(onlyFields, name)
		// Airtable omits empty values, such as "", [], and false, from returned records.
		isEmpty := containsString([]string{"null", `""`, "[]", "false"}, compactJSON(t, value))
		if isRequested && !isEmpty {
			fields[name] = value
		}
	}
	encoded, err := json.Marshal(map[string]any{"id": record.id, "createdTime": "2026-09-30T10:00:00.000Z", "fields": fields})
	require.NoError(t, err)
	return string(encoded)
}

func compactJSON(t *testing.T, value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	var compacted bytes.Buffer
	require.NoError(t, json.Compact(&compacted, value))
	return compacted.String()
}

func copyFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	copied := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		copied[name] = value
	}
	return copied
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type refundDecisionHarness struct {
	flow         *Flow
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newRefundDecisionHarness(t *testing.T) (*fakeAirtable, *refundDecisionHarness) {
	t.Helper()
	provider := newFakeAirtable(t)
	reference := sdkgo.ConnectionRef{Provider: "airtable", Name: ConnectionName}
	client, err := airtable.New(airtable.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[airtable.Credentials]{
		reference: {PersonalAccessToken: sdkgo.NewSecretString(integrationAccessToken)},
	}, airtable.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}))
	require.NoError(t, err)
	connection, err := airtable.NewConnection(client, reference)
	require.NoError(t, err)
	flow, err := NewFlow(connection, &integrationSettings)
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &refundDecisionHarness{flow: flow, cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, harness
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

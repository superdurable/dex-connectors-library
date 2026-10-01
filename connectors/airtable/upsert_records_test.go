// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"math"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpsertRecordsPatchesWithPerformUpsertAndReportsCreatedAndUpdated(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{
			"records":[
				{"id":"recDecisionLog001","createdTime":"2026-09-30T10:00:00.000Z","fields":{"Case ID":"case-1","Amount USD":120,"Policy":["recPolicyStandard"]}},
				{"id":"recDecisionLog002","createdTime":"2026-09-01T10:00:00.000Z","fields":{"Case ID":"case-2","Approved":true}}
			],
			"createdRecords":["recDecisionLog001"],"updatedRecords":["recDecisionLog002"],
			"details":{"message":"partialSuccess","reasons":["attachmentsFailedUploading","`+providerMessageSentinel+`"]}
		}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-log"), client.UpsertRecords(), airtableConnection, airtable.UpsertRecordsInput{
		BaseID: testBaseID, TableIDOrName: "tblDecisionLog001", FieldsToMergeOn: []string{"Case ID"},
		Records: []airtable.RecordUpsert{
			{Fields: map[string]airtable.CellValue{
				"Case ID": airtable.TextCellValue("case-1"), "Amount USD": airtable.NumberCellValue(120),
				"Policy": airtable.LinkedRecordsCellValue("recPolicyStandard"), "Tags": airtable.MultipleSelectsCellValue("refund", "eu"),
				"Notes": airtable.NullCellValue(), "Receipt": airtable.CellValue(`[{"url":"https://example.com/receipt.pdf"}]`),
			}},
			{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-2"), "Approved": airtable.CheckboxCellValue(true)}},
		},
		Typecast: true,
	})
	require.NoError(t, err)
	require.Equal(t, airtable.UpsertRecordsBranchUpserted, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, "/v0/"+testBaseID+"/tblDecisionLog001", request.path)
	require.Equal(t, "application/json", request.contentType)
	require.JSONEq(t, `{
		"performUpsert":{"fieldsToMergeOn":["Case ID"]},
		"records":[
			{"fields":{"Case ID":"case-1","Amount USD":120,"Policy":["recPolicyStandard"],"Tags":["refund","eu"],"Notes":null,
			           "Receipt":[{"url":"https://example.com/receipt.pdf"}]}},
			{"fields":{"Case ID":"case-2","Approved":true}}
		],
		"typecast":true
	}`, string(request.body))
	require.Equal(t, []string{"recDecisionLog001"}, result.Value.CreatedRecordIDs)
	require.Equal(t, []string{"recDecisionLog002"}, result.Value.UpdatedRecordIDs)
	require.Equal(t, []string{"attachmentsFailedUploading"}, result.Value.PartialSuccessReasons, "only documented reasons survive")
	require.Len(t, result.Value.Records, 2)
	require.NotEmpty(t, result.Receipt.IdempotencyKey, "the key is derived from the call ID even though Airtable takes none")
}

func TestUpsertRecordsRejectsUnsafeInputWithoutARequest(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	record := func(caseID string) airtable.RecordUpsert {
		return airtable.RecordUpsert{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue(caseID)}}
	}
	elevenRecords := make([]airtable.RecordUpsert, airtable.MaximumRecordsPerWrite+1)
	for index := range elevenRecords {
		elevenRecords[index] = record("case-" + strconv.Itoa(index))
	}
	for name, input := range map[string]airtable.UpsertRecordsInput{
		"no records":           {FieldsToMergeOn: []string{"Case ID"}},
		"eleven records":       {FieldsToMergeOn: []string{"Case ID"}, Records: elevenRecords},
		"no merge fields":      {Records: []airtable.RecordUpsert{record("case-1")}},
		"four merge fields":    {FieldsToMergeOn: []string{"A", "B", "C", "D"}, Records: []airtable.RecordUpsert{record("case-1")}},
		"repeated merge field": {FieldsToMergeOn: []string{"Case ID", "Case ID"}, Records: []airtable.RecordUpsert{record("case-1")}},
		"missing merge value":  {FieldsToMergeOn: []string{"Case ID", "Region"}, Records: []airtable.RecordUpsert{record("case-1")}},
		"null merge value": {FieldsToMergeOn: []string{"Case ID"}, Records: []airtable.RecordUpsert{
			{Fields: map[string]airtable.CellValue{"Case ID": airtable.NullCellValue()}},
		}},
		"two records with one merge key": {FieldsToMergeOn: []string{"Case ID"}, Records: []airtable.RecordUpsert{
			record("case-1"), {Fields: map[string]airtable.CellValue{"Case ID": airtable.CellValue(` "case-1" `)}},
		}},
		"forgotten zero value": {FieldsToMergeOn: []string{"Case ID"}, Records: []airtable.RecordUpsert{
			{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1"), "Notes": {}}},
		}},
		"invalid raw JSON": {FieldsToMergeOn: []string{"Case ID"}, Records: []airtable.RecordUpsert{
			{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1"), "Notes": airtable.CellValue(`{"`)}},
		}},
		"non-finite number": {FieldsToMergeOn: []string{"Case ID"}, Records: []airtable.RecordUpsert{
			{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1"), "Amount USD": airtable.NumberCellValue(math.NaN())}},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			input.BaseID, input.TableIDOrName = testBaseID, testTableID
			result, err := sdkgo.RunMutation(newStepDexContext("upsert-invalid"), client.UpsertRecords(), airtableConnection, input)
			require.NoError(t, err)
			require.Equal(t, airtable.UpsertRecordsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, provider.recordedRequests())
}

func TestUpsertRecordsRetriesEveryAmbiguousOrTemporaryOutcome(t *testing.T) {
	input := airtable.UpsertRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID, FieldsToMergeOn: []string{"Case ID"},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1")}}},
	}
	for _, testCase := range []struct {
		name    string
		respond func(*testing.T, http.ResponseWriter)
		kind    sdkgo.FailureKind
		delay   time.Duration
	}{
		{"rate limited", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":{"type":"RATE_LIMIT_REACHED"}}`)
		}, sdkgo.FailureRateLimit, 30 * time.Second},
		{"server error after dispatch", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"error":{"type":"SERVER_ERROR"}}`)
		}, sdkgo.FailureAvailability, 0},
		{"dropped connection after dispatch", func(t *testing.T, response http.ResponseWriter) {
			connection, _, err := response.(http.Hijacker).Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		}, sdkgo.FailureTransport, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) { testCase.respond(t, response) })
			client := newTestClient(t, provider.URL, airtable.Config{})
			_, err := sdkgo.RunMutation(newStepDexContext("upsert-retry"), client.UpsertRecords(), airtableConnection, input)
			delay, failure := requireRetry(t, err)
			require.Equal(t, testCase.kind, failure.Kind)
			require.Equal(t, testCase.delay, delay)
		})
	}
}

func TestUpsertRecordsRejectionNamesOnlyAirtablesErrorType(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_VALUE_FOR_COLUMN","message":"`+providerMessageSentinel+`"}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-rejected"), client.UpsertRecords(), airtableConnection, airtable.UpsertRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID, FieldsToMergeOn: []string{"Case ID"},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1")}}},
	})
	require.NoError(t, err)
	require.Equal(t, airtable.UpsertRecordsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "INVALID_VALUE_FOR_COLUMN")
	requireSecretSafeFailure(t, result.Failure)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestUpsertRecordsSelectsInvalidResponseForAnInconsistentAnswer(t *testing.T) {
	record := `{"id":"recDecisionLog001","createdTime":"2026-09-30T10:00:00.000Z","fields":{"Case ID":"case-1"}}`
	for name, body := range map[string]string{
		"missing createdRecords":      `{"records":[` + record + `],"updatedRecords":[]}`,
		"record count mismatch":       `{"records":[],"createdRecords":[],"updatedRecords":[]}`,
		"unknown created record":      `{"records":[` + record + `],"createdRecords":["recSomeoneElse001"],"updatedRecords":[]}`,
		"created and updated at once": `{"records":[` + record + `],"createdRecords":["recDecisionLog001"],"updatedRecords":["recDecisionLog001"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client := newTestClient(t, provider.URL, airtable.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("upsert-inconsistent"), client.UpsertRecords(), airtableConnection, airtable.UpsertRecordsInput{
				BaseID: testBaseID, TableIDOrName: testTableID, FieldsToMergeOn: []string{"Case ID"},
				Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-1")}}},
			})
			require.NoError(t, err)
			require.Equal(t, airtable.UpsertRecordsBranchInvalidResponse, result.Branch)
		})
	}
}

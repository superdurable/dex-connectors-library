// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCreateRecordAppendsTypedFields(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"id":"recReviewCreated1","createdTime":"2026-11-16T09:00:00Z","fields":{"Case ID":"case-created"}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("create-review"), client.CreateRecord(), airtableConnection, airtable.CreateRecordInput{
		BaseID: testBaseID, TableIDOrName: testTableID, Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-created")},
	})
	require.NoError(t, err)
	require.Equal(t, airtable.CreateRecordBranchCreated, result.Branch)
	require.Equal(t, "recReviewCreated1", result.Value.ID)
	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.JSONEq(t, `{"fields":{"Case ID":"case-created"}}`, string(requests[0].body))
}

func TestCreateRecordClassifiesRejectionAndRetry(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		branch sdkgo.BranchID
	}{
		{name: "denied", status: http.StatusForbidden, branch: airtable.CreateRecordBranchProviderRejected},
		{name: "rate limited", status: http.StatusTooManyRequests},
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "unknown", status: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, `{"error":{"type":"TEST_ERROR","message":"`+providerMessageSentinel+`"}}`)
			})
			client := newTestClient(t, provider.URL, airtable.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("create-"+testCase.name), client.CreateRecord(), airtableConnection, airtable.CreateRecordInput{
				BaseID: testBaseID, TableIDOrName: testTableID, Fields: map[string]airtable.CellValue{"Case ID": airtable.TextCellValue("case-created")},
			})
			if testCase.branch != "" {
				require.NoError(t, err)
				require.Equal(t, testCase.branch, result.Branch)
				requireSecretSafeFailure(t, result.Failure)
			} else {
				_, failure := requireRetry(t, err)
				require.NotNil(t, failure)
			}
		})
	}
}

func TestDeleteRecordConvergesOnAbsence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "deleted", status: http.StatusOK, body: `{"id":"recReviewCreated1","deleted":true}`, branch: airtable.DeleteRecordBranchDeleted},
		{name: "already absent", status: http.StatusNotFound, body: `{"error":{"type":"NOT_FOUND"}}`, branch: airtable.DeleteRecordBranchAlreadyAbsent},
		{name: "invalid response", status: http.StatusOK, body: `{"id":"recReviewCreated1","deleted":false}`, branch: airtable.DeleteRecordBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			client := newTestClient(t, provider.URL, airtable.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("delete-"+testCase.name), client.DeleteRecord(), airtableConnection, airtable.DeleteRecordInput{
				BaseID: testBaseID, TableIDOrName: testTableID, RecordID: "recReviewCreated1",
			})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, http.MethodDelete, provider.recordedRequests()[0].method)
		})
	}
}

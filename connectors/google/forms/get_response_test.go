// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const editedResponseResource = `{"formId":"f1","responseId":"r_edited","createTime":"2026-09-29T08:00:00Z",` +
	`"lastSubmittedTime":"2026-09-30T08:30:00.5+02:00","answers":{"q_name":{"questionId":"q_name","textAnswers":{"answers":[{"value":"Acme GmbH"}]}}}}`

func TestGetResponseReturnsAnEditedResponseInUTC(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, editedResponseResource)
	})
	result, err := sdkgo.RunQuery(newDexContext("get-response"), newFormsClient(t, fake.URL).GetResponse(), formsConnection,
		forms.GetResponseInput{FormID: "f1", ResponseID: "r_edited"})
	require.NoError(t, err)
	require.Equal(t, forms.GetResponseBranchFound, result.Branch)
	require.Equal(t, forms.FormResponse{
		ResponseID: "r_edited", FormID: "f1",
		CreatedAt:       time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC),
		LastSubmittedAt: time.Date(2026, time.September, 30, 6, 30, 0, 500000000, time.UTC),
		Answers:         []forms.FormAnswer{{QuestionID: "q_name", Values: []string{"Acme GmbH"}}},
	}, result.Value)
	require.Equal(t, "r_edited", result.Receipt.ProviderObjectID)
	require.Equal(t, "/v1/forms/f1/responses/r_edited", fake.recorded()[0].path)
}

func TestGetResponseClassifiesMissingAndMismatchedResponses(t *testing.T) {
	for _, test := range []struct {
		name       string
		input      forms.GetResponseInput
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantCalls  int
	}{
		{name: "missing response", input: forms.GetResponseInput{FormID: "f1", ResponseID: "gone"}, status: http.StatusNotFound, body: googleError(404, "NOT_FOUND", ""), wantBranch: forms.GetResponseBranchNotFound, wantKind: sdkgo.FailureNotFound, wantCalls: 1},
		{name: "different response ID", input: forms.GetResponseInput{FormID: "f1", ResponseID: "r1"}, status: http.StatusOK, body: `{"formId":"f1","responseId":"r2","lastSubmittedTime":"2026-09-30T09:00:00Z"}`, wantBranch: forms.GetResponseBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "different form ID", input: forms.GetResponseInput{FormID: "f1", ResponseID: "r1"}, status: http.StatusOK, body: `{"formId":"f2","responseId":"r1","lastSubmittedTime":"2026-09-30T09:00:00Z"}`, wantBranch: forms.GetResponseBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "not readable", input: forms.GetResponseInput{FormID: "f1", ResponseID: "r1"}, status: http.StatusForbidden, body: googleError(403, "PERMISSION_DENIED", ""), wantBranch: forms.GetResponseBranchProviderRejected, wantKind: sdkgo.FailureAuthorization, wantCalls: 1},
		{name: "blank response ID", input: forms.GetResponseInput{FormID: "f1"}, wantBranch: forms.GetResponseBranchDefect, wantKind: sdkgo.FailureValidation},
		{name: "response ID traversal", input: forms.GetResponseInput{FormID: "f1", ResponseID: "../../forms"}, wantBranch: forms.GetResponseBranchDefect, wantKind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newDexContext("get-response-"+test.name), newFormsClient(t, fake.URL).GetResponse(), formsConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Len(t, fake.recorded(), test.wantCalls)
		})
	}
}

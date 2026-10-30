// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateConversationStateWritesOnlyWhenTheStateDiffers(t *testing.T) {
	for _, test := range []struct {
		name            string
		current         string
		currentSnoozed  any
		input           intercom.UpdateConversationStateInput
		expectedRequest string
		returned        string
		returnedSnoozed any
	}{
		{
			name: "close", current: "open",
			input:           intercom.UpdateConversationStateInput{State: intercom.ConversationStateClosed},
			expectedRequest: `{"message_type":"close","type":"admin","admin_id":"` + testAdminID + `"}`, returned: "closed",
		},
		{
			name: "reopen a snoozed conversation", current: "snoozed", currentSnoozed: 1767312000,
			input:           intercom.UpdateConversationStateInput{State: intercom.ConversationStateOpen},
			expectedRequest: `{"message_type":"open","admin_id":"` + testAdminID + `"}`, returned: "open",
		},
		{
			name: "snooze", current: "open",
			input:           intercom.UpdateConversationStateInput{State: intercom.ConversationStateSnoozed, SnoozedUntil: "2026-01-02T00:00:00Z"},
			expectedRequest: `{"message_type":"snoozed","admin_id":"` + testAdminID + `","snoozed_until":1767312000}`, returned: "snoozed", returnedSnoozed: 1767312000,
		},
		{
			name: "snooze to another time", current: "snoozed", currentSnoozed: 1767225600,
			input:           intercom.UpdateConversationStateInput{State: intercom.ConversationStateSnoozed, SnoozedUntil: "2026-01-02T01:00:00+01:00"},
			expectedRequest: `{"message_type":"snoozed","admin_id":"` + testAdminID + `","snoozed_until":1767312000}`, returned: "snoozed", returnedSnoozed: 1767312000,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, test.current, test.currentSnoozed))
					return
				}
				writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, test.returned, test.returnedSnoozed))
			})
			input := test.input
			input.ConversationID, input.AdminID = testConversation, testAdminID
			result, err := sdkgo.RunMutation(newTestDexContext("state-"+test.name), newIntercomClient(t, provider.URL).UpdateConversationState(), intercomConnection, input)
			require.NoError(t, err)
			require.Equal(t, intercom.UpdateConversationStateBranchUpdated, result.Branch)
			require.False(t, result.Value.WasAlreadyApplied)
			require.Equal(t, input.State, result.Value.Conversation.State)
			require.Equal(t, 2, provider.requestCount())
			write := provider.request(1)
			require.Equal(t, "/conversations/"+testConversation+"/parts", write.path)
			require.JSONEq(t, test.expectedRequest, write.body)
		})
	}
}

func TestUpdateConversationStateAlreadyInTheStateWritesNothing(t *testing.T) {
	for name, test := range map[string]struct {
		current      string
		snoozedUntil any
		input        intercom.UpdateConversationStateInput
	}{
		"closed":  {current: "closed", input: intercom.UpdateConversationStateInput{State: intercom.ConversationStateClosed}},
		"open":    {current: "open", input: intercom.UpdateConversationStateInput{State: intercom.ConversationStateOpen}},
		"snoozed": {current: "snoozed", snoozedUntil: 1767312000, input: intercom.UpdateConversationStateInput{State: intercom.ConversationStateSnoozed, SnoozedUntil: "2026-01-02T00:00:00.4Z"}},
	} {
		provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, test.current, test.snoozedUntil))
		})
		input := test.input
		input.ConversationID, input.AdminID = testConversation, testAdminID
		result, err := sdkgo.RunMutation(newTestDexContext("state-already-"+name), newIntercomClient(t, provider.URL).UpdateConversationState(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.UpdateConversationStateBranchUpdated, result.Branch, name)
		require.True(t, result.Value.WasAlreadyApplied, name)
		require.Equal(t, 1, provider.requestCount(), "%s: only the read", name)
	}
}

func TestRejectedStateWriteIsReconciledByReadingAgain(t *testing.T) {
	for _, test := range []struct {
		name           string
		reread         string
		expectedBranch sdkgo.BranchID
	}{
		{name: "a concurrent attempt closed it", reread: "closed", expectedBranch: intercom.UpdateConversationStateBranchUpdated},
		{name: "still open", reread: "open", expectedBranch: intercom.UpdateConversationStateBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch {
				case request.Method == http.MethodPost:
					writeJSON(t, response, http.StatusBadRequest, `{"type":"error.list","errors":[{"code":"conflict","message":"SENTINEL"}]}`)
				case index == 0:
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
				default:
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, test.reread, nil))
				}
			})
			result, err := sdkgo.RunMutation(newTestDexContext("state-rejected-"+test.name), newIntercomClient(t, provider.URL).UpdateConversationState(), intercomConnection,
				intercom.UpdateConversationStateInput{ConversationID: testConversation, AdminID: testAdminID, State: intercom.ConversationStateClosed})
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, 3, provider.requestCount(), "read, write, reread")
			if test.expectedBranch == intercom.UpdateConversationStateBranchUpdated {
				require.True(t, result.Value.WasAlreadyApplied)
				return
			}
			require.Equal(t, "Intercom rejected the request (HTTP 400) [conflict]", result.Failure.Message)
		})
	}
}

func TestStateWriteFailuresRetryOrSelectBranches(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		isRetry bool
		branch  sdkgo.BranchID
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{"type":"error.list","errors":[{"code":"server_error"}]}`, isRetry: true},
		{name: "missing conversation", status: http.StatusNotFound, body: `{"type":"error.list","errors":[{"code":"not_found"}]}`, branch: intercom.UpdateConversationStateBranchNotFound},
		{name: "unchanged state returned", status: http.StatusOK, body: "", branch: intercom.UpdateConversationStateBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet || test.body == "" {
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newTestDexContext("state-failure-"+test.name), newIntercomClient(t, provider.URL).UpdateConversationState(), intercomConnection,
				intercom.UpdateConversationStateInput{ConversationID: testConversation, AdminID: testAdminID, State: intercom.ConversationStateClosed})
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "the next attempt reads the state again, so a retry is safe")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestUpdateConversationStateValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newIntercomClient(t, provider.URL)
	for name, input := range map[string]intercom.UpdateConversationStateInput{
		"ticket state":                  {State: "resolved"},
		"snooze without a time":         {State: intercom.ConversationStateSnoozed},
		"snooze time without offset":    {State: intercom.ConversationStateSnoozed, SnoozedUntil: "2026-01-02T00:00:00"},
		"snooze time for another state": {State: intercom.ConversationStateClosed, SnoozedUntil: "2026-01-02T00:00:00Z"},
		"blank state":                   {},
	} {
		input.ConversationID, input.AdminID = testConversation, testAdminID
		result, err := sdkgo.RunMutation(newTestDexContext("state-invalid-"+name), client.UpdateConversationState(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.UpdateConversationStateBranchDefect, result.Branch, name)
	}
	result, err := sdkgo.RunMutation(newTestDexContext("state-invalid-admin"), client.UpdateConversationState(), intercomConnection,
		intercom.UpdateConversationStateInput{ConversationID: testConversation, State: intercom.ConversationStateClosed})
	require.NoError(t, err)
	require.Equal(t, intercom.UpdateConversationStateBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "adminPicker")
}

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestDecideSubmissionFromMarker is the decision table the Python and TypeScript ports repeat.
func TestDecideSubmissionFromMarker(t *testing.T) {
	const callID = sdkgo.CallID("33dc2caf-7622-50bd-86c2-e8f4c1d7e9a6")
	for _, test := range []struct {
		name          string
		heartbeat     string
		attempt       int32
		expectedKind  submissionDecisionKind
		expectedBound int32
	}{
		{"first attempt", "", 1, submissionAllowed, 0},
		{"no marker on a later attempt", "", 3, submissionAllowed, 2},
		{"legacy marker", `{"emailSubmittedCallId":"` + string(callID) + `"}`, 2, submissionAllowed, 1},
		{"foreign marker", `{"emailSubmittedCallId":"other","emailAttempt":1,"emailSubmissionCount":1}`, 2, submissionCheckpointUntrusted, 0},
		{"claim with a later attempt between", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":1,"emailSubmissionCount":1}`, 3, submissionAllowed, 2},
		{"not submitted", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":1}`, 2, submissionAllowed, 0},
		{"marker from the current attempt", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":2,"emailSubmissionCount":1}`, 2, submissionCheckpointUntrusted, 0},
		{"count above its attempt", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":1,"emailSubmissionCount":2}`, 2, submissionCheckpointUntrusted, 0},
		{"accepted", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":1,"emailSubmissionCount":1,"emailAccepted":true}`, 2, submissionAlreadyAccepted, 0},
		{"bound at the cap", `{"emailSubmittedCallId":"` + string(callID) + `","emailAttempt":3,"emailSubmissionCount":3}`, 4, submissionCapReached, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var marker *singleSubmissionMarker
			if test.heartbeat != "" {
				marker = &singleSubmissionMarker{}
				require.NoError(t, json.Unmarshal([]byte(test.heartbeat), marker))
			}
			decision := decideSubmissionFromMarker(callID, test.attempt, 3, marker)
			require.Equal(t, test.expectedKind, decision.kind)
			if test.expectedKind == submissionAllowed || test.expectedKind == submissionCapReached {
				require.Equal(t, test.expectedBound, decision.maximumPriorSubmissions)
			}
		})
	}
}

// TestSubmissionMarkerBytes fixes the wire form the Python and TypeScript ports write.
func TestSubmissionMarkerBytes(t *testing.T) {
	for expected, marker := range map[string]singleSubmissionMarker{
		`{"emailSubmittedCallId":"call","emailAttempt":2,"emailSubmissionCount":2}`:                      {SubmittedCallID: "call", Attempt: 2, SubmissionCount: 2},
		`{"emailSubmittedCallId":"call","emailAttempt":1}`:                                               {SubmittedCallID: "call", Attempt: 1},
		`{"emailSubmittedCallId":"call","emailAttempt":1,"emailSubmissionCount":1,"emailAccepted":true}`: {SubmittedCallID: "call", Attempt: 1, SubmissionCount: 1, IsAccepted: true},
	} {
		encoded, err := json.Marshal(marker)
		require.NoError(t, err)
		require.Equal(t, expected, string(encoded))
	}
}

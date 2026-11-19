// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// fencedUpdateZendesk answers each attempt's ticket read and empty audit page, then the PUT with writeStatuses in order.
func fencedUpdateZendesk(t *testing.T, auditBody func(attempt int) string, writeStatuses ...int) *recordingZendesk {
	t.Helper()
	return newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		attempt := index / 3
		switch index % 3 {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
		case 1:
			writeJSON(t, response, http.StatusOK, auditBody(attempt))
		default:
			require.Equal(t, http.MethodPut, request.Method)
			status := writeStatuses[attempt]
			if status == http.StatusOK {
				writeJSON(t, response, status, ticketEnvelopeJSON(5512, "open", []string{"escalated"}))
				return
			}
			writeJSON(t, response, status, `{"error":"ProviderError","description":"SENTINEL"}`)
		}
	})
}

func emptyAuditPage(int) string { return `{"audits":[]}` }

func TestUpdateTicketResendsAnUnknownWriteBelowTheSendCap(t *testing.T) {
	provider := fencedUpdateZendesk(t, emptyAuditPage, http.StatusInternalServerError, http.StatusOK)
	dexContext := newZendeskDexContext("update-resend")
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	dexContext.advanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 6, provider.requestCount(), "the second attempt reads back, finds no audit, and resends")
	require.Equal(t, provider.request(2).body, provider.request(5).body)
}

func TestUpdateTicketReadsBackAnEarlierWriteBeforeResending(t *testing.T) {
	dexContext := newZendeskDexContext("update-read-back")
	key := stepIdempotencyKey(t, dexContext)
	provider := fencedUpdateZendesk(t, func(attempt int) string {
		if attempt == 0 {
			return `{"audits":[]}`
		}
		return `{"audits":[{"id":10,"metadata":{"custom":{"dex_idempotency_key":"` + key + `"}}}]}`
	}, http.StatusGatewayTimeout)
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.Error(t, err)
	dexContext.advanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 5, provider.requestCount(), "the found audit is never written again")
}

func TestUpdateTicketReadsBackAfterTheLastAllowedSendBeforeSelectingUncertain(t *testing.T) {
	provider := fencedUpdateZendesk(t, emptyAuditPage, http.StatusBadGateway, http.StatusConflict, http.StatusInternalServerError)
	dexContext := newZendeskDexContext("update-cap")
	client := newZendeskClient(t, provider.URL)
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d: an unknown outcome retries, also on the last allowed send", attempt)
		dexContext.advanceAttempt()
	}
	require.Equal(t, 9, provider.requestCount(), "three sends reach the default cap of 3")
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch)
	require.Equal(t, 11, provider.requestCount(), "at the cap an attempt only reads back")
	require.NotContains(t, mustJSON(t, result), "SENTINEL")
}

func TestUpdateTicketCredentialEchoingRefusalResendsBelowTheCapAndIsUncertainAtTheCap(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index % 3 {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
		default:
			require.Equal(t, http.MethodPut, request.Method)
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":"RecordInvalid","description":"`+testAPIToken+`"}`)
		}
	})
	dexContext := newZendeskDexContext("update-echoing-refusal")
	client := newZendeskClient(t, provider.URL)
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d: a refusal that echoes the token is an unknown outcome", attempt)
		require.Equal(t, "claimed", recordedMarker(t, dexContext).State, "nothing records canResend")
		dexContext.advanceAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch)
	require.Equal(t, 11, provider.requestCount(), "three sends reach the cap, then the attempt only reads back")
	require.NotContains(t, mustJSON(t, result), testAPIToken)
}

func TestUpdateTicketReadBackAfterTheLastAllowedSendFindsTheChange(t *testing.T) {
	dexContext := newZendeskDexContext("update-cap-found")
	key := stepIdempotencyKey(t, dexContext)
	provider := fencedUpdateZendesk(t, func(attempt int) string {
		if attempt < 3 {
			return `{"audits":[]}`
		}
		return `{"audits":[{"id":10,"metadata":{"custom":{"dex_idempotency_key":"` + key + `"}}}]}`
	}, http.StatusBadGateway, http.StatusConflict, http.StatusInternalServerError)
	client := newZendeskClient(t, provider.URL)
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d", attempt)
		dexContext.advanceAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch, "the last send applied, and its audit confirms it")
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 11, provider.requestCount())
}

func TestUpdateTicketNeverResendsADefiniteRefusalAndRecordsCanResend(t *testing.T) {
	provider := fencedUpdateZendesk(t, emptyAuditPage, http.StatusUnprocessableEntity)
	dexContext := newZendeskDexContext("update-refused")
	result, err := sdkgo.RunMutation(dexContext, newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, 3, provider.requestCount())
	var marker struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(dexContext.recordedHeartbeat, &marker))
	require.Equal(t, "canResend", marker.State)
}

func TestUpdateTicketRefusalAfterAPossibleSendSelectsUncertain(t *testing.T) {
	provider := fencedUpdateZendesk(t, emptyAuditPage, http.StatusInternalServerError, http.StatusBadRequest)
	dexContext := newZendeskDexContext("update-refused-after-unknown")
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.Error(t, err)
	dexContext.advanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch, "the first send may have applied")
}

func TestUpdateTicketFailClosedCheckpointRetriesWithoutRequests(t *testing.T) {
	provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	dexContext := newZendeskDexContext("update-untrusted")
	dexContext.previousHeartbeat = json.RawMessage(`{"callId":"foreign-call","attempt":1,"state":"notSent"}`)
	dexContext.attempt = 2
	_, err := sdkgo.RunMutation(dexContext, newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

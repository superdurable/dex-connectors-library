// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// fenceMarker is the part of the single-dispatch heartbeat these tests assert.
type fenceMarker struct {
	State      string `json:"state"`
	ProviderID string `json:"providerId"`
	SendCount  int32  `json:"sendCount"`
}

func recordedMarker(t *testing.T, dexContext *zendeskDexContext) fenceMarker {
	t.Helper()
	var marker fenceMarker
	require.NoError(t, json.Unmarshal(dexContext.recordedHeartbeat, &marker))
	return marker
}

func auditPageWithKey(key string) string {
	return `{"audits":[{"id":10,"metadata":{"custom":{"dex_idempotency_key":"` + key + `"}}}]}`
}

// switchableCredentials resolves the test token until err is set.
type switchableCredentials struct {
	err error
}

func (provider *switchableCredentials) Resolve(sdkgo.Call) (support.Credentials, error) {
	if provider.err != nil {
		return support.Credentials{}, provider.err
	}
	return testCredentialProvider()[zendeskConnection], nil
}

func newSwitchableZendeskClient(t *testing.T, baseURL string, credentials *switchableCredentials) *support.Client {
	t.Helper()
	client, err := support.New(support.Config{Subdomain: testSubdomain}, credentials, support.WithAPIBaseURL(baseURL+"/api/v2"))
	require.NoError(t, err)
	return client
}

func TestUpdateTicketReadsBackBeforeResendingAfterARateLimitedResend(t *testing.T) {
	dexContext := newZendeskDexContext("update-read-back-after-429")
	key := stepIdempotencyKey(t, dexContext)
	provider := fencedUpdateZendesk(t, func(attempt int) string {
		if attempt == 2 {
			return auditPageWithKey(key)
		}
		return `{"audits":[]}`
	}, http.StatusInternalServerError, http.StatusTooManyRequests)
	client := newZendeskClient(t, provider.URL)
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d", attempt)
		dexContext.advanceAttempt()
	}
	require.Equal(t, "canResend", fenceMarkerFromJSON(t, dexContext.previousHeartbeat).State)
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 8, provider.requestCount(), "the third attempt reads back and finds the first write")
	require.Equal(t, http.MethodGet, provider.request(6).method)
	require.Equal(t, http.MethodGet, provider.request(7).method)
}

func fenceMarkerFromJSON(t *testing.T, encoded json.RawMessage) fenceMarker {
	t.Helper()
	var marker fenceMarker
	require.NoError(t, json.Unmarshal(encoded, &marker))
	return marker
}

func TestUpdateTicketCredentialFailureAfterAPossibleSendNeverSelectsDefect(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		isUncertain bool
	}{
		{name: "terminal", err: sdkgo.ErrReauthorizationRequired, isUncertain: true},
		{name: "transient", err: errors.New("credential broker unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := fencedUpdateZendesk(t, emptyAuditPage, http.StatusInternalServerError)
			credentials := &switchableCredentials{}
			client := newSwitchableZendeskClient(t, provider.URL, credentials)
			dexContext := newZendeskDexContext("update-credentials-after-send-" + test.name)
			_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
			require.Error(t, err)
			dexContext.advanceAttempt()
			credentials.err = test.err
			result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
			if test.isUncertain {
				require.NoError(t, err)
				require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			}
			require.Nil(t, dexContext.recordedHeartbeat, "notSent would hide the earlier possible send")
			require.Equal(t, 3, provider.requestCount())
		})
	}
}

func TestUpdateTicketCredentialFailureOnAFirstSend(t *testing.T) {
	provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	credentials := &switchableCredentials{err: errors.New("credential broker unavailable")}
	client := newSwitchableZendeskClient(t, provider.URL, credentials)
	dexContext := newZendeskDexContext("update-credentials-first-transient")
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a transient credential failure is retried")
	require.Equal(t, "notSent", recordedMarker(t, dexContext).State)

	credentials.err = sdkgo.ErrReauthorizationRequired
	dexContext = newZendeskDexContext("update-credentials-first-terminal")
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchDefect, result.Branch)
	require.Equal(t, "notSent", recordedMarker(t, dexContext).State)
}

// acceptedUpdateZendesk answers the ticket read, an empty audit page, the PUT with acceptWrite, and then
// read-backs with readBack(index) for every later request.
func acceptedUpdateZendesk(t *testing.T, acceptWrite func(http.ResponseWriter), readBack func(response http.ResponseWriter, request *http.Request, index int)) *recordingZendesk {
	t.Helper()
	return newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
		case 2:
			require.Equal(t, http.MethodPut, request.Method)
			acceptWrite(response)
		default:
			require.Equal(t, http.MethodGet, request.Method, "an accepted write is never resent")
			readBack(response, request, index)
		}
	})
}

// breakBodyAfterHeaders sends 200 headers and part of a body, then drops the connection.
func breakBodyAfterHeaders(t *testing.T) func(http.ResponseWriter) {
	return func(response http.ResponseWriter) {
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Content-Length", "4096")
		response.WriteHeader(http.StatusOK)
		_, err := io.WriteString(response, `{"ticket":{"id":5512,`)
		require.NoError(t, err)
		response.(http.Flusher).Flush()
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}
}

func undecodableAnswer(t *testing.T) func(http.ResponseWriter) {
	return func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"wrong":true}`) }
}

func TestUpdateTicketAcceptedWriteWithAnUnusableAnswerReadsBackInTheSameAttempt(t *testing.T) {
	for name, acceptWrite := range map[string]func(*testing.T) func(http.ResponseWriter){
		"body breaks after 2xx headers": breakBodyAfterHeaders,
		"undecodable 2xx":               undecodableAnswer,
	} {
		t.Run(name, func(t *testing.T) {
			dexContext := newZendeskDexContext("update-accepted-confirmed")
			key := stepIdempotencyKey(t, dexContext)
			provider := acceptedUpdateZendesk(t, acceptWrite(t), func(response http.ResponseWriter, request *http.Request, index int) {
				if strings.HasSuffix(request.URL.Path, "/audits") {
					writeJSON(t, response, http.StatusOK, auditPageWithKey(key))
					return
				}
				writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{"escalated"}))
			})
			result, err := sdkgo.RunMutation(dexContext, newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
			require.NoError(t, err)
			require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
			require.False(t, result.Value.WasAlreadyApplied, "this attempt wrote the change")
			require.Equal(t, 5, provider.requestCount())
			marker := recordedMarker(t, dexContext)
			require.Equal(t, "5512", marker.ProviderID)
			require.Equal(t, "claimed", marker.State)
		})
	}
}

func TestUpdateTicketAcceptedWriteThatCannotBeConfirmedSelectsUncertainWithoutResending(t *testing.T) {
	provider := acceptedUpdateZendesk(t, undecodableAnswer(t), func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/audits") {
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-accepted-unconfirmed"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch, "the cap leaves room, but an accepted write is never resent")
	require.Equal(t, 5, provider.requestCount())
}

func TestUpdateTicketAcceptedWriteRetriesATransientReadBackAndLaterOnlyReadsBack(t *testing.T) {
	dexContext := newZendeskDexContext("update-accepted-later-read-back")
	key := stepIdempotencyKey(t, dexContext)
	provider := acceptedUpdateZendesk(t, undecodableAnswer(t), func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 3:
			writeJSON(t, response, http.StatusServiceUnavailable, `{"error":"Unavailable"}`)
		case strings.HasSuffix(request.URL.Path, "/audits"):
			writeJSON(t, response, http.StatusOK, auditPageWithKey(key))
		default:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{"escalated"}))
		}
	})
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the saved ticket ID lets the next attempt only read back")
	require.Equal(t, "5512", recordedMarker(t, dexContext).ProviderID)

	dexContext.advanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 6, provider.requestCount(), "the second attempt reads the ticket and audits and never writes")
	require.Nil(t, dexContext.recordedHeartbeat)
}

func TestUpdateTicketSavedTicketIDWithoutAConfirmedChangeSelectsUncertain(t *testing.T) {
	provider := acceptedUpdateZendesk(t, undecodableAnswer(t), func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 3:
			writeJSON(t, response, http.StatusBadGateway, `{"error":"BadGateway"}`)
		case strings.HasSuffix(request.URL.Path, "/audits"):
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
		}
	})
	dexContext := newZendeskDexContext("update-accepted-later-missing")
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.Error(t, err)
	dexContext.advanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUncertain, result.Branch, "a missing ticket after an accepted write is never notFound")
	require.Equal(t, 5, provider.requestCount())
}

func TestUpdateTicketRefusalWithAnUnreadableOrContradictoryBodyIsAnUnknownOutcome(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":        `{"error":"RecordInvalid","description":"` + strings.Repeat("x", 4096) + `"}`,
		"carries a ticket": `{"error":"RecordInvalid","ticket":{"id":5512}}`,
		"echoes the token": `{"error":"RecordInvalid","description":"` + testAPIToken + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPut {
					writeJSON(t, response, http.StatusUnprocessableEntity, body)
					return
				}
				if strings.HasSuffix(request.URL.Path, "/audits") {
					writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
					return
				}
				writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
			})
			client, err := support.New(support.Config{Subdomain: testSubdomain, MaxResponseBytes: 2048}, testCredentialProvider(), support.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			dexContext := newZendeskDexContext("update-unknown-refusal")
			_, err = sdkgo.RunMutation(dexContext, client.UpdateTicket(), zendeskConnection, followUpInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the PUT may have applied, so the next attempt reads back first")
			require.Equal(t, "claimed", recordedMarker(t, dexContext).State)
		})
	}
}

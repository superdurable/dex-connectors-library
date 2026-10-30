// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validSendCampaignInput() mailchimp.SendCampaignInput {
	return mailchimp.SendCampaignInput{CampaignID: testCampaignID, ExpectedListID: testListID}
}

// draftThenSend answers the campaign read with a draft and the send with sendReply.
func draftThenSend(t *testing.T, sendReply func(http.ResponseWriter)) *recordingMailchimp {
	t.Helper()
	return newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, campaignBody("save"))
			return
		}
		sendReply(response)
	})
}

func TestSendCampaignReadsTheDraftRecordsTheCheckpointAndSendsOnce(t *testing.T) {
	provider := draftThenSend(t, func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) })
	ctx := newMailchimpDexContext("send")
	result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchSent, result.Branch)
	created := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	require.Equal(t, mailchimp.Campaign{
		ID: testCampaignID, WebID: 98765, Type: "regular", Status: mailchimp.CampaignStatusSave, Title: "Spring launch",
		SubjectLine: "Our spring launch is here", ListID: testListID, ListName: "Customers", RecipientCount: 1200, CreatedAt: &created,
	}, result.Value.Campaign)
	require.Equal(t, testCampaignID, result.Receipt.ProviderObjectID)
	require.Equal(t, 2, provider.requestCount())
	read, send := provider.request(0), provider.request(1)
	require.Equal(t, http.MethodGet, read.method)
	require.Equal(t, "/3.0/campaigns/"+testCampaignID, read.path)
	require.Contains(t, read.query["fields"][0], "recipients.list_id")
	require.Equal(t, http.MethodPost, send.method)
	require.Equal(t, "/3.0/campaigns/"+testCampaignID+"/actions/send", send.path)
	require.Empty(t, send.body)
	require.JSONEq(t, `{"mailchimpDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch checkpoint is recorded before the send")
}

func TestSendCampaignSendsNothingForACampaignThatIsNotADraftForTheAudience(t *testing.T) {
	for _, test := range []struct {
		name     string
		campaign map[string]any
		branch   sdkgo.BranchID
	}{
		{name: "sending", campaign: campaignJSON("sending", "regular", testListID), branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "sent", campaign: campaignJSON("sent", "regular", testListID), branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "scheduled", campaign: campaignJSON("schedule", "regular", testListID), branch: mailchimp.SendCampaignBranchNotSendable},
		{name: "paused", campaign: campaignJSON("paused", "regular", testListID), branch: mailchimp.SendCampaignBranchNotSendable},
		{name: "canceled", campaign: campaignJSON("canceled", "regular", testListID), branch: mailchimp.SendCampaignBranchNotSendable},
		{name: "unknown status", campaign: campaignJSON("reviewing", "regular", testListID), branch: mailchimp.SendCampaignBranchNotSendable},
		{name: "RSS draft", campaign: campaignJSON("save", "rss", testListID), branch: mailchimp.SendCampaignBranchNotSendable},
		{name: "another audience", campaign: campaignJSON("save", "regular", "b1c2d3e4f5"), branch: mailchimp.SendCampaignBranchNotSendable},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "nothing may be sent")
				writeValue(t, response, http.StatusOK, test.campaign)
			})
			ctx := newMailchimpDexContext("not-sendable-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount())
			require.Zero(t, ctx.heartbeatCount, "no checkpoint is needed when nothing is sent")
			if test.branch == mailchimp.SendCampaignBranchNotSendable {
				require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
			}
		})
	}
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, campaignJSON("save", "regular", "b1c2d3e4f5"))
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := sdkgo.RunMutation(newMailchimpDexContext("no-audience-check"), newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection,
		mailchimp.SendCampaignInput{CampaignID: testCampaignID})
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchSent, result.Branch, "a blank expectedListId skips the audience check")
}

func TestSendCampaignNeverResendsASendWhoseOutcomeIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusInternalServerError, "Internal Server Error")
		}, branch: mailchimp.SendCampaignBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "CDN timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadGateway, `<html>SENTINEL</html>`)
		}, branch: mailchimp.SendCampaignBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) },
			branch: mailchimp.SendCampaignBranchUncertain, kind: sdkgo.FailureTransport},
		{name: "reflected key", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"echo":"`+testAPIKey+`"}`)
		}, branch: mailchimp.SendCampaignBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "not ready", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"title":"Bad Request","status":400,"detail":"SENTINEL Your Campaign is not ready to send.","errors":[{"field":"settings.subject_line","message":"SENTINEL"}]}`)
		}, branch: mailchimp.SendCampaignBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "deleted meanwhile", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
		}, branch: mailchimp.SendCampaignBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "role cannot send", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusForbidden, "Forbidden")
		}, branch: mailchimp.SendCampaignBranchProviderRejected, kind: sdkgo.FailureAuthorization},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := draftThenSend(t, test.reply)
			ctx := newMailchimpDexContext("send-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			require.NoError(t, err, "only a provable non-application is retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, testCampaignID, result.Value.Campaign.ID, "the campaign as read before the send")
			requireNoSecretOrProviderText(t, result)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the checkpoint stays, so a replayed attempt sends nothing")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestSendCampaignRetriesOnlyWhatMailchimpProvablyDidNotApply(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		delay time.Duration
	}{
		{name: "too many connections", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "3")
			writeProblem(t, response, http.StatusTooManyRequests, "Too Many Requests")
		}, delay: 3 * time.Second},
		{name: "throttling 403", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusForbidden, ``) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := draftThenSend(t, test.reply)
			ctx := newMailchimpDexContext("send-throttled")
			_, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
			if test.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
			require.Nil(t, ctx.recordedHeartbeat, "a throttled send clears the checkpoint so the retry may send")
			require.Equal(t, 2, ctx.heartbeatCount)

			next := ctx.nextAttempt()
			accepted := draftThenSend(t, func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) })
			result, err := sdkgo.RunMutation(next, newMailchimpClient(t, accepted.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			require.NoError(t, err)
			require.Equal(t, mailchimp.SendCampaignBranchSent, result.Branch)
		})
	}

	refused := newMailchimpDexContext("send-refused")
	_, err := sdkgo.RunMutation(refused, newMailchimpClient(t, closedLoopbackURL(t)).SendCampaign(), mailchimpConnection, validSendCampaignInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the read before the send was refused, so nothing was sent")
	require.Zero(t, refused.heartbeatCount)

	transport := &recordingTransport{reply: func(request *http.Request) *http.Response {
		return localResponse(request, http.StatusOK, campaignBody("save"))
	}}
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(testAPIKey), mailchimp.WithHTTPClient(&http.Client{Transport: dialRefusingSendTransport{read: transport}}))
	require.NoError(t, err)
	sendRefused := newMailchimpDexContext("send-dial-refused")
	_, err = sdkgo.RunMutation(sendRefused, client.SendCampaign(), mailchimpConnection, validSendCampaignInput())
	require.ErrorAs(t, err, &retry, "a send whose connection never opened is retried")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Nil(t, sendRefused.recordedHeartbeat, "the checkpoint is cleared because nothing reached Mailchimp")
}

// dialRefusingSendTransport answers reads locally and fails every send before a connection opens.
type dialRefusingSendTransport struct {
	read *recordingTransport
}

func (transport dialRefusingSendTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return transport.read.RoundTrip(request)
}

func TestSendCampaignReadFailuresBeforeTheSendAreRetriedOrMapped(t *testing.T) {
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter)
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "outage", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusServiceUnavailable, "Service Unavailable")
		}, isRetry: true},
		{name: "lost read", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, isRetry: true},
		{name: "missing", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
		},
			branch: mailchimp.SendCampaignBranchNotFound},
		{name: "invalid key", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusUnauthorized, "API Key Invalid")
		},
			branch: mailchimp.SendCampaignBranchProviderRejected},
		{name: "another campaign", reply: func(response http.ResponseWriter) {
			body := campaignJSON("save", "regular", testListID)
			body["id"] = "ffffffffff"
			writeValue(t, response, http.StatusOK, body)
		}, branch: mailchimp.SendCampaignBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "nothing may be sent")
				test.reply(response)
			})
			ctx := newMailchimpDexContext("send-read-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			require.Zero(t, ctx.heartbeatCount)
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestSendCampaignAfterAnEarlierDispatchNeverSends(t *testing.T) {
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter)
		branch  sdkgo.BranchID
		message string
	}{
		{name: "campaign sending", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("sending")) },
			branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "campaign sent", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("sent")) },
			branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "still a draft", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("save")) },
			branch:  mailchimp.SendCampaignBranchUncertain,
			message: "an earlier attempt of this Step may have sent the campaign and Mailchimp does not show it sending or sent, so it is not sent again"},
		{name: "unreadable", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusInternalServerError, "Internal Server Error")
		},
			branch:  mailchimp.SendCampaignBranchUncertain,
			message: "an earlier attempt of this Step may have sent the campaign and its status could not be read, so it is not sent again"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "a replayed attempt never sends")
				test.reply(response)
			})
			first := newMailchimpDexContext("send-replay")
			first.recordedHeartbeat = json.RawMessage(`{"mailchimpDispatchedCallId":"earlier"}`)
			replay := first.nextAttempt()
			result, err := sdkgo.RunMutation(replay, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.message != "" {
				require.Equal(t, test.message, result.Failure.Message)
			}
			require.Equal(t, 1, provider.requestCount())
			require.Zero(t, replay.heartbeatCount, "the checkpoint is kept")
		})
	}

	unrecordable := newMailchimpDexContext("send-unrecordable")
	unrecordable.rejectsHeartbeat = true
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method, "nothing may be sent without a checkpoint")
		writeJSON(t, response, http.StatusOK, campaignBody("save"))
	})
	_, err := sdkgo.RunMutation(unrecordable, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, validSendCampaignInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Equal(t, 1, provider.requestCount())
}

func TestSendCampaignRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]mailchimp.SendCampaignInput{
		"missing campaign":   {},
		"campaign path":      {CampaignID: testCampaignID + "/actions/send"},
		"invalid audience":   {CampaignID: testCampaignID, ExpectedListID: "a list"},
		"spaced campaign":    {CampaignID: " " + testCampaignID},
		"oversized campaign": {CampaignID: "a123456789a123456789a123456789a123456789a123456789a123456789a12345"},
	} {
		ctx := newMailchimpDexContext("invalid-send")
		result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).SendCampaign(), mailchimpConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.SendCampaignBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}

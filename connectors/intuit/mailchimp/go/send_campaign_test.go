// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

// campaignScript answers reads with the current campaign status and each send with its next reply.
type campaignScript struct {
	*recordingMailchimp
	mutex       sync.Mutex
	status      string
	sendReplies []func(http.ResponseWriter)
	sends       int
}

// newCampaignScript starts a fake whose sends after the scripted replies are accepted and mark it sending.
func newCampaignScript(t *testing.T, status string, sendReplies ...func(http.ResponseWriter)) *campaignScript {
	t.Helper()
	script := &campaignScript{status: status, sendReplies: sendReplies}
	script.recordingMailchimp = newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		script.mutex.Lock()
		if request.Method == http.MethodGet {
			status := script.status
			script.mutex.Unlock()
			writeJSON(t, response, http.StatusOK, campaignBody(status))
			return
		}
		script.sends++
		var reply func(http.ResponseWriter)
		if script.sends <= len(script.sendReplies) {
			reply = script.sendReplies[script.sends-1]
		} else {
			script.status = "sending"
		}
		script.mutex.Unlock()
		if reply == nil {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		reply(response)
	})
	return script
}

func (script *campaignScript) setStatus(status string) {
	script.mutex.Lock()
	defer script.mutex.Unlock()
	script.status = status
}

func (script *campaignScript) sendCount() int {
	script.mutex.Lock()
	defer script.mutex.Unlock()
	return script.sends
}

func runSendCampaign(t *testing.T, ctx *mailchimpDexContext, baseURL string, options ...sdkgo.RunOption) (sdkgo.MutationResult[mailchimp.SendCampaignOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(ctx, newMailchimpClient(t, baseURL).SendCampaign(), mailchimpConnection, validSendCampaignInput(), options...)
}

func recordedMarker(t *testing.T, ctx *mailchimpDexContext) singledispatch.Marker {
	t.Helper()
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(ctx.recordedHeartbeat, &marker))
	return marker
}

func requireRetry(t *testing.T, err error) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	return retry
}

func serverError(t *testing.T) func(http.ResponseWriter) {
	return func(response http.ResponseWriter) {
		writeProblem(t, response, http.StatusInternalServerError, "Internal Server Error")
	}
}

func TestSendCampaignReadsTheDraftClaimsTheSendAndSendsOnce(t *testing.T) {
	provider := draftThenSend(t, func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) })
	ctx := newMailchimpDexContext("send")
	result, err := runSendCampaign(t, ctx, provider.URL)
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
	require.Equal(t, singledispatch.Marker{CallID: result.Receipt.CallID, Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1},
		recordedMarker(t, ctx), "the fence claim with its send count is recorded before the send")
	require.Equal(t, int32(3), mailchimp.SendCampaignDefinition.MaxSends)
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
			result, err := runSendCampaign(t, ctx, provider.URL)
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

func TestSendCampaignRetriesAnUnknownSendOutcomeAndReadsBeforeResending(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{name: "server error", reply: serverError(t), kind: sdkgo.FailureAvailability},
		{name: "CDN timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadGateway, `<html>SENTINEL</html>`)
		}, kind: sdkgo.FailureAvailability},
		{name: "request timeout", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusRequestTimeout, "Request Timeout")
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "reflected key", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"echo":"`+testAPIKey+`"}`)
		}, kind: sdkgo.FailureProtocol},
		{name: "conflict", reply: func(response http.ResponseWriter) { writeProblem(t, response, http.StatusConflict, "Conflict") },
			kind: sdkgo.FailureConflict},
		{name: "too early", reply: func(response http.ResponseWriter) { writeProblem(t, response, http.StatusTooEarly, "Too Early") },
			kind: sdkgo.FailureProviderRejection},
		{name: "redirect", reply: func(response http.ResponseWriter) {
			response.Header().Set("Location", "https://example.com/")
			response.WriteHeader(http.StatusTemporaryRedirect)
		}, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newCampaignScript(t, "save", test.reply)
			first := newMailchimpDexContext("send-unknown-" + test.name)
			_, err := runSendCampaign(t, first, provider.URL)
			retry := requireRetry(t, err)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), testAPIKey)
			require.Equal(t, singledispatch.StateClaimed, recordedMarker(t, first).State, "the claim stays, so the next attempt counts this send")

			second := first.nextAttempt()
			result, err := runSendCampaign(t, second, provider.URL)
			require.NoError(t, err)
			require.Equal(t, mailchimp.SendCampaignBranchSent, result.Branch, "the retry read a draft and resent below the cap")
			require.Equal(t, 2, provider.sendCount())
			require.Equal(t, int32(2), recordedMarker(t, second).SendCount)
		})
	}

	provider := newCampaignScript(t, "save", serverError(t))
	first := newMailchimpDexContext("send-unknown-applied")
	_, err := runSendCampaign(t, first, provider.URL)
	requireRetry(t, err)
	provider.setStatus("sending")
	second := first.nextAttempt()
	result, err := runSendCampaign(t, second, provider.URL)
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchAlreadySent, result.Branch, "the read-back confirms the earlier send, so nothing is resent")
	require.Equal(t, 1, provider.sendCount())
}

func TestSendCampaignSelectsUncertainOnlyAtTheSendCap(t *testing.T) {
	provider := newCampaignScript(t, "save", serverError(t), serverError(t), serverError(t))
	ctx := newMailchimpDexContext("send-cap")
	for attempt := int32(1); attempt < mailchimp.SendCampaignDefinition.MaxSends; attempt++ {
		_, err := runSendCampaign(t, ctx, provider.URL)
		requireRetry(t, err)
		ctx = ctx.nextAttempt()
	}
	result, err := runSendCampaign(t, ctx, provider.URL)
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchUncertain, result.Branch, "the last allowed send is unknown")
	require.Equal(t, testCampaignID, result.Value.Campaign.ID, "the campaign as read before the send")
	require.Equal(t, 3, provider.sendCount())
	requireNoSecretOrProviderText(t, result)

	atCap := ctx.nextAttempt()
	result, err = runSendCampaign(t, atCap, provider.URL)
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchUncertain, result.Branch, "an attempt at the cap only reads")
	require.Equal(t, 3, provider.sendCount())
	require.Zero(t, atCap.heartbeatCount)

	provider.setStatus("sent")
	result, err = runSendCampaign(t, ctx.nextAttempt(), provider.URL)
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchAlreadySent, result.Branch)

	single := newCampaignScript(t, "save", serverError(t))
	result, err = runSendCampaign(t, newMailchimpDexContext("send-cap-one"), single.URL, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, mailchimp.SendCampaignBranchUncertain, result.Branch, "an application cap of 1 never resends")
	require.Equal(t, 1, single.sendCount())
}

func TestSendCampaignSelectsADefiniteRefusalOfTheFirstSendWithoutResending(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "not ready", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"title":"Bad Request","status":400,"detail":"SENTINEL Your Campaign is not ready to send.","errors":[{"field":"settings.subject_line","message":"SENTINEL"}]}`)
		}, branch: mailchimp.SendCampaignBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "deleted meanwhile", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
		}, branch: mailchimp.SendCampaignBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "role cannot send", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusForbidden, "Forbidden")
		}, branch: mailchimp.SendCampaignBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "unprocessable", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusUnprocessableEntity, "Invalid Resource")
		}, branch: mailchimp.SendCampaignBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := draftThenSend(t, test.reply)
			ctx := newMailchimpDexContext("send-refused-" + test.name)
			result, err := runSendCampaign(t, ctx, provider.URL)
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, testCampaignID, result.Value.Campaign.ID, "the campaign as read before the send")
			requireNoSecretOrProviderText(t, result)
			require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "the refusal proves the send was not processed")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestSendCampaignReadsBackARefusedResendInsteadOfSelectingTheRefusal(t *testing.T) {
	refusal := func(response http.ResponseWriter) { writeProblem(t, response, http.StatusBadRequest, "Bad Request") }
	for _, test := range []struct {
		name   string
		status string
		branch sdkgo.BranchID
	}{
		{name: "still a draft", status: "save", branch: mailchimp.SendCampaignBranchUncertain},
		{name: "earlier send landed", status: "sending", branch: mailchimp.SendCampaignBranchAlreadySent},
	} {
		t.Run(test.name, func(t *testing.T) {
			var provider *campaignScript
			provider = newCampaignScript(t, "save", serverError(t), func(response http.ResponseWriter) {
				provider.setStatus(test.status)
				refusal(response)
			})
			first := newMailchimpDexContext("send-refused-resend")
			_, err := runSendCampaign(t, first, provider.URL)
			requireRetry(t, err)
			second := first.nextAttempt()
			result, err := runSendCampaign(t, second, provider.URL)
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch, "a refusal after a possible earlier send cannot prove nothing was sent")
			require.Equal(t, 2, provider.sendCount())
			require.Equal(t, 5, provider.requestCount(), "read, send, read, refused resend, read-back")
		})
	}
}

func TestSendCampaignRetriesWhatMailchimpProvablyDidNotProcessWithoutCountingIt(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
		delay time.Duration
	}{
		{name: "too many connections", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "3")
			writeProblem(t, response, http.StatusTooManyRequests, "Too Many Requests")
		}, kind: sdkgo.FailureRateLimit, delay: 3 * time.Second},
		{name: "throttling 403", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusForbidden, ``) },
			kind: sdkgo.FailureRateLimit},
		{name: "service unavailable", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusServiceUnavailable, "Service Unavailable")
		}, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newCampaignScript(t, "save", test.reply)
			ctx := newMailchimpDexContext("send-throttled")
			_, err := runSendCampaign(t, ctx, provider.URL)
			retry := requireRetry(t, err)
			require.Equal(t, test.kind, retry.Failure.Kind)
			if test.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
			marker := recordedMarker(t, ctx)
			require.Equal(t, singledispatch.StateCanResend, marker.State)
			require.Zero(t, marker.SendCount, "the unprocessed send is not counted")

			next := ctx.nextAttempt()
			result, err := runSendCampaign(t, next, provider.URL)
			require.NoError(t, err)
			require.Equal(t, mailchimp.SendCampaignBranchSent, result.Branch)
			require.Equal(t, int32(1), recordedMarker(t, next).SendCount)
		})
	}

	refused := newMailchimpDexContext("send-refused")
	_, err := runSendCampaign(t, refused, closedLoopbackURL(t))
	requireRetry(t, err)
	require.Equal(t, singledispatch.StateNotSent, recordedMarker(t, refused).State, "the read before the send was refused, so nothing was sent")

	transport := &recordingTransport{reply: func(request *http.Request) *http.Response {
		return localResponse(request, http.StatusOK, campaignBody("save"))
	}}
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(testAPIKey), mailchimp.WithHTTPClient(&http.Client{Transport: dialRefusingSendTransport{read: transport}}))
	require.NoError(t, err)
	sendRefused := newMailchimpDexContext("send-dial-refused")
	_, err = sdkgo.RunMutation(sendRefused, client.SendCampaign(), mailchimpConnection, validSendCampaignInput())
	retry := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind, "a send whose connection never opened is retried")
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, sendRefused).State, "nothing reached Mailchimp")
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

func TestSendCampaignReadFailuresBeforeTheFirstSendAreRetriedOrMapped(t *testing.T) {
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
			result, err := runSendCampaign(t, ctx, provider.URL)
			if test.isRetry {
				requireRetry(t, err)
				require.Equal(t, singledispatch.StateNotSent, recordedMarker(t, ctx).State, "the next attempt does not count a send")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Zero(t, ctx.heartbeatCount)
		})
	}
}

func TestSendCampaignSelectsUncertainForADefiniteReadAfterAPossibleEarlierSend(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
	}{
		{name: "canceled", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("canceled")) }},
		{name: "missing", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
		}},
		{name: "invalid", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"id":"ffffffffff"}`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newCampaignScript(t, "save", serverError(t))
			first := newMailchimpDexContext("send-earlier-" + test.name)
			_, err := runSendCampaign(t, first, provider.URL)
			requireRetry(t, err)
			readBack := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "nothing may be sent")
				test.reply(response)
			})
			result, err := runSendCampaign(t, first.nextAttempt(), readBack.URL)
			require.NoError(t, err)
			require.Equal(t, mailchimp.SendCampaignBranchUncertain, result.Branch, "the earlier send may have landed")
		})
	}

	provider := newCampaignScript(t, "save", serverError(t))
	first := newMailchimpDexContext("send-earlier-outage")
	_, err := runSendCampaign(t, first, provider.URL)
	requireRetry(t, err)
	outage := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeProblem(t, response, http.StatusServiceUnavailable, "Service Unavailable")
	})
	second := first.nextAttempt()
	_, err = runSendCampaign(t, second, outage.URL)
	requireRetry(t, err)
	require.Zero(t, second.heartbeatCount, "notSent would hide the earlier possible send")
}

func TestSendCampaignRetriesAnUnreadableCheckpointWithoutARequest(t *testing.T) {
	provider := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newMailchimpDexContext("send-unreadable")
	first.recordedHeartbeat = json.RawMessage(`"not a checkpoint"`)
	_, err := runSendCampaign(t, first.nextAttempt(), provider.URL)
	retry := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestSendCampaignAfterALegacyCheckpointNeverSends(t *testing.T) {
	probe := draftThenSend(t, func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) })
	probed, err := runSendCampaign(t, newMailchimpDexContext("send-legacy"), probe.URL)
	require.NoError(t, err)
	callID := probed.Receipt.CallID
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter)
		branch  sdkgo.BranchID
		isRetry bool
		message string
	}{
		{name: "campaign sending", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("sending")) },
			branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "campaign sent", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("sent")) },
			branch: mailchimp.SendCampaignBranchAlreadySent},
		{name: "still a draft", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, campaignBody("save")) },
			branch:  mailchimp.SendCampaignBranchUncertain,
			message: "an earlier attempt of this Step may have sent the campaign and Mailchimp does not show it sending or sent, so it is not sent again"},
		{name: "missing", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
		},
			branch:  mailchimp.SendCampaignBranchUncertain,
			message: "an earlier attempt of this Step may have sent the campaign and its status could not be read, so it is not sent again"},
		{name: "outage", reply: func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusInternalServerError, "Internal Server Error")
		}, isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "a pre-fence checkpoint never permits a send")
				test.reply(response)
			})
			first := newMailchimpDexContext("send-legacy")
			first.recordedHeartbeat = json.RawMessage(`{"mailchimpDispatchedCallId":"` + string(callID) + `"}`)
			replay := first.nextAttempt()
			result, err := runSendCampaign(t, replay, provider.URL)
			require.Equal(t, 1, provider.requestCount())
			require.Zero(t, replay.heartbeatCount, "the checkpoint is kept")
			if test.isRetry {
				requireRetry(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.message != "" {
				require.Equal(t, test.message, result.Failure.Message)
			}
		})
	}
}

func TestSendCampaignRetriesWhenTheClaimCannotBeRecorded(t *testing.T) {
	unrecordable := newMailchimpDexContext("send-unrecordable")
	unrecordable.rejectsHeartbeat = true
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method, "nothing may be sent without a claim")
		writeJSON(t, response, http.StatusOK, campaignBody("save"))
	})
	_, err := runSendCampaign(t, unrecordable, provider.URL)
	retry := requireRetry(t, err)
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
